//go:build linux

// Package managedxray owns ONLY new PRO Xray children, never system services.
package managedxray

import (
 "crypto/aes"
 "crypto/cipher"
 "crypto/rand"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "syscall"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
 uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)
var (
 ErrInvalid=errors.New("invalid managed Xray revision")
 ErrLease=errors.New("fresh installation-scoped Xray lease required")
 ErrBusy=errors.New("managed Xray directory is already locked")
 ErrPrivate=errors.New("managed Xray directory or file is not private")
 ErrCorrupt=errors.New("managed Xray journal authentication failed")
 ErrStale=errors.New("managed Xray revision is stale or conflicts")
 ErrRuntime=errors.New("managed Xray candidate did not become ready")
 ErrClosed=errors.New("managed Xray controller is closed or fenced")
)
type Revision struct{Sequence uint64;Candidate xrayconfig.Candidate}
type savedRevision struct{
 Sequence uint64 `json:"sequence"`;Raw []byte `json:"config"`;SHA string `json:"sha"`
 Manifest string `json:"manifest"`;Listeners []xrayconfig.Listener `json:"listeners"`;NoListeners bool `json:"no_listeners,omitempty"`
}
func freeze(r Revision)*savedRevision{
 return &savedRevision{Sequence:r.Sequence,NoListeners:r.Candidate.NoListeners,Raw:append([]byte(nil),r.Candidate.Config...),SHA:r.Candidate.SHA256,Manifest:r.Candidate.ManifestSHA256,Listeners:append([]xrayconfig.Listener(nil),r.Candidate.Listeners...)}
}
func(r *savedRevision)candidate()xrayconfig.Candidate{
 return xrayconfig.Candidate{NoListeners:r.NoListeners,Config:append([]byte(nil),r.Raw...),SHA256:r.SHA,ManifestSHA256:r.Manifest,Listeners:append([]xrayconfig.Listener(nil),r.Listeners...),RequiresXrayTest:true,RequiresDataPlaneProof:true,RequiresSubscriptionCommit:true}
}
type journal struct{
 Admission *usageAdmission `json:"usage_admission,omitempty"`
 Usage *observedUsage `json:"usage,omitempty"`
 DesiredSHA string `json:"desired_sha"`;DesiredManifest string `json:"desired_manifest"`
 Version int `json:"version"`;Highwater uint64 `json:"highwater"`;LastLeaseIssued int64 `json:"last_lease_issued"`;Updated int64 `json:"updated"`
 Good *savedRevision `json:"good,omitempty"`;Previous *savedRevision `json:"previous,omitempty"`;Pending *savedRevision `json:"pending,omitempty"`;Phase string `json:"phase"`
}
type disk struct{dir string;lock *os.File;aead cipher.AEAD;aad []byte}
func privateAncestors(path string)error{
 for p:=filepath.Clean(path);p!="/"&&p!=".";p=filepath.Dir(p){info,err:=os.Lstat(p);if errors.Is(err,os.ErrNotExist){continue};if err!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0{return ErrPrivate}}
 return nil
}
func privateRead(path string,limit int64)([]byte,error){
 fd,err:=syscall.Open(path,syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0);if err!=nil{return nil,err}
 f:=os.NewFile(uintptr(fd),path);defer f.Close();info,err:=f.Stat()
 if err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||info.Size()>limit{return nil,ErrPrivate}
 st,ok:=info.Sys().(*syscall.Stat_t);if !ok||int(st.Uid)!=os.Geteuid()||st.Nlink!=1{return nil,ErrPrivate}
 b,err:=io.ReadAll(io.LimitReader(f,limit+1));if int64(len(b))>limit{return nil,ErrPrivate};return b,err
}
func openDisk(dir string,key []byte,aad string)(*disk,journal,error){
 empty:=journal{Version:1,Phase:"awaiting_fresh_lease"}
 if os.Geteuid()==0||!filepath.IsAbs(dir)||filepath.Clean(dir)!=dir||len(key)!=32{return nil,empty,ErrPrivate}
 if err:=privateAncestors(dir);err!=nil{return nil,empty,err}
 if err:=os.MkdirAll(dir,0700);err!=nil{return nil,empty,err}
 if err:=privateAncestors(dir);err!=nil{return nil,empty,err}
 info,err:=os.Lstat(dir);if err!=nil||info.Mode().Perm()&0077!=0{return nil,empty,ErrPrivate}
 st,ok:=info.Sys().(*syscall.Stat_t);if !ok||int(st.Uid)!=os.Geteuid(){return nil,empty,ErrPrivate}
 fd,err:=syscall.Open(filepath.Join(dir,"runtime.lock"),syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,0600);if err!=nil{return nil,empty,ErrPrivate}
 lock:=os.NewFile(uintptr(fd),"runtime.lock");li,err:=lock.Stat()
 if err!=nil||!li.Mode().IsRegular()||li.Mode().Perm()&0077!=0{lock.Close();return nil,empty,ErrPrivate}
 if err=syscall.Flock(fd,syscall.LOCK_EX|syscall.LOCK_NB);err!=nil{lock.Close();return nil,empty,ErrBusy}
 block,err:=aes.NewCipher(key);if err!=nil{lock.Close();return nil,empty,err};aead,err:=cipher.NewGCM(block);if err!=nil{lock.Close();return nil,empty,err}
 d:=&disk{dir:dir,lock:lock,aead:aead,aad:[]byte(aad)}
 raw,err:=privateRead(filepath.Join(dir,"runtime.enc"),24<<20)
 if errors.Is(err,os.ErrNotExist){return d,empty,nil};if err!=nil{d.close();return nil,empty,err}
 if len(raw)<aead.NonceSize()+aead.Overhead(){d.close();return nil,empty,ErrCorrupt}
 plain,err:=aead.Open(nil,raw[:aead.NonceSize()],raw[aead.NonceSize():],d.aad);if err!=nil{d.close();return nil,empty,ErrCorrupt}
 var j journal
 if json.Unmarshal(plain,&j)!=nil||(j.Version!=1&&j.Version!=2&&j.Version!=3&&j.Version!=4&&j.Version!=5)||j.Good!=nil&&j.Good.Sequence>j.Highwater{d.close();return nil,empty,ErrCorrupt}
 if (j.Version==1&&(j.Usage!=nil||j.Admission!=nil))||(j.Version==2&&(j.Usage==nil||j.Admission!=nil))||((j.Version==3||j.Version==4||j.Version==5)&&!validPersistedAdmission(j)){d.close();return nil,empty,ErrCorrupt}
 if j.Usage!=nil{e,_,err:=uc.CanonicalEpoch(j.Usage.Epoch);if err!=nil||j.Good==nil||e.ConfigSHA!=j.Good.SHA{d.close();return nil,empty,ErrCorrupt};if _,err=uc.StatsArguments(j.Usage.StatsEndpoint);err!=nil{d.close();return nil,empty,ErrCorrupt}}
 return d,j,nil
}
func(d *disk)save(j journal)error{
 raw,err:=json.Marshal(j);if err!=nil||len(raw)>20<<20{return ErrInvalid}
 nonce:=make([]byte,d.aead.NonceSize());if _,err=rand.Read(nonce);err!=nil{return err};raw=d.aead.Seal(nonce,nonce,raw,d.aad)
 f,err:=os.CreateTemp(d.dir,".runtime-");if err!=nil{return err};temp:=f.Name();defer os.Remove(temp)
 if err=f.Chmod(0600);err==nil{_,err=f.Write(raw)};if err==nil{err=f.Sync()};ce:=f.Close();if err==nil{err=ce};if err!=nil{return err}
 if err=os.Rename(temp,filepath.Join(d.dir,"runtime.enc"));err!=nil{return err}
 parent,err:=os.Open(d.dir);if err!=nil{return err};defer parent.Close();return parent.Sync()
}
func(d *disk)close(){if d!=nil&&d.lock!=nil{_=syscall.Flock(int(d.lock.Fd()),syscall.LOCK_UN);_=d.lock.Close()}}
