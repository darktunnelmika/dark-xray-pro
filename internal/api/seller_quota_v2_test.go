package api

import (
 "context"
 "encoding/json"
 "net/http"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestSellerQuotaV2APIDisabledByDefault(t *testing.T){
 s:=enabledOwner(t)
 owner,csrf:=browserLogin(t,s,ownerUsername,ownerPassword)
 path:="/api/v1/workspace/representatives/rep-a/sellers"
 if r:=browserRequest(t,s.Handler(),"GET",path,nil,owner,"","");r.Code!=404{
  t.Fatalf("seller API was public before private opt-in: %d",r.Code)
 }
 if r:=browserRequest(t,s.Handler(),"POST",path,map[string]any{"id":"bad"},owner,csrf,"https://"+browserHost);r.Code!=404{
  t.Fatalf("seller mutation was mounted without gate: %d",r.Code)
 }
 if err:=s.EnableSellerQuotaV2Lab(context.Background());err!=nil{t.Fatal(err)}
 if err:=s.EnableSellerQuotaV2Lab(context.Background());err==nil{t.Fatal("repeated private migration gate accepted")}
}

func TestSellerQuotaV2OwnerOnlyLedgerBackedRoutes(t *testing.T){
 s:=enabledOwner(t)
 ctx:=context.Background()
 if _,err:=s.business.CreateRepresentative(ctx,"repSellerOne","First seller owner");err!=nil{t.Fatal(err)}
 if _,err:=s.business.CreateRepresentative(ctx,"repSellerTwo","Second seller owner");err!=nil{t.Fatal(err)}
 if _,err:=s.business.AdjustCredits(ctx,business.CreditChange{
  EventID:"seller-owner-credit-grant-00001",RepresentativeID:"repSellerOne",
  VolumeDeltaBytes:2000,UnlimitedDeltaUnits:100,Reason:"Explicit owner test grant",
 });err!=nil{t.Fatal(err)}
 if _,err:=s.business.ProvisionRepresentativeLogin(ctx,"repSellerOne","seller-rep-qa","seller-rep-qa-password-123");err!=nil{t.Fatal(err)}
 if err:=s.EnableSellerQuotaV2Lab(ctx);err!=nil{t.Fatal(err)}
 owner,csrf:=browserLogin(t,s,ownerUsername,ownerPassword)
 representative,repCSRF:=browserLogin(t,s,"seller-rep-qa","seller-rep-qa-password-123")
 base:="/api/v1/workspace/representatives/repSellerOne/sellers"
 send:=func(method,path string,body any,cookie *http.Cookie,token,origin string)*httpResponse{
  w:=browserRequest(t,s.Handler(),method,path,body,cookie,token,origin)
  return &httpResponse{Code:w.Code,Body:w.Body.String()}
 }
 input:=business.CreateSellerV2{EventID:"seller-api-create-00000001",RepresentativeID:"repSellerOne",
  SellerID:"sellerJohn",Name:"John sales",TelegramUserID:123456789}
 if r:=send("GET",base,nil,nil,"","");r.Code!=401{t.Fatal("anonymous list allowed",r)}
 if r:=send("GET",base,nil,representative,"","");r.Code!=403{t.Fatal("representative manage permission not reviewed",r)}
 if r:=send("POST",base,input,representative,repCSRF,"https://"+browserHost);r.Code!=403{t.Fatal("representative mint seller",r)}
 if r:=send("POST",base,input,owner,csrf,"https://evil.example");r.Code!=403{t.Fatal("foreign Origin accepted",r)}
 if r:=send("POST",base,input,owner,"","https://"+browserHost);r.Code!=403{t.Fatal("missing CSRF accepted",r)}
 if r:=send("POST",base,input,owner,csrf,"https://"+browserHost);r.Code!=201{t.Fatal("owner create failed",r)}
 if r:=send("POST",base,input,owner,csrf,"https://"+browserHost);r.Code!=201{t.Fatal("exact create replay failed",r)}
 if r:=send("GET",base+"?limit=20&offset=0",nil,owner,"","");r.Code!=200||
   !strings.Contains(r.Body,`"sellerJohn"`) {t.Fatal("owner list failed",r)}
 if r:=send("GET",base+"/sellerJohn",nil,owner,"","");r.Code!=200{
  t.Fatal("owner detail failed",r)
 }
 for _,path:=range []string{base+"?unknown=1",base+"?limit=101",base+"?limit=5&limit=6"}{
  if r:=send("GET",path,nil,owner,"","");r.Code!=422{t.Fatal("unsafe query accepted",path,r)}
 }
 if r:=send("POST","/api/v1/workspace/representatives/repSellerTwo/sellers",input,owner,csrf,
   "https://"+browserHost);r.Code!=422{t.Fatal("body/URL tenant mismatch",r)}
 if r:=send("GET","/api/v1/workspace/representatives/repSellerTwo/sellers/sellerJohn",nil,owner,"","");r.Code!=404{
  t.Fatal("seller leaked across tenant",r)
 }
 transfer:=map[string]any{
  "event_id":"seller-api-quota-alloc-0001","representative_id":"repSellerOne",
  "seller_id":"sellerJohn","action":"allocate","bytes":"600","units":"20","reason":"Owner reviewed reserve",
 }
 quota:=base+"/sellerJohn/quota"
 r:=send("POST",quota,transfer,owner,csrf,"https://"+browserHost)
 if r.Code!=200{t.Fatal("reserve failed",r)}
 var receipt struct{
  FreeUnits string `json:"representative_free_units"`
  SellerUnits string `json:"seller_reserved_units"`
  Replayed bool `json:"replayed"`
 }
 if err:=json.Unmarshal([]byte(r.Body),&receipt);err!=nil||receipt.FreeUnits!="80"||receipt.SellerUnits!="20"||receipt.Replayed{
  t.Fatal("receipt wrong",r,err)
 }
 r=send("POST",quota,transfer,owner,csrf,"https://"+browserHost)
 if r.Code!=200||!strings.Contains(r.Body,`"replayed":true`){t.Fatal("idempotent retry failed",r)}
 bad:=map[string]any{}
 for k,v:=range transfer{bad[k]=v}
 bad["units"]="21"
 if r=send("POST",quota,bad,owner,csrf,"https://"+browserHost);r.Code!=409{t.Fatal("event reuse changed quota",r)}
 bad["event_id"]="seller-api-invalid-amount-0001"
 bad["units"]="900719925474099300000000"
 if r=send("POST",quota,bad,owner,csrf,"https://"+browserHost);r.Code!=422{t.Fatal("overflow accepted",r)}
 bad["units"]="1.2"
 if r=send("POST",quota,bad,owner,csrf,"https://"+browserHost);r.Code!=422{t.Fatal("fractional credit accepted",r)}
 bad["units"]=10
 if r=send("POST",quota,bad,owner,csrf,"https://"+browserHost);r.Code!=400{t.Fatal("unsafe number did not require string representation",r)}
 bad["units"]="01"
 if r=send("POST",quota,bad,owner,csrf,"https://"+browserHost);r.Code!=422{t.Fatal("noncanonical decimal accepted",r)}
 if r=send("POST",quota,transfer,representative,repCSRF,"https://"+browserHost);r.Code!=403{t.Fatal("rep quota allocation without permission",r)}
 after,err:=s.business.Representative(ctx,"repSellerOne")
 seller,err2:=s.business.SellerV2(ctx,"repSellerOne","sellerJohn")
 if err!=nil||err2!=nil||after.UnlimitedCreditUnits!=80||seller.ReservedUnits!=20||
    after.VolumeCreditBytes!=1400||seller.ReservedBytes!=600 {t.Fatal("bad authoritative state",after,seller,err,err2)}
 if w:=browserRequest(t,s.AgentHandler(),"GET",base,nil,owner,"","");w.Code==200{t.Fatal("seller management exposed on Agent listener")}
}
