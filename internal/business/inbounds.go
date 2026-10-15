package business

import (
 "context"
 "crypto/aes"
 "crypto/cipher"
 "crypto/ecdh"
 "crypto/rand"
 "crypto/sha256"
 "database/sql"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "net"
 "time"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
type InboundInput struct{ID uint64 `json:"id"`;Name string `json:"name"`;Transport string `json:"transport"`;GRPCService string `json:"grpc_service"`;RealityTarget string `json:"reality_target"`;ServerNames []string `json:"server_names"`;ShortIDs []string `json:"short_ids"`}
type NativeInbound struct{InboundInput;PublicKey string `json:"public_key"`;BoundClients int `json:"bound_clients"`}
type InboundTarget struct{InboundID uint64 `json:"inbound_id"`;NodeID string `json:"node_id"`;InstallationID string `json:"installation_id"`;ListenIP string `json:"listen_ip"`;DirectPort uint16 `json:"direct_port"`;SwapPort uint16 `json:"swap_port"`}
type NodeConfig struct{Generation uint64 `json:"generation"`;NodeID string `json:"node_id"`;InstallationID string `json:"installation_id"`;ListenIP string `json:"listen_ip"`;Sequence uint64 `json:"sequence"`;ConfigSHA string `json:"config_sha"`;ManifestSHA string `json:"manifest_sha"`;UserCount int `json:"user_count"`;LeaseNotAfter int64 `json:"lease_not_after_unix_nano"`;Candidate xrayconfig.Candidate `json:"-"`}
func migrateNativeInbounds(db *sql.DB)error{
 tx,err:=db.Begin();if err!=nil{return err};defer tx.Rollback()
 for _,q:=range []string{
  "CREATE TABLE IF NOT EXISTS native_inbounds(id INTEGER PRIMARY KEY,name TEXT NOT NULL,profile_enc BLOB NOT NULL,public_key TEXT NOT NULL,created_unix INTEGER NOT NULL)",
  "CREATE TABLE IF NOT EXISTS native_inbound_clients(inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),client_id TEXT NOT NULL REFERENCES clients(id),PRIMARY KEY(inbound_id,client_id))",
  "CREATE INDEX IF NOT EXISTS native_inbound_clients_client ON native_inbound_clients(client_id,inbound_id)",
  "CREATE TABLE IF NOT EXISTS native_inbound_targets(inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,listen_ip TEXT NOT NULL,direct_port INTEGER NOT NULL,swap_port INTEGER NOT NULL,PRIMARY KEY(inbound_id,node_id))",
  "CREATE INDEX IF NOT EXISTS native_inbound_targets_node ON native_inbound_targets(node_id,installation_id,inbound_id)",
  "CREATE TABLE IF NOT EXISTS native_node_revisions(node_id TEXT NOT NULL,installation_id TEXT NOT NULL,sequence INTEGER NOT NULL,config_sha TEXT NOT NULL,manifest_sha TEXT NOT NULL,reported_sequence INTEGER NOT NULL DEFAULT 0,reported_sha TEXT NOT NULL DEFAULT '',reported_phase TEXT NOT NULL DEFAULT '',PRIMARY KEY(node_id,installation_id))",
  "PRAGMA user_version=5",
 }{if _,err=tx.Exec(q);err!=nil{return err}};return tx.Commit()
}
func profileAEAD(key []byte)(cipher.AEAD,error){if len(key)!=32{return nil,ErrInvalid};b,e:=aes.NewCipher(key);if e!=nil{return nil,e};return cipher.NewGCM(b)}
func profileAAD(id uint64)[]byte{return []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v1:%d",id))}
func openProfile(key,encrypted []byte,id uint64)(xrayconfig.Profile,error){
 var p xrayconfig.Profile;a,e:=profileAEAD(key);if e!=nil{return p,e};if len(encrypted)<a.NonceSize()+a.Overhead(){return p,ErrUnavailable}
 raw,e:=a.Open(nil,encrypted[:a.NonceSize()],encrypted[a.NonceSize():],profileAAD(id));if e!=nil||json.Unmarshal(raw,&p)!=nil||p.InboundID!=id{return p,ErrUnavailable};return p,nil
}
func(s *Store)CreateInbound(ctx context.Context,key []byte,in InboundInput)(NativeInbound,error){
 old,found,replayErr:=s.replayInbound(ctx,key,in);if replayErr!=nil{return NativeInbound{},replayErr};if found{return old,nil}
 if in.ID==0||in.ID>1<<53-1||!validName(in.Name,128){return NativeInbound{},ErrInvalid}
 a,err:=profileAEAD(key);if err!=nil{return NativeInbound{},err};priv,err:=ecdh.X25519().GenerateKey(rand.Reader);if err!=nil{return NativeInbound{},err}
 p:=xrayconfig.Profile{InboundID:in.ID,Transport:in.Transport,GRPCService:in.GRPCService,RealityTarget:in.RealityTarget,ServerNames:in.ServerNames,ShortIDs:in.ShortIDs,RealityPrivateKey:base64.RawURLEncoding.EncodeToString(priv.Bytes()),Users:[]xrayconfig.User{}}
 // Validate only: no invented client identity or listener is installed.
 if _,err=xrayconfig.RenderAssigned("profile-validation","00000000000000000000000000000000","127.0.0.1",[]xrayconfig.Assignment{{Profile:p,DirectPort:10443}});err!=nil{return NativeInbound{},ErrInvalid}
 raw,err:=json.Marshal(p);if err!=nil{return NativeInbound{},err};nonce:=make([]byte,a.NonceSize());if _,err=rand.Read(nonce);err!=nil{return NativeInbound{},err}
 sealed:=a.Seal(nonce,nonce,raw,profileAAD(in.ID));pub:=base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
 _,err=s.db.ExecContext(ctx,"INSERT INTO native_inbounds(id,name,profile_enc,public_key,created_unix) VALUES(?,?,?,?,?)",in.ID,in.Name,sealed,pub,time.Now().Unix());if err!=nil{return NativeInbound{},ErrConflict}
 return NativeInbound{InboundInput:in,PublicKey:pub},nil
}
func(s *Store)ListInbounds(ctx context.Context,key []byte)([]NativeInbound,error){
 rows,err:=s.db.QueryContext(ctx,"SELECT i.id,i.name,i.profile_enc,i.public_key,(SELECT COUNT(*) FROM native_inbound_clients c WHERE c.inbound_id=i.id) FROM native_inbounds i ORDER BY i.id LIMIT 257");if err!=nil{return nil,err};defer rows.Close();result:=make([]NativeInbound,0)
 for rows.Next(){var in NativeInbound;var enc []byte;if err=rows.Scan(&in.ID,&in.Name,&enc,&in.PublicKey,&in.BoundClients);err!=nil{return nil,err};p,e:=openProfile(key,enc,in.ID);if e!=nil{return nil,e};in.Transport=p.Transport;in.GRPCService=p.GRPCService;in.RealityTarget=p.RealityTarget;in.ServerNames=p.ServerNames;in.ShortIDs=p.ShortIDs;result=append(result,in)}
 if len(result)>256{return nil,ErrInvalid};return result,rows.Err()
}
func(s *Store)BindInboundClients(ctx context.Context,id uint64,ids []string,attach bool)error{
 if id==0||!validClientIDBatch(ids){return ErrInvalid};tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback();var found int
 if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM native_inbounds WHERE id=?",id).Scan(&found);err!=nil{return err};if found!=1{return ErrNotFound}
 for _,clientID:=range ids{c,e:=readClient(ctx,tx,clientID);if errors.Is(e,sql.ErrNoRows){return ErrNotFound};if e!=nil{return e};if attach&&c.Archived{return ErrConflict}}
 for _,clientID:=range ids{if attach{_,err=tx.ExecContext(ctx,"INSERT OR IGNORE INTO native_inbound_clients(inbound_id,client_id) VALUES(?,?)",id,clientID)}else{_,err=tx.ExecContext(ctx,"DELETE FROM native_inbound_clients WHERE inbound_id=? AND client_id=?",id,clientID)};if err!=nil{return err}}
 return tx.Commit()
}
func validNativeTarget(t InboundTarget)bool{
 ip:=net.ParseIP(t.ListenIP);_,err:=hex.DecodeString(t.InstallationID)
 return t.InboundID>0&&idRE.MatchString(t.NodeID)&&len(t.InstallationID)==32&&err==nil&&ip!=nil&&!ip.IsUnspecified()&&!ip.IsMulticast()&&(t.DirectPort>=1024||t.SwapPort>=1024)&&(t.DirectPort==0||t.DirectPort>=1024)&&(t.SwapPort==0||t.SwapPort>=1024)
}
func(s *Store)PutInboundTarget(ctx context.Context,t InboundTarget)error{
 if !validNativeTarget(t){return ErrInvalid};t.ListenIP=net.ParseIP(t.ListenIP).String();tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback();var n int
 if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM native_inbounds WHERE id=?",t.InboundID).Scan(&n);err!=nil{return err};if n!=1{return ErrNotFound}
 rows,err:=tx.QueryContext(ctx,"SELECT inbound_id,installation_id,listen_ip,direct_port,swap_port FROM native_inbound_targets WHERE node_id=?",t.NodeID);if err!=nil{return err}
 for rows.Next(){var id uint64;var installation,ip string;var direct,swap uint16;if err=rows.Scan(&id,&installation,&ip,&direct,&swap);err!=nil{rows.Close();return err};if id==t.InboundID{continue}
  if installation!=t.InstallationID||ip!=t.ListenIP||t.DirectPort!=0&&(t.DirectPort==direct||t.DirectPort==swap)||t.SwapPort!=0&&(t.SwapPort==direct||t.SwapPort==swap){rows.Close();return ErrConflict}
 };err=rows.Err();rows.Close();if err!=nil{return err}
 _,err=tx.ExecContext(ctx,"INSERT INTO native_inbound_targets(inbound_id,node_id,installation_id,listen_ip,direct_port,swap_port) VALUES(?,?,?,?,?,?) ON CONFLICT(inbound_id,node_id) DO UPDATE SET installation_id=excluded.installation_id,listen_ip=excluded.listen_ip,direct_port=excluded.direct_port,swap_port=excluded.swap_port",t.InboundID,t.NodeID,t.InstallationID,t.ListenIP,t.DirectPort,t.SwapPort);if err!=nil{return err};return tx.Commit()
}
// BuildNodeConfig reads the authoritative native binding snapshot atomically.
// Disabled/expired/archived identities are excluded, not physically deleted.
func(s *Store)BuildNodeConfig(ctx context.Context,key []byte,node,installation string)(NodeConfig,error){
 result:=NodeConfig{NodeID:node,InstallationID:installation,LeaseNotAfter:time.Now().Add(30*time.Second).UnixNano()};if !idRE.MatchString(node)||len(installation)!=32{return result,ErrInvalid}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return result,err};defer tx.Rollback()
 if err=tx.QueryRowContext(ctx,"SELECT value FROM native_config_generation WHERE id=1").Scan(&result.Generation);err!=nil{return result,err}
 type source struct{id uint64;enc []byte;direct,swap uint16};sources:=[]source{}
 rows,err:=tx.QueryContext(ctx,"SELECT i.id,i.profile_enc,t.listen_ip,t.direct_port,t.swap_port FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id WHERE t.node_id=? AND t.installation_id=? ORDER BY i.id LIMIT 257",node,installation);if err!=nil{return result,err}
 for rows.Next(){var x source;var ip string;if err=rows.Scan(&x.id,&x.enc,&ip,&x.direct,&x.swap);err!=nil{rows.Close();return result,err};if result.ListenIP!=""&&result.ListenIP!=ip{rows.Close();return result,ErrConflict};result.ListenIP=ip;sources=append(sources,x)}
 err=rows.Err();rows.Close();if err!=nil{return result,err};if len(sources)==0{return result,ErrNotFound};if len(sources)>256{return result,ErrInvalid}
 assignments:=make([]xrayconfig.Assignment,0,len(sources));seen:=map[string]bool{}
 for _,x:=range sources{
  p,e:=openProfile(key,x.enc,x.id);if e!=nil{return result,e};p.Users=[]xrayconfig.User{}
  clients,e:=tx.QueryContext(ctx,"SELECT c.id,c.uuid,c.expires_unix FROM native_inbound_clients b JOIN clients c ON c.id=b.client_id JOIN representatives r ON r.id=c.representative_id WHERE b.inbound_id=? AND c.archived=0 AND c.deployment_state!='disabled' AND c.expires_unix>? AND r.enabled=1 ORDER BY c.id LIMIT 20001",x.id,time.Now().Unix());if e!=nil{return result,e}
  for clients.Next(){var id,uuid string;var expiry int64;if e=clients.Scan(&id,&uuid,&expiry);e!=nil{clients.Close();return result,e};digest:=sha256.Sum256([]byte(id));email:=hex.EncodeToString(digest[:16])+"@pro.invalid";p.Users=append(p.Users,xrayconfig.User{UUID:uuid,Email:email});seen[id]=true;if expiry*int64(time.Second)<result.LeaseNotAfter{result.LeaseNotAfter=expiry*int64(time.Second)}}
  e=clients.Err();clients.Close();if e!=nil{return result,e};if len(p.Users)>20000{return result,ErrInvalid};assignments=append(assignments,xrayconfig.Assignment{Profile:p,DirectPort:x.direct,SwapPort:x.swap})
 }
 candidate,err:=xrayconfig.RenderAssigned(node,installation,result.ListenIP,assignments);if err!=nil{return result,ErrInvalid}
 result.Candidate=candidate;result.ConfigSHA=candidate.SHA256;result.ManifestSHA=candidate.ManifestSHA256;result.UserCount=len(seen)
 var seq uint64;var sha,manifest string;err=tx.QueryRowContext(ctx,"SELECT sequence,config_sha,manifest_sha FROM native_node_revisions WHERE node_id=? AND installation_id=?",node,installation).Scan(&seq,&sha,&manifest)
 if errors.Is(err,sql.ErrNoRows){seq=0}else if err!=nil{return result,err}
 if sha!=candidate.SHA256||manifest!=candidate.ManifestSHA256{if seq>=1<<53-1{return result,ErrConflict};seq++;_,err=tx.ExecContext(ctx,"INSERT INTO native_node_revisions(node_id,installation_id,sequence,config_sha,manifest_sha) VALUES(?,?,?,?,?) ON CONFLICT(node_id,installation_id) DO UPDATE SET sequence=excluded.sequence,config_sha=excluded.config_sha,manifest_sha=excluded.manifest_sha",node,installation,seq,result.ConfigSHA,result.ManifestSHA);if err!=nil{return result,err}}
 result.Sequence=seq;if err=tx.Commit();err!=nil{return result,err};return result,nil
}
