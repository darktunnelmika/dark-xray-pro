//go:build linux

package inboundruntime

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// runtimeBundle adds only private adapter bridges requested by the immutable
// reviewed manifest. Their random credentials and actual ports are bound into
// the registered process identity, never exported to a retail subscription.
func runtimeBundle(original inboundexec.Bundle)(inboundexec.Bundle,error){
	b:=cloneBundle(original)
	for i:=range b.Components{
		component:=&b.Components[i];if component.Engine!="mtproto"{continue};var manifest map[string]any;if json.Unmarshal(component.Manifest,&manifest)!=nil{return b,ErrRuntime};required,_:=manifest["requires_socks_bridge"].(bool);if !required{continue}
		var cfg map[string]any;if json.Unmarshal(b.NativeConfig,&cfg)!=nil{return b,ErrRuntime};outbound,_:=manifest["outbound_tag"].(string);if outbound==""{outbound="direct"};outbounds,_:=cfg["outbounds"].([]any);found:=false;for _,v:=range outbounds{m,_:=v.(map[string]any);if m["tag"]==outbound{found=true}}
		if !found{return b,errors.New("inbound_bridge_outbound_not_configured")}
		port,e:=reserveConfigPort(b.NativeConfig);if e!=nil{return b,e};username,e:=randomID(16);if e!=nil{return b,e};password,e:=randomID(32);if e!=nil{return b,e};tag:="dark-pro-mtg-bridge-"+strconv.Itoa(i)
		inbounds,_:=cfg["inbounds"].([]any);for _,v:=range inbounds{m,_:=v.(map[string]any);if m["tag"]==tag{return b,ErrRuntime}}
		inbounds=append(inbounds,map[string]any{"tag":tag,"listen":"127.0.0.1","port":port,"protocol":"socks","settings":map[string]any{"auth":"password","accounts":[]any{map[string]any{"user":username,"pass":password}},"udp":false,"userLevel":2}});cfg["inbounds"]=inbounds
		policy,_:=cfg["policy"].(map[string]any);if policy==nil{policy=map[string]any{}};levels,_:=policy["levels"].(map[string]any);if levels==nil{levels=map[string]any{}};levels["2"]=map[string]any{"statsUserUplink":false,"statsUserDownlink":false};policy["levels"]=levels;cfg["policy"]=policy
		routing,_:=cfg["routing"].(map[string]any);if routing==nil{routing=map[string]any{}};rules,_:=routing["rules"].([]any);routing["rules"]=append([]any{map[string]any{"type":"field","inboundTag":[]string{tag},"outboundTag":outbound}},rules...);cfg["routing"]=routing
		b.NativeConfig,e=json.Marshal(cfg);if e!=nil{return b,e};bridge:=url.URL{Scheme:"socks5",Host:net.JoinHostPort("127.0.0.1",strconv.Itoa(int(port))),User:url.UserPassword(username,password)};manifest["socks_bridge_url"]=bridge.String();component.Manifest,e=json.Marshal(manifest);if e!=nil{return b,e}
		b.Listeners=append(b.Listeners,inboundexec.Listener{Tag:tag,Engine:"xray",Protocol:"socks",Listen:"127.0.0.1",Port:port,Network:"tcp",ResourceKind:"socket",Kind:"private_bridge",AccountingMode:inboundexec.AccountingOwnerAggregate})
	}
	return b,nil
}

func reserveConfigPort(raw []byte)(uint16,error){
	var cfg struct{Inbounds []struct{Port uint16 `json:"port"`} `json:"inbounds"`};if json.Unmarshal(raw,&cfg)!=nil{return 0,ErrRuntime};used:=map[uint16]bool{};for _,in:=range cfg.Inbounds{used[in.Port]=true};for i:=0;i<32;i++{port,e:=reservePort();if e!=nil{return 0,e};if !used[port]{return port,nil}};return 0,ErrRuntime
}
