//go:build linux

package usagecheckpoint
import("bytes";"context";"crypto/sha256";"encoding/hex";"fmt";"io";"net";"os";"os/exec";"path/filepath";"strconv";"strings";"syscall";"time")
type ProcessToken struct{BootID string;PID int;StartTicks uint64}
func Process(pid int)(ProcessToken,error){
 if pid<=0{return ProcessToken{},ErrInvalid};raw,err:=os.ReadFile("/proc/"+strconv.Itoa(pid)+"/stat");if err!=nil{return ProcessToken{},ErrEpoch}
 end:=bytes.LastIndexByte(raw,')');if end<0{return ProcessToken{},ErrEpoch};fields:=strings.Fields(string(raw[end+1:]));if len(fields)<=19||fields[0]=="Z"||fields[0]=="X"{return ProcessToken{},ErrEpoch}
 start,err:=strconv.ParseUint(fields[19],10,64);if err!=nil||start==0{return ProcessToken{},ErrEpoch}
 boot,err:=os.ReadFile("/proc/sys/kernel/random/boot_id");if err!=nil{return ProcessToken{},ErrEpoch};return ProcessToken{BootID:strings.TrimSpace(string(boot)),PID:pid,StartTicks:start},nil
}
func sameProcess(e Epoch)error{actual,err:=Process(e.PID);if err!=nil||actual.BootID!=e.KernelBootID||actual.StartTicks!=e.ProcessStartTicks{return ErrEpoch};return nil}
// Never reset counters. The StatsService must be loopback-only and owned by
// this supervised Xray incarnation, not a coincidentally listening process.
func StatsArguments(endpoint string)([]string,error){host,port,err:=net.SplitHostPort(endpoint);if err!=nil||host!="127.0.0.1"{return nil,ErrPrivate};p,err:=strconv.ParseUint(port,10,16);if err!=nil||p<1024||strconv.FormatUint(p,10)!=port{return nil,ErrPrivate};return []string{"api","statsquery","--server="+endpoint,"--timeout=3","-pattern=user>>>","-reset=false"},nil}
func ownsStatsPort(pid int,endpoint string)bool{
 if _,err:=StatsArguments(endpoint);err!=nil{return false};_,port,_:=net.SplitHostPort(endpoint);number,_:=strconv.ParseUint(port,10,16);target:=fmt.Sprintf("0100007F:%04X",number)
 dir:="/proc/"+strconv.Itoa(pid)+"/fd";entries,err:=os.ReadDir(dir);if err!=nil{return false};inodes:=map[string]bool{}
 for _,entry:=range entries{link,err:=os.Readlink(filepath.Join(dir,entry.Name()));if err==nil&&strings.HasPrefix(link,"socket:[")&&strings.HasSuffix(link,"]"){inodes[strings.TrimSuffix(strings.TrimPrefix(link,"socket:["),"]")]=true}}
 raw,err:=os.ReadFile("/proc/net/tcp");if err!=nil{return false};for _,line:=range strings.Split(string(raw),"\n"){fields:=strings.Fields(line);if len(fields)>9&&fields[1]==target&&fields[3]=="0A"&&inodes[fields[9]]{return true}};return false
}
func checkBinary(path,pin string)error{
 if !filepath.IsAbs(path)||filepath.Clean(path)!=path||!hexID(pin,64){return ErrInvalid}
 fd,err:=syscall.Open(path,syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0);if err!=nil{return ErrInvalid};f:=os.NewFile(uintptr(fd),path);defer f.Close();st,err:=f.Stat()
 if err!=nil||!st.Mode().IsRegular()||st.Mode().Perm()&0111==0||st.Mode().Perm()&0022!=0||st.Size()>128<<20{return ErrInvalid}
 h:=sha256.New();if _,err=io.Copy(h,io.LimitReader(f,(128<<20)+1));err!=nil{return err};if hex.EncodeToString(h.Sum(nil))!=pin{return ErrConflict};return nil
}
type boundedOutput struct{data bytes.Buffer;limit int;cancel context.CancelFunc}
func(b *boundedOutput)Write(p []byte)(int,error){if len(p)>b.limit-b.data.Len(){b.cancel();return 0,ErrFull};return b.data.Write(p)}
func Collect(ctx context.Context,binary,pin,endpoint string,epoch Epoch)([]Usage,error){
 e,err:=normalize(epoch);if err!=nil{return nil,err};raw,err:=collectRaw(ctx,binary,pin,endpoint,e);if err!=nil{return nil,err};return ParseStats(raw,e.Bindings)
}
func collectRaw(ctx context.Context,binary,pin,endpoint string,epoch Epoch)([]byte,error){
 return collectPattern(ctx,binary,pin,endpoint,epoch,"user>>>")
}
func collectPattern(ctx context.Context,binary,pin,endpoint string,epoch Epoch,pattern string)([]byte,error){
 e,err:=normalize(epoch);if err!=nil{return nil,err};args,err:=StatsArguments(endpoint);if err!=nil{return nil,err};args[4]="-pattern="+pattern;if err=checkBinary(binary,pin);err!=nil{return nil,err}
 if err=sameProcess(e);err!=nil{return nil,err};if !ownsStatsPort(e.PID,endpoint){return nil,ErrEpoch}
 call,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel();output:=&boundedOutput{limit:MaxStatsBytes,cancel:cancel}
 cmd:=exec.CommandContext(call,binary,args...);cmd.Env=[]string{"PATH=/usr/bin:/bin"};cmd.Stdout=output;cmd.Stderr=io.Discard;cmd.WaitDelay=250*time.Millisecond
 if err=cmd.Run();err!=nil{return nil,ErrIncomplete};if err=sameProcess(e);err!=nil{return nil,err};if !ownsStatsPort(e.PID,endpoint){return nil,ErrEpoch}
 return append([]byte(nil),output.data.Bytes()...),nil
}

// VerifyOwnedStatsEndpoint rechecks the exact kernel-bound child before an API mutation.
func VerifyOwnedStatsEndpoint(epoch Epoch,endpoint string)error{
 e,err:=normalize(epoch);if err!=nil{return err};if err=sameProcess(e);err!=nil{return err};if !ownsStatsPort(e.PID,endpoint){return ErrEpoch};return nil
}
