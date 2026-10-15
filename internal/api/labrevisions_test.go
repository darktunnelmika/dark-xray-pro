package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
	"github.com/darktunnelmika/dark-xray-pro/internal/labrevision"
)

func labRevisionServer(t *testing.T)(*Server,*labrevision.Store,configstage.Metadata){
	t.Helper()
	s,_,op,profiles:=setupStage(t)
	staged:=request(t,s.Handler(),"POST",
		"/api/v1/admin/replacements/"+op.ID+"/candidate",ownerToken,
		map[string]any{"profiles":profiles,"trusted_xray_sha256":expectedXraySHA})
	if staged.Code!=201{t.Fatalf("could not create private test stage: %s",staged.Body.String())}
	var metadata configstage.Metadata
	if err:=json.Unmarshal(staged.Body.Bytes(),&metadata);err!=nil{t.Fatal(err)}
	proof:=configstage.Proof{
		OperationID:op.ID,NodeID:metadata.NodeID,InstallationID:metadata.InstallationID,
		ManifestSHA256:metadata.ManifestSHA256,ConfigSHA256:metadata.ConfigSHA256,
		TrustedXraySHA256:metadata.TrustedXraySHA256,
		SyntaxValid:true,CanaryAttempted:true,CanaryLoopbackBound:true,
	}
	report:=request(t,s.Handler(),"POST","/api/v1/agent/config-stage-report",
		newToken,proof)
	// The plain localhost Agent handler deliberately refuses secret delivery,
	// but the read-only report is accepted through the existing internal API.
	if report.Code!=200{t.Fatalf("authentic canary result rejected: %d %s",report.Code,report.Body.String())}
	versions,err:=labrevision.Open(filepath.Join(t.TempDir(),"lab-only","versions.json"),
		bytes.Repeat([]byte{0x55},32),nil)
	if err!=nil{t.Fatal(err)}
	if err:=s.EnableLabRevisions(versions);err!=nil{t.Fatal(err)}
	metadata,err=s.stages.Get(op.ID)
	if err!=nil{t.Fatal(err)}
	return s,versions,metadata
}

func TestOwnerOnlyLabSelectionNeverAppliesPublicXray(t *testing.T){
	s,store,metadata:=labRevisionServer(t)
	h:=s.Handler()
	uri:="/api/v1/admin/lab-revisions/"+metadata.OperationID+"/prepare"
	if w:=request(t,h,"POST",uri,"",nil);w.Code!=401{
		t.Fatal("unauthenticated selection could read private Reality config")
	}
	if w:=request(t,h,"POST",uri,newToken,nil);w.Code!=401{
		t.Fatal("paired Agent token gained owner revision rights")
	}
	ok:=request(t,h,"POST",uri,ownerToken,nil)
	if ok.Code!=201{t.Fatalf("validated canary was not eligible: %d %s",ok.Code,ok.Body.String())}
	var version labrevision.Version
	if err:=json.Unmarshal(ok.Body.Bytes(),&version);err!=nil{t.Fatal(err)}
	if version.ConfigSHA256!=metadata.ConfigSHA256||version.LiveApplied||version.CanActivate{
		t.Fatalf("LAB revision claimed customer deployment: %+v",version)
	}
	if bytes.Contains(ok.Body.Bytes(),[]byte("privateKey"))||
		bytes.Contains(ok.Body.Bytes(),[]byte("ciphertext")){
		t.Fatal("Reality secret leaked from LAB metadata endpoint")
	}
	list:=request(t,h,"GET","/api/v1/admin/lab-revisions",ownerToken,nil)
	if list.Code!=200{t.Fatal("owner journal unreadable")}
	var snap labrevision.Snapshot
	if err:=json.Unmarshal(list.Body.Bytes(),&snap);err!=nil{t.Fatal(err)}
	if snap.Generation!=2||snap.SelectedVersion!=""||snap.ActualXrayProcessChanged||
		!snap.NeedsLiveHealthAndProtocolProof{
		t.Fatalf("new revision incorrectly changed live Xray: %+v",snap)
	}
	if w:=request(t,h,"POST",uri,ownerToken,nil);w.Code!=201{
		t.Fatal("idempotent prepare was rejected",w.Code)
	}
	selectURL:="/api/v1/admin/lab-revisions/select"
	if w:=request(t,h,"POST",selectURL,newToken,
		map[string]any{"version_id":version.ID,"expected_generation":2});w.Code!=401{
		t.Fatal("Node authenticated as owner")
	}
	if w:=request(t,h,"POST",selectURL,ownerToken,
		map[string]any{"version_id":version.ID,"expected_generation":1});w.Code!=409{
		t.Fatal("stale CAS selected new revision")
	}
	choice:=request(t,h,"POST",selectURL,ownerToken,
		map[string]any{"version_id":version.ID,"expected_generation":2})
	if choice.Code!=200{t.Fatal("selection failed",choice.Code,choice.Body.String())}
	if err:=json.Unmarshal(choice.Body.Bytes(),&snap);err!=nil{t.Fatal(err)}
	if snap.SelectedVersion!=version.ID||snap.Generation!=3||
		snap.ActualXrayProcessChanged||!snap.NeedsSubscriptionCommit{
		t.Fatalf("private LAB selection incorrectly applied to Xray: %+v",snap)
	}
	if w:=request(t,h,"POST","/api/v1/admin/lab-revisions/rollback-selection",
		ownerToken,map[string]any{"expected_generation":2});w.Code!=409{
		t.Fatal("stale rollback generation accepted")
	}
	if w:=request(t,h,"POST","/api/v1/admin/lab-revisions/rollback-selection",
		ownerToken,map[string]any{"expected_generation":3});w.Code!=409{
		t.Fatal("absent prior LAB candidate created a phantom rollback")
	}
	if len(store.Snapshot().Versions)!=1{
		t.Fatal("version retries duplicated encrypted secret configs")
	}
	for _,endpoint:=range []string{
		"/api/v1/admin/lab-revisions/activate",
		"/api/v1/admin/lab-revisions/"+metadata.OperationID+"/apply",
		"/api/v1/admin/subscriptions/publish",
	}{
		if w:=request(t,h,"POST",endpoint,ownerToken,nil);w.Code!=404{
			t.Fatalf("dangerous live operation accidentally exposed: %s %d",endpoint,w.Code)
		}
	}
}

func TestInternetFacingAgentTLSHasNoLABRevisionOwnerAPI(t *testing.T){
	s,_,_:=labRevisionServer(t)
	for _,action:=range []struct{method,path string}{
		{"GET","/api/v1/admin/lab-revisions"},
		{"POST","/api/v1/admin/lab-revisions/select"},
		{"POST","/api/v1/admin/lab-revisions/rollback-selection"},
	}{
		req:=httptest.NewRequest(action.method,action.path,nil)
		req.TLS=&tls.ConnectionState{Version:tls.VersionTLS13}
		req.Header.Set("Authorization","Bearer "+ownerToken)
		w:=httptest.NewRecorder()
		s.AgentHandler().ServeHTTP(w,req)
		if w.Code!=http.StatusNotFound{
			t.Errorf("internet Agent listener leaked owner route %s with HTTP %d",action.path,w.Code)
		}
	}
}
