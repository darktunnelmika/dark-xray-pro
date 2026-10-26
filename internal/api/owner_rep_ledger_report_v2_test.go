package api

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestOwnerRepLedgerReportPrivateScopeAndCorrectFinance(t *testing.T){
 s:=enabledOwner(t);ctx:=context.Background()
 for _,id:=range []string{"reportOwnerA","reportOwnerB"}{
  if _,err:=s.business.CreateRepresentative(ctx,id,"Representative "+id);err!=nil{t.Fatal(err)}
 }
 if _,err:=s.business.AdjustCredits(ctx,business.CreditChange{
  EventID:"owner-report-api-grant-0000001",RepresentativeID:"reportOwnerA",
  UnlimitedDeltaUnits:50,VolumeDeltaBytes:1000,Reason:"Owner audited grant"});err!=nil{t.Fatal(err)}
 if _,err:=s.business.ProvisionRepresentativeLogin(ctx,"reportOwnerA","reportqauser","reportqauser-long-password-0001");err!=nil{t.Fatal(err)}
 owner,_:=browserLogin(t,s,ownerUsername,ownerPassword)
 rep,_:=browserLogin(t,s,"reportqauser","reportqauser-long-password-0001")
 path:="/api/v1/workspace/owner/reports/representatives/reportOwnerA"
 read:=func(route string,cookie *http.Cookie)*httpResponse{
  r:=browserRequest(t,s.Handler(),"GET",route,nil,cookie,"","")
  return &httpResponse{Code:r.Code,Body:r.Body.String()}
 }
 if got:=read(path,nil);got.Code!=401{t.Fatal("anonymous report access",got)}
 if got:=read(path,rep);got.Code!=403{t.Fatal("representative can access owner report",got)}
 if got:=read(path+"?expand=true",owner);got.Code!=422{t.Fatal("unknown query accepted",got)}
 if got:=read("/api/v1/workspace/owner/reports/representatives/owner",owner);got.Code!=422{t.Fatal("owner impersonation",got)}
 if got:=read("/api/v1/workspace/owner/reports/representatives/unknown",owner);got.Code!=404{t.Fatal("unknown rep",got)}
 got:=read(path,owner)
 if got.Code!=200{t.Fatal("owner report not available",got)}
 var report business.OwnerRepLedgerReport
 if err:=json.Unmarshal([]byte(got.Body),&report);err!=nil||report.RepresentativeID!="reportOwnerA"||
 report.FreeVolumeBytes!="1000"||report.FreeUnlimitedUnits!="50"||
 report.SellerQuotaState!="not_enabled"||report.ReservedVolumeBytes!=""||
 !strings.Contains(got.Body,"not_enabled"){t.Fatal("incorrect readout",got,err)}
 if strings.Contains(got.Body,"password")||strings.Contains(got.Body,"token")||strings.Contains(got.Body,"telegram_user_id"){
  t.Fatal("sensitive field returned")
 }
 if bad:=browserRequest(t,s.AgentHandler(),"GET",path,nil,owner,"","");bad.Code==200{
  t.Fatal("private financial report exposed on Agent listener")
 }
}
