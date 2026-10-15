package business

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestOwnerBootstrapLoginSessionAndLogoutSurviveReopen(t *testing.T){
 ctx:=context.Background()
 s,path:=opened(t)
 if _,err:=s.Login(ctx,"owner","bad-password");!errors.Is(err,ErrUnauthenticated){t.Fatal("unbootstrapped login accepted",err)}
 const ownerPass="strong-unique-owner-test-password-2026"
 owner,err:=s.BootstrapOwner(ctx,"MyAdmin",ownerPass)
 if err!=nil||owner.Username!="myadmin"||owner.Role!="owner"{t.Fatalf("bootstrap %v %+v",err,owner)}
 if _,err=s.BootstrapOwner(ctx,"otherowner","another-unique-password");!errors.Is(err,ErrConflict){t.Fatal("second owner accepted",err)}
 if _,err=s.Login(ctx,"myadmin","wrong-password");!errors.Is(err,ErrUnauthenticated){t.Fatal("wrong credentials accepted",err)}
 session,err:=s.Login(ctx,"myadmin",ownerPass)
 if err!=nil{t.Fatal(err)}
 if len(session.Token)!=64||session.User.Role!="owner"{t.Fatal("session lacks entropy or role")}
 valid,err:=s.VerifySession(ctx,session.Token)
 if err!=nil||valid.User.Username!="myadmin"{t.Fatalf("valid session rejected: %+v %v",valid,err)}
 if _,err=s.VerifySession(ctx,"bad");!errors.Is(err,ErrUnauthenticated){t.Fatal("invalid bearer accepted")}
 if err=s.Close();err!=nil{t.Fatal(err)}
 fresh,err:=Open(path);if err!=nil{t.Fatal(err)};defer fresh.Close()
 valid,err=fresh.VerifySession(ctx,session.Token)
 if err!=nil||valid.User.ID!="owner"{t.Fatalf("restart logged all users out: %v",err)}
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(raw),ownerPass)||strings.Contains(string(raw),session.Token){
  t.Fatal("raw password or session token appeared in SQLite file")
 }
 if err=fresh.Logout(ctx,session.Token);err!=nil{t.Fatal(err)}
 if _,err=fresh.VerifySession(ctx,session.Token);!errors.Is(err,ErrUnauthenticated){t.Fatal("logged out session remained valid")}
}
func TestRepresentativeRoleAndDisabledAccess(t *testing.T){
 ctx:=context.Background();s,_:=opened(t)
 if _,err:=s.BootstrapOwner(ctx,"panelowner","owner-login-password-1234");err!=nil{t.Fatal(err)}
 newRep(t,s,"repLogin")
 newRep(t,s,"repOther")
 const pass="representative-unique-password"
 u,err:=s.ProvisionRepresentativeLogin(ctx,"repLogin","RepLogin",pass)
 if err!=nil||u.Role!="representative"||u.RepresentativeID!="repLogin"{t.Fatalf("rep auth failed: %+v %v",u,err)}
 if _,err=s.ProvisionRepresentativeLogin(ctx,"repLogin","two","unique-secondary-password");!errors.Is(err,ErrConflict){t.Fatal("rep identity reinitialized")}
 if _,err=s.ProvisionRepresentativeLogin(ctx,"repOther","RepLogin","another-unique-password");!errors.Is(err,ErrConflict){t.Fatal("username collision accepted")}
 session,err:=s.Login(ctx,"replogin",pass)
 if err!=nil||session.User.Role!="representative"{t.Fatalf("rep login failed: %v %+v",err,session)}
 if _,err=s.db.ExecContext(ctx,"UPDATE representatives SET enabled=0 WHERE id='repLogin'");err!=nil{t.Fatal(err)}
 if _,err=s.VerifySession(ctx,session.Token);!errors.Is(err,ErrUnauthenticated){t.Fatal("disabled representative retained access")}
 if _,err=s.Login(ctx,"replogin",pass);!errors.Is(err,ErrUnauthenticated){t.Fatal("disabled representative logged in")}
}
func TestPasswordRotationRevokesEverySession(t *testing.T){
 ctx:=context.Background();s,_:=opened(t)
 if _,err:=s.BootstrapOwner(ctx,"mainowner","initial-owner-password-2026");err!=nil{t.Fatal(err)}
 one,err:=s.Login(ctx,"mainowner","initial-owner-password-2026");if err!=nil{t.Fatal(err)}
 two,err:=s.Login(ctx,"mainowner","initial-owner-password-2026");if err!=nil{t.Fatal(err)}
 if err=s.RotatePassword(ctx,"owner","bad","new-owner-password-2026");!errors.Is(err,ErrUnauthenticated){t.Fatalf("wrong old password accepted: %v",err)}
 if err=s.RotatePassword(ctx,"owner","initial-owner-password-2026","new-owner-password-2026");err!=nil{t.Fatal(err)}
 for _,token:=range []string{one.Token,two.Token}{
  if _,err=s.VerifySession(ctx,token);!errors.Is(err,ErrUnauthenticated){t.Fatal("old session survived password rotation")}
 }
 if _,err=s.Login(ctx,"mainowner","initial-owner-password-2026");!errors.Is(err,ErrUnauthenticated){t.Fatal("old password retained")}
 if _,err=s.Login(ctx,"mainowner","new-owner-password-2026");err!=nil{t.Fatal("new password refused",err)}
}
func TestExpiredSessionRejected(t *testing.T){
 ctx:=context.Background();s,_:=opened(t)
 if _,err:=s.BootstrapOwner(ctx,"safepanel","long-expiry-password-2026");err!=nil{t.Fatal(err)}
 sess,err:=s.Login(ctx,"safepanel","long-expiry-password-2026")
 if err!=nil{t.Fatal(err)}
 if _,err=s.db.ExecContext(ctx,"UPDATE auth_sessions SET expires_unix=? WHERE token_sha256=?",time.Now().Add(-time.Second).Unix(),digestSession(sess.Token));err!=nil{t.Fatal(err)}
 if _,err=s.VerifySession(ctx,sess.Token);!errors.Is(err,ErrUnauthenticated){t.Fatal("expired login accepted")}
}
func TestAuthSchemaUpgradeFromV1KeepsFinancialRecords(t *testing.T){
 ctx:=context.Background()
 path:=filepath.Join(t.TempDir(),"private","upgrade.sqlite")
 s,err:=Open(path);if err!=nil{t.Fatal(err)}
 newRep(t,s,"olderRep")
 grant(t,s,"olderRep","grant-upgrade-0001",1200,9)
 // Simulate the first released business schema version without any auth
 // state. The real migration path cannot drop balances or event history.
 for _,statement:=range []string{
  "DROP TABLE auth_sessions",
  "DROP TABLE auth_users",
  "DROP TABLE client_operations",
  "ALTER TABLE clients DROP COLUMN archived",
  "DROP TABLE native_inbound_operations","ALTER TABLE native_inbounds DROP COLUMN revision","ALTER TABLE native_inbounds DROP COLUMN archived","ALTER TABLE native_inbounds DROP COLUMN enabled","PRAGMA user_version=1",
 }{
  if _,err=s.db.ExecContext(ctx,statement);err!=nil{t.Fatal(err)}
 }
 if err=s.Close();err!=nil{t.Fatal(err)}
 upgraded,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer upgraded.Close()
 r,err:=upgraded.Representative(ctx,"olderRep")
 if err!=nil||r.VolumeCreditBytes!=1200||r.UnlimitedCreditUnits!=9{t.Fatalf("upgrade lost credits: %+v %v",r,err)}
 evts,err:=upgraded.ListEvents(ctx,"olderRep",20,0)
 if err!=nil||len(evts)!=1{t.Fatalf("upgrade lost audit: %d %v",len(evts),err)}
 if _,err=upgraded.BootstrapOwner(ctx,"newowner","private-setup-password-2026");err!=nil{t.Fatalf("upgrade identity unusable: %v",err)}
}
