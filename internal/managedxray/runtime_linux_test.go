//go:build linux

package managedxray

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "net"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
 "time"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
const testInstall="dddddddddddddddddddddddddddddddd"
func freePort(t *testing.T)uint16{t.Helper();l,e:=net.Listen("tcp4","127.0.0.1:0");if e!=nil{t.Fatal(e)};p:=l.Addr().(*net.TCPAddr).Port;l.Close();return uint16(p)}
func testCandidate(t *testing.T,port uint16,variant string)xrayconfig.Candidate{
 t.Helper();p:=xrayconfig.Profile{InboundID:1,Transport:"tcp",RealityTarget:"www.example.org:443",ServerNames:[]string{variant+".example.org"},ShortIDs:[]string{"aa11"},RealityPrivateKey:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x12},32)),Users:[]xrayconfig.User{{UUID:"af469f46-e18a-45f4-bf57-c7f649b10db5",Email:"native@pro.invalid"}}}
 c,e:=xrayconfig.RenderAssigned("new-node",testInstall,"127.0.0.1",[]xrayconfig.Assignment{{Profile:p,DirectPort:port}});if e!=nil{t.Fatal(e)};return c
}
func fixture(t *testing.T)(string,string){
 t.Helper();script:=`#!/bin/sh
set -eu
if [ "$2" = "-test" ]; then exit 0; fi
exec python3 -u -c 'import json,socket,sys,select
c=json.load(open(sys.argv[1]))
if "reject.example.org" in str(c): sys.exit(19)
servers=[]
for x in c["inbounds"]:
 s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
 s.bind((x["listen"],x["port"]));s.listen(64);servers.append(s)
while True:
 ready,_,_=select.select(servers,[],[],0.2)
 for s in ready:
  con,_=s.accept();con.close()
' "$3"
`
 p:=filepath.Join(t.TempDir(),"test-core");if e:=os.WriteFile(p,[]byte(script),0700);e!=nil{t.Fatal(e)};sha:=sha256.Sum256([]byte(script));return p,hex.EncodeToString(sha[:])
}
func testOptions(t *testing.T)Options{
 t.Helper();if os.Geteuid()==0{t.Skip("managed core never runs as root")};binary,pin:=fixture(t)
 return Options{Directory:filepath.Join(t.TempDir(),"private-runtime"),StateKey:bytes.Repeat([]byte{7},32),LeaseKey:bytes.Repeat([]byte{9},32),NodeID:"new-node",InstallationID:testInstall,ListenIP:"127.0.0.1",Binary:binary,BinarySHA:pin}
}
func grant(t *testing.T,o Options,seq uint64,c xrayconfig.Candidate,duration time.Duration)Lease{
 t.Helper();now:=time.Now();l,e:=SignLease(o.LeaseKey,Lease{NodeID:o.NodeID,InstallationID:o.InstallationID,Sequence:seq,ConfigSHA:c.SHA256,ManifestSHA:c.ManifestSHA256,Issued:now.UnixNano(),Expires:now.Add(duration).UnixNano()});if e!=nil{t.Fatal(e)};return l
}
func mustOpen(t *testing.T,o Options)*Runtime{t.Helper();r,e:=Open(o);if e!=nil{t.Fatal(e)};t.Cleanup(r.Close);return r}
func assertPort(t *testing.T,p uint16,want bool){t.Helper();c,e:=net.DialTimeout("tcp4","127.0.0.1:"+strconv.Itoa(int(p)),100*time.Millisecond);if e==nil{c.Close()};if (e==nil)!=want{t.Fatalf("port %d reachable=%v wanted=%v",p,e==nil,want)}}
func waitFor(t *testing.T,fn func()bool){t.Helper();until:=time.Now().Add(4*time.Second);for time.Now().Before(until){if fn(){return};time.Sleep(30*time.Millisecond)};t.Fatal("bounded condition not reached")}
func TestDurableApplyRollbackExactPortsAndRestartFreshLease(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);port:=freePort(t);c1,c2:=testCandidate(t,port,"one"),testCandidate(t,port,"two");lease:=grant(t,o,1,c1,40*time.Second)
 s,e:=r.Apply(context.Background(),Revision{1,c1},lease);if e!=nil||!s.ProcessReady||s.ActiveSHA!=c1.SHA256||s.DataPlaneVerified||s.SubscriptionPublished{t.Fatalf("apply: %+v %v",s,e)}
 pid:=r.process.cmd.Process.Pid;if _,e=r.Apply(context.Background(),Revision{1,c1},lease);e!=nil||r.process.cmd.Process.Pid!=pid{t.Fatal("replay restarted working core",e)};assertPort(t,port,true)
 if _,e=r.Apply(context.Background(),Revision{2,c2},grant(t,o,2,c2,40*time.Second));e!=nil{t.Fatal(e)}
 s,e=r.Rollback(context.Background(),3,grant(t,o,3,c1,40*time.Second));if e!=nil||!s.ProcessReady||s.ActiveSHA!=c1.SHA256||s.ActiveSequence!=3{t.Fatalf("rollback: %+v %v",s,e)}
 raw,e:=os.ReadFile(filepath.Join(o.Directory,"runtime.enc"));if e!=nil{t.Fatal(e)};if bytes.Contains(raw,[]byte("privateKey"))||bytes.Contains(raw,[]byte("af469f46")){t.Fatal("plaintext config in durable state")}
 if _,e=Open(o);!errors.Is(e,ErrBusy){t.Fatal("two controllers acquired same directory",e)}
 oldLease:=r.lease;r.Close();assertPort(t,port,false);r2:=mustOpen(t,o)
 if s=r2.Snapshot();s.ProcessReady||s.SavedSHA!=c1.SHA256||s.Highwater!=3{t.Fatalf("restart state: %+v",s)}
 if _,e=r2.Recover(context.Background(),oldLease);!errors.Is(e,ErrLease){t.Fatal("reused preboot lease accepted",e)}
 s,e=r2.Recover(context.Background(),grant(t,o,3,c1,40*time.Second));if e!=nil||!s.ProcessReady||s.ActiveSHA!=c1.SHA256{t.Fatalf("durable recovery: %+v %v",s,e)};assertPort(t,port,true)
}
func TestFailedApplyRestoresPreviousAndDoesNotAdoptForeignSocket(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);port:=freePort(t);good,bad:=testCandidate(t,port,"one"),testCandidate(t,port,"reject")
 if _,e:=r.Apply(context.Background(),Revision{1,good},grant(t,o,1,good,40*time.Second));e!=nil{t.Fatal(e)}
 s,e:=r.Apply(context.Background(),Revision{2,bad},grant(t,o,2,bad,40*time.Second));if e==nil||!s.ProcessReady||s.ActiveSHA!=good.SHA256||s.Phase!="rolled_back_after_failed_apply"{t.Fatalf("failed apply: %+v %v",s,e)};assertPort(t,port,true)
 foreign,e:=net.Listen("tcp4","127.0.0.1:0");if e!=nil{t.Fatal(e)};defer foreign.Close();target:=testCandidate(t,uint16(foreign.Addr().(*net.TCPAddr).Port),"three")
 s,e=r.Apply(context.Background(),Revision{3,target},grant(t,o,3,target,40*time.Second));if e==nil||s.ActiveSHA!=good.SHA256||!s.ProcessReady{t.Fatalf("foreign listener impersonated Xray: %+v %v",s,e)}
}
func TestLeaseExpiryStopsOnlyOwnProcessAndKeepsEncryptedGood(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);port:=freePort(t);c:=testCandidate(t,port,"one");lease:=grant(t,o,1,c,time.Second)
 if _,e:=r.Apply(context.Background(),Revision{1,c},lease);e!=nil{t.Fatal(e)};waitFor(t,func()bool{return r.Snapshot().Phase=="lease_expired_stopped"});assertPort(t,port,false)
 s:=r.Snapshot();if s.ProcessReady||s.SavedSHA!=c.SHA256{t.Fatalf("lease expiry lost state: %+v",s)}
 if _,e:=r.Refresh(lease);!errors.Is(e,ErrLease){t.Fatal("expired lease restarted config",e)}
 if _,e:=r.Recover(context.Background(),grant(t,o,1,c,40*time.Second));e!=nil{t.Fatal(e)};assertPort(t,port,true)
}
func TestWrongKeyScopeDigestAndReservedPortsFailClosed(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);port:=freePort(t);c:=testCandidate(t,port,"one");tampered:=grant(t,o,1,c,40*time.Second);tampered.InstallationID=strings.Repeat("a",32)
 if _,e:=r.Apply(context.Background(),Revision{1,c},tampered);!errors.Is(e,ErrLease){t.Fatal("wrong install lease accepted",e)}
 c.Config[0]='!';if _,e:=r.Apply(context.Background(),Revision{1,c},grant(t,o,1,c,40*time.Second));e==nil{t.Fatal("tampered config accepted")}
 c=testCandidate(t,port,"one");r.reserved[port]=true;if _,e:=r.Apply(context.Background(),Revision{1,c},grant(t,o,1,c,40*time.Second));e==nil{t.Fatal("reserved management port accepted")};delete(r.reserved,port)
 if _,e:=r.Apply(context.Background(),Revision{1,c},grant(t,o,1,c,40*time.Second));e!=nil{t.Fatal(e)};r.Close();wrong:=o;wrong.StateKey=bytes.Repeat([]byte{1},32)
 if _,e:=Open(wrong);!errors.Is(e,ErrCorrupt){t.Fatal("wrong state key accepted",e)};wrong=o;wrong.InstallationID=strings.Repeat("a",32);if _,e:=Open(wrong);!errors.Is(e,ErrCorrupt){t.Fatal("different installation adopted state",e)}
}
func TestCrashHelper(t *testing.T){
 if os.Getenv("DARK_PRO_CRASH_HELPER")==""{return};var args struct{Options Options;Port uint16;Marker string};raw,_:=base64.StdEncoding.DecodeString(os.Getenv("DARK_PRO_CRASH_HELPER"));if json.Unmarshal(raw,&args)!=nil{os.Exit(30)}
 r,e:=Open(args.Options);if e!=nil{os.Exit(31)};defer r.Close();c1,c2:=testCandidate(t,args.Port,"one"),testCandidate(t,args.Port,"two")
 if _,e=r.Apply(context.Background(),Revision{1,c1},grant(t,args.Options,1,c1,40*time.Second));e!=nil{os.Exit(32)}
 r.afterIntent=func(){if os.WriteFile(args.Marker,[]byte("intent-synced"),0600)!=nil{os.Exit(33)};time.Sleep(30*time.Second)}
 _,_=r.Apply(context.Background(),Revision{2,c2},grant(t,args.Options,2,c2,40*time.Second))
}
func TestSIGKILLBetweenIntentAndCommitRecoversOnlyLastGood(t *testing.T){
 o:=testOptions(t);port:=freePort(t);marker:=filepath.Join(t.TempDir(),"checkpoint");payload,_:=json.Marshal(struct{Options Options;Port uint16;Marker string}{o,port,marker})
 child:=exec.Command(os.Args[0],"-test.run=^TestCrashHelper$","-test.timeout=40s");child.Env=append(os.Environ(),"DARK_PRO_CRASH_HELPER="+base64.StdEncoding.EncodeToString(payload));if e:=child.Start();e!=nil{t.Fatal(e)};t.Cleanup(func(){_=child.Process.Kill()})
 waitFor(t,func()bool{_,e:=os.Stat(marker);return e==nil});assertPort(t,port,true);if e:=child.Process.Kill();e!=nil{t.Fatal(e)};_=child.Wait()
 waitFor(t,func()bool{c,e:=net.DialTimeout("tcp4","127.0.0.1:"+strconv.Itoa(int(port)),50*time.Millisecond);if e==nil{c.Close()};return e!=nil})
 r:=mustOpen(t,o);s:=r.Snapshot();c1:=testCandidate(t,port,"one");if s.ProcessReady||s.SavedSHA!=c1.SHA256||s.Highwater!=2{t.Fatalf("crash journal: %+v",s)}
 s,e:=r.Recover(context.Background(),grant(t,o,1,c1,40*time.Second));if e!=nil||!s.ProcessReady||s.ActiveSHA!=c1.SHA256||s.ActiveSequence!=1{t.Fatalf("wrong recovery: %+v %v",s,e)}
}
func TestRealPinnedXrayDurableApplyRollback(t *testing.T){
 binary,pin:=os.Getenv("DARK_PRO_TEST_XRAY_BINARY"),os.Getenv("DARK_PRO_TEST_XRAY_SHA256");if binary==""&&pin==""{t.Skip("explicit pinned real core not supplied")};if binary==""||pin==""{t.Fatal("incomplete pinned binary contract")}
 o:=testOptions(t);o.Binary=binary;o.BinarySHA=pin;r:=mustOpen(t,o);port:=freePort(t);c1,c2:=testCandidate(t,port,"one"),testCandidate(t,port,"two")
 if _,e:=r.Apply(context.Background(),Revision{1,c1},grant(t,o,1,c1,40*time.Second));e!=nil{t.Fatal(e)}
 if _,e:=r.Apply(context.Background(),Revision{2,c2},grant(t,o,2,c2,40*time.Second));e!=nil{t.Fatal(e)}
 if _,e:=r.Rollback(context.Background(),3,grant(t,o,3,c1,40*time.Second));e!=nil{t.Fatal(e)}
 r.Close();next:=mustOpen(t,o);s,e:=next.Recover(context.Background(),grant(t,o,3,c1,40*time.Second));if e!=nil||!s.ProcessReady||s.ActiveSHA!=c1.SHA256{t.Fatalf("real core persistent recovery: %+v %v",s,e)};assertPort(t,port,true)
}
