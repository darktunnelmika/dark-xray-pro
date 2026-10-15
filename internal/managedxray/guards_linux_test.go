//go:build linux

package managedxray

import (
 "context"
 "errors"
 "net"
 "os"
 "path/filepath"
 "strconv"
 "testing"
 "time"
)
func TestLeaseKillSwitchDoesNotWaitForControllerMutex(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);port:=freePort(t);c:=testCandidate(t,port,"one")
 if _,err:=r.Apply(context.Background(),Revision{1,c},grant(t,o,1,c,800*time.Millisecond));err!=nil{t.Fatal(err)}
 r.mu.Lock();p:=r.process;time.Sleep(900*time.Millisecond)
 select{case <-p.done:case <-time.After(2*time.Second):r.mu.Unlock();t.Fatal("expired lease waited for controller lock")}
 conn,err:=net.DialTimeout("tcp4","127.0.0.1:"+strconv.Itoa(int(port)),100*time.Millisecond);if err==nil{conn.Close();r.mu.Unlock();t.Fatal("expired process kept serving while controller blocked")};r.mu.Unlock()
}
func TestFailedRevisionCanRetryWithFreshLeaseButCannotChangeContent(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);foreign,err:=net.Listen("tcp4","127.0.0.1:0");if err!=nil{t.Fatal(err)};defer foreign.Close()
 port:=uint16(foreign.Addr().(*net.TCPAddr).Port);c:=testCandidate(t,port,"one");old:=grant(t,o,1,c,40*time.Second)
 if _,err=r.Apply(context.Background(),Revision{1,c},old);err==nil{t.Fatal("foreign socket treated as successful core")};foreign.Close()
 if _,err=r.Apply(context.Background(),Revision{1,c},old);!errors.Is(err,ErrLease){t.Fatal("old intent replay accepted",err)}
 altered:=testCandidate(t,port,"two");if _,err=r.Apply(context.Background(),Revision{1,altered},grant(t,o,1,altered,40*time.Second));!errors.Is(err,ErrStale){t.Fatal("same sequence changed bytes",err)}
 s,err:=r.Apply(context.Background(),Revision{1,c},grant(t,o,1,c,40*time.Second));if err!=nil||!s.ProcessReady{t.Fatalf("fresh retry after port recovery: %+v %v",s,err)}
}
func TestJournalWriteFailureStopsLiveProcessInsteadOfFalseCommit(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);port:=freePort(t);c:=testCandidate(t,port,"one")
 if _,err:=r.Apply(context.Background(),Revision{1,c},grant(t,o,1,c,40*time.Second));err!=nil{t.Fatal(err)}
 path:=filepath.Join(o.Directory,"runtime.enc");if err:=os.Remove(path);err!=nil{t.Fatal(err)};if err:=os.Mkdir(path,0700);err!=nil{t.Fatal(err)};defer os.Remove(path)
 _,err:=r.Refresh(grant(t,o,1,c,40*time.Second));if err==nil{t.Fatal("journal failure accepted")};assertPort(t,port,false)
 if s:=r.Snapshot();s.ProcessReady||s.Phase!="journal_failure_fenced"{t.Fatalf("bad fenced state: %+v",s)}
}
func TestUnassignedAndWildcardIPRefused(t *testing.T){
 for _,ip:=range []string{"0.0.0.0","::","not-an-ip","192.0.2.251"}{if CheckLocalAddress(ip)==nil{t.Errorf("invalid/unassigned bind accepted: %s",ip)}};if err:=CheckLocalAddress("127.0.0.1");err!=nil{t.Fatal(err)}
}
