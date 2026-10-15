package api

import (
 "bytes"
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)
func TestInboundLifecycleOwnerCSRFRevisionAndSafeArchive(t *testing.T){
 s:=enabledOwner(t);key:=bytes.Repeat([]byte{7},32)
 if err:=s.EnableNativeInbounds(key);err!=nil{t.Fatal(err)}
 ctx:=context.Background();profile:=business.InboundInput{ID:701,Name:"Lifecycle",Transport:"tcp",RealityTarget:"example.org:443",ServerNames:[]string{"example.org"},ShortIDs:[]string{"ab"}}
 in,err:=s.business.CreateInbound(ctx,key,profile);if err!=nil{t.Fatal(err)}
 cookie,csrf:=browserLogin(t,s,ownerUsername,ownerPassword)
 path:="/api/v1/workspace/native-inbounds/701/lifecycle"
 mutation:=business.InboundMutation{EventID:"owner-inbound-edit-001",ExpectedRevision:1,Action:"edit",Reason:"Verified owner edit",Profile:&profile};profile.Name="Updated"
 noSession:=browserRequest(t,s.Handler(),"POST",path,mutation,nil,csrf,"https://"+browserHost)
 if noSession.Code!=401{t.Fatal("anonymous edit accepted",noSession.Code)}
 noCSRF:=browserRequest(t,s.Handler(),"POST",path,mutation,cookie,"","https://"+browserHost)
 if noCSRF.Code!=403{t.Fatal("missing CSRF accepted",noCSRF.Code)}
 cross:=browserRequest(t,s.Handler(),"POST",path,mutation,cookie,csrf,"https://other.example")
 if cross.Code!=403{t.Fatal("cross-origin edit accepted",cross.Code)}
 if _,err=s.business.CreateRepresentative(ctx,"rep-inbound-life","Representative");err!=nil{t.Fatal(err)}
 if _,err=s.business.ProvisionRepresentativeLogin(ctx,"rep-inbound-life","rep-inbound-life","private-test-login-password");err!=nil{t.Fatal(err)}
 rc,rt:=browserLogin(t,s,"rep-inbound-life","private-test-login-password")
 denied:=browserRequest(t,s.Handler(),"POST",path,mutation,rc,rt,"https://"+browserHost)
 if denied.Code!=403{t.Fatal("representative edited owner inbound",denied.Code)}
 accepted:=browserRequest(t,s.Handler(),"POST",path,mutation,cookie,csrf,"https://"+browserHost)
 if accepted.Code!=200{t.Fatalf("edit failed %d %s",accepted.Code,accepted.Body.String())}
 var response struct{Inbound business.NativeInbound `json:"inbound"`;Deleted bool `json:"clients_deleted"`;Ledger bool `json:"ledger_modified"`}
 if err=json.Unmarshal(accepted.Body.Bytes(),&response);err!=nil{t.Fatal(err)}
 if response.Inbound.PublicKey!=in.PublicKey||response.Inbound.Revision!=2||response.Deleted||response.Ledger{t.Fatal("edit broke preservation contract")}
 if strings.Contains(accepted.Body.String(),"private_key")||strings.Contains(accepted.Body.String(),"privateKey"){t.Fatal("profile exposed private key")}
 if w:=browserRequest(t,s.Handler(),"POST",path,mutation,cookie,csrf,"https://"+browserHost);w.Code!=200{t.Fatal("exact retry refused",w.Code)}
 stale:=business.InboundMutation{EventID:"stale-owner-inbound01",ExpectedRevision:1,Action:"archive",Reason:"Stale delete"}
 if w:=browserRequest(t,s.Handler(),"POST",path,stale,cookie,csrf,"https://"+browserHost);w.Code!=409{t.Fatal("stale delete accepted",w.Code)}
 stale.ExpectedRevision=2;stale.EventID="archive-owner-inbound1"
 if w:=browserRequest(t,s.Handler(),"POST",path,stale,cookie,csrf,"https://"+browserHost);w.Code!=200{t.Fatal("archive failed",w.Code)}
 ops:=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/native-inbounds/701/operations?limit=20",nil,cookie,"","")
 if ops.Code!=200||strings.Count(ops.Body.String(),`"event_id"`)!=2{t.Fatalf("missing or duplicate audit %d %s",ops.Code,ops.Body.String())}
 denied=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/native-inbounds/701/operations",nil,rc,"","")
 if denied.Code!=403{t.Fatal("representative read inbound audit",denied.Code)}
 if w:=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/native-inbounds/701/operations?limit=201",nil,cookie,"","");w.Code!=422{t.Fatal("unbounded inbound history",w.Code)}
 if w:=browserRequest(t,s.AgentHandler(),"POST",path,mutation,cookie,csrf,"https://"+browserHost);w.Code==200{t.Fatal("Agent plane exposed owner lifecycle")}
}
