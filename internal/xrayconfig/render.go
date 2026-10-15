// Package xrayconfig renders private, non-published NEW PRO Xray candidates.
package xrayconfig

import (
 "crypto/ecdh"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "net"
 "regexp"
 "sort"
 "strconv"
 "strings"
 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)
var ErrInvalid=errors.New("invalid Xray candidate")
var ErrConflict=errors.New("inbound port or profile conflict")
var ErrAmbiguousAttribution=errors.New("a shared Direct/Tunnel Node port cannot distinguish path traffic")
const MaxCandidateBytes=4<<20
var(
 uuidRE=regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
 emailRE=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}@[A-Za-z0-9.-]{1,80}$`)
 dnsLabel=regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
 shortIDRE=regexp.MustCompile(`^[0-9a-f]{0,16}$`)
 serviceRE=regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)
// Native Xray counter email is not itself a financial ledger.
type User struct{UUID string `json:"uuid"`;Email string `json:"email"`}
// Profile contains PRIVATE key material and must not be returned to browsers.
type Profile struct{InboundID uint64 `json:"inbound_id"`;Transport string `json:"transport"`;GRPCService string `json:"grpc_service,omitempty"`;RealityTarget string `json:"reality_target"`;ServerNames []string `json:"server_names"`;RealityPrivateKey string `json:"reality_private_key"`;ShortIDs []string `json:"short_ids"`;Users []User `json:"users"`}
type Listener struct{InboundID uint64 `json:"inbound_id"`;NodePort uint16 `json:"node_port"`;Kind string `json:"kind"`;Tag string `json:"tag"`;Attribution string `json:"attribution"`}
type Candidate struct{
 Config []byte `json:"-"`;SHA256 string `json:"sha256"`;ManifestSHA256 string `json:"manifest_sha256"`;Listeners []Listener `json:"listeners"`
 CanActivate bool `json:"can_activate"`;RequiresXrayTest bool `json:"requires_xray_test"`;RequiresDataPlaneProof bool `json:"requires_data_plane_proof"`;RequiresSubscriptionCommit bool `json:"requires_subscription_commit"`
}
func validDNS(name string)bool{
 if name==""||len(name)>253||strings.ContainsAny(name,":/\\@% \t\r\n"){return false};if net.ParseIP(name)!=nil{return false};labels:=strings.Split(name,".");if len(labels)<2{return false};for _,label:=range labels{if !dnsLabel.MatchString(label){return false}};return true
}
func validTarget(target string)bool{
 host,port,err:=net.SplitHostPort(target);if err!=nil{return false};p,err:=strconv.Atoi(port);if err!=nil||p<1||p>65535||strings.Contains(host,"%"){return false}
 if ip:=net.ParseIP(host);ip!=nil{return !ip.IsLoopback()&&!ip.IsPrivate()&&!ip.IsLinkLocalUnicast()&&!ip.IsUnspecified()&&!ip.IsMulticast()};return validDNS(host)
}
func validProfile(p Profile)error{
 if p.InboundID==0||len(p.Users)>20000||(p.Transport!="tcp"&&p.Transport!="grpc")||!validTarget(p.RealityTarget)||len(p.ServerNames)==0||len(p.ServerNames)>16||len(p.ShortIDs)==0||len(p.ShortIDs)>16{return ErrInvalid}
 if p.Transport=="tcp"&&p.GRPCService!=""{return ErrInvalid};if p.Transport=="grpc"&&!serviceRE.MatchString(p.GRPCService){return ErrInvalid}
 for _,name:=range p.ServerNames{if !validDNS(name){return ErrInvalid}}
 decoded,err:=base64.RawURLEncoding.DecodeString(p.RealityPrivateKey);if err!=nil||len(decoded)!=32{return ErrInvalid}
 nonzero:=false;for _,b:=range decoded{if b!=0{nonzero=true;break}};if !nonzero{return ErrInvalid};if _,err:=ecdh.X25519().NewPrivateKey(decoded);err!=nil{return ErrInvalid}
 ids:=map[string]bool{};for _,id:=range p.ShortIDs{if !shortIDRE.MatchString(id)||len(id)%2!=0||ids[id]{return ErrInvalid};ids[id]=true}
 server:=map[string]bool{};for _,name:=range p.ServerNames{lower:=strings.ToLower(name);if server[lower]{return ErrInvalid};server[lower]=true}
 users:=map[string]bool{};uuids:=map[string]bool{}
 for _,u:=range p.Users{key:=strings.ToLower(u.Email);if !uuidRE.MatchString(u.UUID)||!emailRE.MatchString(u.Email)||strings.Contains(u.Email,"..")||users[key]||uuids[strings.ToLower(u.UUID)]{return ErrInvalid};users[key]=true;uuids[strings.ToLower(u.UUID)]=true};return nil
}
type xrayUser struct{ID string `json:"id"`;Email string `json:"email"`;Level int `json:"level"`;Flow string `json:"flow,omitempty"`}
type reality struct{Dest string `json:"dest"`;ServerNames []string `json:"serverNames"`;PrivateKey string `json:"privateKey"`;ShortIDs []string `json:"shortIds"`}
type stream struct{Network string `json:"network"`;Security string `json:"security"`;Reality reality `json:"realitySettings"`;GRPC map[string]string `json:"grpcSettings,omitempty"`}
type inbound struct{
 Listen string `json:"listen"`;Port uint16 `json:"port"`;Tag string `json:"tag"`;Protocol string `json:"protocol"`
 Settings struct{Clients []xrayUser `json:"clients"`;Decryption string `json:"decryption"`} `json:"settings"`
 Stream stream `json:"streamSettings"`
}
type outbound struct{Tag string `json:"tag"`;Protocol string `json:"protocol"`}
func validatedPreview(p routeplan.Preview)error{
 if p.OldNodeID==p.NewNodeID||p.HasInheritedOldRoutes||p.CanActivate||!p.NeedsNewAgentVerification||!p.NeedsReachabilityProbes{return ErrInvalid}
 d,err:=routeplan.New(p.OldNodeID,p.NewNodeID);if err!=nil{return ErrInvalid};seen:=map[uint64]bool{}
 for _,row:=range p.Deployments{if row.InboundID==0||seen[row.InboundID]||(row.Direct==nil&&row.Swap==nil){return ErrInvalid};seen[row.InboundID]=true
  if row.Direct!=nil{if err:=d.SetDirect(row.InboundID,int(row.Direct.NodePort));err!=nil{return ErrInvalid}}
  if row.Swap!=nil{if err:=d.SetSwap(row.InboundID,int(row.Swap.TunnelPort),int(row.Swap.NodePort),row.Swap.TunnelAddress);err!=nil{return ErrInvalid}}
 };actual,err:=d.Preview();if err!=nil||actual.ManifestSHA256!=p.ManifestSHA256{return ErrInvalid};return nil
}
// Legacy replacement Render preserves its original nonempty-user contract.
// Native RenderAssigned supports empty listeners for last-user revocation.
func Render(plan routeplan.Preview,profiles []Profile)(Candidate,error){
 if err:=validatedPreview(plan);err!=nil{return Candidate{},err};if len(profiles)!=len(plan.Deployments){return Candidate{},ErrInvalid}
 byID:=map[uint64]Profile{}
 for _,p:=range profiles{if len(p.Users)==0{return Candidate{},ErrInvalid};if err:=validProfile(p);err!=nil{return Candidate{},fmt.Errorf("%w: inbound %d",ErrInvalid,p.InboundID)};if _,duplicate:=byID[p.InboundID];duplicate{return Candidate{},ErrConflict};byID[p.InboundID]=p}
 seenPort:=map[uint16]uint64{};listeners:=make([]Listener,0,2*len(profiles));result:=make([]inbound,0,2*len(profiles));rows:=append([]routeplan.Deployment(nil),plan.Deployments...)
 sort.Slice(rows,func(i,j int)bool{return rows[i].InboundID<rows[j].InboundID})
 for _,row:=range rows{p,ok:=byID[row.InboundID];if !ok{return Candidate{},ErrInvalid}
  add:=func(nodePort uint16,kind string)error{
   if nodePort==0{return ErrInvalid};if _,exists:=seenPort[nodePort];exists{return ErrConflict};seenPort[nodePort]=row.InboundID
   tag:=fmt.Sprintf("dxp-in-%d-%s",row.InboundID,kind);clients:=make([]xrayUser,0,len(p.Users));orderedUsers:=append([]User(nil),p.Users...);sort.Slice(orderedUsers,func(i,j int)bool{return orderedUsers[i].Email<orderedUsers[j].Email})
   for _,u:=range orderedUsers{c:=xrayUser{ID:u.UUID,Email:u.Email,Level:0};if p.Transport=="tcp"{c.Flow="xtls-rprx-vision"};clients=append(clients,c)}
   x:=inbound{Listen:"0.0.0.0",Port:nodePort,Tag:tag,Protocol:"vless",Stream:stream{Network:p.Transport,Security:"reality",Reality:reality{Dest:p.RealityTarget,ServerNames:append([]string(nil),p.ServerNames...),PrivateKey:p.RealityPrivateKey,ShortIDs:append([]string(nil),p.ShortIDs...)}}}
   x.Settings.Decryption="none";x.Settings.Clients=clients;if p.Transport=="grpc"{x.Stream.GRPC=map[string]string{"serviceName":p.GRPCService}}
   attribution:="distinct";if kind=="shared"{attribution="ambiguous_shared_port"};listeners=append(listeners,Listener{InboundID:row.InboundID,NodePort:nodePort,Kind:kind,Tag:tag,Attribution:attribution});result=append(result,x);return nil
  }
  if row.Direct!=nil&&row.Swap!=nil&&row.Direct.NodePort==row.Swap.NodePort{if err:=add(row.Direct.NodePort,"shared");err!=nil{return Candidate{},err}}else{if row.Direct!=nil{if err:=add(row.Direct.NodePort,"direct");err!=nil{return Candidate{},err}};if row.Swap!=nil{if err:=add(row.Swap.NodePort,"swap");err!=nil{return Candidate{},err}}}
 }
 sort.Slice(listeners,func(i,j int)bool{if listeners[i].InboundID==listeners[j].InboundID{return listeners[i].Kind<listeners[j].Kind};return listeners[i].InboundID<listeners[j].InboundID})
 doc:=struct{Log map[string]string `json:"log"`;Stats map[string]any `json:"stats"`;Policy map[string]any `json:"policy"`;Inbounds []inbound `json:"inbounds"`;Outbounds []outbound `json:"outbounds"`}{
  Log:map[string]string{"loglevel":"warning"},Stats:map[string]any{},Policy:map[string]any{"levels":map[string]any{"0":map[string]bool{"statsUserUplink":true,"statsUserDownlink":true}},"system":map[string]bool{"statsInboundUplink":true,"statsInboundDownlink":true}},Inbounds:result,Outbounds:[]outbound{{Tag:"direct",Protocol:"freedom"}},
 }
 config,err:=json.MarshalIndent(doc,"","  ");if err!=nil||len(config)>MaxCandidateBytes{return Candidate{},ErrInvalid};digest:=sha256.Sum256(config)
 return Candidate{Config:config,SHA256:hex.EncodeToString(digest[:]),ManifestSHA256:plan.ManifestSHA256,Listeners:listeners,CanActivate:false,RequiresXrayTest:true,RequiresDataPlaneProof:true,RequiresSubscriptionCommit:true},nil
}
