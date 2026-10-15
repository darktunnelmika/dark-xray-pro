package api

import (
 "encoding/json"
 "net/http"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestClientLifecycleTenantScopeCSRFIdempotencyAndArchive(t *testing.T){
 s:=enabledOwner(t);h:=s.Handler()
 ownerCookie,ownerCSRF:=browserLogin(t,s,ownerUsername,ownerPassword)
 send:=func(method,path string,body any,c *http.Cookie,csrf string)*httpResponse{
  w:=browserRequest(t,h,method,path,body,c,csrf,"https://"+browserHost)
  return &httpResponse{Code:w.Code,Body:w.Body.String()}
 }
 for _,rep:=range []string{"rep-first","rep-other"}{
  if w:=send("POST","/api/v1/workspace/representatives",
   map[string]string{"id":rep,"name":rep},ownerCookie,ownerCSRF);w.Code!=201{t.Fatalf("representative init: %+v",w)}
  if w:=send("POST","/api/v1/workspace/representatives/"+rep+"/credits",business.CreditChange{
   EventID:"grant-client-ops-"+rep,RepresentativeID:rep,UnlimitedDeltaUnits:100,
   VolumeDeltaBytes:1000,Reason:"Private owner credit",
  },ownerCookie,ownerCSRF);w.Code!=200{t.Fatalf("fund rep: %+v",w)}
 }
 for _,rep:=range []struct{user,pwd string}{
  {"rep-first","private-first-rep-password"},
  {"rep-other","private-other-rep-password"},
 }{
  if w:=send("POST","/api/v1/workspace/representatives/"+rep.user+"/login",
   map[string]string{"username":rep.user,"password":rep.pwd},ownerCookie,ownerCSRF);w.Code!=201{t.Fatalf("login setup: %+v",w)}
 }
 repCookie,repCSRF:=browserLogin(t,s,"rep-first","private-first-rep-password")
 otherCookie,otherCSRF:=browserLogin(t,s,"rep-other","private-other-rep-password")
 c:=business.NewClient{EventID:"create-lifecycle-client-01",ClientID:"client-first",
  RepresentativeID:"rep-first",Name:"Pending unlimited",DurationDays:30,IPCount:5,Unlimited:true}
 if w:=send("POST","/api/v1/workspace/clients",c,repCookie,repCSRF);w.Code!=201{t.Fatalf("create: %+v",w)}
 if w:=send("GET","/api/v1/workspace/clients/client-first",nil,otherCookie,"");w.Code!=403{
  t.Fatalf("other representative read client: %+v",w)
 }
 extension:=business.ExtendClient{EventID:"extend-client-event-01",ClientID:"client-first",AdditionalDays:30,Reason:"Paid renewal"}
 url:="/api/v1/workspace/clients/client-first/extend"
 if w:=send("POST",url,extension,repCookie,"");w.Code!=403{t.Fatalf("missing CSRF charged client: %+v",w)}
 if w:=send("POST",url,extension,otherCookie,otherCSRF);w.Code!=403{
  t.Fatalf("other rep extended unauthorized client: %+v",w)
 }
 if w:=send("POST",url,business.ExtendClient{
  EventID:extension.EventID,ClientID:"client-other",AdditionalDays:30,Reason:"Invalid",
 },repCookie,repCSRF);w.Code!=422{t.Fatalf("path vs body mismatch: %+v",w)}
 valid:=send("POST",url,extension,repCookie,repCSRF)
 if valid.Code!=200||!strings.Contains(valid.Body,`"subscription_changed":false`)||
  !strings.Contains(valid.Body,`"charged_unlimited_units":10`){
  t.Fatalf("renewal did not preserve nonactivation: %+v",valid)
 }
 if w:=send("POST",url,extension,repCookie,repCSRF);w.Code!=200{t.Fatalf("idempotent retry rejected: %+v",w)}
 me:=send("GET","/api/v1/workspace/me",nil,repCookie,"")
 if me.Code!=200||!strings.Contains(me.Body,`"unlimited_credit_units":90`){
  t.Fatalf("renewal double debited or refunded: %+v",me)
 }
 ops:=send("GET","/api/v1/workspace/client-operations",nil,repCookie,"")
 if ops.Code!=200||strings.Count(ops.Body,`"event_id"`)!=1{
  t.Fatalf("replayed renewal produced multiple audit events: %+v",ops)
 }
 if w:=send("GET","/api/v1/workspace/client-operations?representative_id=rep-first",nil,otherCookie,"");w.Code!=403{
  t.Fatalf("representative saw another rep's operations: %+v",w)
 }
 state:=business.SetClientState{EventID:"archive-client-event-1",ClientID:"client-first",Action:"archive",Reason:"End of service"}
 archived:=send("POST","/api/v1/workspace/clients/client-first/state",state,repCookie,repCSRF)
 if archived.Code!=200||!strings.Contains(archived.Body,`"archived":true`)||
  !strings.Contains(archived.Body,`"billing_refunded":false`)||
  !strings.Contains(archived.Body,`"applied_to_xray":false`){
  t.Fatalf("soft archive incorrectly touched billing/core: %+v",archived)
 }
 if w:=send("POST","/api/v1/workspace/clients/client-first/state",state,repCookie,repCSRF);w.Code!=200{
  t.Fatalf("archival idempotency failed: %+v",w)
 }
 if w:=send("POST",url,business.ExtendClient{
  EventID:"renew-archived-event-1",ClientID:"client-first",AdditionalDays:10,Reason:"Invalid",
 },repCookie,repCSRF);w.Code!=409{t.Fatalf("archived record was renewed: %+v",w)}
 me=send("GET","/api/v1/workspace/me",nil,repCookie,"")
 if me.Code!=200||!strings.Contains(me.Body,`"unlimited_credit_units":90`){
  t.Fatalf("archive refunded credit: %+v",me)
 }
 // Ensure all sensitive routes remain owner-browser-only and not Agent TLS.
 for _,path:=range []string{"/api/v1/workspace/clients/client-first","/api/v1/workspace/client-operations"}{
  w:=browserRequest(t,s.AgentHandler(),"GET",path,nil,repCookie,"","")
  if w.Code==200{t.Fatalf("Agent handler exposed client finance route: %s",path)}
 }
 var body map[string]any
 if err:=json.Unmarshal([]byte(archived.Body),&body);err!=nil{t.Fatal(err)}
}
