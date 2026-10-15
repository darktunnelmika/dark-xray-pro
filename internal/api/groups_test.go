package api

import (
 "encoding/json"
 "net/http"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestGroupBrowserScopeAndDeletionNeverDestroysClientsOrCredits(t *testing.T){
 s:=enabledOwner(t);h:=s.Handler()
 ownerCookie,ownerCSRF:=browserLogin(t,s,ownerUsername,ownerPassword)
 req:=func(method,path string,body any,c *http.Cookie,csrf string)*httpResponse{
  w:=browserRequest(t,h,method,path,body,c,csrf,"https://"+browserHost)
  return &httpResponse{Code:w.Code,Body:w.Body.String()}
 }
 for _,rep:=range []string{"rep-alpha","rep-beta"}{
  w:=req("POST","/api/v1/workspace/representatives",
   map[string]string{"id":rep,"name":"Private "+rep},ownerCookie,ownerCSRF)
  if w.Code!=201{t.Fatalf("rep init failed: %+v",w)}
  w=req("POST","/api/v1/workspace/representatives/"+rep+"/credits",business.CreditChange{
   EventID:"grant-groups-"+rep+"-0001",RepresentativeID:rep,
   UnlimitedDeltaUnits:100,VolumeDeltaBytes:10000,Reason:"Private group acceptance",
  },ownerCookie,ownerCSRF)
  if w.Code!=200{t.Fatalf("rep credit failed: %+v",w)}
 }
 for _,value:=range []struct{rep,pass string}{
  {"rep-alpha","private-alpha-login-2026"},
  {"rep-beta","private-beta-login-2026"},
 }{
  w:=req("POST","/api/v1/workspace/representatives/"+value.rep+"/login",
   map[string]string{"username":value.rep,"password":value.pass},ownerCookie,ownerCSRF)
  if w.Code!=201{t.Fatalf("rep setup failed: %+v",w)}
 }
 repCookie,repCSRF:=browserLogin(t,s,"rep-alpha","private-alpha-login-2026")
 ownerClient:=func(rep,id string){
  t.Helper()
  w:=req("POST","/api/v1/workspace/clients",business.NewClient{
   EventID:"create-group-api-"+id,ClientID:id,RepresentativeID:rep,
   Name:"Customer "+id,VolumeBytes:100,IPCount:0,DurationDays:30,
  },ownerCookie,ownerCSRF)
  if w.Code!=201{t.Fatalf("client setup failed: %+v",w)}
 }
 ownerClient("rep-alpha","alpha-client")
 ownerClient("rep-beta","beta-client")
 group:=business.NewGroup{ID:"alpha-private",RepresentativeID:"rep-alpha",Name:"VIP"}
 created:=req("POST","/api/v1/workspace/groups",group,repCookie,repCSRF)
 if created.Code!=201{t.Fatalf("representative cannot create own group: %+v",created)}
 if w:=req("POST","/api/v1/workspace/groups",business.NewGroup{
  ID:"cross-group",RepresentativeID:"rep-beta",Name:"Unauthorized",
 },repCookie,repCSRF);w.Code!=403{t.Fatalf("rep created another tenant's group: %+v",w)}
 if w:=req("GET","/api/v1/workspace/groups?representative_id=rep-beta",nil,repCookie,"");w.Code!=403{
  t.Fatalf("rep saw other tenant's groups: %+v",w)
 }
 if w:=req("POST","/api/v1/workspace/groups/alpha-private/members",
  business.AssignGroupMembers{ClientIDs:[]string{"alpha-client","beta-client"}},repCookie,repCSRF);w.Code!=409{
  t.Fatalf("cross-tenant group assignment was not rejected: %+v",w)
 }
 if w:=req("GET","/api/v1/workspace/groups/alpha-private/members",nil,repCookie,"");w.Code!=200||!strings.Contains(w.Body,`"client_ids":[]`){
  t.Fatalf("failed batch left partial member: %+v",w)
 }
 if w:=req("POST","/api/v1/workspace/groups/alpha-private/members",
  business.AssignGroupMembers{ClientIDs:[]string{"alpha-client"}},repCookie,repCSRF);w.Code!=200{
  t.Fatalf("valid group assign failed: %+v",w)
 }
 listed:=req("GET","/api/v1/workspace/groups?limit=20",nil,repCookie,"")
 if listed.Code!=200||!strings.Contains(listed.Body,`"member_count":1`){
  t.Fatalf("member count not returned: %+v",listed)
 }
 renamed:=req("PATCH","/api/v1/workspace/groups/alpha-private",
  map[string]string{"name":"New Persian Group","color":"#123ABC"},repCookie,repCSRF)
 if renamed.Code!=200||!strings.Contains(renamed.Body,`"New Persian Group"`){
  t.Fatalf("group edit failed: %+v",renamed)
 }
 deleted:=req("DELETE","/api/v1/workspace/groups/alpha-private",map[string]any{},repCookie,repCSRF)
 if deleted.Code!=200{t.Fatalf("group delete failed: %+v",deleted)}
 var response struct{
  GroupDeleted bool `json:"group_deleted"`
  ClientsDeleted bool `json:"clients_deleted"`
  LedgerModified bool `json:"ledger_modified"`
  DetachedMemberCount int64 `json:"detached_member_count"`
 }
 if err:=json.Unmarshal([]byte(deleted.Body),&response);err!=nil{t.Fatal(err)}
 if !response.GroupDeleted||response.ClientsDeleted||response.LedgerModified||response.DetachedMemberCount!=1{
  t.Fatalf("group deletion violated history guarantee: %+v",response)
 }
 clients:=req("GET","/api/v1/workspace/clients?representative_id=rep-alpha",nil,repCookie,"")
 if clients.Code!=200||!strings.Contains(clients.Body,"alpha-client"){t.Fatalf("group delete lost client: %+v",clients)}
 balance:=req("GET","/api/v1/workspace/me",nil,repCookie,"")
 if balance.Code!=200||!strings.Contains(balance.Body,`"volume_credit_bytes":9900`){
  t.Fatalf("group delete refunded consumed quota: %+v",balance)
 }
 events:=req("GET","/api/v1/workspace/events",nil,repCookie,"")
 if events.Code!=200||strings.Count(events.Body,`"event_id"`)!=2{
  t.Fatalf("group delete destroyed event history: %+v",events)
 }
}
type httpResponse struct{Code int;Body string}

func TestGroupMutationRequiresSameOriginCSRF(t *testing.T){
 s:=enabledOwner(t)
 c,token:=browserLogin(t,s,ownerUsername,ownerPassword)
 group:=business.NewGroup{ID:"secure-group",RepresentativeID:"owner",Name:"Secure"}
 for _,tt:=range []struct{origin,csrf string;expect int}{
  {"",token,403},
  {"https://evil.example",token,403},
  {"https://"+browserHost,"bad",403},
  {"https://"+browserHost,token,201},
 }{
  w:=browserRequest(t,s.Handler(),"POST","/api/v1/workspace/groups",group,c,tt.csrf,tt.origin)
  if w.Code!=tt.expect{t.Fatalf("group browser mutation %s: %d %s",tt.origin,w.Code,w.Body.String())}
 }
 if w:=browserRequest(t,s.Handler(),"POST","/api/v1/workspace/groups",group,nil,token,"https://"+browserHost);w.Code!=401{
  t.Fatalf("anonymous group modified: %d",w.Code)
 }
 if w:=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/groups?limit=201",nil,c,"","");w.Code!=422{
  t.Fatalf("unbounded groups list accepted: %d",w.Code)
 }
}
