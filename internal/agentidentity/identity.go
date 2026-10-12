// Package agentidentity persists only a NEW LAB Node installation identity.
// No legacy HUB credentials, Node leases, tunnels, or Xray settings are loaded.
package agentidentity

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ErrUnsafeFile = errors.New("unsafe agent identity file or directory")
	ErrInvalid = errors.New("invalid agent identity or pairing code")
	ErrPairRequired = errors.New("new agent requires a one-time pairing code")
	idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Identity struct {
	NodeID string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	AgentToken string `json:"agent_token"`
	// Retained only until HUB has persisted a matching enrollment.
	PairCode string `json:"pending_pair_code,omitempty"`
	Paired bool `json:"paired"`
}

func randomHex(bytes int) (string,error) {
	value:=make([]byte,bytes)
	if _,err:=rand.Read(value);err!=nil { return "",err }
	return hex.EncodeToString(value),nil
}

func valid(i Identity) bool {
	if !idPattern.MatchString(i.NodeID) || !hex32.MatchString(i.InstallationID) || !hex64.MatchString(i.AgentToken) {
		return false
	}
	if i.Paired { return i.PairCode=="" }
	return hex64.MatchString(i.PairCode)
}

func parentDir(path string) (string,error) {
	abs,err:=filepath.Abs(path)
	if err!=nil {return "",err}
	dir:=filepath.Dir(abs)
	if err:=os.MkdirAll(dir,0700);err!=nil {return "",err}
	info,err:=os.Lstat(dir)
	if err!=nil {return "",err}
	if !info.IsDir() || info.Mode().Perm()&0077!=0 {return "",ErrUnsafeFile}
	return abs,nil
}

// LoadOrCreate writes a new identity BEFORE making a network request. An
// interrupted enrollment can retry the same code/token without new credentials.
func LoadOrCreate(path,id,code string) (Identity,error) {
	var zero Identity
	if !idPattern.MatchString(id) {return zero,ErrInvalid}
	path,err:=parentDir(path)
	if err!=nil {return zero,err}
	info,err:=os.Lstat(path)
	if err==nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077!=0 || info.Size()>2048 {
			return zero,ErrUnsafeFile
		}
		data,err:=os.ReadFile(path)
		if err!=nil {return zero,err}
		var identity Identity
		if err:=json.Unmarshal(data,&identity);err!=nil {return zero,ErrInvalid}
		if !valid(identity) || identity.NodeID!=id {return zero,ErrInvalid}
		if code!="" && (identity.Paired || code!=identity.PairCode) {return zero,ErrInvalid}
		return identity,nil
	}
	if !errors.Is(err,os.ErrNotExist) {return zero,err}
	if code=="" {return zero,ErrPairRequired}
	if !hex64.MatchString(code) {return zero,ErrInvalid}
	install,err:=randomHex(16)
	if err!=nil {return zero,err}
	token,err:=randomHex(32)
	if err!=nil {return zero,err}
	identity:=Identity{NodeID:id,InstallationID:install,AgentToken:token,PairCode:code}
	if err:=writeNew(path,identity);err!=nil {return zero,err}
	return identity,nil
}

func writeNew(path string, i Identity) error {
	body,err:=json.Marshal(i)
	if err!=nil {return err}
	file,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
	if err!=nil {return err}
	_,err=file.Write(body)
	if err==nil {err=file.Sync()}
	closeErr:=file.Close()
	if err==nil {err=closeErr}
	if err!=nil {return err}
	return nil
}

// MarkPaired atomically removes the secret Pair Code after server ACK.
// On failure the pending credential stays intact for idempotent retry.
func MarkPaired(path string, old Identity) (Identity,error) {
	if !valid(old) || old.Paired {
		return Identity{},ErrInvalid
	}
	path,err:=parentDir(path)
	if err!=nil {return Identity{},err}
	loaded,err:=LoadOrCreate(path,old.NodeID,"")
	if err!=nil || loaded!=old {return Identity{},ErrInvalid}
	next:=old
	next.Paired=true
	next.PairCode=""
	encoded,err:=json.Marshal(next)
	if err!=nil {return Identity{},err}
	fd,err:=os.CreateTemp(filepath.Dir(path),".agent-identity-")
	if err!=nil {return Identity{},err}
	temp:=fd.Name()
	defer os.Remove(temp)
	if err:=fd.Chmod(0600);err!=nil {fd.Close();return Identity{},err}
	if _,err:=fd.Write(encoded);err!=nil {fd.Close();return Identity{},err}
	if err:=fd.Sync();err!=nil {fd.Close();return Identity{},err}
	if err:=fd.Close();err!=nil {return Identity{},err}
	if err:=os.Rename(temp,path);err!=nil {return Identity{},err}
	d,err:=os.Open(filepath.Dir(path))
	if err!=nil {return Identity{},err}
	defer d.Close()
	if err:=d.Sync();err!=nil {return Identity{},err}
	return next,nil
}

// SecureRead deliberately never returns private metadata in API responses.
func SecureRead(path,id string) (Identity,error) {
	return LoadOrCreate(path,id,"")
}

// FileReadable ensures the local identity remains private after provisioning.
func FileReadable(path string) error {
	file,err:=os.Open(path)
	if err!=nil {return err}
	defer file.Close()
	_,err=io.Copy(io.Discard,io.LimitReader(file,1))
	return err
}
