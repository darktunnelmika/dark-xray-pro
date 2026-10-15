package labrevision

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
	"github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func revisionCandidate(t *testing.T,operationID string,port int)(configstage.Metadata,[]byte){
	t.Helper()
	d,err:=routeplan.New("old-lab","new-lab")
	if err!=nil{t.Fatal(err)}
	if err:=d.SetDirect(1,port);err!=nil{t.Fatal(err)}
	preview,err:=d.Preview()
	if err!=nil{t.Fatal(err)}
	p:=xrayconfig.Profile{
		InboundID:1,Transport:"tcp",RealityTarget:"www.example.org:443",
		ServerNames:[]string{"www.example.org"},
		RealityPrivateKey:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x11},32)),
		ShortIDs:[]string{"0123456789abcdef"},
		Users:[]xrayconfig.User{{UUID:"af469f46-e18a-45f4-bf57-c7f649b10db5",Email:"lab@pro.invalid"}},
	}
	c,err:=xrayconfig.Render(preview,[]xrayconfig.Profile{p})
	if err!=nil{t.Fatal(err)}
	now:=time.Date(2026,10,15,9,0,0,0,time.UTC)
	meta:=configstage.Metadata{
		OperationID:operationID,NodeID:"new-lab",
		InstallationID:strings.Repeat("c",32),
		ManifestSHA256:preview.ManifestSHA256,ConfigSHA256:c.SHA256,
		TrustedXraySHA256:strings.Repeat("d",64),
		Listeners:c.Listeners,Status:configstage.Verified,Revision:2,
		CreatedAt:now,UpdatedAt:now,
		CanaryAttempted:true,CanaryLoopbackBound:true,
		CanActivate:false,NeedsDataPlaneProof:true,NeedsSubscriptionCommit:true,
	}
	return meta,c.Config
}

func newStore(t *testing.T)(*Store,[]byte,string){
	t.Helper()
	key:=make([]byte,32)
	if _,err:=rand.Read(key);err!=nil{t.Fatal(err)}
	path:=filepath.Join(t.TempDir(),"private","lab-revisions.json")
	s,err:=Open(path,key,nil)
	if err!=nil{t.Fatal(err)}
	return s,key,path
}

func TestEncryptedTwoRevisionSelectionAndRollbackSurvivesRestart(t *testing.T){
	s,key,path:=newStore(t)
	m1,raw1:=revisionCandidate(t,strings.Repeat("a",32),18569)
	v1,err:=s.Prepare(m1,raw1)
	if err!=nil{t.Fatal(err)}
	if v1.LiveApplied||v1.CanActivate||s.Snapshot().Generation!=2{
		t.Fatal("prepare activated a production process or did not version state")
	}
	one,err:=s.Select(v1.ID,2)
	if err!=nil{t.Fatal(err)}
	if one.Generation!=3||one.SelectedVersion!=v1.ID||one.ActualXrayProcessChanged{
		t.Fatalf("initial selection corrupted process state: %+v",one)
	}
	m2,raw2:=revisionCandidate(t,strings.Repeat("b",32),18570)
	v2,err:=s.Prepare(m2,raw2)
	if err!=nil{t.Fatal(err)}
	two,err:=s.Select(v2.ID,4)
	if err!=nil{t.Fatal(err)}
	if two.SelectedVersion!=v2.ID||two.PreviousVersion!=v1.ID||
		two.ActualXrayProcessChanged||!two.NeedsLiveHealthAndProtocolProof||
		!two.NeedsSubscriptionCommit{
		t.Fatalf("new revision made unsafe claims: %+v",two)
	}
	rolled,err:=s.RollbackSelection(5)
	if err!=nil{t.Fatal(err)}
	if rolled.SelectedVersion!=v1.ID||rolled.PreviousVersion!=""||
		rolled.Generation!=6||rolled.ActualXrayProcessChanged{
		t.Fatalf("rollback of LAB selection did not restore last known good candidate: %+v",rolled)
	}
	selected,private,err:=s.SelectedPrivate("new-lab",strings.Repeat("c",32))
	if err!=nil||selected.ID!=v1.ID||!bytes.Equal(private,raw1){
		t.Fatalf("rollback returned new or mismatched private config: %v",err)
	}
	if _,_,err:=s.SelectedPrivate("legacy-01",strings.Repeat("c",32));!errors.Is(err,ErrMissing){
		t.Fatal("old Node gained access to new LAB candidate")
	}
	if _,_,err:=s.SelectedPrivate("new-lab",strings.Repeat("e",32));!errors.Is(err,ErrMissing){
		t.Fatal("different installation can read lab revision")
	}
	stored,err:=os.ReadFile(path)
	if err!=nil{t.Fatal(err)}
	if bytes.Contains(stored,raw1)||bytes.Contains(stored,raw2)||
		bytes.Contains(stored,[]byte("privateKey")){
		t.Fatal("unencrypted REALITY private keys written to revision journal")
	}
	info,err:=os.Stat(path)
	if err!=nil||info.Mode().Perm()!=0600{t.Fatal("revision file is not private",err)}
	restarted,err:=Open(path,key,nil)
	if err!=nil{t.Fatal("rollback journal did not survive restart",err)}
	if !reflect.DeepEqual(restarted.Snapshot(),rolled){
		t.Fatal("journal restart did not retain selected previous version")
	}
	if _,err:=restarted.RollbackSelection(6);!errors.Is(err,ErrNoPrevious){
		t.Fatal("replayed rollback toggled back to failed config")
	}
	if _,err:=restarted.Select(v2.ID,5);!errors.Is(err,ErrConflict){
		t.Fatal("stale version acknowledgement overwrote current selection")
	}
}

func TestCanaryAndPrivateConfigMustBeVerifiedBeforePrepare(t *testing.T){
	s,_,_:=newStore(t)
	m,raw:=revisionCandidate(t,strings.Repeat("a",32),18569)
	for _,mutate:=range []func(*configstage.Metadata){
		func(x *configstage.Metadata){x.Status=configstage.Pending},
		func(x *configstage.Metadata){x.CanaryAttempted=false},
		func(x *configstage.Metadata){x.CanaryLoopbackBound=false},
		func(x *configstage.Metadata){x.CanActivate=true},
		func(x *configstage.Metadata){x.NeedsDataPlaneProof=false},
		func(x *configstage.Metadata){x.NeedsSubscriptionCommit=false},
		func(x *configstage.Metadata){x.TrustedXraySHA256="untrusted"},
	}{
		bad:=m
		mutate(&bad)
		if _,err:=s.Prepare(bad,raw);!errors.Is(err,ErrInvalid){
			t.Fatalf("unverified/canary-bypassing LAB candidate accepted: %+v %v",bad,err)
		}
	}
	modified:=append([]byte(nil),raw...)
	modified[0]='!'
	if _,err:=s.Prepare(m,modified);!errors.Is(err,ErrInvalid){
		t.Fatal("hash-changed private Reality config accepted")
	}
	if s.Snapshot().Generation!=1||len(s.Snapshot().Versions)!=0{
		t.Fatal("failed preflight changed revision journal")
	}
}

func TestReprepareIsIdempotentAndConflictingStageRefused(t *testing.T){
	s,_,_:=newStore(t)
	m,raw:=revisionCandidate(t,strings.Repeat("a",32),18569)
	first,err:=s.Prepare(m,raw)
	if err!=nil{t.Fatal(err)}
	second,err:=s.Prepare(m,raw)
	if err!=nil||first!=second||s.Snapshot().Generation!=2{
		t.Fatal("retry generated duplicate revision",err)
	}
	changed,_:=revisionCandidate(t,strings.Repeat("a",32),18570)
	if _,err:=s.Prepare(changed,raw);!errors.Is(err,ErrInvalid){
		t.Fatal("altered hash accepted with stale config")
	}
	changed=m
	changed.ConfigSHA256=strings.Repeat("e",64)
	if _,err:=s.Prepare(changed,raw);!errors.Is(err,ErrInvalid){
		t.Fatal("same stage ID changed config identity")
	}
}

func TestConcurrentCASSelectionHasOneWinner(t *testing.T){
	s,_,_:=newStore(t)
	m1,raw1:=revisionCandidate(t,strings.Repeat("a",32),18569)
	m2,raw2:=revisionCandidate(t,strings.Repeat("b",32),18570)
	a,err:=s.Prepare(m1,raw1)
	if err!=nil{t.Fatal(err)}
	b,err:=s.Prepare(m2,raw2)
	if err!=nil{t.Fatal(err)}
	var wg sync.WaitGroup
	result:=make(chan error,2)
	for _,id:=range []string{a.ID,b.ID}{
		wg.Add(1)
		go func(version string){
			defer wg.Done()
			_,err:=s.Select(version,3)
			result<-err
		}(id)
	}
	wg.Wait()
	close(result)
	success,conflict:=0,0
	for err:=range result{
		if err==nil{success++}else if errors.Is(err,ErrConflict){conflict++}else{t.Fatal(err)}
	}
	if success!=1||conflict!=1||s.Snapshot().Generation!=4{
		t.Fatalf("simultaneous owner revisions were not fenced: success=%d conflict=%d",success,conflict)
	}
}

func TestWrongEncryptionKeyCorruptionAndSymlinksRefused(t *testing.T){
	s,key,path:=newStore(t)
	m,raw:=revisionCandidate(t,strings.Repeat("a",32),18569)
	if _,err:=s.Prepare(m,raw);err!=nil{t.Fatal(err)}
	wrong:=bytes.Repeat([]byte{0xee},32)
	if _,err:=Open(path,wrong,nil);!errors.Is(err,ErrUnsafe){
		t.Fatal("wrong AES key decrypted private revision journal")
	}
	buf,err:=os.ReadFile(path)
	if err!=nil{t.Fatal(err)}
	var doc map[string]any
	if err:=json.Unmarshal(buf,&doc);err!=nil{t.Fatal(err)}
	entry:=doc["versions"].(map[string]any)
	for _,v:=range entry{v.(map[string]any)["ciphertext"]="AAAA";break}
	corrupted,err:=json.Marshal(doc)
	if err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(path,corrupted,0600);err!=nil{t.Fatal(err)}
	if _,err:=Open(path,key,nil);!errors.Is(err,ErrUnsafe){
		t.Fatal("tampered ciphertext loaded")
	}
	if err:=os.WriteFile(path,buf,0644);err!=nil{t.Fatal(err)}
	if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
	if _,err:=Open(path,key,nil);!errors.Is(err,ErrUnsafe){
		t.Fatal("world readable private revision state accepted")
	}
	if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
	link:=filepath.Join(filepath.Dir(path),"alias.json")
	if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
	if _,err:=Open(link,key,nil);!errors.Is(err,ErrUnsafe){
		t.Fatal("symlink private revision state accepted")
	}
}

func TestRevisionPointerAuditAndGenerationAreAuthenticated(t *testing.T){
	s,key,path:=newStore(t)
	m1,raw1:=revisionCandidate(t,strings.Repeat("a",32),18569)
	m2,raw2:=revisionCandidate(t,strings.Repeat("b",32),18570)
	v1,err:=s.Prepare(m1,raw1)
	if err!=nil{t.Fatal(err)}
	v2,err:=s.Prepare(m2,raw2)
	if err!=nil{t.Fatal(err)}
	if _,err:=s.Select(v1.ID,3);err!=nil{t.Fatal(err)}
	if _,err:=s.Select(v2.ID,4);err!=nil{t.Fatal(err)}
	original,err:=os.ReadFile(path)
	if err!=nil{t.Fatal(err)}
	for _,tamper:=range []func(map[string]any){
		func(doc map[string]any){doc["selected"]=v1.ID},
		func(doc map[string]any){doc["generation"]=float64(999)},
		func(doc map[string]any){doc["events"].([]any)[0].(map[string]any)["action"]="forged-owner-approval"},
	}{
		var decoded map[string]any
		if err:=json.Unmarshal(original,&decoded);err!=nil{t.Fatal(err)}
		tamper(decoded)
		written,err:=json.Marshal(decoded)
		if err!=nil{t.Fatal(err)}
		if err:=os.WriteFile(path,written,0600);err!=nil{t.Fatal(err)}
		if _,err:=Open(path,key,nil);!errors.Is(err,ErrUnsafe){
			t.Fatalf("unauthenticated journal selection/event modification loaded: %v",err)
		}
	}
	if err:=os.WriteFile(path,original,0600);err!=nil{t.Fatal(err)}
	if _,err:=Open(path,key,nil);err!=nil{
		t.Fatalf("untampered revision journal did not recover: %v",err)
	}
}

func TestSelectionsCannotCrossNodeInstallations(t *testing.T){
	s,_,_:=newStore(t)
	m1,raw1:=revisionCandidate(t,strings.Repeat("a",32),18569)
	m2,raw2:=revisionCandidate(t,strings.Repeat("b",32),18570)
	m2.NodeID="different-new-lab"
	m2.InstallationID=strings.Repeat("e",32)
	v1,err:=s.Prepare(m1,raw1)
	if err!=nil{t.Fatal(err)}
	v2,err:=s.Prepare(m2,raw2)
	if err!=nil{t.Fatal(err)}
	if _,err:=s.Select(v1.ID,3);err!=nil{t.Fatal(err)}
	if _,err:=s.Select(v2.ID,4);!errors.Is(err,ErrConflict){
		t.Fatalf("one Node was replaced by another Node's candidate: %v",err)
	}
	snap:=s.Snapshot()
	if snap.SelectedVersion!=v1.ID||snap.PreviousVersion!=""||snap.Generation!=4{
		t.Fatalf("cross-Node selection changed live journal: %+v",snap)
	}
}
