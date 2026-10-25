package business

import (
 "context"
 "errors"
 "fmt"
 "sync"
 "testing"
)

func setupSellerQuota(t *testing.T)(*Store,context.Context){
 t.Helper()
 store,_:=opened(t)
 ctx:=context.Background()
 newRep(t,store,"repSellerA")
 newRep(t,store,"repSellerB")
 grant(t,store,"repSellerA","seller-grant-rep-A-0001",1000,100)
 grant(t,store,"repSellerB","seller-grant-rep-B-0001",500,50)
 if err:=store.EnableSellerQuotaV2(ctx);err!=nil{t.Fatal(err)}
 _,err:=store.CreateSellerV2(ctx,CreateSellerV2{EventID:"seller-create-alice-0001",
  RepresentativeID:"repSellerA",SellerID:"sellerAlice",Name:"Alice QA",TelegramUserID:123456})
 if err!=nil{t.Fatal(err)}
 return store,ctx
}
func TestSellerV2NoIndependentCreditOrAccountLeak(t *testing.T){
 s,ctx:=setupSellerQuota(t)
 rep,err:=s.Representative(ctx,"repSellerA")
 if err!=nil||rep.VolumeCreditBytes!=1000||rep.UnlimitedCreditUnits!=100{t.Fatal(rep,err)}
 alice,err:=s.SellerV2(ctx,"repSellerA","sellerAlice")
 if err!=nil||alice.ReservedUnits!=0||alice.ReservedBytes!=0||alice.RepresentativeID!="repSellerA"{t.Fatal(alice,err)}
 if _,err=s.SellerV2(ctx,"repSellerB","sellerAlice");!errors.Is(err,ErrNotFound){t.Fatal("cross-account read",err)}
 if _,err=s.TransferSellerQuotaV2(ctx,SellerQuotaTransferV2{EventID:"seller-cross-account-0001",RepresentativeID:"repSellerB",
  SellerID:"sellerAlice",Action:"allocate",Units:2,Reason:"Foreign seller"});!errors.Is(err,ErrNotFound){t.Fatal("cross-account transfer",err)}
 other,err:=s.Representative(ctx,"repSellerB")
 if err!=nil||other.UnlimitedCreditUnits!=50{t.Fatal("other rep mutated",other,err)}
}
func TestSellerV2AllocateReclaimReplayConservation(t *testing.T){
 s,ctx:=setupSellerQuota(t)
 allocation:=SellerQuotaTransferV2{EventID:"seller-allocate-20-00001",RepresentativeID:"repSellerA",
  SellerID:"sellerAlice",Action:"allocate",Bytes:200,Units:20,Reason:"Reviewed quota"}
 got,err:=s.TransferSellerQuotaV2(ctx,allocation)
 if err!=nil||got.Replayed||got.RepFreeBytes!=800||got.RepFreeUnits!=80||got.SellerReservedBytes!=200||got.SellerReservedUnits!=20{
  t.Fatal("allocation",got,err)
 }
 replay,err:=s.TransferSellerQuotaV2(ctx,allocation)
 if err!=nil||!replay.Replayed||replay.SellerReservedUnits!=20{t.Fatal("non-idempotent retry",replay,err)}
 changed:=allocation;changed.Units=21
 if _,err=s.TransferSellerQuotaV2(ctx,changed);!errors.Is(err,ErrConflict){t.Fatal("reused event with new amount",err)}
 reclaim:=SellerQuotaTransferV2{EventID:"seller-reclaim-10-00001",RepresentativeID:"repSellerA",SellerID:"sellerAlice",
  Action:"reclaim",Bytes:100,Units:10,Reason:"Owner review"}
 result,err:=s.TransferSellerQuotaV2(ctx,reclaim)
 if err!=nil||result.RepFreeBytes!=900||result.RepFreeUnits!=90||result.SellerReservedBytes!=100||result.SellerReservedUnits!=10{t.Fatal(result,err)}
 rep,err:=s.Representative(ctx,"repSellerA")
 seller,e:=s.SellerV2(ctx,"repSellerA","sellerAlice")
 if err!=nil||e!=nil||rep.VolumeCreditBytes+seller.ReservedBytes!=1000||rep.UnlimitedCreditUnits+seller.ReservedUnits!=100{
  t.Fatal("credits minted or lost",rep,seller,err,e)
 }
 if _,err=s.db.ExecContext(ctx,"DELETE FROM seller_quota_events_v2");err==nil{t.Fatal("deletable seller audit")}
 if _,err=s.db.ExecContext(ctx,"UPDATE seller_quota_events_v2 SET reason='hijacked'");err==nil{t.Fatal("editable seller audit")}
}
func TestSellerV2FailedAndDuplicateMutationsLeaveBalancesUntouched(t *testing.T){
 s,ctx:=setupSellerQuota(t)
 cases:=[]struct{name string;input SellerQuotaTransferV2;want error}{
  {"overdraft rep",SellerQuotaTransferV2{"seller-overdraw-0000001","repSellerA","sellerAlice","allocate",2000,101,"bad"},ErrInsufficient},
  {"reclaim nonexistent",SellerQuotaTransferV2{"seller-unfunded-0000001","repSellerA","sellerAlice","reclaim",1,1,"bad"},ErrInsufficient},
  {"negative",SellerQuotaTransferV2{"seller-negative-0000001","repSellerA","sellerAlice","allocate",-1,0,"bad"},ErrInvalid},
  {"zero",SellerQuotaTransferV2{"seller-zero-0000000001","repSellerA","sellerAlice","allocate",0,0,"bad"},ErrInvalid},
 }
 for _,v:=range cases{
  t.Run(v.name,func(t *testing.T){_,err:=s.TransferSellerQuotaV2(ctx,v.input);if !errors.Is(err,v.want){t.Fatalf("got %v want %v",err,v.want)}})
 }
 rep,err:=s.Representative(ctx,"repSellerA")
 alice,e:=s.SellerV2(ctx,"repSellerA","sellerAlice")
 if err!=nil||e!=nil||rep.UnlimitedCreditUnits!=100||alice.ReservedUnits!=0{t.Fatal(rep,alice,err,e)}
 _,err=s.CreateSellerV2(ctx,CreateSellerV2{EventID:"seller-create-alice-0001",RepresentativeID:"repSellerA",SellerID:"sellerAlice",Name:"Alice QA",TelegramUserID:123456})
 if err!=nil{t.Fatal("identical create replay",err)}
 _,err=s.CreateSellerV2(ctx,CreateSellerV2{EventID:"seller-create-alice-0001",RepresentativeID:"repSellerA",SellerID:"sellerAlice",Name:"Different",TelegramUserID:123456})
 if !errors.Is(err,ErrConflict){t.Fatal("same event different seller name",err)}
 list,err:=s.ListSellersV2(ctx,"repSellerA",100,0)
 if err!=nil||len(list)!=1||list[0].ID!="sellerAlice"{t.Fatal(list,err)}
}
func TestSellerV2ConcurrentTransfersAreSerialized(t *testing.T){
 s,ctx:=setupSellerQuota(t)
 var wg sync.WaitGroup
 var mu sync.Mutex
 success:=0
 for n:=0;n<25;n++{
  wg.Add(1)
  go func(i int){defer wg.Done()
   _,e:=s.TransferSellerQuotaV2(ctx,SellerQuotaTransferV2{EventID:fmt.Sprintf("seller-concurrent-%08d",i),
    RepresentativeID:"repSellerA",SellerID:"sellerAlice",Action:"allocate",Units:6,Reason:"Concurrent quota review"})
   if e==nil{mu.Lock();success++;mu.Unlock()}else if !errors.Is(e,ErrInsufficient){t.Errorf("unexpected concurrent error %v",e)}
  }(n)
 }
 wg.Wait()
 rep,err:=s.Representative(ctx,"repSellerA")
 seller,e:=s.SellerV2(ctx,"repSellerA","sellerAlice")
 if err!=nil||e!=nil||success!=16||rep.UnlimitedCreditUnits!=4||seller.ReservedUnits!=96||
  rep.UnlimitedCreditUnits+seller.ReservedUnits!=100{t.Fatal("not strictly conserved",success,rep,seller,err,e)}
}
func TestSellerV2DisabledByDefaultAndExistingClientDebitsRemainIndependent(t *testing.T){
 s,_:=opened(t);ctx:=context.Background()
 newRep(t,s,"repSellerDefault")
 grant(t,s,"repSellerDefault","seller-v2-grant-default-0001",100,20)
 _,e:=s.TransferSellerQuotaV2(ctx,SellerQuotaTransferV2{EventID:"seller-disabled-default-0001",
  RepresentativeID:"repSellerDefault",SellerID:"never",Action:"allocate",Units:1,Reason:"must fail"})
 if e==nil{t.Fatal("seller service silently enabled")}
 if _,e=s.CreateClient(ctx,NewClient{EventID:"seller-main-client-create-0001",ClientID:"regular",
  RepresentativeID:"repSellerDefault",Name:"Regular owner approved",Unlimited:true,IPCount:2,DurationDays:30});e!=nil{
  t.Fatal("existing client create changed",e)
 }
 rep,e:=s.Representative(ctx,"repSellerDefault")
 if e!=nil||rep.UnlimitedCreditUnits!=18{t.Fatal("existing usage accounting affected",rep,e)}
}
