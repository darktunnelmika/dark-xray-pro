package agentidentity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	code="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherCode="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestEnrollmentIdentityCanRecoverAfterLostResponse(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"agent","identity.json")
	first,err:=LoadOrCreate(path,"de-new-01",code)
	if err!=nil {t.Fatal(err)}
	if first.Paired || first.PairCode!=code || len(first.AgentToken)!=64 || len(first.InstallationID)!=32 {
		t.Fatalf("new agent identity invalid: %+v",first)
	}
	stat,err:=os.Stat(path)
	if err!=nil {t.Fatal(err)}
	if stat.Mode().Perm()!=0600 {t.Fatalf("identity must be private: %o",stat.Mode().Perm())}
	persisted,err:=LoadOrCreate(path,"de-new-01","")
	if err!=nil || persisted!=first {t.Fatalf("lost-response retry changed credentials: %v",err)}
	if _,err:=LoadOrCreate(path,"de-new-01",otherCode);!errors.Is(err,ErrInvalid) {
		t.Fatalf("new Pair Code changed pending credentials: %v",err)
	}
	paired,err:=MarkPaired(path,first)
	if err!=nil {t.Fatal(err)}
	if !paired.Paired || paired.PairCode!="" || paired.AgentToken!=first.AgentToken {
		t.Fatal("successful pairing did not retain the same Agent identity")
	}
	raw,err:=os.ReadFile(path)
	if err!=nil {t.Fatal(err)}
	if strings.Contains(string(raw),code) {t.Fatal("completed identity retained Pair Code")}
	if _,err:=LoadOrCreate(path,"de-new-01",code);!errors.Is(err,ErrInvalid) {
		t.Fatalf("already-paired identity accepted a second Pair Code: %v",err)
	}
	final,err:=SecureRead(path,"de-new-01")
	if err!=nil || final!=paired {t.Fatalf("restart lost durable identity: %v",err)}
}

func TestMustNotInventOrReuseLegacyIdentity(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"private","agent.json")
	if _,err:=LoadOrCreate(path,"de-01","");!errors.Is(err,ErrPairRequired) {
		t.Fatalf("created identity without enrollment: %v",err)
	}
	if _,err:=LoadOrCreate(path,"../escape",code);!errors.Is(err,ErrInvalid) {
		t.Fatalf("path-like node ID accepted: %v",err)
	}
	if _,err:=LoadOrCreate(path,"de-01","bad");!errors.Is(err,ErrInvalid) {
		t.Fatalf("invalid code accepted: %v",err)
	}
	initial,err:=LoadOrCreate(path,"de-01",code)
	if err!=nil {t.Fatal(err)}
	if _,err:=LoadOrCreate(path,"nl-01","");!errors.Is(err,ErrInvalid) {
		t.Fatalf("reused a Node identity across logical IDs: %v",err)
	}
	if _,err:=MarkPaired(path,Identity{NodeID:initial.NodeID,InstallationID:initial.InstallationID,AgentToken:initial.AgentToken,PairCode:otherCode});!errors.Is(err,ErrInvalid) {
		t.Fatalf("replaced pending Agent key on a mismatched write: %v",err)
	}
}

func TestUnsafePermissionsAndSymlinkRefused(t *testing.T) {
	parent:=filepath.Join(t.TempDir(),"private")
	if err:=os.Mkdir(parent,0700);err!=nil {t.Fatal(err)}
	path:=filepath.Join(parent,"id.json")
	_,err:=LoadOrCreate(path,"de-01",code)
	if err!=nil {t.Fatal(err)}
	if err:=os.Chmod(path,0644);err!=nil {t.Fatal(err)}
	if _,err:=LoadOrCreate(path,"de-01","");!errors.Is(err,ErrUnsafeFile) {
		t.Fatalf("world-readable credential accepted: %v",err)
	}
	if err:=os.Chmod(path,0600);err!=nil {t.Fatal(err)}
	link:=filepath.Join(parent,"link.json")
	if err:=os.Symlink(path,link);err!=nil {t.Fatal(err)}
	if _,err:=LoadOrCreate(link,"de-01","");!errors.Is(err,ErrUnsafeFile) {
		t.Fatalf("symlink credential accepted: %v",err)
	}
	if err:=os.Chmod(parent,0755);err!=nil {t.Fatal(err)}
	if _,err:=LoadOrCreate(path,"de-01","");!errors.Is(err,ErrUnsafeFile) {
		t.Fatalf("unsafe parent directory accepted: %v",err)
	}
}
