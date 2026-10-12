package routeplan

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestNoLegacyTunnelInheritanceAndDirectCanCoexist(t *testing.T){
	d,err:=New("de-old","de-fresh")
	if err!=nil{t.Fatal(err)}
	if _,err:=d.Preview();!errors.Is(err,ErrEmpty){
		t.Fatalf("new replacement inherited old routes: %v",err)
	}
	if err:=d.SetDirect(1,8569);err!=nil{t.Fatal(err)}
	if err:=d.SetSwap(1,1185,1185,"10.10.0.2");err!=nil{t.Fatal(err)}
	preview,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	if preview.CanActivate || preview.HasInheritedOldRoutes ||
		!preview.NeedsNewAgentVerification || !preview.NeedsReachabilityProbes ||
		preview.Status!="requires_new_agent_and_route_preflight" {
		t.Fatalf("unverified destination was claimed active: %+v",preview)
	}
	if preview.OldNodeID!="de-old" || preview.NewNodeID!="de-fresh" ||
		len(preview.Deployments)!=1 || preview.Deployments[0].Direct==nil ||
		preview.Deployments[0].Swap==nil {
		t.Fatalf("lost Direct when adding Tunnel: %+v",preview)
	}
	if preview.Deployments[0].Swap.TunnelPort!=1185||
		preview.Deployments[0].Swap.NodePort!=1185||
		preview.Deployments[0].Swap.TunnelAddress!="10.10.0.2" {
		t.Fatalf("three-field Swap changed: %+v",preview.Deployments[0])
	}
	// Callers must not mutate internal state through the preview.
	preview.Deployments[0].Swap.TunnelAddress="203.0.113.1"
	preview.Deployments[0].Direct.NodePort=1
	next,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	if next.Deployments[0].Direct.NodePort!=8569 ||
		next.Deployments[0].Swap.TunnelAddress!="10.10.0.2" {
		t.Fatal("preview mutated the future replacement plan")
	}
	d.RemoveSwap(1)
	retained,err:=d.Preview()
	if err!=nil||retained.Deployments[0].Direct==nil||retained.Deployments[0].Swap!=nil{
		t.Fatal("removing a tunnel removed Direct deployment")
	}
	d.RemoveDirect(1)
	if _,err:=d.Preview();!errors.Is(err,ErrEmpty){
		t.Fatal("empty replacement exposed inherited routes")
	}
}

func TestManualRecreatedRouteAndDistinctPorts(t *testing.T){
	d,err:=New("nl-old","nl-new")
	if err!=nil{t.Fatal(err)}
	if err:=d.SetSwap(80,6800,1185,"tunnel.example.org");err!=nil{t.Fatal(err)}
	if err:=d.SetDirect(80,8569);err!=nil{t.Fatal(err)}
	plan,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	if plan.Deployments[0].Swap.TunnelPort!=6800 ||
		plan.Deployments[0].Swap.NodePort!=1185 ||
		plan.Deployments[0].Direct.NodePort!=8569 {
		t.Fatal("new Direct/Tunnel ports were not independent")
	}
	original:=plan.ManifestSHA256
	if len(original)!=64{t.Fatal("invalid stable manifest")}
	if err:=d.SetSwap(80,6800,1185,"2001:db8::1");err!=nil{t.Fatal(err)}
	changed,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	if changed.ManifestSHA256==original{
		t.Fatal("altered address reused previous confirmation digest")
	}
}

func TestRejectAmbiguousInjectionAndBadPorts(t *testing.T){
	for _,ids:=range[][2]string{{"","node"},{"old","old"},{"../old","new"},{"old","new/host"}}{
		if _,err:=New(ids[0],ids[1]);!errors.Is(err,ErrInvalidNode){
			t.Errorf("invalid identity permitted: %q %q",ids[0],ids[1])
		}
	}
	d,_:=New("old","new")
	for _,p:=range []int{-1,0,65536} {
		if err:=d.SetDirect(1,p);!errors.Is(err,ErrInvalidRoute){t.Fatalf("bad direct port %d",p)}
		if err:=d.SetSwap(1,p,1185,"10.0.0.2");!errors.Is(err,ErrInvalidRoute){t.Fatalf("bad tunnel port %d",p)}
		if err:=d.SetSwap(1,1185,p,"10.0.0.2");!errors.Is(err,ErrInvalidRoute){t.Fatalf("bad Node port %d",p)}
	}
	for _,address:=range[]string{
		"", "http://example.com","10.0.0.1:1185", "bad@host.test",
		"../old", " tunnel.example.com", "tunnel.example.com/path",
		"a..example.org", strings.Repeat("x",254),
	} {
		if err:=d.SetSwap(1,1185,1185,address);!errors.Is(err,ErrInvalidRoute){
			t.Errorf("unsafe tunnel address permitted: %q",address)
		}
	}
	if err:=d.SetDirect(0,8569);!errors.Is(err,ErrInvalidRoute){t.Fatal("zero Inbound accepted")}
	if err:=d.SetSwap(0,1185,1185,"10.10.0.1");!errors.Is(err,ErrInvalidRoute){t.Fatal("zero Inbound accepted")}
	if _,err:=d.Preview();!errors.Is(err,ErrEmpty){t.Fatal("invalid attempts changed plan")}
}

func TestStableSortedDigestAndConcurrentSelections(t *testing.T){
	a,_:=New("old","fresh")
	b,_:=New("old","fresh")
	if err:=a.SetSwap(9,1185,1185,"10.1.1.1");err!=nil{t.Fatal(err)}
	if err:=a.SetDirect(3,8569);err!=nil{t.Fatal(err)}
	if err:=b.SetDirect(3,8569);err!=nil{t.Fatal(err)}
	if err:=b.SetSwap(9,1185,1185,"10.1.1.1");err!=nil{t.Fatal(err)}
	ap,_:=a.Preview()
	bp,_:=b.Preview()
	if ap.ManifestSHA256!=bp.ManifestSHA256 ||
		ap.Deployments[0].InboundID!=3 || ap.Deployments[1].InboundID!=9 {
		t.Fatal("equivalent route choices produced different preview")
	}
	var wg sync.WaitGroup
	for i:=1;i<=30;i++{
		wg.Add(1)
		go func(id uint64){
			defer wg.Done()
			if err:=a.SetDirect(id,8569);err!=nil{t.Error(err)}
			if _,err:=a.Preview();err!=nil{t.Error(err)}
		}(uint64(i))
	}
	wg.Wait()
}
