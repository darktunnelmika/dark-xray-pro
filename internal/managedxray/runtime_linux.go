//go:build linux

package managedxray

import (
 "context"
 "crypto/hmac"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "net"
 "sync"
 "time"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
// Lease is installation-, revision-, config- and expiry-bound authority.
// Signing material is private and separate from the encrypted journal key.
type Lease struct {
 NodeID string `json:"node_id"`;InstallationID string `json:"installation_id"`
 Sequence uint64 `json:"sequence"`;ConfigSHA string `json:"config_sha"`;ManifestSHA string `json:"manifest_sha"`
 Issued int64 `json:"issued_unix_nano"`;Expires int64 `json:"expires_unix_nano"`;Signature string `json:"signature"`
}
func(l Lease)signature(key []byte)string{l.Signature="";raw,_:=json.Marshal(l);m:=hmac.New(sha256.New,key);m.Write([]byte("DARK-PRO-RUNTIME-LEASE-v1\x00"));m.Write(raw);return hex.EncodeToString(m.Sum(nil))}
func SignLease(key []byte,l Lease)(Lease,error){
 if len(key)<32||l.Sequence==0||l.NodeID==""||len(l.InstallationID)!=32||len(l.ConfigSHA)!=64||len(l.ManifestSHA)!=64||l.Expires<=l.Issued||l.Expires-l.Issued>int64(45*time.Second){return Lease{},ErrLease}
 l.Signature=l.signature(key);return l,nil
}
type Options struct{Directory string;StateKey []byte;LeaseKey []byte;NodeID string;InstallationID string;ListenIP string;Binary string;BinarySHA string;ReservedPorts []uint16;ExperimentalFinalTCP bool}
type Snapshot struct{
 Usage *UsageObservationStatus `json:"usage_observation,omitempty"`
 NoListeners bool `json:"no_listeners"`;
 ActiveSequence uint64 `json:"active_sequence"`;ActiveSHA string `json:"active_sha"`;SavedSequence uint64 `json:"saved_sequence"`;SavedSHA string `json:"saved_sha"`
 PreviousSHA string `json:"previous_sha"`;Highwater uint64 `json:"highwater"`;Phase string `json:"phase"`;ProcessReady bool `json:"process_ready"`
 LeaseExpires int64 `json:"lease_expires_unix_nano"`;DataPlaneVerified bool `json:"data_plane_verified"`;SubscriptionPublished bool `json:"subscription_published"`
}
type Runtime struct{
 observer *usageObserver
 mu sync.Mutex;options Options;disk *disk;state journal;process *child;active *savedRevision;lease Lease;deadline time.Time
 previousLease Lease;previousDeadline time.Time;reserved map[uint16]bool;closed bool;fenced bool;stop chan struct{};done chan struct{}
 afterIntent func() // Test-only crash checkpoint, never externally configured.
}
func Open(o Options)(*Runtime,error){
 ip:=net.ParseIP(o.ListenIP)
 if ip==nil||ip.IsUnspecified()||ip.IsMulticast()||o.NodeID==""||len(o.NodeID)>128||len(o.InstallationID)!=32||len(o.LeaseKey)<32{return nil,ErrInvalid}
 if _,err:=hex.DecodeString(o.InstallationID);err!=nil{return nil,ErrInvalid}
 if err:=CheckLocalAddress(ip.String());err!=nil{return nil,err}
 o.ListenIP=ip.String();o.StateKey=append([]byte(nil),o.StateKey...);o.LeaseKey=append([]byte(nil),o.LeaseKey...)
 d,j,err:=openDisk(o.Directory,o.StateKey,"DARK-PRO-RUNTIME-v1\x00"+o.NodeID+"\x00"+o.InstallationID+"\x00"+o.ListenIP);if err!=nil{return nil,err}
 if j.Usage!=nil&&(j.Usage.Epoch.NodeID!=o.NodeID||j.Usage.Epoch.InstallationID!=o.InstallationID){d.close();return nil,ErrCorrupt}
 if j.Updated>time.Now().Add(2*time.Second).UnixNano(){d.close();return nil,ErrLease}
 j.Phase="awaiting_fresh_lease" // Persisted readiness is not process evidence.
 r:=&Runtime{options:o,disk:d,state:j,reserved:map[uint16]bool{},stop:make(chan struct{}),done:make(chan struct{})}
 for _,p:=range o.ReservedPorts{r.reserved[p]=true};go r.watch();return r,nil
}
func(r *Runtime)validLease(l Lease,v *savedRevision)bool{
 now:=time.Now().UnixNano()
 return v!=nil&&l.NodeID==r.options.NodeID&&l.InstallationID==r.options.InstallationID&&l.Sequence==v.Sequence&&l.ConfigSHA==v.SHA&&l.ManifestSHA==v.Manifest&&l.Issued>0&&l.Issued<=now+int64(time.Second)&&l.Expires>now&&l.Expires>l.Issued&&l.Expires-l.Issued<=int64(45*time.Second)&&len(l.Signature)==64&&hmac.Equal([]byte(l.Signature),[]byte(l.signature(r.options.LeaseKey)))
}
func leaseDeadline(l Lease)time.Time{return time.Now().Add(time.Duration(l.Expires-time.Now().UnixNano()))}
func(r *Runtime)persist(j journal)error{
 j.Updated=time.Now().UnixNano()
 if err:=r.disk.save(j);err!=nil{if r.observer!=nil{r.observer.cancel()};r.process.stop();r.process=nil;r.active=nil;r.fenced=true;r.state.Phase="journal_failure_fenced";return err};r.state=j;return nil
}
func(r *Runtime)start(ctx context.Context,v *savedRevision,deadline time.Time)(*child,error){
 c:=v.candidate();if xrayconfig.RequiresUsageAdmission(c){return nil,ErrUsageSettlement};if err:=validateLayout(c,r.options.ListenIP,r.reserved);err!=nil{return nil,err}
 if _,err:=xrayconfig.ValidateOffline(ctx,c,r.options.Binary,r.options.BinarySHA);err!=nil{return nil,err}
 return startChild(ctx,c,r.options.Binary,deadline)
}
func(r *Runtime)snapshot()Snapshot{
 s:=Snapshot{Phase:r.state.Phase,Highwater:r.state.Highwater};if r.state.Usage!=nil{usage:=r.usageStatusLocked();s.Usage=&usage}
 if r.state.Good!=nil{s.SavedSequence=r.state.Good.Sequence;s.SavedSHA=r.state.Good.SHA};if r.state.Previous!=nil{s.PreviousSHA=r.state.Previous.SHA}
 if r.active!=nil&&!r.process.dead()&&time.Now().Before(r.deadline){s.NoListeners=r.active.NoListeners;s.ActiveSequence=r.active.Sequence;s.ActiveSHA=r.active.SHA;s.LeaseExpires=r.lease.Expires;s.ProcessReady=r.process.bound(r.active.Listeners)}
 return s
}
func(r *Runtime)Snapshot()Snapshot{r.mu.Lock();defer r.mu.Unlock();return r.snapshot()}
// Apply validates before disruption and fsyncs its encrypted intent first.
// It replaces processes on ACTUAL assigned ports, not remapped fake listeners.
func(r *Runtime)Apply(ctx context.Context,v Revision,l Lease)(Snapshot,error){
 r.mu.Lock();defer r.mu.Unlock();return r.applyLocked(ctx,v,l,nil)
}
// A staged command checks its reviewed base under the same lock as Apply.
func(r *Runtime)applyConditional(ctx context.Context,v Revision,l Lease,expected RuntimeCondition)(Snapshot,error){
 r.mu.Lock();defer r.mu.Unlock();return r.applyLocked(ctx,v,l,&expected)
}
func(r *Runtime)applyLocked(ctx context.Context,v Revision,l Lease,expected *RuntimeCondition)(Snapshot,error){
 if r.closed||r.fenced{return r.snapshot(),ErrClosed}
 if expected!=nil&&Condition(r.snapshot())!=*expected{return r.snapshot(),ErrStale}
 saved:=freeze(v);if !r.validLease(l,saved){return r.snapshot(),ErrLease}
 if saved.Sequence==r.state.Highwater&&r.active!=nil&&saved.Sequence==r.active.Sequence&&saved.SHA==r.active.SHA&&saved.Manifest==r.active.Manifest{return r.refreshLocked(l)}
 if r.state.Usage!=nil||r.state.Admission!=nil||xrayconfig.RequiresUsageAdmission(saved.candidate()){return r.snapshot(),ErrUsageSettlement}
 if saved.Sequence<r.state.Highwater||(saved.Sequence==r.state.Highwater&&(saved.SHA!=r.state.DesiredSHA||saved.Manifest!=r.state.DesiredManifest)){return r.snapshot(),ErrStale}
 if l.Issued<=r.state.LastLeaseIssued{return r.snapshot(),ErrLease}
 if err:=validateLayout(saved.candidate(),r.options.ListenIP,r.reserved);err!=nil{return r.snapshot(),err}
 if _,err:=xrayconfig.ValidateOffline(ctx,saved.candidate(),r.options.Binary,r.options.BinarySHA);err!=nil{return r.snapshot(),err}
 if ctx.Err()!=nil||!r.validLease(l,saved){return r.snapshot(),ErrLease}
 if expected!=nil&&Condition(r.snapshot())!=*expected{return r.snapshot(),ErrStale}
 next:=r.state;next.Highwater=saved.Sequence;next.DesiredSHA=saved.SHA;next.DesiredManifest=saved.Manifest;next.Pending=saved;next.Phase="applying";next.LastLeaseIssued=l.Issued
 if err:=r.persist(next);err!=nil{return r.snapshot(),err};if r.afterIntent!=nil{r.afterIntent()}
 old:=r.state.Good;oldLease,oldDeadline:=r.lease,r.deadline
 if removesAuthority(old,saved){oldLease=Lease{};oldDeadline=time.Time{}}
 r.process.stop();r.process=nil;r.active=nil
 p,err:=startChild(ctx,saved.candidate(),r.options.Binary,leaseDeadline(l))
 if err==nil&&!r.validLease(l,saved){p.stop();p=nil;err=ErrLease}
 if err!=nil{
  next=r.state;next.Pending=nil;next.Phase="apply_failed_stopped"
  if old!=nil&&r.validLease(oldLease,old)&&time.Now().Before(oldDeadline){
   recoverCtx,cancel:=context.WithTimeout(context.Background(),7*time.Second);previous,e:=r.start(recoverCtx,old,oldDeadline);cancel()
   if e==nil&&time.Now().Before(oldDeadline){r.process=previous;r.active=old;r.lease=oldLease;r.deadline=oldDeadline;next.Phase="rolled_back_after_failed_apply"}else if previous!=nil{previous.stop()}
  }
  if e:=r.persist(next);e!=nil{return r.snapshot(),e};return r.snapshot(),err
 }
 r.process=p;r.active=saved;r.previousLease=oldLease;r.previousDeadline=oldDeadline;r.lease=l;r.deadline=leaseDeadline(l)
 if saved.NoListeners{r.previousLease=Lease{};r.previousDeadline=time.Time{}}
 next=r.state;next.Previous=old;next.Good=saved;next.Pending=nil;next.Phase="ready"
 if err:=r.persist(next);err!=nil{return r.snapshot(),err};return r.snapshot(),nil
}
func(r *Runtime)refreshLocked(l Lease)(Snapshot,error){
 if r.state.Admission!=nil&&r.state.Usage==nil{return r.snapshot(),ErrUsageSettlement}
 if r.state.Usage!=nil&&(r.observer==nil||!r.observer.healthy()){return r.snapshot(),ErrUsageSettlement}
 if r.active==nil||r.process.dead()||!time.Now().Before(r.deadline)||!r.validLease(l,r.active){return r.snapshot(),ErrLease}
 if l.Issued<=r.state.LastLeaseIssued{if l==r.lease{return r.snapshot(),nil};return r.snapshot(),ErrLease}
 next:=r.state;next.LastLeaseIssued=l.Issued;if err:=r.persist(next);err!=nil{return r.snapshot(),err}
 if !r.process.arm(leaseDeadline(l)){return r.snapshot(),ErrLease};r.lease=l;r.deadline=leaseDeadline(l);return r.snapshot(),nil
}
func(r *Runtime)Refresh(l Lease)(Snapshot,error){r.mu.Lock();defer r.mu.Unlock();if r.closed||r.fenced{return r.snapshot(),ErrClosed};return r.refreshLocked(l)}
// Recover needs a fresh lease, never adopts PIDs or starts an interrupted intent.
func(r *Runtime)Recover(ctx context.Context,l Lease)(Snapshot,error){
 r.mu.Lock();defer r.mu.Unlock();if r.closed||r.fenced{return r.snapshot(),ErrClosed};if r.process!=nil&&!r.process.dead(){return r.snapshot(),ErrBusy}
 if r.state.Usage!=nil||r.state.Admission!=nil{return r.snapshot(),ErrUsageSettlement}
 v:=r.state.Good;if !r.validLease(l,v)||l.Issued<=r.state.LastLeaseIssued{return r.snapshot(),ErrLease}
 p,err:=r.start(ctx,v,leaseDeadline(l));if err!=nil{return r.snapshot(),err};if !r.validLease(l,v){p.stop();return r.snapshot(),ErrLease}
 r.process=p;r.active=v;r.lease=l;r.deadline=leaseDeadline(l)
 next:=r.state;next.Pending=nil;next.LastLeaseIssued=l.Issued;next.Phase="recovered_last_good"
 if err:=r.persist(next);err!=nil{return r.snapshot(),err};return r.snapshot(),nil
}
// Rollback is a NEW monotonic signed revision; an old lease is not permission.
func(r *Runtime)Rollback(ctx context.Context,sequence uint64,l Lease)(Snapshot,error){
 r.mu.Lock();if r.closed||r.fenced{snap:=r.snapshot();r.mu.Unlock();return snap,ErrClosed}
 previous:=r.state.Previous;if previous==nil{snap:=r.snapshot();r.mu.Unlock();return snap,ErrStale}
 c:=previous.candidate();r.mu.Unlock();return r.Apply(ctx,Revision{Sequence:sequence,Candidate:c},l)
}
func(r *Runtime)watch(){
 defer close(r.done);tick:=time.NewTicker(100*time.Millisecond);defer tick.Stop()
 for{select{case <-r.stop:return;case <-tick.C:
  r.mu.Lock()
  if !r.closed&&!r.fenced&&r.process!=nil{
   expired:=!time.Now().Before(r.deadline)
   if expired||r.process.dead(){
    r.process.stop();r.process=nil;r.active=nil;next:=r.state;next.Phase="process_exited_stopped";if expired{next.Phase="lease_expired_stopped"}
    // Old settings only regain authority while their prior lease is valid.
    if !expired&&r.state.Usage==nil&&r.state.Admission==nil&&r.state.Previous!=nil&&time.Now().Before(r.previousDeadline)&&r.validLease(r.previousLease,r.state.Previous){
     previous:=r.state.Previous;ctx,cancel:=context.WithTimeout(context.Background(),6*time.Second);p,err:=r.start(ctx,previous,r.previousDeadline);cancel()
     if err==nil&&time.Now().Before(r.previousDeadline){r.process=p;r.active=previous;r.lease=r.previousLease;r.deadline=r.previousDeadline;next.Good=previous;next.Previous=nil;next.Phase="rolled_back_after_process_exit"}else if p!=nil{p.stop()}
    }
    _=r.persist(next)
   }
  };r.mu.Unlock()
 }}
}
func(r *Runtime)Close(){r.mu.Lock();if r.closed{r.mu.Unlock();return};r.closed=true;close(r.stop);if r.observer!=nil{r.observer.cancel()};r.process.stop();r.process=nil;r.active=nil;r.state.Phase="closed";r.disk.close();r.mu.Unlock();<-r.done;if r.observer!=nil{select{case <-r.observer.done:case <-time.After(3*time.Second):}}}
