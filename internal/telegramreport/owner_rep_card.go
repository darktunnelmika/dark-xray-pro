// Package telegramreport prepares untrusted representative names and ledger
// metrics for Telegram HTML messages. It never sends a message, reads bot
// tokens, or authenticates a sender. Authentication belongs to the future bot.
package telegramreport

import (
 "errors"
 "fmt"
 "html"
 "math/big"
 "regexp"
 "strings"
)

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)
var unitBytes = big.NewInt(1024 * 1024 * 1024)

type OwnerRepresentativeCard struct {
 RepresentativeID string `json:"representative_id"`
 DisplayName string `json:"display_name"`
 Enabled bool `json:"enabled"`
 FreeVolumeBytes string `json:"free_volume_bytes"`
 FreeUnlimitedUnits string `json:"free_unlimited_units"`
 SellerQuotaState string `json:"seller_quota_state"`
 SellerCount int64 `json:"seller_count"`
 ReservedVolumeBytes string `json:"reserved_volume_bytes"`
 ReservedUnlimitedUnits string `json:"reserved_unlimited_units"`
 BalanceSemantics string `json:"balance_semantics"`
}

func parseDecimal(s string)(*big.Int,error){
 if !decimal.MatchString(s){return nil,errors.New("invalid ledger decimal")}
 n,ok:=new(big.Int).SetString(s,10)
 if !ok||n.Sign()<0{return nil,errors.New("invalid ledger amount")}
 return n,nil
}

func bytesGiB(s string)(string,error){
 b,e:=parseDecimal(s);if e!=nil{return "",e}
 whole:=new(big.Int).Div(b,unitBytes)
 rest:=new(big.Int).Mod(b,unitBytes)
 frac:=new(big.Int).Div(new(big.Int).Mul(rest,big.NewInt(100)),unitBytes)
 return fmt.Sprintf("%s.%02s",whole.String(),frac.String()),nil
}

// FormatOwnerRepresentativeFA produces an escaped, size-bounded, read-only
// Telegram HTML report; it does not fabricate omitted seller credit.
// No markdown parsing or direct callback payload is allowed here.
func FormatOwnerRepresentativeFA(c OwnerRepresentativeCard)(string,error){
 if c.BalanceSemantics!="authoritative_representative_free_credit_not_total_sales_or_live_traffic" ||
    (c.SellerQuotaState!="not_enabled"&&c.SellerQuotaState!="enabled")||
    len(c.RepresentativeID)<1||len(c.RepresentativeID)>64||
    len(c.DisplayName)<1||len(c.DisplayName)>256||
    c.SellerCount<0||c.SellerCount>1_000_000{
  return "",errors.New("unverified owner representative report")
 }
 freeGiB,e:=bytesGiB(c.FreeVolumeBytes);if e!=nil{return "",e}
 freeUnits,e:=parseDecimal(c.FreeUnlimitedUnits);if e!=nil{return "",e}
 status:="🟢 فعال";if !c.Enabled {status="🔴 غیرفعال"}
 b:=&strings.Builder{}
 fmt.Fprint(b,"📊 <b>DARK HUB • گزارش نماینده</b>\n\n")
 fmt.Fprintf(b,"👤 <b>%s</b>\n",html.EscapeString(c.DisplayName))
 fmt.Fprintf(b,"🆔 <code>%s</code>\n",html.EscapeString(c.RepresentativeID))
 fmt.Fprintf(b,"وضعیت: %s\n\n",status)
 fmt.Fprintf(b,"💳 <b>اعتبار آزاد</b>\nحجمی: %s GiB\nنامحدود: %s واحد\n",freeGiB,freeUnits.String())
 if c.SellerQuotaState=="not_enabled"{
  if c.SellerCount!=0||c.ReservedVolumeBytes!=""||c.ReservedUnlimitedUnits!=""{
   return "",errors.New("inconsistent seller quota state")
  }
  fmt.Fprint(b,"\n🔒 سهمیه فروشندگان: <i>هنوز فعال نشده</i>\n")
 }else{
  reserveGiB,e:=bytesGiB(c.ReservedVolumeBytes);if e!=nil{return "",e}
  reserveUnits,e:=parseDecimal(c.ReservedUnlimitedUnits);if e!=nil{return "",e}
  fmt.Fprintf(b,"\n👥 <b>سهمیه رزروشده فروشندگان</b>\nفروشندگان: %d\nحجمی: %s GiB\nنامحدود: %s واحد\n",
   c.SellerCount,reserveGiB,reserveUnits.String())
 }
 fmt.Fprint(b,"\n<i>مقادیر از دفتر اعتبار ثبت‌شده‌اند؛ آمار فروش یا ترافیک لحظه‌ای نیستند.</i>")
 if b.Len()>4096{return "",errors.New("telegram message exceeds length limit")}
 return b.String(),nil
}
