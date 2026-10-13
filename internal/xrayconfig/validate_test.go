package xrayconfig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testBinary(t *testing.T, script string)(string,string){
	t.Helper()
	if runtime.GOOS!="linux"{t.Skip("offline Xray core command test requires Linux")}
	path:=filepath.Join(t.TempDir(),"test-xray")
	if err:=os.WriteFile(path,[]byte(script),0700);err!=nil{t.Fatal(err)}
	sum:=sha256.Sum256([]byte(script))
	return path,hex.EncodeToString(sum[:])
}

func TestPrivateOfflineSyntaxPassWithoutDeployment(t *testing.T){
	binary,pin:=testBinary(t,`#!/bin/sh
set -eu
[ "$#" -eq 4 ]
[ "$1" = "run" ] && [ "$2" = "-test" ] && [ "$3" = "-config" ]
[ -r "$4" ] && [ "$(stat -c %a "$4")" = "600" ]
grep -q '"protocol": "vless"' "$4"
grep -q '"statsInboundUplink": true' "$4"
`)
	c,err:=Render(planFor(t,8569,1185),[]Profile{testProfile(1)})
	if err!=nil{t.Fatal(err)}
	proof,err:=ValidateOffline(context.Background(),c,binary,pin)
	if err!=nil{t.Fatal(err)}
	if !proof.XraySyntaxValidated||proof.CanActivate||!proof.NeedsDataPlaneProof||
		!proof.NeedsSubscriptionCommit||proof.ConfigSHA256!=c.SHA256||
		proof.ManifestSHA256!=c.ManifestSHA256 {
		t.Fatalf("offline config validation made production activation claims: %+v",proof)
	}
	meta,_:=json.Marshal(proof)
	if bytes.Contains(meta,[]byte(testProfile(1).RealityPrivateKey)){
		t.Fatal("core validation proof leaks Reality secret")
	}
}

func TestRejectTamperedCandidateAndIncorrectBinaryPin(t *testing.T){
	binary,pin:=testBinary(t,"#!/bin/sh\nexit 0\n")
	c,err:=Render(planFor(t,8569,0),[]Profile{testProfile(1)})
	if err!=nil{t.Fatal(err)}
	altered:=c
	altered.Config=append([]byte(nil),c.Config...)
	altered.Config[0]='!'
	if _,err:=ValidateOffline(context.Background(),altered,binary,pin);!errors.Is(err,ErrCandidateInvalid){
		t.Fatalf("tampered Reality candidate was validated: %v",err)
	}
	if _,err:=ValidateOffline(context.Background(),c,binary,strings.Repeat("0",64));!errors.Is(err,ErrCoreMismatch){
		t.Fatalf("untrusted binary was executed: %v",err)
	}
	if _,err:=ValidateOffline(context.Background(),c,"xray",pin);!errors.Is(err,ErrCoreUnavailable){
		t.Fatalf("PATH-based arbitrary executable allowed: %v",err)
	}
	other:=filepath.Join(t.TempDir(),"symlink")
	if err:=os.Symlink(binary,other);err!=nil{t.Fatal(err)}
	if _,err:=ValidateOffline(context.Background(),c,other,pin);!errors.Is(err,ErrCoreUnavailable){
		t.Fatalf("symlink Xray was accepted: %v",err)
	}
	if err:=os.Chmod(binary,0777);err!=nil{t.Fatal(err)}
	if _,err:=ValidateOffline(context.Background(),c,binary,pin);!errors.Is(err,ErrCoreUnavailable){
		t.Fatalf("world-writable Xray binary was accepted: %v",err)
	}
}

func TestCoreErrorAndTimeoutDoNotReturnSecrets(t *testing.T){
	reject,pin:=testBinary(t,"#!/bin/sh\necho PRIVATE-KEY-MUST-NOT-LEAK >&2\nexit 23\n")
	c,err:=Render(planFor(t,8080,0),[]Profile{testProfile(1)})
	if err!=nil{t.Fatal(err)}
	if _,err:=ValidateOffline(context.Background(),c,reject,pin);!errors.Is(err,ErrCoreRejected){
		t.Fatalf("rejected config reported success: %v",err)
	}else if strings.Contains(err.Error(),"PRIVATE-KEY")||strings.Contains(err.Error(),testProfile(1).RealityPrivateKey){
		t.Fatal("Xray error message leaked private config")
	}
	slow,slowPin:=testBinary(t,"#!/bin/sh\nexec sleep 30\n")
	ctx,cancel:=context.WithTimeout(context.Background(),100*time.Millisecond)
	defer cancel()
	start:=time.Now()
	if _,err:=ValidateOffline(ctx,c,slow,slowPin);!errors.Is(err,ErrCoreRejected){
		t.Fatalf("unbounded or unreported hung validator: %v",err)
	}
	if time.Since(start)>2*time.Second {t.Fatal("cancelled validation exceeded bounded latency")}
}
