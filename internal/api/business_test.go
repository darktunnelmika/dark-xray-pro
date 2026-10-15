package api

import (
 "encoding/json"
 "net/http"
 "path/filepath"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func businessLab(t *testing.T)*Server{
 t.Helper()
 s,_,_,_:=newLab(t)
 path:=filepath.Join(t.TempDir(),"private","business.sqlite")
 db,err:=business.Open(path)
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ =db.Close()})
 if err=s.EnableBusinessLab(db);err!=nil{t.Fatal(err)}
 return s
}

func TestBusinessAdminAPINotPublicAndNoAgents(t *testing.T){
 s,_,_,_:=newLab(t)
 if got:=request(t,s.Handler(),"GET","/api/v1/admin/business/representatives",ownerToken,nil);got.Code!=404{
  t.Fatalf("business endpoint enabled without private database: %d",got.Code)
 }
 s=businessLab(t)
 h:=s.Handler()
 for _,route:=range []string{"/api/v1/admin/business/representatives","/api/v1/admin/business/clients","/api/v1/admin/business/events"}{
  for _,token:=range []string{"",nodeToken,otherToken}{
   if got:=request(t,h,"GET",route,token,nil);got.Code!=401{
    t.Fatalf("credential %q could access %s: %d",token[:0],route,got.Code)
   }
  }
  if got:=request(t,s.AgentHandler(),"GET",route,ownerToken,nil);got.Code==200{
   t.Fatalf("Agent-only interface exposed owner finance route %q",route)
  }
 }
 if got:=request(t,h,"POST","/api/v1/admin/business/clients",nodeToken,
  business.NewClient{EventID:"api-client-event-0001",ClientID:"user-a",RepresentativeID:"owner",Name:"unauthorized",DurationDays:30,VolumeBytes:100});got.Code!=401{
  t.Fatal("Node credential minted a client",got.Code)
 }
}

func TestOwnerBusinessCreditsAndClientsHTTP(t *testing.T){
 s:=businessLab(t);h:=s.Handler()
 created:=request(t,h,"POST","/api/v1/admin/business/representatives",ownerToken,map[string]string{"id":"reseller1","name":"Representative One"})
 if created.Code!=201{t.Fatalf("create rep: %d %s",created.Code,created.Body.String())}
 if got:=request(t,h,"POST","/api/v1/admin/business/representatives",ownerToken,map[string]any{"id":"reseller1","name":"changed","unapproved":1});got.Code!=400{
  t.Fatalf("unknown rep fields were accepted: %d",got.Code)
 }
 adjusted:=request(t,h,"POST","/api/v1/admin/business/representatives/reseller1/credits",ownerToken,business.CreditChange{
  EventID:"api-credit-event-001",RepresentativeID:"reseller1",UnlimitedDeltaUnits:40,
  VolumeDeltaBytes:2000,Reason:"LAB owner allocation",
 })
 if adjusted.Code!=200{t.Fatalf("adjust failed: %d %s",adjusted.Code,adjusted.Body.String())}
 if got:=request(t,h,"POST","/api/v1/admin/business/representatives/someoneelse/credits",ownerToken,business.CreditChange{
  EventID:"api-credit-event-002",RepresentativeID:"reseller1",UnlimitedDeltaUnits:2,Reason:"mismatch",
 });got.Code!=422{t.Fatalf("body/path mismatch accepted: %d",got.Code)}
 n:=business.NewClient{EventID:"api-client-event-001",ClientID:"newproclient",RepresentativeID:"reseller1",
  Name:"Two months 5 users",Unlimited:true,IPCount:5,DurationDays:60}
 received:=request(t,h,"POST","/api/v1/admin/business/clients",ownerToken,n)
 if received.Code!=201{t.Fatalf("create client failed: %d %s",received.Code,received.Body.String())}
 var client business.Client
 if err:=json.Unmarshal(received.Body.Bytes(),&client);err!=nil{t.Fatal(err)}
 if client.ChargeUnlimitedUnits!=10||client.DeploymentState!="pending_node_apply"{
  t.Fatalf("claimed a live node or charged incorrect user-months: %+v",client)
 }
 again:=request(t,h,"POST","/api/v1/admin/business/clients",ownerToken,n)
 if again.Code!=201{t.Fatalf("retry failed: %d",again.Code)}
 var duplicate business.Client
 if err:=json.Unmarshal(again.Body.Bytes(),&duplicate);err!=nil{t.Fatal(err)}
 if duplicate.UUID!=client.UUID{t.Fatal("same event minted another UUID")}
 read:=request(t,h,"GET","/api/v1/admin/business/representatives?limit=20&offset=0",ownerToken,nil)
 if read.Code!=200||!strings.Contains(read.Body.String(),`"unlimited_credit_units":30`){t.Fatalf("double debit? %d %s",read.Code,read.Body.String())}
 list:=request(t,h,"GET","/api/v1/admin/business/clients?representative_id=reseller1&limit=10",ownerToken,nil)
 if list.Code!=200||!strings.Contains(list.Body.String(),`"newproclient"`){t.Fatalf("client not listed: %d %s",list.Code,list.Body.String())}
 audit:=request(t,h,"GET","/api/v1/admin/business/events?representative_id=reseller1&limit=50",ownerToken,nil)
 if audit.Code!=200||strings.Count(audit.Body.String(),`"event_id"`)!=2{t.Fatalf("audit mutated by retry: %d %s",audit.Code,audit.Body.String())}
 if got:=request(t,h,"GET","/api/v1/admin/business/events?limit=201",ownerToken,nil);got.Code!=422{
  t.Fatalf("unbounded list allowed: %d",got.Code)
 }
 if got:=request(t,h,"GET","/api/v1/admin/business/events?unknown_field=1",ownerToken,nil);got.Code!=422{
  t.Fatalf("unexpected query allowed: %d",got.Code)
 }
}

func TestBusinessRequestsStrictAndFailClosed(t *testing.T){
 s:=businessLab(t);h:=s.Handler()
 bad:=request(t,h,"POST","/api/v1/admin/business/clients",ownerToken,map[string]any{
  "event_id":"valid-event-000001","client_id":"bad","representative_id":"owner",
  "name":"bad","duration_days":30,"volume_bytes":0,
 })
 if bad.Code!=422{t.Fatalf("invalid zero plan accepted: %d",bad.Code)}
 forbidden:=request(t,h,"GET","/api/v1/admin/business/clients","bad-token",nil)
 if forbidden.Code!=http.StatusUnauthorized{t.Fatal("business read leaked",forbidden.Code)}
}
