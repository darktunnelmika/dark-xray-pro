// Package routeplan builds an explicit, SIDE-EFFECT-FREE Node replacement
// preview. It never reads or inherits retired Node tunnels, Xray configs,
// old Agent credentials, or customer subscription destinations.
package routeplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalidNode = errors.New("invalid or unchanged replacement Node identity")
	ErrInvalidRoute = errors.New("invalid inbound deployment or tunnel settings")
	ErrEmpty = errors.New("replacement has no selected deployment routes")
	nodeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	labelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
)

type Direct struct {
	NodePort uint16 `json:"node_port"`
}

type Swap struct {
	// Only three operator-supplied fields after inbound and new Node selection.
	TunnelPort uint16 `json:"tunnel_port"`
	NodePort uint16 `json:"node_port"`
	TunnelAddress string `json:"tunnel_address"`
}

type Deployment struct {
	InboundID uint64 `json:"inbound_id"`
	Direct *Direct `json:"direct,omitempty"`
	Swap *Swap `json:"swap,omitempty"`
}

type Preview struct {
	OldNodeID string `json:"old_node_id"`
	NewNodeID string `json:"new_node_id"`
	Deployments []Deployment `json:"deployments"`
	ManifestSHA256 string `json:"manifest_sha256"`
	NeedsNewAgentVerification bool `json:"needs_new_agent_verification"`
	NeedsReachabilityProbes bool `json:"needs_reachability_probes"`
	HasInheritedOldRoutes bool `json:"has_inherited_old_routes"`
	CanActivate bool `json:"can_activate"`
	Status string `json:"status"`
}

type Draft struct {
	mu sync.RWMutex
	oldNode string
	newNode string
	inbounds map[uint64]Deployment
}

func New(oldNode,newNode string)(*Draft,error){
	if !nodeIDPattern.MatchString(oldNode)||!nodeIDPattern.MatchString(newNode)||oldNode==newNode {
		return nil,ErrInvalidNode
	}
	return &Draft{oldNode:oldNode,newNode:newNode,inbounds:make(map[uint64]Deployment)},nil
}

func port(value int)(uint16,error){
	if value<=0 || value>65535 {return 0,ErrInvalidRoute}
	return uint16(value),nil
}

func validAddress(addr string) bool {
	if addr=="" || len(addr)>253 || strings.TrimSpace(addr)!=addr {return false}
	if ip:=net.ParseIP(addr);ip!=nil{return true}
	if !strings.Contains(addr,".") {return false}
	for _,segment:=range strings.Split(addr,".") {
		if !labelPattern.MatchString(segment) {return false}
	}
	return true
}

func (d *Draft) SetDirect(inboundID uint64,nodePort int)error{
	if inboundID==0 {return ErrInvalidRoute}
	value,err:=port(nodePort)
	if err!=nil{return err}
	d.mu.Lock()
	defer d.mu.Unlock()
	row:=d.inbounds[inboundID]
	row.InboundID=inboundID
	row.Direct=&Direct{NodePort:value}
	d.inbounds[inboundID]=row
	return nil
}

func (d *Draft) SetSwap(inboundID uint64,tunnelPort,nodePort int,tunnelAddress string)error{
	if inboundID==0 || !validAddress(tunnelAddress) {return ErrInvalidRoute}
	tp,err:=port(tunnelPort)
	if err!=nil{return err}
	np,err:=port(nodePort)
	if err!=nil{return err}
	d.mu.Lock()
	defer d.mu.Unlock()
	row:=d.inbounds[inboundID]
	row.InboundID=inboundID
	row.Swap=&Swap{TunnelPort:tp,NodePort:np,TunnelAddress:tunnelAddress}
	d.inbounds[inboundID]=row
	return nil
}

func (d *Draft) RemoveDirect(inboundID uint64){
	d.mu.Lock()
	defer d.mu.Unlock()
	row,ok:=d.inbounds[inboundID]
	if !ok{return}
	row.Direct=nil
	if row.Swap==nil{delete(d.inbounds,inboundID)}else{d.inbounds[inboundID]=row}
}

func (d *Draft) RemoveSwap(inboundID uint64){
	d.mu.Lock()
	defer d.mu.Unlock()
	row,ok:=d.inbounds[inboundID]
	if !ok{return}
	row.Swap=nil
	if row.Direct==nil{delete(d.inbounds,inboundID)}else{d.inbounds[inboundID]=row}
}

// Preview returns an immutable copy of the explicit choices, plus a stable
// digest for later staged acceptance. It CAN NEVER authorize activation;
// dynamic Node identity, TLS, port conflicts and reachability are still unknown.
func (d *Draft) Preview()(Preview,error){
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.inbounds)==0{return Preview{},ErrEmpty}
	rows:=make([]Deployment,0,len(d.inbounds))
	for _,raw:=range d.inbounds {
		row:=Deployment{InboundID:raw.InboundID}
		if raw.Direct!=nil{direct:=*raw.Direct;row.Direct=&direct}
		if raw.Swap!=nil{swap:=*raw.Swap;row.Swap=&swap}
		if row.Direct==nil && row.Swap==nil{continue}
		rows=append(rows,row)
	}
	if len(rows)==0{return Preview{},ErrEmpty}
	sort.Slice(rows,func(i,j int)bool{return rows[i].InboundID<rows[j].InboundID})
	material:=struct{
		Old string `json:"old"`
		New string `json:"new"`
		Rows []Deployment `json:"routes"`
	}{Old:d.oldNode,New:d.newNode,Rows:rows}
	raw,err:=json.Marshal(material)
	if err!=nil{return Preview{},err}
	digest:=sha256.Sum256(raw)
	return Preview{
		OldNodeID:d.oldNode,NewNodeID:d.newNode,Deployments:rows,
		ManifestSHA256:hex.EncodeToString(digest[:]),
		NeedsNewAgentVerification:true,
		NeedsReachabilityProbes:true,
		HasInheritedOldRoutes:false,
		CanActivate:false,
		Status:"requires_new_agent_and_route_preflight",
	},nil
}
