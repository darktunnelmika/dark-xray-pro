package xrayconfig

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

const uuidA="af469f46-e18a-45f4-bf57-c7f649b10db5"
const uuidB="11223344-5566-4788-99aa-bbccddeeff00"

func testProfile(id uint64)Profile{
	return Profile{
		InboundID:id,Transport:"tcp",
		RealityTarget:"www.example.org:443",
		ServerNames:[]string{"www.example.org"},
		RealityPrivateKey:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17},32)),
		ShortIDs:[]string{"0123456789abcdef"},
		Users:[]User{{UUID:uuidA,Email:"one@pro.invalid"},{UUID:uuidB,Email:"two@pro.invalid"}},
	}
}

func planFor(t *testing.T, direct,swap int) routeplan.Preview{
	t.Helper()
	d,err:=routeplan.New("old-de","new-de")
	if err!=nil{t.Fatal(err)}
	if direct>0 {if err:=d.SetDirect(1,direct);err!=nil{t.Fatal(err)}}
	if swap>0 {if err:=d.SetSwap(1,6800,swap,"10.10.0.2");err!=nil{t.Fatal(err)}}
	p,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	return p
}

func TestRealityVisionSeparateDirectAndTunnelAreIndependentlyMetered(t *testing.T){
	plan:=planFor(t,8569,1185)
	c,err:=Render(plan,[]Profile{testProfile(1)})
	if err!=nil{t.Fatal(err)}
	if c.CanActivate||!c.RequiresXrayTest||!c.RequiresDataPlaneProof||
		!c.RequiresSubscriptionCommit||c.ManifestSHA256!=plan.ManifestSHA256{
		t.Fatalf("renderer falsely activated customer routes: %+v",c)
	}
	if len(c.Listeners)!=2||c.Listeners[0].Kind!="direct"||
		c.Listeners[0].NodePort!=8569||c.Listeners[1].Kind!="swap"||
		c.Listeners[1].NodePort!=1185{
		t.Fatalf("direct/tunnel listening ports lost: %+v",c.Listeners)
	}
	if c.Listeners[0].Attribution!="distinct"||c.Listeners[1].Attribution!="distinct"{
		t.Fatal("separate ports cannot lose independent inbound attribution")
	}
	var doc struct{
		Inbounds []struct{
			Tag string `json:"tag"`
			Port int `json:"port"`
			Protocol string `json:"protocol"`
			Settings struct{
				Decryption string `json:"decryption"`
				Clients []struct{
					ID string `json:"id"`
					Flow string `json:"flow"`
					Email string `json:"email"`
				} `json:"clients"`
			} `json:"settings"`
			Stream struct{
				Network string `json:"network"`
				Security string `json:"security"`
				Reality struct{PrivateKey string `json:"privateKey"`} `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
		Stats map[string]any `json:"stats"`
		Policy map[string]any `json:"policy"`
	}
	if err:=json.Unmarshal(c.Config,&doc);err!=nil{t.Fatal(err)}
	if len(doc.Inbounds)!=2||doc.Inbounds[0].Tag==doc.Inbounds[1].Tag{
		t.Fatalf("port-separated Xray inbound tags are ambiguous: %+v",doc.Inbounds)
	}
	for _,in:=range doc.Inbounds{
		if in.Protocol!="vless"||in.Settings.Decryption!="none"||
			in.Stream.Network!="tcp"||in.Stream.Security!="reality"||
			len(in.Settings.Clients)!=2||in.Settings.Clients[0].Flow!="xtls-rprx-vision"||
			in.Stream.Reality.PrivateKey!=testProfile(1).RealityPrivateKey{
			t.Fatalf("invalid Reality Vision wire config: %+v",in)
		}
	}
	if doc.Stats==nil||doc.Policy==nil {t.Fatal("traffic counters were not enabled")}
	if !strings.Contains(string(c.Config),`"statsInboundUplink": true`) {
		t.Fatal("per-Inbound uplink accounting was not configured")
	}
	if !strings.Contains(string(c.Config),`"statsUserDownlink": true`) {
		t.Fatal("per-user downlink counters were not configured")
	}
	if strings.Contains(string(c.Config),"10.10.0.2")||strings.Contains(string(c.Config),`"port": 6800`){
		t.Fatal("external tunnel address/port were incorrectly bound as an Xray listener")
	}
	public,_:=json.Marshal(c)
	if bytes.Contains(public,[]byte(testProfile(1).RealityPrivateKey)){
		t.Fatal("metadata serialization revealed Reality private key")
	}
	again,err:=Render(plan,[]Profile{testProfile(1)})
	if err!=nil||c.SHA256!=again.SHA256 {t.Fatal("identical config was not deterministic")}
}

func TestSharedInboundCannotInventDirectVsTunnelTrafficSplit(t *testing.T){
	plan:=planFor(t,1185,1185)
	c,err:=Render(plan,[]Profile{testProfile(1)})
	if err!=nil{t.Fatal(err)}
	if len(c.Listeners)!=1||c.Listeners[0].Kind!="shared"||
		c.Listeners[0].Attribution!="ambiguous_shared_port"{
		t.Fatalf("shared Node port misrepresented as two billed routes: %+v",c.Listeners)
	}
	var doc struct{Inbounds []any `json:"inbounds"`}
	if err:=json.Unmarshal(c.Config,&doc);err!=nil{t.Fatal(err)}
	if len(doc.Inbounds)!=1{t.Fatal("shared direct/tunnel listener duplicated Xray config")}
}

func TestRealityGRPCNeverEnablesVisionFlow(t *testing.T){
	p:=testProfile(1)
	p.Transport="grpc"
	p.GRPCService="dark-pro"
	c,err:=Render(planFor(t,443,0),[]Profile{p})
	if err!=nil{t.Fatal(err)}
	var d struct{Inbounds []struct{
		Settings struct{Clients []map[string]any `json:"clients"`} `json:"settings"`
		Stream struct{
			Network string `json:"network"`
			GRPC map[string]string `json:"grpcSettings"`
		} `json:"streamSettings"`
	} `json:"inbounds"`}
	if err:=json.Unmarshal(c.Config,&d);err!=nil{t.Fatal(err)}
	if d.Inbounds[0].Stream.Network!="grpc"||
		d.Inbounds[0].Stream.GRPC["serviceName"]!="dark-pro"{
		t.Fatal("gRPC Reality profile was not rendered")
	}
	for _,client:=range d.Inbounds[0].Settings.Clients {
		if _,ok:=client["flow"];ok {t.Fatal("gRPC Reality illegally inherited TCP Vision flow")}
	}
}

func TestTamperedPreviewAndInvalidProfilesFailClosed(t *testing.T){
	good:=planFor(t,8569,1185)
	changed:=good
	changed.ManifestSHA256=strings.Repeat("f",64)
	if _,err:=Render(changed,[]Profile{testProfile(1)});!errors.Is(err,ErrInvalid){
		t.Fatalf("tampered SHA manifest accepted: %v",err)
	}
	changed=good
	changed.CanActivate=true
	if _,err:=Render(changed,[]Profile{testProfile(1)});!errors.Is(err,ErrInvalid){
		t.Fatal("pre-activated preview accepted")
	}
	tests:=[]struct{name string; change func(*Profile)}{
		{"zero key",func(p *Profile){p.RealityPrivateKey=base64.RawURLEncoding.EncodeToString(make([]byte,32))}},
		{"bad key",func(p *Profile){p.RealityPrivateKey="bad"}},
		{"missing users",func(p *Profile){p.Users=nil}},
		{"duplicate user",func(p *Profile){p.Users=append(p.Users,p.Users[0])}},
		{"nonuuid",func(p *Profile){p.Users[0].UUID="not-a-uuid"}},
		{"invalid email",func(p *Profile){p.Users[0].Email="bad<name>@pro.invalid"}},
		{"insecure target localhost",func(p *Profile){p.RealityTarget="127.0.0.1:443"}},
		{"target path",func(p *Profile){p.RealityTarget="www.example.org:443/path"}},
		{"bad server name",func(p *Profile){p.ServerNames=[]string{"127.0.0.1"}}},
		{"empty short ids",func(p *Profile){p.ShortIDs=nil}},
		{"odd short ID",func(p *Profile){p.ShortIDs=[]string{"abc"}}},
		{"bad network",func(p *Profile){p.Transport="ws"}},
		{"grpc without service",func(p *Profile){p.Transport="grpc";p.GRPCService=""}},
		{"tcp with grpc service",func(p *Profile){p.GRPCService="foo"}},
	}
	for _,tc:=range tests {
		t.Run(tc.name,func(t *testing.T){
			p:=testProfile(1)
			tc.change(&p)
			if _,err:=Render(good,[]Profile{p});!errors.Is(err,ErrInvalid){
				t.Errorf("invalid Reality config allowed: %v",err)
			}
		})
	}
	if _,err:=Render(good,nil);!errors.Is(err,ErrInvalid){t.Fatal("zero profiles allowed")}
	if _,err:=Render(good,[]Profile{testProfile(1),testProfile(1)});!errors.Is(err,ErrInvalid){
		t.Fatal("duplicate profile allowed")
	}
}

func TestDuplicateNodeListeningPortsAcrossInboundsRejected(t *testing.T){
	d,err:=routeplan.New("old","new")
	if err!=nil{t.Fatal(err)}
	if err:=d.SetDirect(1,1185);err!=nil{t.Fatal(err)}
	if err:=d.SetSwap(2,6800,1185,"10.0.0.2");err!=nil{t.Fatal(err)}
	p,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	_,err=Render(p,[]Profile{testProfile(2),testProfile(1)})
	if !errors.Is(err,ErrConflict){t.Fatalf("colliding node-port accepted: %v",err)}
	if err:=d.SetSwap(2,6800,1186,"10.0.0.2");err!=nil{t.Fatal(err)}
	p,_=d.Preview()
	a,err:=Render(p,[]Profile{testProfile(2),testProfile(1)})
	if err!=nil{t.Fatal(err)}
	b,err:=Render(p,[]Profile{testProfile(1),testProfile(2)})
	if err!=nil||a.SHA256!=b.SHA256||!reflect.DeepEqual(a.Listeners,b.Listeners){
		t.Fatalf("profile order altered config identity: %v",err)
	}
}
