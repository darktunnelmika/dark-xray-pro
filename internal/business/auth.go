package business

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "database/sql"
 "encoding/hex"
 "errors"
 "strings"
 "time"
 "unicode/utf8"

 "golang.org/x/crypto/argon2"
)

const (
 SessionLifetime = 12*time.Hour
 argonIterations uint32 = 3
 argonMemoryKiB uint32 = 32*1024
 argonThreads uint8 = 2
 argonKeyBytes uint32 = 32
)
var ErrUnauthenticated=errors.New("invalid authentication")

type AuthUser struct {
 ID string `json:"id"`
 Username string `json:"username"`
 Role string `json:"role"`
 RepresentativeID string `json:"representative_id"`
}
type LoginSession struct {
 Token string `json:"-"`
 User AuthUser `json:"user"`
 ExpiresUnix int64 `json:"expires_unix"`
}
func validUsername(username string) bool{
 if len(username)<3||len(username)>64{return false}
 for _,r:=range username{
  if !(r>='a'&&r<='z'||r>='0'&&r<='9'||r=='.'||r=='_'||r=='-'){return false}
 }
 return username[0]>='a'&&username[0]<='z'
}
func validPassword(pass string)bool{
 if len(pass)<12||len(pass)>256||!utf8.ValidString(pass)||strings.TrimSpace(pass)==""{return false}
 for _,r:=range pass{if r==0{return false}}
 return true
}
func passwordHash(pass string)([]byte,[]byte,error){
 salt:=make([]byte,16)
 if _,err:=rand.Read(salt);err!=nil{return nil,nil,err}
 hash:=argon2.IDKey([]byte(pass),salt,argonIterations,argonMemoryKiB,argonThreads,argonKeyBytes)
 return salt,hash,nil
}
func digestSession(token string)string{
 sum:=sha256.Sum256([]byte(token))
 return hex.EncodeToString(sum[:])
}

// BootstrapOwner is intentionally NOT a public HTTP API. A restricted new-HUB
// local CLI reads the first password from a private 0600 file. Once created,
// a replay or alternate account fails rather than resetting credentials.
func(s *Store)BootstrapOwner(ctx context.Context,username,password string)(AuthUser,error){
 username=strings.ToLower(username)
 if !validUsername(username)||!validPassword(password){return AuthUser{},ErrInvalid}
 salt,hash,err:=passwordHash(password);if err!=nil{return AuthUser{},err}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return AuthUser{},err}
 defer tx.Rollback()
 var n int
 if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM auth_users").Scan(&n);err!=nil{return AuthUser{},err}
 if n!=0{return AuthUser{},ErrConflict}
 if _,err=tx.ExecContext(ctx,"INSERT INTO auth_users(id,username,representative_id,role,password_salt,password_hash,created_unix) VALUES('owner',?,'owner','owner',?,?,?)",username,salt,hash,time.Now().UTC().Unix());err!=nil{return AuthUser{},ErrConflict}
 if err=tx.Commit();err!=nil{return AuthUser{},err}
 return AuthUser{ID:"owner",Username:username,Role:"owner",RepresentativeID:"owner"},nil
}

// ProvisionRepresentativeLogin requires an existing funded/scoped PRO
// representative identity. This operation does not copy a legacy password.
func(s *Store)ProvisionRepresentativeLogin(ctx context.Context,repID,username,password string)(AuthUser,error){
 username=strings.ToLower(username)
 if !idRE.MatchString(repID)||repID=="owner"||!validUsername(username)||!validPassword(password){return AuthUser{},ErrInvalid}
 salt,hash,err:=passwordHash(password);if err!=nil{return AuthUser{},err}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return AuthUser{},err}
 defer tx.Rollback()
 rep,err:=readRep(ctx,tx,repID)
 if errors.Is(err,sql.ErrNoRows){return AuthUser{},ErrNotFound}
 if err!=nil{return AuthUser{},err}
 if rep.Kind!="representative"||!rep.Enabled{return AuthUser{},ErrConflict}
 if _,err=tx.ExecContext(ctx,"INSERT INTO auth_users(id,username,representative_id,role,password_salt,password_hash,created_unix) VALUES(?, ?, ?, 'representative', ?, ?, ?)",repID,username,repID,salt,hash,time.Now().UTC().Unix());err!=nil{return AuthUser{},ErrConflict}
 if err=tx.Commit();err!=nil{return AuthUser{},err}
 return AuthUser{ID:repID,Username:username,Role:"representative",RepresentativeID:repID},nil
}

func (s *Store) Login(ctx context.Context,username,password string)(LoginSession,error){
 username=strings.ToLower(username)
 if !validUsername(username)||len(password)>256{return LoginSession{},ErrUnauthenticated}
 var u AuthUser;var salt,expected []byte;var disabled,repEnabled int
 err:=s.db.QueryRowContext(ctx,
  "SELECT a.id,a.username,a.role,a.representative_id,a.password_salt,a.password_hash,a.disabled,r.enabled FROM auth_users a JOIN representatives r ON r.id=a.representative_id WHERE a.username=?",
  username).Scan(&u.ID,&u.Username,&u.Role,&u.RepresentativeID,&salt,&expected,&disabled,&repEnabled)
 valid:=err==nil && len(salt)==16 && len(expected)==32 && disabled==0 && repEnabled==1
 // Compute a real Argon2id hash even for missing/disabled users to reduce
 // credential-enumeration timing. A bounded API gate limits parallel load.
 if !valid{salt=[]byte("privatefake-salt");expected=make([]byte,32)}
 got:=argon2.IDKey([]byte(password),salt,argonIterations,argonMemoryKiB,argonThreads,argonKeyBytes)
 if subtle.ConstantTimeCompare(got,expected)!=1||!valid{return LoginSession{},ErrUnauthenticated}
 tokenBytes:=make([]byte,32)
 if _,err=rand.Read(tokenBytes);err!=nil{return LoginSession{},err}
 token:=hex.EncodeToString(tokenBytes)
 now:=time.Now().UTC().Unix()
 expiry:=now+int64(SessionLifetime/time.Second)
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return LoginSession{},err}
 defer tx.Rollback()
 if _,err=tx.ExecContext(ctx,"DELETE FROM auth_sessions WHERE expires_unix<=?",now);err!=nil{return LoginSession{},err}
 if _,err=tx.ExecContext(ctx,"INSERT INTO auth_sessions(token_sha256,user_id,created_unix,expires_unix) VALUES(?,?,?,?)",digestSession(token),u.ID,now,expiry);err!=nil{return LoginSession{},err}
 // Cap live sessions per account without trusting client-side logout. New
 // successful logins keep their own token while oldest sessions are evicted.
 if _,err=tx.ExecContext(ctx,`DELETE FROM auth_sessions WHERE user_id=? AND token_sha256 NOT IN (
  SELECT token_sha256 FROM auth_sessions WHERE user_id=? ORDER BY created_unix DESC, rowid DESC LIMIT 20
 )`,u.ID,u.ID);err!=nil{return LoginSession{},err}
 if err=tx.Commit();err!=nil{return LoginSession{},err}
 return LoginSession{Token:token,User:u,ExpiresUnix:expiry},nil
}

func (s *Store) VerifySession(ctx context.Context,token string)(LoginSession,error){
 if len(token)!=64 {return LoginSession{},ErrUnauthenticated}
 for _,c:=range token{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return LoginSession{},ErrUnauthenticated}}
 var u AuthUser
 var expires int64
 var disabled,repEnabled int
 err:=s.db.QueryRowContext(ctx,
  "SELECT a.id,a.username,a.role,a.representative_id,ss.expires_unix,a.disabled,r.enabled FROM auth_sessions ss JOIN auth_users a ON a.id=ss.user_id JOIN representatives r ON r.id=a.representative_id WHERE ss.token_sha256=?",
  digestSession(token)).Scan(&u.ID,&u.Username,&u.Role,&u.RepresentativeID,&expires,&disabled,&repEnabled)
 if err!=nil||expires<=time.Now().UTC().Unix()||disabled!=0||repEnabled!=1{return LoginSession{},ErrUnauthenticated}
 return LoginSession{Token:token,User:u,ExpiresUnix:expires},nil
}
func(s *Store) Logout(ctx context.Context,token string)error{
 if len(token)!=64{return ErrUnauthenticated}
 _,err:=s.db.ExecContext(ctx,"DELETE FROM auth_sessions WHERE token_sha256=?",digestSession(token))
 return err
}

// Rotating the password invalidates ALL sessions in the same transaction.
func(s *Store)RotatePassword(ctx context.Context,userID,oldPassword,newPassword string)error{
 if !idRE.MatchString(userID)||!validPassword(newPassword){return ErrInvalid}
 var salt,expected []byte
 err:=s.db.QueryRowContext(ctx,"SELECT password_salt,password_hash FROM auth_users WHERE id=?",userID).Scan(&salt,&expected)
 if err!=nil{return ErrUnauthenticated}
 got:=argon2.IDKey([]byte(oldPassword),salt,argonIterations,argonMemoryKiB,argonThreads,argonKeyBytes)
 if subtle.ConstantTimeCompare(got,expected)!=1{return ErrUnauthenticated}
 newSalt,newHash,err:=passwordHash(newPassword);if err!=nil{return err}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err}
 defer tx.Rollback()
 result,err:=tx.ExecContext(ctx,"UPDATE auth_users SET password_salt=?,password_hash=? WHERE id=? AND password_hash=?",newSalt,newHash,userID,expected)
 if err!=nil{return err}
 affected,err:=result.RowsAffected();if err!=nil{return err}
 if affected!=1{return ErrConflict}
 if _,err=tx.ExecContext(ctx,"DELETE FROM auth_sessions WHERE user_id=?",userID);err!=nil{return err}
 return tx.Commit()
}
