//go:build linux

package managedxray

import (
 "context"
 "encoding/json"
 "io"
 "net"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strconv"
 "strings"
 "sync"
 "syscall"
 "time"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
type child struct{cmd *exec.Cmd;dir string;done chan struct{};guardMu sync.Mutex;timer *time.Timer;leaseDeadline time.Time;ending bool;cleanup sync.Once}
// The process expiry timer never waits for the controller mutex or validation.
func(p *child)arm(deadline time.Time)bool{
 p.guardMu.Lock();defer p.guardMu.Unlock();if p.ending||p.dead()||!time.Now().Before(deadline){return false}
 p.leaseDeadline=deadline;if p.timer!=nil{p.timer.Stop()}
 p.timer=time.AfterFunc(time.Until(deadline),func(){p.guardMu.Lock();defer p.guardMu.Unlock();if !p.ending&&!time.Now().Before(p.leaseDeadline){p.ending=true;if !p.dead(){_=p.cmd.Process.Kill()}}});return true
}
func(p *child)dead()bool{if p==nil{return true};select{case <-p.done:return true;default:return false}}
func(p *child)stop(){if p==nil{return};p.guardMu.Lock();p.ending=true;if p.timer!=nil{p.timer.Stop()};if !p.dead(){_=p.cmd.Process.Kill()};p.guardMu.Unlock();<-p.done;p.cleanup.Do(func(){_=os.RemoveAll(p.dir)})}
func validateLayout(c xrayconfig.Candidate,ip string,reserved map[uint16]bool)error{
 if c.NoListeners {if xrayconfig.IsStoppedCandidate(c){return nil};return ErrInvalid}
 var body struct{Inbounds []struct{Listen string `json:"listen"`;Port uint16 `json:"port"`;Tag string `json:"tag"`;Protocol string `json:"protocol"`} `json:"inbounds"`}
 if len(c.Listeners)==0||len(c.Listeners)>512||json.Unmarshal(c.Config,&body)!=nil||len(body.Inbounds)!=len(c.Listeners){return ErrInvalid}
 tags:=map[string]uint16{};ports:=map[uint16]bool{}
 for _,in:=range body.Inbounds{parsed:=net.ParseIP(in.Listen);if parsed==nil||parsed.String()!=ip||in.Protocol!="vless"||in.Port<1024||reserved[in.Port]||ports[in.Port]||in.Tag==""||tags[in.Tag]!=0{return ErrInvalid};tags[in.Tag]=in.Port;ports[in.Port]=true}
 seen:=map[string]bool{};for _,l:=range c.Listeners{if tags[l.Tag]!=l.NodePort||seen[l.Tag]{return ErrInvalid};seen[l.Tag]=true};return nil
}
// Readiness requires this specific child's socket ownership, not a stray TCP listener.
func(p *child)bound(listeners []xrayconfig.Listener)bool{
 if p.dead(){return false};fdDir:="/proc/"+strconv.Itoa(p.cmd.Process.Pid)+"/fd";fds,err:=os.ReadDir(fdDir);if err!=nil{return false};sockets:=map[string]bool{}
 for _,fd:=range fds{value,err:=os.Readlink(filepath.Join(fdDir,fd.Name()));if err==nil&&strings.HasPrefix(value,"socket:["){sockets[strings.TrimSuffix(strings.TrimPrefix(value,"socket:["),"]")]=true}}
 bound:=map[uint16]bool{}
 for _,name:=range []string{"/proc/net/tcp","/proc/net/tcp6"}{raw,err:=os.ReadFile(name);if err!=nil{continue};for _,line:=range strings.Split(string(raw),"\n"){cols:=strings.Fields(line);if len(cols)<10||cols[3]!="0A"||!sockets[cols[9]]{continue};pieces:=strings.Split(cols[1],":");if len(pieces)!=2{continue};port,err:=strconv.ParseUint(pieces[1],16,16);if err==nil{bound[uint16(port)]=true}}}
 if len(listeners)==0{return len(bound)==0&&!p.dead()};for _,l:=range listeners{if !bound[l.NodePort]{return false}};return !p.dead()
}
func startChild(ctx context.Context,c xrayconfig.Candidate,binary string,deadline time.Time,finalTCP ...bool)(*child,error){
 if ctx.Err()!=nil{return nil,ctx.Err()};dir,err:=os.MkdirTemp("","dark-pro-managed-xray-");if err!=nil{return nil,ErrRuntime}
 path:=filepath.Join(dir,"active.json");if err=os.WriteFile(path,c.Config,0600);err!=nil{os.RemoveAll(dir);return nil,ErrRuntime}
 cmd:=exec.Command(binary,"run","-config",path);cmd.Dir=dir;cmd.Env=[]string{"PATH=/usr/bin:/bin","HOME="+dir};cmd.Stdout=io.Discard;cmd.Stderr=io.Discard
 if len(finalTCP)==1&&finalTCP[0]{cmd.Env=append(cmd.Env,"DARK_PRO_FINAL_TCP_V1=1")}
 cmd.SysProcAttr=&syscall.SysProcAttr{Pdeathsig:syscall.SIGKILL};p:=&child{cmd:cmd,dir:dir,done:make(chan struct{})};started:=make(chan error,1)
 go func(){
  // Pdeathsig follows the creating OS thread. Pin it until the child exits.
  runtime.LockOSThread();defer runtime.UnlockOSThread();err:=cmd.Start();started<-err;if err==nil{_=cmd.Wait()};close(p.done)
 }()
 if err=<-started;err!=nil{<-p.done;os.RemoveAll(dir);return nil,ErrRuntime};if !p.arm(deadline){p.stop();return nil,ErrLease}
 wait,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel();ticker:=time.NewTicker(40*time.Millisecond);defer ticker.Stop();healthyAt:=time.Time{}
 for{if p.dead(){p.stop();return nil,ErrRuntime};if p.bound(c.Listeners){if healthyAt.IsZero(){healthyAt=time.Now()};if time.Since(healthyAt)>=150*time.Millisecond{return p,nil}}else{healthyAt=time.Time{}}
 select{case <-wait.Done():p.stop();return nil,ErrRuntime;case <-p.done:p.stop();return nil,ErrRuntime;case <-ticker.C:}}
}
func CheckLocalAddress(address string)error{
 ip:=net.ParseIP(address);if ip==nil||ip.IsUnspecified()||ip.IsMulticast(){return ErrInvalid}
 addrs,err:=net.InterfaceAddrs();if err!=nil{return err}
 for _,a:=range addrs{var local net.IP;switch x:=a.(type){case *net.IPNet:local=x.IP;case *net.IPAddr:local=x.IP};if local!=nil&&local.Equal(ip){return nil}};return ErrInvalid
}
