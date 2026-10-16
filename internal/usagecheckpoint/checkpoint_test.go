//go:build linux

package usagecheckpoint

import (
 "bytes"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"
)
func testEpoch(t *testing.T)Epoch{t.Helper();p,e:=Process(os.Getpid());if e!=nil{t.Fatal(e)};return Epoch{NodeID:"test-node",InstallationID:strings.Repeat("a",32),ID:strings.Repeat("b",32),ConfigSHA:strings.Repeat("c",64),KernelBootID:p.BootID,PID:p.PID,ProcessStartTicks:p.StartTicks,Bindings:[]Binding{{ClientID:"one",Email:"one@pro.invalid",Billable:true},{ClientID:"two",Email:"two@pro.invalid",Billable:false}}}}
func testBox(t *testing.T)(*Outbox,Options){t.Helper();o:=Options{Directory:filepath.Join(t.TempDir(),"outbox"),Key:bytes.Repeat([]byte{7},32),Epoch:testEpoch(t),Create:true};b,e:=Open(o);if e!=nil{t.Fatal(e)};t.Cleanup(func(){_=b.Close()});return b,o}
func values(n int64)[]Usage{return []Usage{{ClientID:"one",Uplink:n,Downlink:n},{ClientID:"two",Uplink:n*2,Downlink:n*2}}}
func receipt(r Report)Receipt{return Receipt{EpochID:r.EpochID,Sequence:r.Sequence,SHA:r.SHA}}
func TestStatsPrecisionStrictMissingAndDuplicate(t *testing.T){
 b:=testEpoch(t).Bindings
 good:=[]byte(`{"stat":[{"name":"user>>>one@pro.invalid>>>traffic>>>uplink","value":"9007199254740993"},{"name":"user>>>one@pro.invalid>>>traffic>>>downlink"},{"name":"user>>>two@pro.invalid>>>traffic>>>uplink","value":12},{"name":"user>>>two@pro.invalid>>>traffic>>>downlink","value":"0"},{"name":"inbound>>>aggregate>>>traffic>>>uplink","value":"99999"}]}`)
 v,e:=ParseStats(good,b);if e!=nil||v[0].Uplink!=9007199254740993||v[0].Downlink!=0{t.Fatalf("precision lost: %+v %v",v,e)}
 for _,raw:=range []string{`{}`,`{"stat":[]}`,`{"stat":null}`,`{"stat":[],"stat":[]}`,`{"stat":[]} {}`,`[]`,`null`}{t.Run(raw,func(t *testing.T){if _,e:=ParseStats([]byte(raw),b);e==nil{t.Fatal("invalid stats accepted")}})}
 for _,number:=range []string{`"-1"`,`-1`,`"1.5"`,`1.5`,`"01"`,`null`,`"9223372036854775808"`,`true`,`""`,`[]`}{t.Run(number,func(t *testing.T){bad:=bytes.Replace(good,[]byte(`"9007199254740993"`),[]byte(number),1);if _,e:=ParseStats(bad,b);e==nil{t.Fatal("invalid number accepted")}})}
 for _,pair:=range [][2]string{{"one@pro.invalid","unknown@pro.invalid"},{`"value":"9007199254740993"`,`"value":1,"value":2`},{`"name":"user>>>one@pro.invalid>>>traffic>>>uplink"`,`"name":"user>>>two@pro.invalid>>>traffic>>>uplink"`}}{bad:=strings.Replace(string(good),pair[0],pair[1],1);if _,e:=ParseStats([]byte(bad),b);e==nil{t.Fatal("unmapped/duplicate stat accepted")}}
}
func TestQueueRestartExactReceiptAndNoDoubleCharge(t *testing.T){
 b,o:=testBox(t);r,e:=b.Observe(values(10),false);if e!=nil{t.Fatal(e)}
 cursor,charges,dup,e:=Calculate(o.Epoch,Cursor{},r);if e!=nil||dup||len(charges)!=1||charges[0].Uplink!=10{t.Fatalf("transit charged or bad delta: %+v %v",charges,e)}
 if _,ch,duplicate,e:=Calculate(o.Epoch,cursor,r);e!=nil||!duplicate||len(ch)!=0{t.Fatal("replay added charges",e)}
 if e=b.Close();e!=nil{t.Fatal(e)};o.Create=false;b,e=Open(o);if e!=nil{t.Fatal(e)};defer b.Close()
 q,e:=b.Pending();if e!=nil||len(q)!=1||q[0].SHA!=r.SHA{t.Fatal("restart lost pending report",e)}
 bad:=receipt(r);bad.Sequence++;if e=b.Acknowledge(bad);!errors.Is(e,ErrReceipt){t.Fatal("unknown receipt discarded data",e)}
 if e=b.Acknowledge(receipt(r));e!=nil{t.Fatal(e)}
 repeat,e:=b.Observe(values(10),false);if e!=nil||repeat.Sequence!=1{t.Fatal("unchanged count created new sample",e)}
 q,_=b.Pending();if len(q)!=0{t.Fatal("acknowledged sample requeued")}
 r2,e:=b.Observe(values(20),false);if e!=nil{t.Fatal(e)}
 _,delta,_,e:=Calculate(o.Epoch,cursor,r2);if e!=nil||delta[0].Uplink!=10{t.Fatal("acknowledgement reset counters",e)}
 if _,e=b.Observe(values(1),false);!errors.Is(e,ErrRegression){t.Fatal("counter regression accepted",e)}
 last,_,_:=b.Last();if last.SHA!=r2.SHA{t.Fatal("bad sample advanced checkpoint")}
 raw,e:=os.ReadFile(filepath.Join(o.Directory,"usage.enc"));if e!=nil{t.Fatal(e)};if bytes.Contains(raw,[]byte("one@pro.invalid")){t.Fatal("plaintext epoch escaped")}
}
func TestQueueBoundsSealingOverflowAndScope(t *testing.T){
 b,o:=testBox(t);for i:=int64(1);i<=MaxPending;i++{if _,e:=b.Observe(values(i),false);e!=nil{t.Fatal(e)}}
 if _,e:=b.Observe(values(99),false);!errors.Is(e,ErrFull){t.Fatal("overflow dropped reports",e)}
 q,_:=b.Pending();if len(q)!=MaxPending{t.Fatal("queue was trimmed")}
 if e:=b.Acknowledge(receipt(q[1]));!errors.Is(e,ErrReceipt){t.Fatal("reordered ack consumed queue",e)}
 for _,r:=range q{if e:=b.Acknowledge(receipt(r));e!=nil{t.Fatal(e)}}
 sealed,e:=b.Observe(values(MaxPending),true);if e!=nil{t.Fatal(e)}
 if _,e=b.Observe(values(MaxPending+1),false);!errors.Is(e,ErrSealed){t.Fatal("sealed process epoch reopened",e)}
 if _,_,_,e=Calculate(o.Epoch,Cursor{EpochID:o.Epoch.ID,Sequence:sealed.Sequence,ReportSHA:sealed.SHA,Users:sealed.Users,Quiesced:true},sealed);e!=nil{t.Fatal(e)}
 if _,e=PlanTotals(map[string]int64{"one":math.MaxInt64},[]Charge{{ClientID:"one",Uplink:1}});!errors.Is(e,ErrOverflow){t.Fatal("usage integer overflow",e)}
 different:=o.Epoch;different.ID=strings.Repeat("d",32);if _,_,_,e=Calculate(different,Cursor{},sealed);e==nil{t.Fatal("cross-epoch replay accepted")}
}
func TestQueuePrivateFilesLocksAndWrongKeys(t *testing.T){
 b,o:=testBox(t);if _,e:=Open(o);!errors.Is(e,ErrLocked){t.Fatal("multiple outbox writers",e)}
 if _,e:=b.Observe(values(1),false);e!=nil{t.Fatal(e)};_=b.Close();o.Create=false
 wrong:=o;wrong.Key=bytes.Repeat([]byte{1},32);if _,e:=Open(wrong);!errors.Is(e,ErrCorrupt){t.Fatal("wrong key accepted",e)}
 wrong=o;wrong.Epoch.ID=strings.Repeat("d",32);if _,e:=Open(wrong);!errors.Is(e,ErrCorrupt){t.Fatal("wrong epoch adopted state",e)}
 if e:=os.Chmod(filepath.Join(o.Directory,"usage.enc"),0644);e!=nil{t.Fatal(e)};if _,e:=Open(o);!errors.Is(e,ErrPrivate){t.Fatal("world-readable state accepted",e)}
 root:=t.TempDir();link:=filepath.Join(root,"alias");if e:=os.Symlink(o.Directory,link);e!=nil{t.Fatal(e)};wrong=o;wrong.Directory=link;if _,e:=Open(wrong);!errors.Is(e,ErrPrivate){t.Fatal("symlink path accepted",e)}
 if _,e:=Open(Options{Directory:filepath.Join(t.TempDir(),"missing"),Key:o.Key,Epoch:o.Epoch,Create:false});e==nil{t.Fatal("missing state guessed zero")}
}
func TestQueueSaveFailuresFenceUntilVerifiedReopen(t *testing.T){
 for _,stage:=range []string{"before_rename","after_rename"}{t.Run(stage,func(t *testing.T){b,o:=testBox(t);first,e:=b.Observe(values(1),false);if e!=nil{t.Fatal(e)}
  b.hook=func(s string)error{if s==stage{return fmt.Errorf("test fault")};return nil}
  if _,e=b.Observe(values(2),false);e==nil{t.Fatal("fault ignored")};if _,e=b.Pending();!errors.Is(e,ErrClosed){t.Fatal("uncertain writer not fenced",e)}
  _=b.Close();o.Create=false;recovered,e:=Open(o);if e!=nil{t.Fatal(e)};defer recovered.Close();q,e:=recovered.Pending();if e!=nil{t.Fatal(e)}
  if q[0].SHA!=first.SHA{t.Fatal("fault lost original record")};want:=1;if stage=="after_rename"{want=2};if len(q)!=want{t.Fatalf("torn journal: %d wanted %d",len(q),want)}
 })}
}
func TestConcurrentIdenticalObservationOneCheckpoint(t *testing.T){b,_:=testBox(t);var wg sync.WaitGroup;errs:=make(chan error,16);for i:=0;i<16;i++{wg.Add(1);go func(){defer wg.Done();_,e:=b.Observe(values(2),false);errs<-e}()};wg.Wait();close(errs);for e:=range errs{if e!=nil{t.Fatal(e)}};q,e:=b.Pending();if e!=nil||len(q)!=1{t.Fatal("concurrent repeat created reports",e)}}
func TestCheckpointSIGKILLHelper(t *testing.T){
 if os.Getenv("PRO_USAGE_CRASH_TEST")==""{return};var o Options;if json.Unmarshal([]byte(os.Getenv("PRO_USAGE_CRASH_TEST")),&o)!=nil{os.Exit(31)}
 b,e:=Open(o);if e!=nil{os.Exit(32)};defer b.Close();if _,e=b.Observe(values(1),false);e!=nil{os.Exit(33)}
 b.hook=func(s string)error{if s==os.Getenv("PRO_USAGE_CRASH_STAGE"){if os.WriteFile(os.Getenv("PRO_USAGE_CRASH_MARKER"),[]byte("synced"),0600)!=nil{os.Exit(34)};select{}};return nil}
 _,_=b.Observe(values(2),false)
}
func TestCheckpointAbruptExitBeforeAfterRename(t *testing.T){
 for _,stage:=range []string{"before_rename","after_rename"}{t.Run(stage,func(t *testing.T){o:=Options{Directory:filepath.Join(t.TempDir(),"new"),Key:bytes.Repeat([]byte{9},32),Epoch:testEpoch(t),Create:true};raw,_:=json.Marshal(o);marker:=filepath.Join(t.TempDir(),"marker")
  cmd:=exec.Command(os.Args[0],"-test.run=^TestCheckpointSIGKILLHelper$","-test.timeout=15s");cmd.Env=append(os.Environ(),"PRO_USAGE_CRASH_TEST="+string(raw),"PRO_USAGE_CRASH_STAGE="+stage,"PRO_USAGE_CRASH_MARKER="+marker)
  if e:=cmd.Start();e!=nil{t.Fatal(e)};defer func(){_=cmd.Process.Kill();_=cmd.Wait()}()
  reached:=false;for i:=0;i<150;i++{if _,e:=os.Stat(marker);e==nil{reached=true;break};time.Sleep(20*time.Millisecond)};if !reached{t.Fatal("crash checkpoint not reached")}
  _=cmd.Process.Kill();_=cmd.Wait();o.Create=false;b,e:=Open(o);if e!=nil{t.Fatal(e)};defer b.Close();q,e:=b.Pending();if e!=nil||len(q)<1||q[0].Sequence!=1{t.Fatal("SIGKILL lost durable sample",e)}
  want:=1;if stage=="after_rename"{want=2};if len(q)!=want{t.Fatal("invalid recovered history")}
 })}
}
