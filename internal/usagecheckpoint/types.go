// Package usagecheckpoint provides a durable Agent outbox and validated
// cumulative usage deltas. It never grants a subscription or quota by itself.
package usagecheckpoint

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "math"
 "regexp"
 "sort"
)
const(MaxBindings=20000;MaxStatsBytes=8<<20;MaxPending=32)
const SystemProbeRole="system_probe"
var(
 ErrInvalid=errors.New("invalid usage checkpoint")
 ErrIncomplete=errors.New("incomplete usage observation; never substitute zero")
 ErrRegression=errors.New("counter decreased in the same process epoch")
 ErrConflict=errors.New("usage sequence or binding conflict")
 ErrStale=errors.New("usage report is older than the durable cursor")
 ErrEpoch=errors.New("process epoch differs; explicit registration required")
 ErrClosed=errors.New("usage outbox is closed or fenced")
 ErrSealed=errors.New("usage epoch is sealed")
 ErrFull=errors.New("usage outbox is full; checkpoint was not advanced")
 ErrPrivate=errors.New("usage checkpoint path is not private")
 ErrLocked=errors.New("usage checkpoint already has a writer")
 ErrCorrupt=errors.New("usage checkpoint authentication or structure failed")
 ErrReceipt=errors.New("acknowledgement does not match oldest pending report")
 ErrOverflow=errors.New("usage byte total exceeds signed 64-bit range")
 idPattern=regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
 emailPattern=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}@[A-Za-z0-9.-]{1,80}$`)
)
// Bindings are supplied by trusted topology, never by the usage report.
type Binding struct{ClientID string `json:"client_id"`;Email string `json:"email"`;Billable bool `json:"billable"`;Role string `json:"role,omitempty"`}
// Each epoch is ONE supervised process incarnation, not merely a config hash.
type Epoch struct{
 NodeID string `json:"node_id"`;InstallationID string `json:"installation_id"`;ID string `json:"epoch_id"`;ConfigSHA string `json:"config_sha"`
 KernelBootID string `json:"kernel_boot_id"`;PID int `json:"pid"`;ProcessStartTicks uint64 `json:"process_start_ticks,string"`;Bindings []Binding `json:"bindings"`
}
// String-encoded int64 counters preserve precision across JSON consumers.
type Usage struct{ClientID string `json:"client_id"`;Uplink int64 `json:"uplink_bytes,string"`;Downlink int64 `json:"downlink_bytes,string"`}
type Report struct{Version int `json:"version"`;EpochID string `json:"epoch_id"`;BindingSHA string `json:"binding_sha"`;Sequence uint64 `json:"sequence,string"`;Quiesced bool `json:"quiesced"`;Users []Usage `json:"users"`;SHA string `json:"sha256"`}
type Receipt struct{EpochID string `json:"epoch_id"`;Sequence uint64 `json:"sequence,string"`;SHA string `json:"sha256"`}
type Charge struct{ClientID string;Uplink,Downlink int64}
// Cursor and debit must be persisted in the SAME HUB transaction.
type Cursor struct{EpochID string;Sequence uint64;ReportSHA string;Quiesced bool;Users []Usage}
func hexID(s string,n int)bool{if len(s)!=n{return false};if _,e:=hex.DecodeString(s);e!=nil{return false};for _,c:=range s{if c>='A'&&c<='F'{return false}};return true}
func normalize(e Epoch)(Epoch,error){
 if !idPattern.MatchString(e.NodeID)||!hexID(e.InstallationID,32)||!hexID(e.ID,32)||!hexID(e.ConfigSHA,64)||len(e.KernelBootID)!=36||e.PID<=0||e.ProcessStartTicks==0||len(e.Bindings)==0||len(e.Bindings)>MaxBindings{return Epoch{},ErrInvalid}
 for i,c:=range e.KernelBootID{if i==8||i==13||i==18||i==23{if c!='-'{return Epoch{},ErrInvalid};continue};if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return Epoch{},ErrInvalid}}
 e.Bindings=append([]Binding(nil),e.Bindings...);sort.Slice(e.Bindings,func(i,j int)bool{return e.Bindings[i].ClientID<e.Bindings[j].ClientID})
 ids,emails:=map[string]bool{},map[string]bool{}
 for _,b:=range e.Bindings{if !idPattern.MatchString(b.ClientID)||!emailPattern.MatchString(b.Email)||ids[b.ClientID]||emails[b.Email]||(b.Role!=""&&(b.Role!=SystemProbeRole||b.Billable)){return Epoch{},ErrInvalid};ids[b.ClientID]=true;emails[b.Email]=true}
 return e,nil
}
func hash(v any)string{b,_:=json.Marshal(v);h:=sha256.Sum256(b);return hex.EncodeToString(h[:])}
func bindingSHA(e Epoch)string{return hash(e)}
func reportSHA(r Report)string{r.SHA="";return hash(r)}
func cloneReport(r Report)Report{r.Users=append([]Usage(nil),r.Users...);return r}
func add(a,b int64)(int64,error){if a<0||b<0||a>math.MaxInt64-b{return 0,ErrOverflow};return a+b,nil}
func canonicalUsers(e Epoch,users []Usage)([]Usage,error){
 if len(users)!=len(e.Bindings){return nil,ErrIncomplete};users=append([]Usage(nil),users...)
 sort.Slice(users,func(i,j int)bool{return users[i].ClientID<users[j].ClientID})
 for i,u:=range users{if u.ClientID!=e.Bindings[i].ClientID||u.Uplink<0||u.Downlink<0{return nil,ErrInvalid};if _,err:=add(u.Uplink,u.Downlink);err!=nil{return nil,err}}
 return users,nil
}
func makeReport(e Epoch,seq uint64,users []Usage,quiesced bool)(Report,error){
 u,err:=canonicalUsers(e,users);if err!=nil{return Report{},err};if seq==0{return Report{},ErrOverflow}
 r:=Report{Version:1,EpochID:e.ID,BindingSHA:bindingSHA(e),Sequence:seq,Users:u,Quiesced:quiesced};r.SHA=reportSHA(r);return r,nil
}
func validateReport(e Epoch,r Report)error{
 if r.Version!=1||r.EpochID!=e.ID||r.BindingSHA!=bindingSHA(e)||r.Sequence==0||!hexID(r.SHA,64)||reportSHA(r)!=r.SHA{return ErrInvalid}
 u,err:=canonicalUsers(e,r.Users);if err!=nil{return err};for i:=range u{if u[i]!=r.Users[i]{return ErrInvalid}};return nil
}
// Calculate authenticates no caller. The input authority must already be
// registered, and the caller must commit returned cursor and debit together.
func Calculate(authorized Epoch,current Cursor,report Report)(Cursor,[]Charge,bool,error){
 e,err:=normalize(authorized);if err!=nil{return current,nil,false,err};if err=validateReport(e,report);err!=nil{return current,nil,false,err}
 if current.Sequence==0{if current.EpochID!=""||current.ReportSHA!=""||current.Quiesced||len(current.Users)!=0{return current,nil,false,ErrInvalid}}else{
  if current.EpochID!=e.ID{return current,nil,false,ErrEpoch};if _,err=canonicalUsers(e,current.Users);err!=nil{return current,nil,false,err}
  previous:=Report{Version:1,EpochID:e.ID,BindingSHA:bindingSHA(e),Sequence:current.Sequence,Users:current.Users,Quiesced:current.Quiesced,SHA:current.ReportSHA}
  if err=validateReport(e,previous);err!=nil{return current,nil,false,err}
 }
 if report.Sequence<current.Sequence{return current,nil,false,ErrStale}
 if report.Sequence==current.Sequence{if report.SHA!=current.ReportSHA{return current,nil,false,ErrConflict};return current,nil,true,nil}
 if current.Quiesced{return current,nil,false,ErrSealed}
 charges:=make([]Charge,0,len(report.Users))
 for i,u:=range report.Users{
  up,down:=int64(0),int64(0);if current.Sequence>0{up,down=current.Users[i].Uplink,current.Users[i].Downlink}
  if u.Uplink<up||u.Downlink<down{return current,nil,false,ErrRegression}
  if e.Bindings[i].Billable{charges=append(charges,Charge{ClientID:u.ClientID,Uplink:u.Uplink-up,Downlink:u.Downlink-down})}
 }
 next:=Cursor{EpochID:e.ID,Sequence:report.Sequence,ReportSHA:report.SHA,Quiesced:report.Quiesced,Users:append([]Usage(nil),report.Users...)}
 return next,charges,false,nil
}
func PlanTotals(previous map[string]int64,charges []Charge)(map[string]int64,error){
 next:=make(map[string]int64,len(previous)+len(charges));for id,total:=range previous{if !idPattern.MatchString(id)||total<0{return nil,ErrInvalid};next[id]=total}
 seen:=map[string]bool{};for _,c:=range charges{if !idPattern.MatchString(c.ClientID)||seen[c.ClientID]{return nil,ErrInvalid};seen[c.ClientID]=true
  inc,err:=add(c.Uplink,c.Downlink);if err!=nil{return nil,err};value,err:=add(next[c.ClientID],inc);if err!=nil{return nil,err};next[c.ClientID]=value}
 return next,nil
}
