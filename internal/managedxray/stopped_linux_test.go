//go:build linux

package managedxray

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
func stoppedScenario(t *testing.T,binary,pin string){
 t.Helper();o:=testOptions(t)
 if binary!=""{o.Binary=binary;o.BinarySHA=pin}
 r:=mustOpen(t,o);port:=freePort(t);active:=testCandidate(t,port,"one")
 if _,e:=r.Apply(context.Background(),Revision{1,active},grant(t,o,1,active,40*time.Second));e!=nil{t.Fatal(e)}
 assertPort(t,port,true)
 stopped,e:=xrayconfig.RenderStopped(o.NodeID,o.InstallationID,o.ListenIP);if e!=nil{t.Fatal(e)}
 s,e:=r.Apply(context.Background(),Revision{2,stopped},grant(t,o,2,stopped,40*time.Second))
 if e!=nil||!s.ProcessReady||!s.NoListeners||s.ActiveSequence!=2||s.DataPlaneVerified||s.SubscriptionPublished{t.Fatalf("explicit no-listener apply %+v %v",s,e)}
 assertPort(t,port,false)
 // A crashed empty core must not automatically roll back to listening.
 r.mu.Lock();p:=r.process;r.mu.Unlock()
 if e=p.cmd.Process.Kill();e!=nil{t.Fatal(e)}
 waitFor(t,func()bool{return r.Snapshot().Phase=="process_exited_stopped"})
 assertPort(t,port,false)
 r.Close();r2:=mustOpen(t,o)
 s,e=r2.Recover(context.Background(),grant(t,o,2,stopped,40*time.Second))
 if e!=nil||!s.ProcessReady||!s.NoListeners{t.Fatalf("empty recovery %+v %v",s,e)}
 assertPort(t,port,false)
 // Only a fresh owner-authorized higher revision can reopen listeners.
 s,e=r2.Apply(context.Background(),Revision{3,active},grant(t,o,3,active,40*time.Second))
 if e!=nil||!s.ProcessReady||s.NoListeners{t.Fatalf("explicit resume %+v %v",s,e)}
 assertPort(t,port,true)
}
func TestNativeStoppedRevisionClosesPortsAndSurvivesCrash(t *testing.T){stoppedScenario(t,"","")}
func TestRealPinnedNativeStoppedRevisionClosesPortsAndSurvivesCrash(t *testing.T){
 binary,pin:=os.Getenv("DARK_PRO_TEST_XRAY_BINARY"),os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
 if binary==""&&pin==""{t.Skip("explicit pinned real core not provided")}
 if binary==""||pin==""{t.Fatal("incomplete pinned core contract")}
 stoppedScenario(t,binary,pin)
}
func TestStoppedMarkerCannotHideAnAPIListener(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o)
 c,e:=xrayconfig.RenderStopped(o.NodeID,o.InstallationID,o.ListenIP);if e!=nil{t.Fatal(e)}
 c.Config=[]byte(`{"inbounds":[],"api":{"listen":"127.0.0.1:28888","services":["HandlerService"]}}`)
 h:=sha256.Sum256(c.Config);c.SHA256=hex.EncodeToString(h[:])
 if _,e=r.Apply(context.Background(),Revision{1,c},grant(t,o,1,c,40*time.Second));!errors.Is(e,ErrInvalid){t.Fatal("fake empty config accepted",e)}
 if r.Snapshot().ProcessReady{t.Fatal("unapproved API listener started")}
}

func TestFailedRestrictiveApplyCannotReopenRevokedUUID(t *testing.T){
 o:=testOptions(t);r:=mustOpen(t,o);port:=freePort(t);good:=testCandidate(t,port,"one")
 if _,e:=r.Apply(context.Background(),Revision{1,good},grant(t,o,1,good,40*time.Second));e!=nil{t.Fatal(e)}
 bad:=testCandidate(t,port,"reject")
 // A signed, syntactically valid candidate revokes the UUID, but the test
 // subprocess refuses startup. Revocation must not resurrect the old core.
 var doc map[string]any
 if e:=json.Unmarshal(bad.Config,&doc);e!=nil{t.Fatal(e)}
 in:=doc["inbounds"].([]any)[0].(map[string]any);in["settings"].(map[string]any)["clients"]=[]any{}
 raw,e:=json.Marshal(doc);if e!=nil{t.Fatal(e)};h:=sha256.Sum256(raw);bad.Config=raw;bad.SHA256=hex.EncodeToString(h[:])
 s,e:=r.Apply(context.Background(),Revision{2,bad},grant(t,o,2,bad,40*time.Second))
 if e==nil||s.ProcessReady||s.Phase!="apply_failed_stopped"{t.Fatalf("revoked authority was restored %+v %v",s,e)}
 assertPort(t,port,false)
 if r.Snapshot().SavedSHA!=good.SHA256{t.Fatal("failure erased encrypted history")}
}
