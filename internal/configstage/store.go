// Package configstage keeps Reality Xray candidates encrypted at rest and
// returns only a metadata view to the owner. It cannot activate services.
package configstage

import (
 "bytes"
 "crypto/aes"
 "crypto/cipher"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "regexp"
 "sort"
 "sync"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const maxFileBytes=16<<20
const maxEntries=128
var (
 ErrUnsafe=errors.New("unsafe encrypted Xray staging state")
 ErrInvalid=errors.New("invalid or unverified Xray staging request")
 ErrConflict=errors.New("Xray staging conflict")
 ErrNotFound=errors.New("Xray staging not found")
 ErrMismatch=errors.New("mismatched Xray validation evidence")
 hex32=regexp.MustCompile(`^[a-f0-9]{32}$`)
 hex64=regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Status string
const (
 Pending Status="pending_offline_validation"
 Verified Status="offline_syntax_validated_not_active"
 Rejected Status="offline_validation_failed_not_active"
)

type Metadata struct {
 OperationID string `json:"operation_id"`
 NodeID string `json:"node_id"`
 InstallationID string `json:"installation_id"`
 ManifestSHA256 string `json:"manifest_sha256"`
 ConfigSHA256 string `json:"config_sha256"`
 Listeners []xrayconfig.Listener `json:"listeners"`
 TrustedXraySHA256 string `json:"trusted_xray_sha256"`
 Status Status `json:"status"`
 Revision uint64 `json:"revision"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
 CanActivate bool `json:"can_activate"`
 NeedsDataPlaneProof bool `json:"needs_data_plane_proof"`
 NeedsSubscriptionCommit bool `json:"needs_subscription_commit"`
}

type Proof struct {
 OperationID string `json:"operation_id"`
 NodeID string `json:"node_id"`
 InstallationID string `json:"installation_id"`
 ManifestSHA256 string `json:"manifest_sha256"`
 ConfigSHA256 string `json:"config_sha256"`
 TrustedXraySHA256 string `json:"trusted_xray_sha256"`
 SyntaxValid bool `json:"syntax_valid"`
 ErrorCode string `json:"error_code,omitempty"`
}

type Delivery struct {
 Metadata Metadata `json:"metadata"`
 ConfigBase64 string `json:"config_base64"`
}

type sealed struct {
 Meta Metadata `json:"metadata"`
 Nonce []byte `json:"nonce"`
 Ciphertext []byte `json:"ciphertext"`
 Proof *Proof `json:"proof,omitempty"`
}

type disk struct {
 Version int `json:"version"`
 Stages map[string]sealed `json:"stages"`
}

type Store struct {
 mu sync.RWMutex
 path string
 key []byte
 clock func()time.Time
 state disk
}

func copyMeta(m Metadata) Metadata {
 m.Listeners=append([]xrayconfig.Listener(nil),m.Listeners...)
 return m
}

func Open(path string,key []byte,clock func()time.Time)(*Store,error){
 if len(key)!=32||path=="" {return nil,ErrUnsafe}
 absolute,err:=filepath.Abs(path)
 if err!=nil{return nil,err}
 dir:=filepath.Dir(absolute)
 if err:=os.MkdirAll(dir,0700);err!=nil{return nil,err}
 di,err:=os.Lstat(dir)
 if err!=nil{return nil,err}
 if !di.IsDir()||di.Mode().Perm()&0077!=0{return nil,ErrUnsafe}
 if clock==nil{clock=time.Now}
 s:=&Store{
  path:absolute,key:append([]byte(nil),key...),clock:clock,
  state:disk{Version:1,Stages:map[string]sealed{}},
 }
 fi,err:=os.Lstat(absolute)
 if errors.Is(err,os.ErrNotExist){return s,nil}
 if err!=nil{return nil,err}
 if !fi.Mode().IsRegular()||fi.Mode().Perm()&0077!=0||fi.Size()>maxFileBytes{return nil,ErrUnsafe}
 data,err:=os.ReadFile(absolute)
 if err!=nil{return nil,err}
 decoder:=json.NewDecoder(bytes.NewReader(data))
 decoder.DisallowUnknownFields()
 var current disk
 if err:=decoder.Decode(&current);err!=nil{return nil,ErrUnsafe}
 var trailing any
 if err:=decoder.Decode(&trailing);!errors.Is(err,io.EOF){return nil,ErrUnsafe}
 if current.Version!=1||current.Stages==nil||len(current.Stages)>maxEntries{return nil,ErrUnsafe}
 aead,err:=makeAEAD(s.key)
 if err!=nil{return nil,err}
 for id,entry:=range current.Stages{
  m:=entry.Meta
  if !hex32.MatchString(id)||id!=m.OperationID||!validMeta(m)||
   len(entry.Nonce)!=aead.NonceSize()||len(entry.Ciphertext)<aead.Overhead()||
   (m.Status==Pending&&entry.Proof!=nil)||(m.Status!=Pending&&entry.Proof==nil){
   return nil,ErrUnsafe
  }
  clear,err:=aead.Open(nil,entry.Nonce,entry.Ciphertext,aad(m))
  if err!=nil{return nil,ErrUnsafe}
  digest:=sha256.Sum256(clear)
  if subtle.ConstantTimeCompare(digest[:],mustHex(m.ConfigSHA256))!=1{return nil,ErrUnsafe}
  if entry.Proof!=nil&&(!matches(m,*entry.Proof)||
    (m.Status==Verified&&!entry.Proof.SyntaxValid)||
    (m.Status==Rejected&&entry.Proof.SyntaxValid)) {return nil,ErrUnsafe}
 }
 s.state=current
 return s,nil
}

func makeAEAD(key []byte)(cipher.AEAD,error){
 block,err:=aes.NewCipher(key)
 if err!=nil{return nil,err}
 return cipher.NewGCM(block)
}
func mustHex(s string)[]byte{value,_:=hex.DecodeString(s);return value}
func aad(m Metadata)[]byte{
 listenerData,_:=json.Marshal(m.Listeners)
 listenerHash:=sha256.Sum256(listenerData)
 return []byte("DARK-PRO-STAGE-v1|"+hex.EncodeToString(listenerHash[:])+"|"+m.OperationID+"|"+m.NodeID+"|"+
  m.InstallationID+"|"+m.ManifestSHA256+"|"+m.ConfigSHA256+"|"+m.TrustedXraySHA256)
}
func validMeta(m Metadata)bool{
 if !hex32.MatchString(m.OperationID)||!hex32.MatchString(m.InstallationID)||
  !hex64.MatchString(m.ManifestSHA256)||!hex64.MatchString(m.ConfigSHA256)||
  !hex64.MatchString(m.TrustedXraySHA256)||
  m.NodeID==""||len(m.Listeners)==0||m.Revision==0||m.CanActivate||
  !m.NeedsDataPlaneProof||!m.NeedsSubscriptionCommit||
  m.CreatedAt.IsZero()||m.UpdatedAt.IsZero(){return false}
 switch m.Status{case Pending,Verified,Rejected:return true}
 return false
}
func matches(m Metadata,p Proof)bool{
 return m.OperationID==p.OperationID&&m.NodeID==p.NodeID&&
  m.InstallationID==p.InstallationID&&m.ManifestSHA256==p.ManifestSHA256&&
  m.ConfigSHA256==p.ConfigSHA256&&m.TrustedXraySHA256==p.TrustedXraySHA256
}
func clone(s disk)disk{
 r:=disk{Version:1,Stages:make(map[string]sealed,len(s.Stages))}
 for id,v:=range s.Stages{r.Stages[id]=v}
 return r
}
func (s *Store) persist(next disk)error{
 raw,err:=json.Marshal(next)
 if err!=nil{return err}
 if len(raw)>maxFileBytes{return ErrUnsafe}
 dir:=filepath.Dir(s.path)
 file,err:=os.CreateTemp(dir,".stage-")
 if err!=nil{return err}
 temp:=file.Name()
 defer os.Remove(temp)
 if err:=file.Chmod(0600);err!=nil{file.Close();return err}
 if _,err:=file.Write(raw);err!=nil{file.Close();return err}
 if err:=file.Sync();err!=nil{file.Close();return err}
 if err:=file.Close();err!=nil{return err}
 if err:=os.Rename(temp,s.path);err!=nil{return err}
 d,err:=os.Open(dir)
 if err!=nil{return err}
 defer d.Close()
 if err:=d.Sync();err!=nil{return err}
 s.state=next
 return nil
}

// Create requires the exact previously checked TCP-replacement operation.
// It deliberately cannot generate a candidate from an unpaired/legacy Node.
// The config stays AES-256-GCM encrypted at rest; the caller supplies an
// independently trusted future Xray SHA256 to be checked by the new Agent.
func (s *Store) Create(op replacement.Operation,c xrayconfig.Candidate,trustedXray string)(Metadata,error){
 if op.Phase!=replacement.TCPChecked||op.CanActivate||op.Evidence==nil||
  !op.NeedsXrayValidation||!op.NeedsDataPlaneProbe||
  !op.NeedsSubscriptionCommit||!hex32.MatchString(op.ID)||
  !hex32.MatchString(op.InstallationID)||!hex64.MatchString(op.Preview.ManifestSHA256)||
  !hex64.MatchString(trustedXray)||op.Preview.HasInheritedOldRoutes||
  c.CanActivate||!c.RequiresXrayTest||!c.RequiresDataPlaneProof||
  !c.RequiresSubscriptionCommit||c.ManifestSHA256!=op.Preview.ManifestSHA256||
  len(c.Config)==0||len(c.Config)>xrayconfig.MaxCandidateBytes||
  !hex64.MatchString(c.SHA256)||len(c.Listeners)==0{return Metadata{},ErrInvalid}
 if op.Evidence.OperationID!=op.ID||op.Evidence.NewNodeID!=op.NewNodeID||
  op.Evidence.InstallationID!=op.InstallationID||
  op.Evidence.ManifestSHA256!=op.Preview.ManifestSHA256{return Metadata{},ErrInvalid}
 for _,check:=range op.Evidence.Probes{if !check.Reachable||check.Failure!=""{return Metadata{},ErrInvalid}}
 digest:=sha256.Sum256(c.Config)
 if subtle.ConstantTimeCompare(digest[:],mustHex(c.SHA256))!=1{return Metadata{},ErrInvalid}
 s.mu.Lock()
 defer s.mu.Unlock()
 if found,ok:=s.state.Stages[op.ID];ok{
  if found.Meta.ConfigSHA256==c.SHA256&&
   found.Meta.TrustedXraySHA256==trustedXray&&
   found.Meta.ManifestSHA256==c.ManifestSHA256&&
   found.Meta.InstallationID==op.InstallationID{return copyMeta(found.Meta),nil}
  return Metadata{},ErrConflict
 }
 if len(s.state.Stages)>=maxEntries{return Metadata{},ErrConflict}
 now:=s.clock().UTC()
 meta:=Metadata{
  OperationID:op.ID,NodeID:op.NewNodeID,InstallationID:op.InstallationID,
  ManifestSHA256:c.ManifestSHA256,ConfigSHA256:c.SHA256,
  Listeners:append([]xrayconfig.Listener(nil),c.Listeners...),
  TrustedXraySHA256:trustedXray,Status:Pending,Revision:1,
  CreatedAt:now,UpdatedAt:now,CanActivate:false,
  NeedsDataPlaneProof:true,NeedsSubscriptionCommit:true,
 }
 aead,err:=makeAEAD(s.key)
 if err!=nil{return Metadata{},err}
 nonce:=make([]byte,aead.NonceSize())
 if _,err:=rand.Read(nonce);err!=nil{return Metadata{},err}
 ciphertext:=aead.Seal(nil,nonce,c.Config,aad(meta))
 next:=clone(s.state)
 next.Stages[op.ID]=sealed{Meta:meta,Nonce:nonce,Ciphertext:ciphertext}
 if err:=s.persist(next);err!=nil{return Metadata{},err}
 return copyMeta(meta),nil
}

func (s *Store) Get(id string)(Metadata,error){
 s.mu.RLock();defer s.mu.RUnlock()
 entry,ok:=s.state.Stages[id]
 if !ok{return Metadata{},ErrNotFound}
 return copyMeta(entry.Meta),nil
}
func (s *Store) List()[]Metadata{
 s.mu.RLock();defer s.mu.RUnlock()
 entries:=make([]Metadata,0,len(s.state.Stages))
 for _,v:=range s.state.Stages{entries=append(entries,copyMeta(v.Meta))}
 sort.Slice(entries,func(i,j int)bool{return entries[i].CreatedAt.Before(entries[j].CreatedAt)})
 return entries
}

// PendingFor returns a verified private config only for the exact new Node
// installation. The HTTP caller must additionally enforce paired Agent auth,
// TLS1.3 and prevent cache/redirects. Never use this on the owner API.
func (s *Store) PendingFor(nodeID,installation string)(Metadata,[]byte,bool,error){
 s.mu.RLock();defer s.mu.RUnlock()
 var selected *sealed
 for _,v:=range s.state.Stages{
  if v.Meta.NodeID==nodeID&&v.Meta.InstallationID==installation&&v.Meta.Status==Pending{
   if selected!=nil{return Metadata{},nil,false,ErrConflict}
   x:=v
   selected=&x
  }
 }
 if selected==nil{return Metadata{},nil,false,nil}
 aead,err:=makeAEAD(s.key)
 if err!=nil{return Metadata{},nil,false,err}
 raw,err:=aead.Open(nil,selected.Nonce,selected.Ciphertext,aad(selected.Meta))
 if err!=nil{return Metadata{},nil,false,ErrUnsafe}
 return copyMeta(selected.Meta),raw,true,nil
}

func (s *Store) Report(proof Proof)(Metadata,error){
 s.mu.Lock();defer s.mu.Unlock()
 entry,ok:=s.state.Stages[proof.OperationID]
 if !ok{return Metadata{},ErrNotFound}
 if !matches(entry.Meta,proof){return Metadata{},ErrMismatch}
 if proof.SyntaxValid {
  if proof.ErrorCode!=""{return Metadata{},ErrInvalid}
 }else {
  switch proof.ErrorCode{
  case "binary_unavailable","binary_digest_mismatch","candidate_invalid",
   "xray_rejected","timeout","validation_error":
  default:return Metadata{},ErrInvalid
  }
 }
 if entry.Meta.Status!=Pending{
  if entry.Proof!=nil&&*entry.Proof==proof{return copyMeta(entry.Meta),nil}
  return Metadata{},ErrConflict
 }
 next:=clone(s.state)
 meta:=entry.Meta
 meta.Revision++
 meta.UpdatedAt=s.clock().UTC()
 meta.Status=Rejected
 if proof.SyntaxValid{meta.Status=Verified}
 entry.Meta=meta
 entry.Proof=&proof
 next.Stages[proof.OperationID]=entry
 if err:=s.persist(next);err!=nil{return Metadata{},err}
 return copyMeta(meta),nil
}
