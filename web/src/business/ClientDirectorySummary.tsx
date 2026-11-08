import type {Client} from "./shared";
import "./ClientServices.css";
export type DirectorySummary={representative?:{id:string;display_name:string};scope:"saved_membership_and_committed_usage";inbounds:{id:number;name:string}[];usage?:{client_id:string;representative_id:string;total_bytes:string;uplink_bytes:string;downlink_bytes:string;limit_bytes:string;reserved_historical_bytes:string;remaining_native_budget_bytes?:string;known:boolean;reconciliation_holds:number;plan_kind:string}};
const bytes=(v:unknown):v is string=>typeof v==="string"&&/^(0|[1-9][0-9]{0,18})$/.test(v)&&BigInt(v)<=9223372036854775807n;
const format=(v:string)=>{const n=BigInt(v);return (n*100n/1073741824n/100n).toString()+"."+((n*100n/1073741824n)%100n).toString().padStart(2,"0")+" GiB";};
export default function ClientDirectorySummary({client:c,language}:{client:Client;language:"fa"|"en"}){
 const fa=language==="fa",d=c.directory,u=d?.usage;
 const valid=d?.scope==="saved_membership_and_committed_usage"&&Array.isArray(d.inbounds)&&d.inbounds.length<=256&&d.inbounds.every(x=>Number.isSafeInteger(x.id)&&x.id>0&&typeof x.name==="string")&&new Set(d.inbounds.map(x=>x.id)).size===d.inbounds.length;
 let usage=fa?"مصرف: نامشخص":"Usage: unavailable",remaining=fa?"باقی‌مانده: نامشخص":"Remaining: unknown";
 if(valid&&u&&u.client_id===c.id&&u.representative_id===c.representative_id&&u.plan_kind===c.plan_kind&&[u.total_bytes,u.uplink_bytes,u.downlink_bytes,u.limit_bytes,u.reserved_historical_bytes].every(bytes)&&typeof u.known==="boolean"&&Number.isSafeInteger(u.reconciliation_holds)&&u.reconciliation_holds>=0&&BigInt(u.uplink_bytes)+BigInt(u.downlink_bytes)===BigInt(u.total_bytes)){
  usage=(fa?"ثبت‌شده این پنل: ":"Recorded here: ")+format(u.total_bytes)+(u.known?"":(fa?" (ناقص)":" (partial)"));
  if(u.known&&u.reconciliation_holds===0){
   if(u.plan_kind==="unlimited")remaining=fa?"حجم نامحدود":"Unlimited volume";
   else if(bytes(u.remaining_native_budget_bytes)){
    const budget=BigInt(u.limit_bytes)-BigInt(u.reserved_historical_bytes)-BigInt(u.total_bytes),expected=budget>0n?budget:0n;
    if(BigInt(u.remaining_native_budget_bytes)===expected)remaining=(fa?"باقی‌مانده: ":"Remaining: ")+format(u.remaining_native_budget_bytes);
   }
  }
 }
 return <div className="client-directory-summary" aria-label={fa?"خلاصه سرویس و مصرف":"Service and usage summary"}>
 <span title={valid?d.inbounds.map(x=>x.name).join(" · "):undefined}>{fa?"سرویس: ":"Services: "}{valid?(d.inbounds.length?d.inbounds.slice(0,2).map(x=>x.name).join(" · ")+(d.inbounds.length>2?" +"+(d.inbounds.length-2):""):(fa?"بدون عضویت":"None assigned")):(fa?"نامشخص":"Unavailable")}</span><span>{usage}</span><span>{remaining}</span>
 </div>;
}
