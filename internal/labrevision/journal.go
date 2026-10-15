// Package labrevision provides an encrypted, crash-safe lab *selection*
// journal with compare-and-swap and reversible selection. It NEVER touches
// active Xray processes, systemd, tunnel routes or subscriptions.
package labrevision

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
	"sort"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

var (
	ErrUnsafe = errors.New("unsafe lab revision journal")
	ErrInvalid = errors.New("invalid or unverified LAB candidate")
	ErrConflict = errors.New("stale LAB generation or conflicting candidate")
	ErrMissing = errors.New("LAB version not found")
	ErrNoPrevious = errors.New("no prior LAB candidate selection to restore")
)
const (
	maxStoredBytes = 32<<20
	maxVersions = 100
	maxEvents = 1000
)

type Version struct {
	ID string `json:"id"`
	SourceOperationID string `json:"source_operation_id"`
	NodeID string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	ManifestSHA256 string `json:"manifest_sha256"`
	ConfigSHA256 string `json:"config_sha256"`
	TrustedXraySHA256 string `json:"trusted_xray_sha256"`
	ListenerCount int `json:"listener_count"`
	CreatedAt time.Time `json:"created_at"`
	LiveApplied bool `json:"live_applied"`
	CanActivate bool `json:"can_activate"`
}

type Event struct {
	Generation uint64 `json:"generation"`
	Action string `json:"action"`
	SelectedVersion string `json:"selected_version"`
	PreviousVersion string `json:"previous_version"`
	At time.Time `json:"at"`
}

type Snapshot struct {
	Generation uint64 `json:"generation"`
	SelectedVersion string `json:"selected_version"`
	PreviousVersion string `json:"previous_version"`
	Versions []Version `json:"versions"`
	Events []Event `json:"events"`
	ActualXrayProcessChanged bool `json:"actual_xray_process_changed"`
	NeedsLiveHealthAndProtocolProof bool `json:"needs_live_health_and_protocol_proof"`
	NeedsSubscriptionCommit bool `json:"needs_subscription_commit"`
}

type sealed struct {
	Version Version `json:"version"`
	Nonce []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}
type disk struct {
	Schema int `json:"schema"`
	Generation uint64 `json:"generation"`
	Selected string `json:"selected"`
	Previous string `json:"previous"`
	Versions map[string]sealed `json:"versions"`
	Events []Event `json:"events"`
}

type Store struct {
	mu sync.RWMutex
	path string
	key []byte
	clock func()time.Time
	state disk
}

func newDisk()disk{return disk{Schema:1,Generation:1,Versions:map[string]sealed{},Events:[]Event{}}}
func validHex(s string,n int)bool{
	if len(s)!=n{return false}
	raw,err:=hex.DecodeString(s)
	return err==nil&&len(raw)*2==n&&hex.EncodeToString(raw)==s
}
func stageReady(m configstage.Metadata)bool{
	return validHex(m.OperationID,32)&&validHex(m.InstallationID,32)&&
		validHex(m.ManifestSHA256,64)&&validHex(m.ConfigSHA256,64)&&
		validHex(m.TrustedXraySHA256,64)&&m.NodeID!=""&&
		m.Status==configstage.Verified&&m.CanaryAttempted&&m.CanaryLoopbackBound&&
		!m.CanActivate&&m.NeedsDataPlaneProof&&m.NeedsSubscriptionCommit&&
		len(m.Listeners)>0&&m.Revision>=2
}
func validVersion(v Version)bool{
	return validHex(v.ID,32)&&validHex(v.SourceOperationID,32)&&
		validHex(v.InstallationID,32)&&validHex(v.ManifestSHA256,64)&&
		validHex(v.ConfigSHA256,64)&&validHex(v.TrustedXraySHA256,64)&&
		v.NodeID!=""&&v.ListenerCount>0&&v.ListenerCount<=64&&
		!v.LiveApplied&&!v.CanActivate&&!v.CreatedAt.IsZero()
}
func versionAAD(v Version)[]byte{
	encoded,_:=json.Marshal(struct{
		ID string `json:"id"`
		Node string `json:"node"`
		Installation string `json:"installation"`
		Manifest string `json:"manifest"`
		Config string `json:"config"`
		Binary string `json:"binary"`
		Operation string `json:"operation"`
	}{v.ID,v.NodeID,v.InstallationID,v.ManifestSHA256,v.ConfigSHA256,v.TrustedXraySHA256,v.SourceOperationID})
	return append([]byte("DARK-PRO-LABREV-v1|"),encoded...)
}
func aeadFor(key []byte)(cipher.AEAD,error){
	block,err:=aes.NewCipher(key)
	if err!=nil{return nil,err}
	return cipher.NewGCM(block)
}
func checkState(current disk,key []byte)error{
	if current.Schema!=1||current.Generation==0||
		current.Versions==nil||len(current.Versions)>maxVersions||
		len(current.Events)>maxEvents{return ErrUnsafe}
	if current.Selected!="" {
		if _,ok:=current.Versions[current.Selected];!ok{return ErrUnsafe}
	}
	if current.Previous!=""{
		if current.Previous==current.Selected{return ErrUnsafe}
		if _,ok:=current.Versions[current.Previous];!ok{return ErrUnsafe}
	}
	aead,err:=aeadFor(key)
	if err!=nil{return ErrUnsafe}
	for id,entry:=range current.Versions{
		if entry.Version.ID!=id||!validVersion(entry.Version)||
			len(entry.Nonce)!=aead.NonceSize()||
			len(entry.Ciphertext)<=aead.Overhead(){return ErrUnsafe}
		plain,err:=aead.Open(nil,entry.Nonce,entry.Ciphertext,versionAAD(entry.Version))
		if err!=nil{return ErrUnsafe}
		hash:=sha256.Sum256(plain)
		decoded,_:=hex.DecodeString(entry.Version.ConfigSHA256)
		if subtle.ConstantTimeCompare(hash[:],decoded)!=1{return ErrUnsafe}
	}
	var last uint64
	for _,event:=range current.Events{
		if event.Generation<=last||event.Generation>current.Generation||
			event.Action==""||event.At.IsZero(){return ErrUnsafe}
		last=event.Generation
	}
	return nil
}

// Open fails closed for missing/wrong key, corrupted state and insecure file
// permissions. The caller must persist and recover the SAME external 256-bit
// secret; it must never be written to GitHub or the journal.
func Open(path string,key []byte,clock func()time.Time)(*Store,error){
	if path==""||len(key)!=32{return nil,ErrUnsafe}
	abs,err:=filepath.Abs(path)
	if err!=nil{return nil,err}
	dir:=filepath.Dir(abs)
	if err:=os.MkdirAll(dir,0700);err!=nil{return nil,err}
	info,err:=os.Lstat(dir)
	if err!=nil{return nil,err}
	if !info.IsDir()||info.Mode().Perm()&0077!=0{return nil,ErrUnsafe}
	if clock==nil{clock=time.Now}
	s:=&Store{path:abs,key:append([]byte(nil),key...),clock:clock,state:newDisk()}
	file,err:=os.Lstat(abs)
	if errors.Is(err,os.ErrNotExist){return s,nil}
	if err!=nil{return nil,err}
	if !file.Mode().IsRegular()||file.Mode().Perm()&0077!=0||
		file.Size()>maxStoredBytes{return nil,ErrUnsafe}
	bytesOnDisk,err:=os.ReadFile(abs)
	if err!=nil{return nil,err}
	dec:=json.NewDecoder(bytes.NewReader(bytesOnDisk))
	dec.DisallowUnknownFields()
	var current disk
	if err:=dec.Decode(&current);err!=nil{return nil,ErrUnsafe}
	var trailing any
	if err:=dec.Decode(&trailing);!errors.Is(err,io.EOF){return nil,ErrUnsafe}
	if err:=checkState(current,s.key);err!=nil{return nil,err}
	s.state=current
	return s,nil
}

func (s *Store) persist(next disk)error{
	raw,err:=json.Marshal(next)
	if err!=nil||len(raw)>maxStoredBytes{return ErrUnsafe}
	dir:=filepath.Dir(s.path)
	fd,err:=os.CreateTemp(dir,".labrev-")
	if err!=nil{return err}
	temp:=fd.Name()
	defer os.Remove(temp)
	if err:=fd.Chmod(0600);err!=nil{fd.Close();return err}
	if _,err:=fd.Write(raw);err!=nil{fd.Close();return err}
	if err:=fd.Sync();err!=nil{fd.Close();return err}
	if err:=fd.Close();err!=nil{return err}
	if err:=os.Rename(temp,s.path);err!=nil{return err}
	d,err:=os.Open(dir)
	if err!=nil{return err}
	defer d.Close()
	if err:=d.Sync();err!=nil{return err}
	s.state=next
	return nil
}
func clone(d disk)disk{
	out:=disk{
		Schema:1,Generation:d.Generation,Selected:d.Selected,Previous:d.Previous,
		Versions:make(map[string]sealed,len(d.Versions)),
		Events:append([]Event(nil),d.Events...),
	}
	for k,v:=range d.Versions{out.Versions[k]=v}
	return out
}
func generateID()(string,error){
	var raw [16]byte
	if _,err:=rand.Read(raw[:]);err!=nil{return "",err}
	return hex.EncodeToString(raw[:]),nil
}
func view(d disk)Snapshot{
	output:=Snapshot{
		Generation:d.Generation,SelectedVersion:d.Selected,PreviousVersion:d.Previous,
		Versions:make([]Version,0,len(d.Versions)),
		Events:append([]Event(nil),d.Events...),
		ActualXrayProcessChanged:false,
		NeedsLiveHealthAndProtocolProof:true,
		NeedsSubscriptionCommit:true,
	}
	for _,v:=range d.Versions{output.Versions=append(output.Versions,v.Version)}
	sort.Slice(output.Versions,func(i,j int)bool{return output.Versions[i].ID<output.Versions[j].ID})
	return output
}
func (s *Store) Snapshot()Snapshot{
	s.mu.RLock()
	defer s.mu.RUnlock()
	return view(s.state)
}
func (s *Store) Prepare(meta configstage.Metadata,config []byte)(Version,error){
	if !stageReady(meta)||len(config)==0||len(config)>xrayconfig.MaxCandidateBytes{
		return Version{},ErrInvalid
	}
	sum:=sha256.Sum256(config)
	given,_:=hex.DecodeString(meta.ConfigSHA256)
	if subtle.ConstantTimeCompare(sum[:],given)!=1{return Version{},ErrInvalid}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _,known:=range s.state.Versions{
		v:=known.Version
		if v.SourceOperationID==meta.OperationID{
			if v.ConfigSHA256==meta.ConfigSHA256&&v.NodeID==meta.NodeID&&
				v.InstallationID==meta.InstallationID{return v,nil}
			return Version{},ErrConflict
		}
	}
	if len(s.state.Versions)>=maxVersions||len(s.state.Events)>=maxEvents{return Version{},ErrConflict}
	id,err:=generateID()
	if err!=nil{return Version{},err}
	v:=Version{
		ID:id,SourceOperationID:meta.OperationID,NodeID:meta.NodeID,
		InstallationID:meta.InstallationID,ManifestSHA256:meta.ManifestSHA256,
		ConfigSHA256:meta.ConfigSHA256,TrustedXraySHA256:meta.TrustedXraySHA256,
		ListenerCount:len(meta.Listeners),CreatedAt:s.clock().UTC(),
		LiveApplied:false,CanActivate:false,
	}
	aead,err:=aeadFor(s.key)
	if err!=nil{return Version{},err}
	nonce:=make([]byte,aead.NonceSize())
	if _,err:=rand.Read(nonce);err!=nil{return Version{},err}
	sealedData:=aead.Seal(nil,nonce,config,versionAAD(v))
	next:=clone(s.state)
	next.Generation++
	next.Versions[id]=sealed{Version:v,Nonce:nonce,Ciphertext:sealedData}
	next.Events=append(next.Events,Event{
		Generation:next.Generation,Action:"prepared_no_apply",
		SelectedVersion:next.Selected,PreviousVersion:next.Previous,At:s.clock().UTC(),
	})
	if err:=s.persist(next);err!=nil{return Version{},err}
	return v,nil
}

// Select changes ONLY a private LAB selection pointer with CAS. It NEVER
// changes systemd, Xray socket binds, historical usage or subscriptions.
func (s *Store) Select(id string,expectedGeneration uint64)(Snapshot,error){
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Generation!=expectedGeneration{return Snapshot{},ErrConflict}
	if _,ok:=s.state.Versions[id];!ok{return Snapshot{},ErrMissing}
	if s.state.Selected==id{return view(s.state),nil}
	if len(s.state.Events)>=maxEvents{return Snapshot{},ErrConflict}
	next:=clone(s.state)
	next.Generation++
	next.Previous=next.Selected
	next.Selected=id
	next.Events=append(next.Events,Event{
		Generation:next.Generation,Action:"selected_for_lab_rehearsal_not_applied",
		SelectedVersion:next.Selected,PreviousVersion:next.Previous,At:s.clock().UTC(),
	})
	if err:=s.persist(next);err!=nil{return Snapshot{},err}
	return view(next),nil
}

// RollbackSelection reverses only the last un-applied LAB version selection.
// Repeated/stale rollback is rejected rather than toggling back to a bad plan.
func (s *Store) RollbackSelection(expectedGeneration uint64)(Snapshot,error){
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Generation!=expectedGeneration{return Snapshot{},ErrConflict}
	if s.state.Selected==""||s.state.Previous==""{return Snapshot{},ErrNoPrevious}
	if len(s.state.Events)>=maxEvents{return Snapshot{},ErrConflict}
	next:=clone(s.state)
	next.Generation++
	next.Selected=next.Previous
	next.Previous=""
	next.Events=append(next.Events,Event{
		Generation:next.Generation,Action:"lab_selection_rolled_back_no_process_change",
		SelectedVersion:next.Selected,At:s.clock().UTC(),
	})
	if err:=s.persist(next);err!=nil{return Snapshot{},err}
	return view(next),nil
}

// SelectedPrivate may ONLY be called internally for a previously paired LAB
// installation. It must never be wired to owner JSON or public transport.
func (s *Store) SelectedPrivate(nodeID,installation string)(Version,[]byte,error){
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.Selected==""{return Version{},nil,ErrMissing}
	v:=s.state.Versions[s.state.Selected]
	if v.Version.NodeID!=nodeID||v.Version.InstallationID!=installation{
		return Version{},nil,ErrMissing
	}
	aead,err:=aeadFor(s.key)
	if err!=nil{return Version{},nil,ErrUnsafe}
	raw,err:=aead.Open(nil,v.Nonce,v.Ciphertext,versionAAD(v.Version))
	if err!=nil{return Version{},nil,ErrUnsafe}
	return v.Version,raw,nil
}
