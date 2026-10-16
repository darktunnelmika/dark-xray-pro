//go:build linux

package usageledger

import (
 "bytes"
 "context"
 "database/sql"
 "errors"
 "fmt"
 "net/url"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"
 uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
 _ "modernc.org/sqlite"
)
func testDB(t *testing.T)(*sql.DB,string){
 t.Helper();path:=filepath.Join(t.TempDir(),"native.sqlite");db:=connectDB(t,path)
 for _,q:=range []string{
 "CREATE TABLE representatives(id TEXT PRIMARY KEY,enabled INTEGER,volume_credit_bytes INTEGER,unlimited_credit_units INTEGER)",
 "INSERT INTO representatives VALUES('owner',1,0,0)",
 "CREATE TABLE clients(id TEXT PRIMARY KEY,uuid TEXT,representative_id TEXT,plan_kind TEXT,volume_bytes INTEGER,archived INTEGER,expires_unix INTEGER,deployment_state TEXT)",
 "CREATE TABLE native_config_generation(id INTEGER PRIMARY KEY,value INTEGER)","INSERT INTO native_config_generation VALUES(1,1)","PRAGMA user_version=7",
 }{if _,e:=db.Exec(q);e!=nil{t.Fatal(e)}}
 return db,path
}
func connectDB(t *testing.T,path string)*sql.DB{
 t.Helper();u:=url.URL{Scheme:"file",Path:path};q:=url.Values{};for _,p:=range []string{"foreign_keys(1)","journal_mode(WAL)","synchronous(2)","busy_timeout(3000)","trusted_schema(0)"}{q.Add("_pragma",p)};u.RawQuery=q.Encode()
 db,e:=sql.Open("sqlite",u.String());if e!=nil{t.Fatal(e)};db.SetMaxOpenConns(1);t.Cleanup(func(){_=db.Close()});return db
}
func account(t *testing.T,db *sql.DB,l *Ledger,id string,complete bool){
 t.Helper();_,e:=db.Exec("INSERT INTO clients VALUES(?,?,'owner','volume',1000,0,?,'pending_node_apply')",id,"uuid-"+id,time.Now().Add(time.Hour).Unix());if e!=nil{t.Fatal(e)}
 if e=l.InitializeAccount(context.Background(),Seed{ClientID:id,EventID:"seed-local-native-"+id,Complete:complete,Reason:"Known test fixture baseline"});e!=nil{t.Fatal(e)}
}
func registered(t *testing.T,l *Ledger,id,node string,clients []string,billable bool)(uc.Epoch,*uc.Outbox){
 t.Helper();p,e:=uc.Process(os.Getpid());if e!=nil{t.Fatal(e)}
 epoch:=uc.Epoch{NodeID:node,InstallationID:strings.Repeat("a",32),ID:id,ConfigSHA:strings.Repeat("c",64),KernelBootID:p.BootID,PID:p.PID,ProcessStartTicks:p.StartTicks}
 for _,id:=range clients{email,e:=uc.CounterEmail(id);if e!=nil{t.Fatal(e)};epoch.Bindings=append(epoch.Bindings,uc.Binding{ClientID:id,Email:email,Billable:billable})}
 if e=l.RegisterEpoch(context.Background(),epoch);e!=nil{t.Fatal(e)}
 b,e:=uc.Open(uc.Options{Directory:filepath.Join(t.TempDir(),"outbox"),Key:bytes.Repeat([]byte{5},32),Epoch:epoch,Create:true});if e!=nil{t.Fatal(e)};t.Cleanup(func(){_=b.Close()});return epoch,b
}
func principal(e uc.Epoch)Principal{return Principal{NodeID:e.NodeID,InstallationID:e.InstallationID}}
func TestRealModerncLedgerReceiptQuotaAndAncientReplay(t *testing.T){
 ctx:=context.Background();db,_:=testDB(t);l,e:=Open(ctx,db);if e!=nil{t.Fatal(e)};account(t,db,l,"customer",true)
 epoch,b:=registered(t,l,strings.Repeat("b",32),"node-one",[]string{"customer"},true)
 r1,e:=b.Observe([]uc.Usage{{ClientID:"customer",Uplink:100,Downlink:200}},false);if e!=nil{t.Fatal(e)}
 a1,e:=l.Accept(ctx,principal(epoch),r1);if e!=nil{t.Fatal(e)}
 r2,e:=b.Observe([]uc.Usage{{ClientID:"customer",Uplink:800,Downlink:900}},false);if e!=nil{t.Fatal(e)}
 if _,e=l.Accept(ctx,principal(epoch),r2);e!=nil{t.Fatal(e)};again,e:=l.Accept(ctx,principal(epoch),r1);if e!=nil||again!=a1{t.Fatal("ancient exact receipt double charged",e)}
 a,e:=l.Account(ctx,"customer");if e!=nil||a.Total!=1700||a.Eligible||!a.Exhausted{t.Fatalf("overshoot was discarded: %+v %v",a,e)}
 for _,q:=range []string{"DELETE FROM pro_usage_receipts","UPDATE pro_usage_deltas SET uplink=0","DELETE FROM pro_usage_seeds"}{if _,e=db.Exec(q);e==nil{t.Fatal("immutable usage audit mutated")}}
}
func TestCommitFailuresAtomicAndLostReceiptSurvivesRestart(t *testing.T){
 for _,point:=range []string{"after_totals","before_commit","after_commit"}{t.Run(point,func(t *testing.T){ctx:=context.Background();db,path:=testDB(t);l,e:=Open(ctx,db);if e!=nil{t.Fatal(e)};account(t,db,l,"customer",true)
  epoch,b:=registered(t,l,strings.Repeat("b",32),"node-one",[]string{"customer"},true);r,e:=b.Observe([]uc.Usage{{ClientID:"customer",Uplink:10,Downlink:20}},false);if e!=nil{t.Fatal(e)}
  l.checkpoint=func(stage string)error{if stage==point{return fmt.Errorf("test-only failure")};return nil}
  if _,e=l.Accept(ctx,principal(epoch),r);e==nil{t.Fatal("failed transaction acknowledged")};l.checkpoint=nil
  a,e:=l.Account(ctx,"customer");want:=int64(0);if point=="after_commit"{want=30};if e!=nil||a.Total!=want{t.Fatalf("partial commit %+v %v",a,e)}
  if e=db.Close();e!=nil{t.Fatal(e)};fresh:=connectDB(t,path);l,e=Open(ctx,fresh);if e!=nil{t.Fatal(e)}
  if _,e=l.Accept(ctx,principal(epoch),r);e!=nil{t.Fatal(e)};a,e=l.Account(ctx,"customer");if e!=nil||a.Total!=30{t.Fatal("retry after restart double charged",e)}
 })}
}
func TestIndependentConnectionsCannotDoubleCommit(t *testing.T){
 ctx:=context.Background();db,path:=testDB(t);l,e:=Open(ctx,db);if e!=nil{t.Fatal(e)};account(t,db,l,"customer",true)
 second,e:=Open(ctx,connectDB(t,path));if e!=nil{t.Fatal(e)};epoch,b:=registered(t,l,strings.Repeat("b",32),"node-one",[]string{"customer"},true)
 report,e:=b.Observe([]uc.Usage{{ClientID:"customer",Uplink:20,Downlink:40}},false);if e!=nil{t.Fatal(e)}
 var wg sync.WaitGroup;errs:=make(chan error,12)
 for i:=0;i<12;i++{wg.Add(1);go func(i int){defer wg.Done();ledger:=l;if i%2==1{ledger=second};_,e:=ledger.Accept(ctx,principal(epoch),report);errs<-e}(i)}
 wg.Wait();close(errs);for e:=range errs{if e!=nil{t.Fatal(e)}};a,e:=l.Account(ctx,"customer");if e!=nil||a.Total!=60{t.Fatal("multiple database handles duplicated charge",e)}
}
func TestMultipleNodeTerminalAndTransitRoleIsAuthorityNotReport(t *testing.T){
 ctx:=context.Background();db,_:=testDB(t);l,e:=Open(ctx,db);if e!=nil{t.Fatal(e)};account(t,db,l,"customer",true)
 for i,node:=range []string{"node-one","node-two","transit-only"}{epoch,b:=registered(t,l,fmt.Sprintf("%032x",i+1),node,[]string{"customer"},i<2);r,e:=b.Observe([]uc.Usage{{ClientID:"customer",Uplink:40,Downlink:60}},false);if e!=nil{t.Fatal(e)};if _,e=l.Accept(ctx,principal(epoch),r);e!=nil{t.Fatal(e)}
  wrong:=principal(epoch);wrong.InstallationID=strings.Repeat("f",32);if _,e=l.Accept(ctx,wrong,r);!errors.Is(e,ErrAuthority){t.Fatal("different installation accepted",e)}
 }
 a,e:=l.Account(ctx,"customer");if e!=nil||a.Total!=200{t.Fatalf("transit traffic double counted: %+v %v",a,e)}
}
func TestUnknownBaselineAndUnobservedTailNeverBecomeZero(t *testing.T){
 ctx:=context.Background();db,_:=testDB(t);l,e:=Open(ctx,db);if e!=nil{t.Fatal(e)};account(t,db,l,"unknown",false);account(t,db,l,"known",true)
 a,e:=l.Account(ctx,"unknown");if e!=nil||a.Eligible||a.Known{t.Fatal("unknown baseline permitted",e)}
 epoch,b:=registered(t,l,strings.Repeat("b",32),"node-one",[]string{"known"},true)
 if e=l.MarkEpochUncertain(ctx,principal(epoch),epoch.ID,"Unobserved crash tail");e!=nil{t.Fatal(e)}
 if e=l.MarkEpochUncertain(ctx,principal(epoch),epoch.ID,"Unobserved crash tail");e!=nil{t.Fatal(e)}
 r,e:=b.Observe([]uc.Usage{{ClientID:"known",Uplink:10,Downlink:20}},true);if e!=nil{t.Fatal(e)};if _,e=l.Accept(ctx,principal(epoch),r);e!=nil{t.Fatal(e)}
 a,e=l.Account(ctx,"known");if e!=nil||a.Total!=30||a.Holds!=1||a.Known||a.Eligible{t.Fatalf("late known sample forgave missing tail: %+v %v",a,e)}
}
func TestRegistrationCannotResetAProcessAndSchemaFailsClosed(t *testing.T){
 ctx:=context.Background();db,_:=testDB(t);l,e:=Open(ctx,db);if e!=nil{t.Fatal(e)};account(t,db,l,"customer",true)
 epoch,_:=registered(t,l,strings.Repeat("b",32),"node-one",[]string{"customer"},true)
 changed:=epoch;changed.ID=strings.Repeat("d",32);if e=l.RegisterEpoch(ctx,changed);!errors.Is(e,ErrConflict){t.Fatal("same process reset baseline",e)}
 seed:=Seed{ClientID:"customer",EventID:"seed-local-native-customer",Complete:true,Reason:"Known test fixture baseline"}
 if e=l.InitializeAccount(ctx,seed);e!=nil{t.Fatal(e)};seed.Uplink=1;if e=l.InitializeAccount(ctx,seed);!errors.Is(e,ErrConflict){t.Fatal("baseline silently overwritten",e)}
 if _,e=db.Exec("PRAGMA user_version=99");e!=nil{t.Fatal(e)};if _,e=Open(ctx,db);!errors.Is(e,ErrSchema){t.Fatal("unreviewed schema adopted",e)}
}
func Test742SyntheticClientsCommitOneExactReceipt(t *testing.T){
 ctx:=context.Background();db,_:=testDB(t);l,e:=Open(ctx,db);if e!=nil{t.Fatal(e)};ids:=make([]string,742);samples:=make([]uc.Usage,742)
 for i:=range ids{ids[i]=fmt.Sprintf("synthetic-%04d",i);account(t,db,l,ids[i],true);samples[i]=uc.Usage{ClientID:ids[i],Uplink:int64(i+1),Downlink:50}}
 epoch,b:=registered(t,l,strings.Repeat("b",32),"node-one",ids,true);r,e:=b.Observe(samples,false);if e!=nil{t.Fatal(e)}
 for i:=0;i<3;i++{if _,e=l.Accept(ctx,principal(epoch),r);e!=nil{t.Fatal(e)}}
 var count,total int64;if e=db.QueryRow("SELECT COUNT(*),SUM(uplink+downlink) FROM pro_usage_accounts").Scan(&count,&total);e!=nil{t.Fatal(e)}
 if count!=742||total!=742*743/2+742*50{t.Fatalf("unexpected aggregates: %d %d",count,total)}
}
