package business

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "sync"
 "testing"
)

func opened(t *testing.T)(*Store,string){
 t.Helper()
 path:=filepath.Join(t.TempDir(),"private","business.sqlite")
 s,err:=Open(path)
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ =s.Close()})
 return s,path
}
func newRep(t *testing.T,s *Store,id string){
 t.Helper()
 if _,err:=s.CreateRepresentative(context.Background(),id,"Representative "+id);err!=nil{t.Fatal(err)}
}
func grant(t *testing.T,s *Store,id,evt string,volume,units int64)Representative{
 t.Helper()
 result,err:=s.AdjustCredits(context.Background(),CreditChange{
  EventID:evt,RepresentativeID:id,VolumeDeltaBytes:volume,
  UnlimitedDeltaUnits:units,Reason:"Operator allocation",
 })
 if err!=nil{t.Fatal(err)}
 return result
}
func TestBusinessDurableCreditsAndUserMonthFormula(t *testing.T){
 s,path:=opened(t);ctx:=context.Background();newRep(t,s,"repA")
 after:=grant(t,s,"repA","grant-000000000001",5000,500)
 if after.VolumeCreditBytes!=5000||after.UnlimitedCreditUnits!=500{t.Fatalf("initial allocation: %+v",after)}
 n:=NewClient{EventID:"issue-000000000001",ClientID:"unlimited-a",RepresentativeID:"repA",
  Name:"Two-month five-user",Unlimited:true,IPCount:5,DurationDays:60}
 c,err:=s.CreateClient(ctx,n)
 if err!=nil{t.Fatal(err)}
 if c.ChargeUnlimitedUnits!=10||c.PlanKind!="unlimited"||c.DeploymentState!="pending_node_apply"{
  t.Fatalf("wrong user-month charge or fabricated Xray activation: %+v",c)
 }
 again,err:=s.CreateClient(ctx,n)
 if err!=nil||again.UUID!=c.UUID||again.ExpiresUnix!=c.ExpiresUnix{t.Fatalf("idempotency failed: %+v %v",again,err)}
 after,err=s.Representative(ctx,"repA")
 if err!=nil||after.UnlimitedCreditUnits!=490 {t.Fatalf("duplicate deducted credits: %+v %v",after,err)}
 n.EventID="issue-000000000002";n.ClientID="unlimited-b";n.DurationDays=45
 c,err=s.CreateClient(ctx,n)
 if err!=nil||c.ChargeUnlimitedUnits!=8{t.Fatalf("partial user-month must round entire service up: %+v %v",c,err)}
 n.EventID="issue-000000000003";n.ClientID="vol-b";n.Unlimited=false;n.VolumeBytes=1000;n.IPCount=0
 c,err=s.CreateClient(ctx,n)
 if err!=nil||c.ChargeVolumeBytes!=1000||c.IPCount!=0{t.Fatalf("volumetric unlimited IP policy lost: %+v %v",c,err)}
 after,err=s.Representative(ctx,"repA")
 if err!=nil||after.VolumeCreditBytes!=4000||after.UnlimitedCreditUnits!=482 {t.Fatalf("credit mismatch: %+v %v",after,err)}
 if err=s.Close();err!=nil{t.Fatal(err)}
 reopened,err:=Open(path)
 if err!=nil{t.Fatal(err)}
 defer reopened.Close()
 clients,err:=reopened.ListClients(ctx,"repA",100,0)
 if err!=nil||len(clients)!=3{t.Fatalf("restart lost clients: %d %v",len(clients),err)}
 events,err:=reopened.ListEvents(ctx,"repA",100,0)
 if err!=nil||len(events)!=4{t.Fatalf("restart lost immutable events: %d %v",len(events),err)}
 after,err=reopened.Representative(ctx,"repA")
 if err!=nil||after.UnlimitedCreditUnits!=482{t.Fatalf("restart reset balances: %+v %v",after,err)}
}
func TestBusinessReplayMismatchInsufficientAndAtomicity(t *testing.T){
 s,_:=opened(t);ctx:=context.Background();newRep(t,s,"repB")
 grant(t,s,"repB","grant-000000000002",200,2)
 same,err:=s.AdjustCredits(ctx,CreditChange{EventID:"grant-000000000002",RepresentativeID:"repB",VolumeDeltaBytes:200,UnlimitedDeltaUnits:2,Reason:"Operator allocation"})
 if err!=nil||same.VolumeCreditBytes!=200||same.UnlimitedCreditUnits!=2{t.Fatalf("replay credited twice: %+v %v",same,err)}
 _,err=s.AdjustCredits(ctx,CreditChange{EventID:"grant-000000000002",RepresentativeID:"repB",VolumeDeltaBytes:200,UnlimitedDeltaUnits:3,Reason:"Operator allocation"})
 if !errors.Is(err,ErrConflict){t.Fatalf("mismatched replay accepted: %v",err)}
 _,err=s.AdjustCredits(ctx,CreditChange{EventID:"negative-00000001",RepresentativeID:"repB",VolumeDeltaBytes:-201,Reason:"Bad debit"})
 if !errors.Is(err,ErrInsufficient){t.Fatalf("credit underflow accepted: %v",err)}
 insufficient:=NewClient{EventID:"issue-000000000010",ClientID:"x",RepresentativeID:"repB",Name:"Too much",DurationDays:60,Unlimited:true,IPCount:5}
 _,err=s.CreateClient(ctx,insufficient)
 if !errors.Is(err,ErrInsufficient){t.Fatalf("unfunded client created: %v",err)}
 if _,err=s.Client(ctx,"x");!errors.Is(err,ErrNotFound){t.Fatalf("unfunded client persisted: %v",err)}
 insufficient.EventID="issue-000000000011";insufficient.ClientID="good"
 insufficient.Unlimited=false;insufficient.VolumeBytes=100;insufficient.IPCount=0
 c,err:=s.CreateClient(ctx,insufficient)
 if err!=nil||c.PlanKind!="volume"{t.Fatalf("paid client failed: %+v %v",c,err)}
 collision:=insufficient;collision.VolumeBytes=150
 if _,err=s.CreateClient(ctx,collision);!errors.Is(err,ErrConflict){
  t.Fatalf("recycled event ID with altered payload accepted: %v",err)
 }
 collision.EventID="issue-000000000012"
 if _,err=s.CreateClient(ctx,collision);!errors.Is(err,ErrConflict){
  t.Fatalf("duplicate client identity minted: %v",err)
 }
 r,err:=s.Representative(ctx,"repB")
 if err!=nil||r.VolumeCreditBytes!=100||r.UnlimitedCreditUnits!=2{
  t.Fatalf("failed transactions incorrectly debited credits: %+v %v",r,err)
 }
}
func TestOwnerClientsDoNotDebitRepAndNeverPublish(t *testing.T){
 s,_:=opened(t);ctx:=context.Background()
 owner,err:=s.Representative(ctx,"owner")
 if err!=nil||owner.Kind!="owner"{t.Fatal(err)}
 n:=NewClient{EventID:"owner-issue-000001",RepresentativeID:"owner",ClientID:"owner1",
  Name:"Owner native client",DurationDays:30,Unlimited:true,IPCount:3}
 c,err:=s.CreateClient(ctx,n)
 if err!=nil||c.ChargeUnlimitedUnits!=3||c.DeploymentState!="pending_node_apply"{t.Fatalf("owner client failed: %+v %v",c,err)}
 after,err:=s.Representative(ctx,"owner")
 if err!=nil||after.UnlimitedCreditUnits!=0{t.Fatalf("owner balance touched: %+v %v",after,err)}
 if _,err=s.CreateRepresentative(ctx,"owner","hijack");!errors.Is(err,ErrInvalid){t.Fatal("owner identity overwritten")}
}
func TestSQLiteEventAuditCannotBeUpdatedOrDeleted(t *testing.T){
 s,_:=opened(t);ctx:=context.Background();newRep(t,s,"repC")
 grant(t,s,"repC","grant-immutable-0001",10,20)
 if _,err:=s.db.ExecContext(ctx,"UPDATE events SET unlimited_delta_units=999 WHERE event_id='grant-immutable-0001'");err==nil{t.Fatal("audit event updated")}
 if _,err:=s.db.ExecContext(ctx,"DELETE FROM events WHERE event_id='grant-immutable-0001'");err==nil{t.Fatal("audit event deleted")}
 events,err:=s.ListEvents(ctx,"repC",100,0)
 if err!=nil||len(events)!=1||events[0].UnlimitedDeltaUnits!=20||events[0].Reason!="Operator allocation"{t.Fatalf("audit corrupted: %+v %v",events,err)}
}
func TestConcurrentIdempotentDebitsNeverGoBelowZero(t *testing.T){
 s,_:=opened(t);ctx:=context.Background();newRep(t,s,"repD")
 grant(t,s,"repD","grant-concurrent-01",100,0)
 var wg sync.WaitGroup; wg.Add(24)
 successes:=make(chan error,24)
 for i:=0;i<24;i++{go func(i int){
   defer wg.Done()
   _,err:=s.CreateClient(ctx,NewClient{
    EventID:fmt.Sprintf("concurrent-event-%04d",i),ClientID:fmt.Sprintf("client-%04d",i),
    RepresentativeID:"repD",Name:"Concurrent client",VolumeBytes:10,DurationDays:30,IPCount:0,
   })
   successes<-err
  }(i)}
 wg.Wait();close(successes)
 ok:=0
 for err:=range successes{
  if err==nil{ok++;continue}
  if !errors.Is(err,ErrInsufficient){t.Fatalf("unexpected concurrent sqlite error: %v",err)}
 }
 if ok!=10{t.Fatalf("expected 10 atomic debits, got %d",ok)}
 r,err:=s.Representative(ctx,"repD")
 if err!=nil||r.VolumeCreditBytes!=0{t.Fatalf("credit underflow or oversell: %+v %v",r,err)}
 clients,err:=s.ListClients(ctx,"repD",100,0)
 if err!=nil||len(clients)!=10{t.Fatalf("partial clients persisted: %d %v",len(clients),err)}
}
func TestUnsafeDatabasePathsAndUnknownSchemaFailClosed(t *testing.T){
 parent:=t.TempDir()
 exposed:=filepath.Join(parent,"exposed")
 if err:=os.Mkdir(exposed,0755);err!=nil{t.Fatal(err)}
 if _,err:=Open(filepath.Join(exposed,"state.sqlite"));!errors.Is(err,ErrUnsafePath){t.Fatalf("world-readable parent accepted: %v",err)}
 private:=filepath.Join(parent,"private")
 if err:=os.Mkdir(private,0700);err!=nil{t.Fatal(err)}
 real:=filepath.Join(private,"original")
 if err:=os.WriteFile(real,[]byte("no"),0600);err!=nil{t.Fatal(err)}
 link:=filepath.Join(private,"state.sqlite")
 if err:=os.Symlink(real,link);err!=nil{t.Fatal(err)}
 if _,err:=Open(link);!errors.Is(err,ErrUnsafePath){t.Fatalf("symlink database accepted: %v",err)}
 s,_:=opened(t)
 if _,err:=s.db.Exec("PRAGMA user_version=93");err!=nil{t.Fatal(err)}
 // Current open handle may persist; next process must refuse incompatible state.
 dbpath:=filepath.Join(parent,"newprivate","ledger.sqlite")
 fresh,err:=Open(dbpath);if err!=nil{t.Fatal(err)}
 if _,err=fresh.db.Exec("PRAGMA user_version=93");err!=nil{t.Fatal(err)}
 if err=fresh.Close();err!=nil{t.Fatal(err)}
 if _,err=Open(dbpath);err==nil{t.Fatal("incompatible newer schema silently downgraded")}
}
func TestInvalidFinancialAndClientRequests(t *testing.T){
 s,_:=opened(t);ctx:=context.Background();newRep(t,s,"repE")
 _,err:=s.AdjustCredits(ctx,CreditChange{EventID:"invalid",RepresentativeID:"repE",VolumeDeltaBytes:1,Reason:"bad"})
 if !errors.Is(err,ErrInvalid){t.Fatalf("short event id allowed: %v",err)}
 _,err=s.CreateClient(ctx,NewClient{EventID:"issue-invalid-0001",ClientID:"invalid",RepresentativeID:"repE",Name:"x",DurationDays:30,Unlimited:true,IPCount:0})
 if !errors.Is(err,ErrInvalid){t.Fatalf("unlimited with unlimited IP allowed: %v",err)}
 _,err=s.CreateClient(ctx,NewClient{EventID:"issue-invalid-0002",ClientID:"invalid",RepresentativeID:"repE",Name:"x",DurationDays:30,VolumeBytes:0,IPCount:0})
 if !errors.Is(err,ErrInvalid){t.Fatalf("zero volumetric plan allowed: %v",err)}
}

func TestCannotAdoptForeignSQLiteEvenWhenSchemaVersionIsZero(t *testing.T){
 s,path:=opened(t)
 if _,err:=s.db.Exec("CREATE TABLE foreign_old_panel_records(id TEXT PRIMARY KEY)");err!=nil{t.Fatal(err)}
 if _,err:=s.db.Exec("PRAGMA user_version=0");err!=nil{t.Fatal(err)}
 if err:=s.Close();err!=nil{t.Fatal(err)}
 if _,err:=Open(path);!errors.Is(err,ErrUnsafePath){
  t.Fatalf("foreign SQLite schema was silently merged into PRO: %v",err)
 }
}
