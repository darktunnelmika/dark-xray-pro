//go:build linux

package xraycanary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func candidate(t *testing.T, multiple bool) xrayconfig.Candidate {
	t.Helper()
	d,err:=routeplan.New("retiring-lab","fresh-lab")
	if err!=nil{t.Fatal(err)}
	if err:=d.SetDirect(1,18569);err!=nil{t.Fatal(err)}
	if multiple {
		if err:=d.SetSwap(1,16800,11185,"10.20.10.2");err!=nil{t.Fatal(err)}
	}
	preview,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	profile:=xrayconfig.Profile{
		InboundID:1,Transport:"tcp",RealityTarget:"www.example.org:443",
		ServerNames:[]string{"www.example.org"},ShortIDs:[]string{"abcdef0123456789"},
		RealityPrivateKey:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x11},32)),
		Users:[]xrayconfig.User{{UUID:"af469f46-e18a-45f4-bf57-c7f649b10db5",Email:"lab@pro.invalid"}},
	}
	c,err:=xrayconfig.Render(preview,[]xrayconfig.Profile{profile})
	if err!=nil{t.Fatal(err)}
	return c
}

func fixture(t *testing.T,kind string)(string,string){
	t.Helper()
	// The test double implements the Xray "run -test -config" and "run
	// -config" command-line contract. It NEVER uses a real production Xray.
	var command string
	switch kind {
	case "listen":
		command=`exec python3 -u -c 'import json,socket,sys,time
with open(sys.argv[1],encoding="utf-8") as f: c=json.load(f)
servers=[]
for x in c["inbounds"]:
 s=socket.socket(socket.AF_INET,socket.SOCK_STREAM)
 s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
 s.bind((x["listen"],x["port"]))
 s.listen(4)
 servers.append(s)
time.sleep(30)' "$3"`
	case "reject":command="exit 19"
	case "sleep":command="exec sleep 30"
	default:t.Fatal("unknown fixture kind")
	}
	script:="#!/bin/sh\nset -eu\n[ \"$1\" = \"run\" ] || exit 10\n"+
		"if [ \"$2\" = \"-test\" ]; then [ \"$3\" = \"-config\" ] || exit 11; exit 0; fi\n"+
		"[ \"$2\" = \"-config\" ] || exit 12\n"+command+"\n"
	path:=filepath.Join(t.TempDir(),"pinned-xray-fixture")
	if err:=os.WriteFile(path,[]byte(script),0700);err!=nil{t.Fatal(err)}
	digest:=sha256.Sum256([]byte(script))
	return path,hex.EncodeToString(digest[:])
}

func lab(t *testing.T){
	t.Helper()
	if os.Geteuid()==0{t.Skip("canary must never run as root")}
	t.Setenv("DARK_PRO_LAB_MODE","1")
	t.Setenv("DARK_PRO_ENABLE_LOOPBACK_CANARY","1")
}

func TestLoopbackRebaseNeverBindsActualPortsOrLeaksRealityConfig(t *testing.T){
	c:=candidate(t,true)
	before:=append([]byte(nil),c.Config...)
	converted,ports,err:=remap(c)
	if err!=nil{t.Fatal(err)}
	if len(ports)!=2{t.Fatalf("expected both selected Direct/Tunnel listeners, got %d",len(ports))}
	if bytes.Equal(before,converted){t.Fatal("canary copied public listener ports")}
	if !bytes.Equal(c.Config,before){t.Fatal("canary mutated caller's original private config")}
	for _,port:=range ports{
		for _,listener:=range c.Listeners{
			if port==int(listener.NodePort){t.Fatal("canary reserved an actual new Node production port")}
		}
	}
	var doc struct{Inbounds []struct{
		Listen string `json:"listen"`
		Port int `json:"port"`
		Tag string `json:"tag"`
	} `json:"inbounds"`}
	if err:=json.Unmarshal(converted,&doc);err!=nil{t.Fatal(err)}
	if len(doc.Inbounds)!=2{t.Fatal("lost one inbound during loopback remap")}
	for _,in:=range doc.Inbounds{
		if in.Listen!="127.0.0.1"||in.Port==0{
			t.Fatalf("Xray canary would expose public listener: %+v",in)
		}
	}
	if !bytes.Contains(converted,[]byte("privateKey")){
		t.Fatal("test fixture was not an authentic Reality candidate")
	}
	ok,err:=ValidateRemappedOnly(c)
	if err!=nil||!ok{t.Fatalf("rebase safety check failed: %v",err)}
}

func TestBoundCanaryGetsStoppedBeforeReportingResult(t *testing.T){
	lab(t)
	c:=candidate(t,true)
	binary,pin:=fixture(t,"listen")
	start:=time.Now()
	proof,err:=Run(context.Background(),c,binary,pin)
	if err!=nil{t.Fatal(err)}
	if time.Since(start)>8*time.Second{t.Fatal("canary exceeded bounded startup")}
	if !proof.SyntaxValidated||!proof.LoopbackBound||proof.RemappedListeners!=2||
		proof.CanActivate||proof.DataPlaneVerified||!proof.NeedsRollbackPlan||
		!proof.NeedsSubscriptionCommit||
		proof.Status!="loopback_canary_bound_then_stopped_not_active"{
		t.Fatalf("bound TCP sockets incorrectly became production-ready: %+v",proof)
	}
}

func TestDeadCanaryAndCancellationFailWithoutPromoting(t *testing.T){
	lab(t)
	c:=candidate(t,false)
	binary,pin:=fixture(t,"reject")
	result,err:=Run(context.Background(),c,binary,pin)
	if !errors.Is(err,ErrNotReady)||result.CanActivate||result.LoopbackBound{
		t.Fatalf("dead canary promoted after failed process startup: %+v %v",result,err)
	}
	binary,pin=fixture(t,"sleep")
	ctx,cancel:=context.WithTimeout(context.Background(),150*time.Millisecond)
	defer cancel()
	start:=time.Now()
	result,err=Run(ctx,c,binary,pin)
	if !errors.Is(err,ErrNotReady)||result.CanActivate {
		t.Fatalf("unready canary did not stop: %+v %v",result,err)
	}
	if time.Since(start)>2*time.Second {t.Fatal("cancellation did not stop the canary process group")}
}

func TestRootAndNonLabOptInRefused(t *testing.T){
	c:=candidate(t,false)
	binary,pin:=fixture(t,"listen")
	t.Setenv("DARK_PRO_LAB_MODE","")
	t.Setenv("DARK_PRO_ENABLE_LOOPBACK_CANARY","1")
	p,err:=Run(context.Background(),c,binary,pin)
	if !errors.Is(err,ErrStart)||p.CanActivate {
		t.Fatal("canary ran outside isolated LAB mode")
	}
	t.Setenv("DARK_PRO_LAB_MODE","1")
	t.Setenv("DARK_PRO_ENABLE_LOOPBACK_CANARY","")
	if _,err:=Run(context.Background(),c,binary,pin);!errors.Is(err,ErrStart){
		t.Fatal("canary ran without deliberate opt-in")
	}
}

func TestForgedListenerOrTamperedConfigIsRejectedBeforeSubprocess(t *testing.T){
	c:=candidate(t,false)
	original:=append([]byte(nil),c.Config...)
	c.Listeners[0].Tag="forged-public-tunnel"
	if _,_,err:=remap(c);!errors.Is(err,ErrCandidate){
		t.Fatalf("forged Listener metadata matched an untrusted private config: %v",err)
	}
	c=candidate(t,false)
	c.Config=append([]byte(nil),c.Config...)
	c.Config[0]='!'
	if _,_,err:=remap(c);!errors.Is(err,ErrCandidate){
		t.Fatalf("tampered config was used to start private Xray canary: %v",err)
	}
	if reflect.DeepEqual(c.Config,original){t.Fatal("test setup failed to tamper candidate")}
	c=candidate(t,false)
	if strings.Contains(string(c.Config),`"listen": "127.0.0.1"`){
		t.Fatal("renderer unexpectedly bound the original candidate to loopback")
	}
	// Proof of SYN accept is not an application-layer VLESS handshake.
}
