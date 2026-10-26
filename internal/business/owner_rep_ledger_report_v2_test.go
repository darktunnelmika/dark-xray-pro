package business

import (
 "context"
 "errors"
 "testing"
)

func TestOwnerLedgerReportWithoutSellerSchemaNeverReportsFakeZero(t *testing.T){
 s,_:=opened(t)
 ctx:=context.Background()
 newRep(t,s,"repReportA")
 grant(t,s,"repReportA","owner-report-grant-0000001",5<<30,200)
 report,err:=s.OwnerRepresentativeLedgerReport(ctx,"repReportA")
 if err!=nil{t.Fatal(err)}
 if report.SellerQuotaState!="not_enabled" || report.ReservedVolumeBytes!=""||
 report.ReservedUnlimitedUnits!="" || report.FreeVolumeBytes!="5368709120"||
 report.FreeUnlimitedUnits!="200" || !report.Enabled || report.SellerCount!=0{
  t.Fatalf("report invented escrow state: %+v",report)
 }
 if _,err:=s.OwnerRepresentativeLedgerReport(ctx,"owner");!errors.Is(err,ErrInvalid){t.Fatal("owner must not impersonate rep",err)}
 if _,err:=s.OwnerRepresentativeLedgerReport(ctx,"other");!errors.Is(err,ErrNotFound){t.Fatal("unknown rep was reported",err)}
}

func TestOwnerLedgerReportShowsExactFreeAndEscrow(t *testing.T){
 s,_:=opened(t);ctx:=context.Background()
 newRep(t,s,"repReportB")
 grant(t,s,"repReportB","owner-report-grant-0000002",1024,100)
 if err:=s.EnableSellerQuotaV2(ctx);err!=nil{t.Fatal(err)}
 _,err:=s.CreateSellerV2(ctx,CreateSellerV2{EventID:"owner-report-seller-create-001",
 RepresentativeID:"repReportB",SellerID:"salesA",Name:"A",TelegramUserID:77111})
 if err!=nil{t.Fatal(err)}
 _,err=s.CreateSellerV2(ctx,CreateSellerV2{EventID:"owner-report-seller-create-002",
 RepresentativeID:"repReportB",SellerID:"salesB",Name:"B",TelegramUserID:77222})
 if err!=nil{t.Fatal(err)}
 for _,item:=range []struct{event,seller string;bytes,units int64}{
  {"owner-report-allocate-00001","salesA",256,20},
  {"owner-report-allocate-00002","salesB",512,30},
 }{
  if _,err=s.TransferSellerQuotaV2(ctx,SellerQuotaTransferV2{EventID:item.event,
   RepresentativeID:"repReportB",SellerID:item.seller,Action:"allocate",
   Bytes:item.bytes,Units:item.units,Reason:"Verified owner allocation"});err!=nil{t.Fatal(err)}
 }
 report,err:=s.OwnerRepresentativeLedgerReport(ctx,"repReportB")
 if err!=nil{t.Fatal(err)}
 if report.SellerQuotaState!="enabled"||report.SellerCount!=2||
 report.FreeVolumeBytes!="256"||report.FreeUnlimitedUnits!="50"||
 report.ReservedVolumeBytes!="768"||report.ReservedUnlimitedUnits!="50"{
  t.Fatalf("inconsistent finance snapshot: %+v",report)
 }
 if report.BalanceSemantics!="authoritative_representative_free_credit_not_total_sales_or_live_traffic"{
  t.Fatal("unknown accounting metric",report)
 }
}

func TestOwnerLedgerReportRejectsPartialOrCorruptSellerSchema(t *testing.T){
 s,_:=opened(t);ctx:=context.Background()
 newRep(t,s,"repReportC")
 if _,err:=s.db.ExecContext(ctx,"CREATE TABLE seller_quota_v2_meta(id INTEGER PRIMARY KEY,version INTEGER)");err!=nil{t.Fatal(err)}
 if _,err:=s.OwnerRepresentativeLedgerReport(ctx,"repReportC");!errors.Is(err,ErrUnavailable){
  t.Fatalf("partial schema mistaken for zero escrow: %v",err)
 }
}
