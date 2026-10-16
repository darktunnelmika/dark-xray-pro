// Package usageingest exposes only authenticated, pre-enrolled usage reporting.
package usageingest
import("context";"crypto/tls";"encoding/json";"errors";"io";"mime";"net/http";"net/url";"regexp";"strings";"time";uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint";"github.com/darktunnelmika/dark-xray-pro/internal/usageledger";"github.com/darktunnelmika/dark-xray-pro/internal/usagewire")
const AgentPath="/api/v1/agent/native-usage"
const MaxReportBytes=8<<20
var nodeRE=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var installationRE=regexp.MustCompile(`^[0-9a-f]{32}$`)
type Authenticate func(context.Context,string,string,string)(usageledger.Principal,bool)
type Handler struct{ledger *usageledger.Ledger;authenticate Authenticate;inflight chan struct{}}
func New(ledger *usageledger.Ledger,authenticate Authenticate)(*Handler,error){if ledger==nil||authenticate==nil{return nil,errors.New("usage ledger and registry authentication are required")};return &Handler{ledger:ledger,authenticate:authenticate,inflight:make(chan struct{},4)},nil}
func respond(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_=json.NewEncoder(w).Encode(value)}
func fail(w http.ResponseWriter,status int,code string){respond(w,status,map[string]string{"error":code})}
func(h *Handler)ServeHTTP(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store");w.Header().Set("X-Content-Type-Options","nosniff");w.Header().Set("Referrer-Policy","no-referrer")
 if r.URL.Path!=AgentPath{http.NotFound(w,r);return};if r.Method!=http.MethodPost{w.Header().Set("Allow","POST");fail(w,405,"method_not_allowed");return}
 if r.TLS==nil||r.TLS.Version<tls.VersionTLS13{fail(w,426,"tls13_required");return}
 q,err:=httpQuery(r);if err!=nil{fail(w,400,"invalid_agent_scope");return}
 auth:=r.Header.Values("Authorization");if len(auth)!=1||!strings.HasPrefix(auth[0],"Bearer "){fail(w,401,"unauthorized");return}
 token:=strings.TrimPrefix(auth[0],"Bearer ");if len(token)<32||len(token)>1024||strings.ContainsAny(token," \r\n\t"){fail(w,401,"unauthorized");return}
 ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second);defer cancel();p,ok:=h.authenticate(ctx,q[0],q[1],token)
 if !ok||p.NodeID!=q[0]||p.InstallationID!=q[1]{fail(w,401,"unauthorized");return}
 select{case h.inflight<-struct{}{}:defer func(){<-h.inflight}();default:w.Header().Set("Retry-After","1");fail(w,429,"usage_ingest_busy");return}
 media,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"));if err!=nil||media!="application/json"{fail(w,415,"json_required");return}
 reader:=http.MaxBytesReader(w,r.Body,MaxReportBytes);defer reader.Close();raw,err:=io.ReadAll(reader);if err!=nil{fail(w,413,"report_too_large");return}
 var report uc.Report;if usagewire.Decode(raw,&report)!=nil{fail(w,400,"invalid_usage_json");return}
 receipt,err:=h.ledger.Accept(ctx,p,report)
 if err!=nil{switch{
 case errors.Is(err,usageledger.ErrMissing):fail(w,404,"unregistered_usage_epoch")
 case errors.Is(err,usageledger.ErrAuthority):fail(w,403,"usage_scope_denied")
 case errors.Is(err,uc.ErrConflict)||errors.Is(err,uc.ErrStale)||errors.Is(err,uc.ErrSealed)||errors.Is(err,uc.ErrRegression):fail(w,409,"usage_report_conflict")
 case errors.Is(err,uc.ErrInvalid)||errors.Is(err,uc.ErrIncomplete)||errors.Is(err,uc.ErrOverflow):fail(w,422,"invalid_usage_report")
 default:fail(w,503,"usage_commit_unconfirmed")};return}
 respond(w,200,receipt)
}
func httpQuery(r *http.Request)([2]string,error){
 q,e:=url.ParseQuery(r.URL.RawQuery);var out [2]string;if e!=nil{return out,e}
 if len(q)!=2||len(q["node_id"])!=1||len(q["installation_id"])!=1{return out,errors.New("scope")}
 out=[2]string{q.Get("node_id"),q.Get("installation_id")};if !nodeRE.MatchString(out[0])||!installationRE.MatchString(out[1]){return out,errors.New("scope")};return out,nil
}
