package telegramreport

import (
 "strings"
 "testing"
)
const verified="authoritative_representative_free_credit_not_total_sales_or_live_traffic"
func TestOwnerReportEscapesHTMLWithoutFakeReservedCredit(t *testing.T){
 c:=OwnerRepresentativeCard{
  RepresentativeID:"repQA",DisplayName:"<b>Alice & Bob</b>",Enabled:true,
  FreeVolumeBytes:"1073741824",FreeUnlimitedUnits:"200",
  SellerQuotaState:"not_enabled",BalanceSemantics:verified,
 }
 result,err:=FormatOwnerRepresentativeFA(c)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(result,"&lt;b&gt;Alice &amp; Bob&lt;/b&gt;")||
 strings.Contains(result,"<b>Alice & Bob</b>")||
 !strings.Contains(result,"هنوز فعال نشده")||
 strings.Contains(result,"فروشندگان: 0"){
  t.Fatal("unescaped owner name or fake escrow claim",result)
 }
 if !strings.Contains(result,"1.00 GiB")||!strings.Contains(result,"200 واحد"){
  t.Fatal("credit formatting wrong",result)
 }
}
func TestOwnerReportTwoDimensionReservedAccounting(t *testing.T){
 c:=OwnerRepresentativeCard{RepresentativeID:"repA",DisplayName:"مهدی",Enabled:false,
 FreeVolumeBytes:"536870912",FreeUnlimitedUnits:"80",SellerQuotaState:"enabled",
 SellerCount:2,ReservedVolumeBytes:"2147483648",ReservedUnlimitedUnits:"20",
 BalanceSemantics:verified}
 result,err:=FormatOwnerRepresentativeFA(c)
 if err!=nil{t.Fatal(err)}
 for _,field:=range []string{"0.50 GiB","2.00 GiB","80 واحد","20 واحد","فروشندگان: 2","🔴"}{
  if !strings.Contains(result,field){t.Fatal("missing required report field",field,result)}
 }
}
func TestOwnerReportFailsClosedForUnverifiedMetrics(t *testing.T){
 good:=OwnerRepresentativeCard{RepresentativeID:"repA",DisplayName:"Valid",
  FreeVolumeBytes:"0",FreeUnlimitedUnits:"10",SellerQuotaState:"not_enabled",BalanceSemantics:verified}
 bad:=[]OwnerRepresentativeCard{}
 zero:=good;zero.SellerQuotaState="corrupt";bad=append(bad,zero)
 zero=good;zero.FreeUnlimitedUnits="-1";bad=append(bad,zero)
 zero=good;zero.FreeVolumeBytes="01";bad=append(bad,zero)
 zero=good;zero.BalanceSemantics="online_traffic";bad=append(bad,zero)
 zero=good;zero.SellerCount=2;bad=append(bad,zero)
 zero=good;zero.SellerQuotaState="enabled";zero.ReservedVolumeBytes="";bad=append(bad,zero)
 for i,item:=range bad{if _,err:=FormatOwnerRepresentativeFA(item);err==nil{t.Fatalf("invalid report %d accepted",i)}}
}
