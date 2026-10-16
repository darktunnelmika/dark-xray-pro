//go:build linux

package usagecheckpoint

import("crypto/aes";"crypto/cipher";"crypto/rand";"encoding/json";"errors";"io";"math";"os";"path/filepath";"reflect";"sync";"syscall")
const maxStateBytes=64<<20
type Options struct{
 Directory string;Key []byte;Epoch Epoch
 // Create is only for a newly registered epoch, never recovery with guessed zero.
 Create bool
}
type persisted struct{Version int `json:"version"`;Epoch Epoch `json:"epoch"`;Last *Report `json:"last,omitempty"`;Pending []Report `json:"pending"`;Ack Receipt `json:"last_ack"`}
type Outbox struct{mu sync.Mutex;dir string;file *os.File;aead cipher.AEAD;aad []byte;state persisted;closed,fenced bool;hook func(string)error}
func checkParents(dir string)error{for p:=dir;p!="/"&&p!=".";p=filepath.Dir(p){info,e:=os.Lstat(p);if errors.Is(e,os.ErrNotExist){continue};if e!=nil||!info.IsDir()||info.Mode()&os.ModeSymlink!=0{return ErrPrivate}};return nil}
func checkPrivate(info os.FileInfo,directory bool)error{
 st,ok:=info.Sys().(*syscall.Stat_t);if !ok||int(st.Uid)!=os.Geteuid()||info.Mode().Perm()&0077!=0{return ErrPrivate}
 if directory{if !info.IsDir(){return ErrPrivate}}else if !info.Mode().IsRegular()||st.Nlink!=1{return ErrPrivate};return nil
}
func Open(o Options)(*Outbox,error){
 e,err:=normalize(o.Epoch);if err!=nil{return nil,err}
 if !filepath.IsAbs(o.Directory)||filepath.Clean(o.Directory)!=o.Directory||len(o.Key)!=32{return nil,ErrPrivate}
 if err=checkParents(o.Directory);err!=nil{return nil,err};if o.Create{if err=os.MkdirAll(o.Directory,0700);err!=nil{return nil,err}}
 if err=checkParents(o.Directory);err!=nil{return nil,err};info,err:=os.Lstat(o.Directory);if err!=nil{return nil,err};if err=checkPrivate(info,true);err!=nil{return nil,err}
 lockPath:=filepath.Join(o.Directory,"usage.lock");fd,err:=syscall.Open(lockPath,syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0600);if err!=nil{return nil,ErrPrivate}
 lock:=os.NewFile(uintptr(fd),lockPath);fail:=func(e error)(*Outbox,error){lock.Close();return nil,e}
 info,err=lock.Stat();if err!=nil{return fail(err)};if err=checkPrivate(info,false);err!=nil{return fail(err)}
 if syscall.Flock(fd,syscall.LOCK_EX|syscall.LOCK_NB)!=nil{return fail(ErrLocked)}
 block,err:=aes.NewCipher(o.Key);if err!=nil{return fail(err)};aead,err:=cipher.NewGCM(block);if err!=nil{return fail(err)}
 b:=&Outbox{dir:o.Directory,file:lock,aead:aead,aad:[]byte("DARK-PRO-USAGE-CHECKPOINT-v1:"+bindingSHA(e)),state:persisted{Version:1,Epoch:e,Pending:[]Report{}}}
 raw,err:=privateRead(filepath.Join(o.Directory,"usage.enc"))
 if o.Create{
  if err==nil{return fail(ErrConflict)};if !errors.Is(err,os.ErrNotExist){return fail(err)}
  entries,readErr:=os.ReadDir(o.Directory);if readErr!=nil{return fail(readErr)};for _,entry:=range entries{if entry.Name()!="usage.lock"{return fail(ErrPrivate)}}
  if err=b.save(b.state);err!=nil{return fail(err)};return b,nil
 }
 if err!=nil{return fail(err)};n:=aead.NonceSize();if len(raw)<n+aead.Overhead(){return fail(ErrCorrupt)}
 clear,err:=aead.Open(nil,raw[:n],raw[n:],b.aad);if err!=nil{return fail(ErrCorrupt)};defer func(){for i:=range clear{clear[i]=0}}()
 var saved persisted;if json.Unmarshal(clear,&saved)!=nil{return fail(ErrCorrupt)};if err=validateSaved(e,saved);err!=nil{return fail(ErrCorrupt)}
 b.state=saved;return b,nil
}
func privateRead(path string)([]byte,error){
 fd,err:=syscall.Open(path,syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC,0);if err!=nil{return nil,err}
 f:=os.NewFile(uintptr(fd),path);defer f.Close();info,err:=f.Stat();if err!=nil{return nil,err};if err=checkPrivate(info,false);err!=nil{return nil,err}
 if info.Size()>maxStateBytes+64{return nil,ErrCorrupt};raw,err:=io.ReadAll(io.LimitReader(f,maxStateBytes+65));if len(raw)>maxStateBytes+64{return nil,ErrCorrupt};return raw,err
}
func validateSaved(e Epoch,s persisted)error{
 if s.Version!=1||!reflect.DeepEqual(e,s.Epoch)||len(s.Pending)>MaxPending{return ErrCorrupt}
 if s.Last==nil{if len(s.Pending)!=0||s.Ack!=(Receipt{}){return ErrCorrupt};return nil}
 if validateReport(e,*s.Last)!=nil{return ErrCorrupt}
 if s.Ack.Sequence>0{if s.Ack.EpochID!=e.ID||!hexID(s.Ack.SHA,64){return ErrCorrupt}}else if s.Ack!=(Receipt{}){return ErrCorrupt}
 if s.Last.Sequence<s.Ack.Sequence||s.Last.Sequence-s.Ack.Sequence!=uint64(len(s.Pending)){return ErrCorrupt}
 for i,r:=range s.Pending{
  if validateReport(e,r)!=nil||r.Sequence!=s.Ack.Sequence+uint64(i)+1{return ErrCorrupt}
  if i>0{prev:=s.Pending[i-1];cur:=Cursor{EpochID:e.ID,Sequence:prev.Sequence,ReportSHA:prev.SHA,Users:prev.Users,Quiesced:prev.Quiesced};if _,_,_,err:=Calculate(e,cur,r);err!=nil{return ErrCorrupt}}
 }
 if len(s.Pending)>0{if s.Pending[len(s.Pending)-1].SHA!=s.Last.SHA{return ErrCorrupt}}else if s.Ack.SHA!=s.Last.SHA{return ErrCorrupt};return nil
}
func(b *Outbox)save(s persisted)error{
 clear,err:=json.Marshal(s);if err!=nil{return err};defer func(){for i:=range clear{clear[i]=0}}();if len(clear)>maxStateBytes{return ErrFull}
 nonce:=make([]byte,b.aead.NonceSize());if _,err=rand.Read(nonce);err!=nil{return err};raw:=b.aead.Seal(nonce,nonce,clear,b.aad)
 f,err:=os.CreateTemp(b.dir,".usage-");if err!=nil{return err};path:=f.Name();defer os.Remove(path)
 if err=f.Chmod(0600);err==nil{_,err=f.Write(raw)};if err==nil{err=f.Sync()};endErr:=f.Close();if err==nil{err=endErr};if err!=nil{return err}
 if b.hook!=nil{if err=b.hook("before_rename");err!=nil{return err}}
 if err=os.Rename(path,filepath.Join(b.dir,"usage.enc"));err!=nil{return err}
 if b.hook!=nil{if err=b.hook("after_rename");err!=nil{return err}}
 dir,err:=os.Open(b.dir);if err!=nil{return err};defer dir.Close();return dir.Sync()
}
func(b *Outbox)commit(s persisted)error{if err:=b.save(s);err!=nil{if !errors.Is(err,ErrFull){b.fenced=true};return err};b.state=s;return nil}
// Quiesced means the SUPERVISOR has drained traffic, not merely read counters.
// Persistence/epoch errors must withhold runtime authorization at the caller.
func(b *Outbox)Observe(users []Usage,quiesced bool)(Report,error){
 b.mu.Lock();defer b.mu.Unlock();if b.closed||b.fenced{return Report{},ErrClosed}
 users,err:=canonicalUsers(b.state.Epoch,users);if err!=nil{return Report{},err};seq:=uint64(1)
 if old:=b.state.Last;old!=nil{
  if old.Quiesced!=quiesced&&old.Quiesced{return Report{},ErrSealed}
  if old.Quiesced==quiesced&&reflect.DeepEqual(old.Users,users){return cloneReport(*old),nil}
  if old.Quiesced{return Report{},ErrSealed};if old.Sequence==math.MaxUint64{return Report{},ErrOverflow}
  for i,u:=range users{if u.Uplink<old.Users[i].Uplink||u.Downlink<old.Users[i].Downlink{return Report{},ErrRegression}};seq=old.Sequence+1
 }
 if len(b.state.Pending)>=MaxPending{return Report{},ErrFull};report,err:=makeReport(b.state.Epoch,seq,users,quiesced);if err!=nil{return Report{},err}
 state:=b.state;state.Pending=append(append([]Report(nil),state.Pending...),report);state.Last=&report
 if err=b.commit(state);err!=nil{return Report{},err};return cloneReport(report),nil
}
func(b *Outbox)Pending()([]Report,error){b.mu.Lock();defer b.mu.Unlock();if b.closed||b.fenced{return nil,ErrClosed};result:=make([]Report,len(b.state.Pending));for i,r:=range b.state.Pending{result[i]=cloneReport(r)};return result,nil}
// Only an authenticated exact committed receipt can remove the oldest report.
func(b *Outbox)Acknowledge(receipt Receipt)error{
 b.mu.Lock();defer b.mu.Unlock();if b.closed||b.fenced{return ErrClosed};if receipt.Sequence!=0&&receipt==b.state.Ack{return nil}
 if len(b.state.Pending)==0{return ErrReceipt};first:=b.state.Pending[0]
 if receipt.EpochID!=first.EpochID||receipt.Sequence!=first.Sequence||receipt.SHA!=first.SHA{return ErrReceipt}
 state:=b.state;state.Pending=append([]Report{},state.Pending[1:]...);state.Ack=receipt;return b.commit(state)
}
func(b *Outbox)Last()(Report,bool,error){b.mu.Lock();defer b.mu.Unlock();if b.closed||b.fenced{return Report{},false,ErrClosed};if b.state.Last==nil{return Report{},false,nil};return cloneReport(*b.state.Last),true,nil}
func(b *Outbox)Close()error{b.mu.Lock();defer b.mu.Unlock();if b.closed{return nil};b.closed=true;return b.file.Close()}
