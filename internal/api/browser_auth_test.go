package api

import (
 "bytes"
 "context"
 "crypto/tls"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

const (
 browserHost="pro.example"
 ownerUsername="proowner"
 ownerPassword="unique-owner-password-for-lab"
 repPassword="unique-rep-password-for-lab"
)
func browserRequest(t *testing.T,h http.Handler,method,path string,body any,cookie *http.Cookie,csrf,origin string)*httptest.ResponseRecorder{
 t.Helper()
 var raw []byte
 if body!=nil{
  var err error;raw,err=json.Marshal(body);if err!=nil{t.Fatal(err)}
 }
 req:=httptest.NewRequest(method,"https://"+browserHost+path,bytes.NewReader(raw))
 if body!=nil{req.Header.Set("Content-Type","application/json")}
 if origin!=""{req.Header.Set("Origin",origin)}
 if csrf!=""{req.Header.Set("X-CSRF-Token",csrf)}
 if cookie!=nil{req.AddCookie(cookie)}
 w:=httptest.NewRecorder();h.ServeHTTP(w,req)
 return w
}
func enabledOwner(t *testing.T)*Server{
 t.Helper()
 s:=businessLab(t)
 if _,err:=s.business.BootstrapOwner(context.Background(),ownerUsername,ownerPassword);err!=nil{t.Fatal(err)}
 return s
}
func browserLogin(t *testing.T,s *Server,username,password string)(*http.Cookie,string){
 t.Helper()
 w:=browserRequest(t,s.Handler(),"POST","/api/v1/session/login",
  map[string]string{"username":username,"password":password},nil,"","https://"+browserHost)
 if w.Code!=200{t.Fatalf("login failed (%s): %d %s",username,w.Code,w.Body.String())}
 cookies:=w.Result().Cookies()
 if len(cookies)!=1{t.Fatal("missing secure auth cookie")}
 c:=cookies[0]
 if c.Name!=sessionCookieName||!c.HttpOnly||!c.Secure||c.SameSite!=http.SameSiteStrictMode||
   c.Path!="/"||c.Value==""{
  t.Fatalf("insecure session cookie: %+v",c)
 }
 var payload struct{ CSRF string `json:"csrf_token"`; User business.AuthUser `json:"user"` }
 if err:=json.Unmarshal(w.Body.Bytes(),&payload);err!=nil{t.Fatal(err)}
 if payload.CSRF==""||len(payload.CSRF)!=64||payload.User.Username!=username{
  t.Fatalf("invalid browser login state: %s",w.Body.String())
 }
 if strings.Contains(w.Body.String(),c.Value)||strings.Contains(w.Body.String(),password){
  t.Fatal("browser API leaked session cookie or password")
 }
 return c,payload.CSRF
}
func TestBrowserSessionCookieOriginCSRFAndLogout(t *testing.T){
 s:=enabledOwner(t);h:=s.Handler()
 anon:=browserRequest(t,h,"GET","/api/v1/session/status",nil,nil,"","")
 if anon.Code!=200||!strings.Contains(anon.Body.String(),`"authenticated":false`){t.Fatalf("anonymous session: %d %s",anon.Code,anon.Body.String())}
 failed:=browserRequest(t,h,"POST","/api/v1/session/login",
  map[string]string{"username":ownerUsername,"password":"wrong"},nil,"","https://"+browserHost)
 if failed.Code!=401||len(failed.Result().Cookies())!=0{t.Fatalf("bad login accepted: %d",failed.Code)}
 badOrigin:=browserRequest(t,h,"POST","/api/v1/session/login",
  map[string]string{"username":ownerUsername,"password":ownerPassword},nil,"","https://evil.example")
 if badOrigin.Code!=403{t.Fatalf("cross-origin password POST accepted: %d",badOrigin.Code)}
 c,csrf:=browserLogin(t,s,ownerUsername,ownerPassword)
 for _,item:=range []struct{origin,token string;want int}{
  {"",csrf,403},
  {"https://evil.example",csrf,403},
  {"https://"+browserHost,"invalid",403},
  {"https://"+browserHost,"",403},
  {"https://"+browserHost,csrf,200},
 }{
  w:=browserRequest(t,h,"POST","/api/v1/session/logout",map[string]any{},c,item.token,item.origin)
  if w.Code!=item.want{t.Fatalf("logout scope check %+v returned %d %s",item,w.Code,w.Body.String())}
 }
 if w:=browserRequest(t,h,"GET","/api/v1/session/status",nil,c,"","");!strings.Contains(w.Body.String(),`"authenticated":false`){
  t.Fatalf("session remained active after logout: %s",w.Body.String())
 }
}
func TestRepresentativeWorkspaceScopeAndAtomicCredit(t *testing.T){
 s:=enabledOwner(t);h:=s.Handler()
 ownerCookie,ownerCSRF:=browserLogin(t,s,ownerUsername,ownerPassword)
 req:=func(method,path string,body any,c *http.Cookie,csrf string)*httptest.ResponseRecorder{
  return browserRequest(t,h,method,path,body,c,csrf,"https://"+browserHost)
 }
 for _,id:=range []string{"rep-a","rep-b"}{
  if w:=req("POST","/api/v1/workspace/representatives",
   map[string]string{"id":id,"name":"Representative "+id},ownerCookie,ownerCSRF);w.Code!=201{
   t.Fatalf("failed representative creation: %d %s",w.Code,w.Body.String())
  }
 }
 if w:=req("POST","/api/v1/workspace/representatives/rep-a/credits",business.CreditChange{
  EventID:"test-credit-session-0001",RepresentativeID:"rep-a",VolumeDeltaBytes:2000,
  UnlimitedDeltaUnits:100,Reason:"Private owner grant",
 },ownerCookie,ownerCSRF);w.Code!=200{t.Fatalf("owner credit failed: %d %s",w.Code,w.Body.String())}
 for _,id:=range []string{"rep-a","rep-b"}{
  pass:=repPassword
  if id=="rep-b"{pass="another-long-unique-rep-password"}
  if w:=req("POST","/api/v1/workspace/representatives/"+id+"/login",
   map[string]string{"username":id,"password":pass},ownerCookie,ownerCSRF);w.Code!=201{
   t.Fatalf("owner cannot provision representative login: %d %s",w.Code,w.Body.String())
  }
 }
 repCookie,repCSRF:=browserLogin(t,s,"rep-a",repPassword)
 if w:=req("GET","/api/v1/workspace/representatives",nil,repCookie,"");w.Code!=403{
  t.Fatalf("rep can view all representative accounts: %d",w.Code)
 }
 if w:=req("POST","/api/v1/workspace/representatives/rep-b/credits",business.CreditChange{
  EventID:"fake-credit-change-0001",RepresentativeID:"rep-b",VolumeDeltaBytes:500,Reason:"spoofed",
 },repCookie,repCSRF);w.Code!=403{t.Fatalf("rep minted another rep's credit: %d",w.Code)}
 if w:=req("GET","/api/v1/workspace/clients?representative_id=rep-b",nil,repCookie,"");w.Code!=403{
  t.Fatalf("rep read another rep's clients: %d",w.Code)
 }
 if w:=req("GET","/api/v1/workspace/events?representative_id=rep-b",nil,repCookie,"");w.Code!=403{
  t.Fatalf("rep read another rep's events: %d",w.Code)
 }
 x:=business.NewClient{EventID:"new-pro-rep-a-client001",ClientID:"first-pro-rep-client",RepresentativeID:"rep-b",
  Name:"Cross-tenant blocked",DurationDays:60,IPCount:5,Unlimited:true}
 if w:=req("POST","/api/v1/workspace/clients",x,repCookie,repCSRF);w.Code!=403{
  t.Fatalf("rep wrote another rep's client: %d",w.Code)
 }
 x.RepresentativeID="rep-a"
 if w:=req("POST","/api/v1/workspace/clients",x,repCookie,"");w.Code!=403{
  t.Fatalf("missing CSRF was accepted for client billing: %d",w.Code)
 }
 w:=req("POST","/api/v1/workspace/clients",x,repCookie,repCSRF)
 if w.Code!=201{t.Fatalf("rep client creation failed: %d %s",w.Code,w.Body.String())}
 if !strings.Contains(w.Body.String(),`"pending_node_apply"`){t.Fatal("Xray deployment falsely declared ready")}
 w=req("POST","/api/v1/workspace/clients",x,repCookie,repCSRF)
 if w.Code!=201{t.Fatalf("idempotent retry rejected: %d %s",w.Code,w.Body.String())}
 me:=req("GET","/api/v1/workspace/me",nil,repCookie,"")
 if me.Code!=200||!strings.Contains(me.Body.String(),`"unlimited_credit_units":90`){
  t.Fatalf("credit did not deduct exactly 10 units: %d %s",me.Code,me.Body.String())
 }
 list:=req("GET","/api/v1/workspace/clients",nil,repCookie,"")
 if list.Code!=200||!strings.Contains(list.Body.String(),`"first-pro-rep-client"`){
  t.Fatalf("representative list empty: %d %s",list.Code,list.Body.String())
 }
 audit:=req("GET","/api/v1/workspace/events",nil,repCookie,"")
 if audit.Code!=200||strings.Count(audit.Body.String(),`"event_id"`)!=2{
  t.Fatalf("client repeat double charged: %d %s",audit.Code,audit.Body.String())
 }
 if w:=req("GET","/api/v1/workspace/me",nil,ownerCookie,"");w.Code!=200{
  t.Fatalf("owner lost own role: %d",w.Code)
 }
}
func TestBrowserAuthOnlyOnOwnerListenerAndOptIn(t *testing.T){
 s,_,_,_:=newLab(t)
 for _,route:=range []string{"/api/v1/session/status","/api/v1/workspace/me"}{
  if w:=browserRequest(t,s.Handler(),"GET",route,nil,nil,"","");w.Code!=404{
   t.Fatalf("feature exposed without business DB: %s returned %d",route,w.Code)
  }
 }
 s=enabledOwner(t)
 c,_:=browserLogin(t,s,ownerUsername,ownerPassword)
 for _,route:=range []string{"/api/v1/session/status","/api/v1/workspace/me","/api/v1/workspace/representatives"}{
  req:=httptest.NewRequest("GET","https://"+browserHost+route,nil)
  req.AddCookie(c)
  req.TLS=&tls.ConnectionState{Version:tls.VersionTLS13}
  w:=httptest.NewRecorder()
  s.AgentHandler().ServeHTTP(w,req)
  if w.Code!=404{t.Fatalf("Agent TLS listener exposed browser route %s: %d",route,w.Code)}
 }
 // Owner Bearer must never grant cookie-authenticated workspace access.
 bearer:=httptest.NewRequest("GET","https://"+browserHost+"/api/v1/workspace/me",nil)
 bearer.Header.Set("Authorization","Bearer "+ownerToken)
 w:=httptest.NewRecorder();s.Handler().ServeHTTP(w,bearer)
 if w.Code!=401{t.Fatalf("legacy owner Bearer impersonated browser: %d",w.Code)}
}
func TestBrowserPasswordRotationRevokesCookie(t *testing.T){
 s:=enabledOwner(t)
 cookie,csrf:=browserLogin(t,s,ownerUsername,ownerPassword)
 resp:=browserRequest(t,s.Handler(),"POST","/api/v1/session/change-password",
  map[string]string{"old_password":ownerPassword,"new_password":"replacement-owner-password-2026"},
  cookie,csrf,"https://"+browserHost)
 if resp.Code!=200{t.Fatalf("password rotation: %d %s",resp.Code,resp.Body.String())}
 status:=browserRequest(t,s.Handler(),"GET","/api/v1/session/status",nil,cookie,"","")
 if !strings.Contains(status.Body.String(),`"authenticated":false`){t.Fatal("old cookie survived change password")}
 browserLogin(t,s,ownerUsername,"replacement-owner-password-2026")
}
