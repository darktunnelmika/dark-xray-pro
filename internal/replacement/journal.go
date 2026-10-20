// Package replacement is a crash-safe, nonactivating replacement journal.
// It never copies a retired Node's tunnels or touches Xray/subscriptions.
package replacement

import (
 "bytes"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net"
 "os"
 "path/filepath"
 "sort"
 "strconv"
 "sync"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

const maxFileBytes = 8 << 20
const maxOperations = 1000
const maxDeployments = 64

var (
 ErrUnsafe = errors.New("unsafe replacement journal")
 ErrConflict = errors.New("replacement conflicts with active operation")
 ErrNotFound = errors.New("replacement operation not found")
 ErrMismatch = errors.New("replacement identity or manifest mismatch")
 ErrInvalid = errors.New("invalid replacement or TCP evidence")
 ErrCancelled = errors.New("replacement cancelled")
)

type Phase string
const (
 Pending Phase = "pending_tcp_preflight"
 ProbeFailed Phase = "tcp_preflight_failed"
 TCPChecked Phase = "tcp_preflight_passed_not_activated"
 Cancelled Phase = "cancelled"
)

type Probe struct {
 InboundID uint64 `json:"inbound_id"`
 Kind string `json:"kind"` // direct | swap
 Address string `json:"address"`
 Reachable bool `json:"tcp_reachable"`
 Failure string `json:"failure,omitempty"`
}

type Evidence struct {
 OperationID string `json:"operation_id"`
 ManifestSHA256 string `json:"manifest_sha256"`
 NewNodeID string `json:"new_node_id"`
 InstallationID string `json:"installation_id"`
 Probes []Probe `json:"probes"`
}

type Operation struct {
 ID string `json:"id"`
 OldNodeID string `json:"old_node_id"`
 NewNodeID string `json:"new_node_id"`
 InstallationID string `json:"installation_id"`
 Preview routeplan.Preview `json:"preview"`
 Phase Phase `json:"phase"`
 Revision uint64 `json:"revision"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
 Evidence *Evidence `json:"tcp_evidence,omitempty"`
 CanActivate bool `json:"can_activate"`
 NeedsXrayValidation bool `json:"needs_xray_validation"`
 NeedsDataPlaneProbe bool `json:"needs_data_plane_probe"`
 NeedsSubscriptionCommit bool `json:"needs_subscription_commit"`
}

type disk struct {
 Version int `json:"version"`
 Operations map[string]Operation `json:"operations"`
}

type Journal struct {
 mu sync.RWMutex
 path string
 clock func()time.Time
 state disk
}

func empty()disk{return disk{Version:1,Operations:map[string]Operation{}}}

func copyOperation(op Operation) Operation {
 b,_:=json.Marshal(op)
 var result Operation
 _=json.Unmarshal(b,&result)
 return result
}

func Open(path string, clock func()time.Time)(*Journal,error){
 if path==""{return nil,ErrUnsafe}
 abs,err:=filepath.Abs(path)
 if err!=nil{return nil,err}
 dir:=filepath.Dir(abs)
 if err:=os.MkdirAll(dir,0700);err!=nil{return nil,err}
 info,err:=os.Lstat(dir)
 if err!=nil{return nil,err}
 if !info.IsDir()||info.Mode().Perm()&0077!=0{return nil,ErrUnsafe}
 if clock==nil{clock=time.Now}
 j:=&Journal{path:abs,clock:clock,state:empty()}
 f,err:=os.Lstat(abs)
 if errors.Is(err,os.ErrNotExist){return j,nil}
 if err!=nil{return nil,err}
 if !f.Mode().IsRegular()||f.Mode().Perm()&0077!=0||f.Size()>maxFileBytes{return nil,ErrUnsafe}
 raw,err:=os.ReadFile(abs)
 if err!=nil{return nil,err}
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 decoder.DisallowUnknownFields()
 var state disk
 if err:=decoder.Decode(&state);err!=nil{return nil,ErrUnsafe}
 var trailing any
 if err:=decoder.Decode(&trailing);!errors.Is(err,io.EOF){return nil,ErrUnsafe}
 if state.Version!=1||state.Operations==nil||len(state.Operations)>maxOperations{return nil,ErrUnsafe}
 reserved:=map[string]bool{}
 for id,op:=range state.Operations {
  if len(id)!=32||op.ID!=id||op.Revision==0||op.NewNodeID==op.OldNodeID||
   op.Preview.NewNodeID!=op.NewNodeID||op.Preview.OldNodeID!=op.OldNodeID||
   op.Preview.ManifestSHA256==""||op.CanActivate||!op.NeedsXrayValidation||
   !op.NeedsDataPlaneProbe||!op.NeedsSubscriptionCommit||
   len(op.Preview.Deployments)==0||len(op.Preview.Deployments)>maxDeployments {
    return nil,ErrUnsafe
   }
  switch op.Phase {case Pending,ProbeFailed,TCPChecked,Cancelled: default:return nil,ErrUnsafe}
  if op.Phase!=Cancelled {
   if reserved[op.OldNodeID]||reserved[op.NewNodeID]{return nil,ErrUnsafe}
   reserved[op.OldNodeID]=true;reserved[op.NewNodeID]=true
  }
 }
 j.state=state
 return j,nil
}

func (j *Journal) commit(next disk)error{
 raw,err:=json.Marshal(next)
 if err!=nil{return err}
 if len(raw)>maxFileBytes{return ErrUnsafe}
 fd,err:=os.CreateTemp(filepath.Dir(j.path),".replacement-")
 if err!=nil{return err}
 tmp:=fd.Name()
 defer os.Remove(tmp)
 if err:=fd.Chmod(0600);err!=nil{fd.Close();return err}
 if _,err:=fd.Write(raw);err!=nil{fd.Close();return err}
 if err:=fd.Sync();err!=nil{fd.Close();return err}
 if err:=fd.Close();err!=nil{return err}
 if err:=os.Rename(tmp,j.path);err!=nil{return err}
 dir,err:=os.Open(filepath.Dir(j.path))
 if err!=nil{return err}
 defer dir.Close()
 if err:=dir.Sync();err!=nil{return err}
 j.state=next
 return nil
}

func clone(s disk)disk{
 next:=empty()
 for id,op:=range s.Operations{next.Operations[id]=op}
 return next
}

func newID()(string,error){
 b:=make([]byte,16)
 if _,err:=rand.Read(b);err!=nil{return "",err}
 return hex.EncodeToString(b),nil
}

func (j *Journal) Create(preview routeplan.Preview, installationID string)(Operation,error){
 if preview.OldNodeID==""||preview.NewNodeID==""||preview.OldNodeID==preview.NewNodeID||
  preview.ManifestSHA256==""||preview.CanActivate||preview.HasInheritedOldRoutes||
  len(installationID)!=32||len(preview.Deployments)==0||
  len(preview.Deployments)>maxDeployments{return Operation{},ErrInvalid}
 // Both paths are explicit: no old-server settings or credentials are read.
 if _,err:=Expect(preview);err!=nil{return Operation{},err}
 j.mu.Lock()
 defer j.mu.Unlock()
 if len(j.state.Operations)>=maxOperations{return Operation{},ErrConflict}
 for _,op:=range j.state.Operations {
  if op.Phase!=Cancelled&&(op.OldNodeID==preview.OldNodeID||
    op.NewNodeID==preview.NewNodeID||op.OldNodeID==preview.NewNodeID||
    op.NewNodeID==preview.OldNodeID){return Operation{},ErrConflict}
 }
 id,err:=newID()
 if err!=nil{return Operation{},err}
 now:=j.clock().UTC()
 op:=Operation{
  ID:id,OldNodeID:preview.OldNodeID,NewNodeID:preview.NewNodeID,
  InstallationID:installationID,Preview:preview,Phase:Pending,
  Revision:1,CreatedAt:now,UpdatedAt:now,
  CanActivate:false,NeedsXrayValidation:true,
  NeedsDataPlaneProbe:true,NeedsSubscriptionCommit:true,
 }
 next:=clone(j.state);next.Operations[id]=copyOperation(op)
 if err:=j.commit(next);err!=nil{return Operation{},err}
 return copyOperation(op),nil
}

func (j *Journal) Get(id string)(Operation,error){
 j.mu.RLock();defer j.mu.RUnlock()
 op,ok:=j.state.Operations[id]
 if !ok{return Operation{},ErrNotFound}
 return copyOperation(op),nil
}

func (j *Journal) List()[]Operation{
 j.mu.RLock();defer j.mu.RUnlock()
 list:=make([]Operation,0,len(j.state.Operations))
 for _,op:=range j.state.Operations{list=append(list,copyOperation(op))}
 sort.Slice(list,func(i,k int)bool{
  if list[i].CreatedAt.Equal(list[k].CreatedAt){return list[i].ID<list[k].ID}
  return list[i].CreatedAt.Before(list[k].CreatedAt)
 })
 return list
}

func (j *Journal) Next(newNodeID,installationID string)(Operation,bool){
 j.mu.RLock();defer j.mu.RUnlock()
 var pending []Operation
 for _,op:=range j.state.Operations{
  if op.NewNodeID==newNodeID&&op.InstallationID==installationID&&op.Phase==Pending{
   pending=append(pending,copyOperation(op))
  }
 }
 if len(pending)==0{return Operation{},false}
 sort.Slice(pending,func(i,k int)bool{return pending[i].CreatedAt.Before(pending[k].CreatedAt)})
 return pending[0],true
}

func Expect(preview routeplan.Preview)([]Probe,error){
 if len(preview.Deployments)==0||len(preview.Deployments)>maxDeployments{return nil,ErrInvalid}
 probes:=make([]Probe,0,2*len(preview.Deployments))
 seen:=map[uint64]bool{}
 for _,row:=range preview.Deployments{
  if row.InboundID==0||seen[row.InboundID]||row.Direct==nil&&row.Swap==nil{return nil,ErrInvalid}
  seen[row.InboundID]=true
  if row.Direct!=nil{
   if row.Direct.NodePort==0{return nil,ErrInvalid}
   probes=append(probes,Probe{InboundID:row.InboundID,Kind:"direct",
    Address:net.JoinHostPort("127.0.0.1",strconv.Itoa(int(row.Direct.NodePort)))})
  }
  if row.Swap!=nil{
   if row.Swap.NodePort==0||row.Swap.TunnelPort==0||
    row.Swap.TunnelAddress==""{return nil,ErrInvalid}
   probes=append(probes,Probe{InboundID:row.InboundID,Kind:"swap",
    Address:net.JoinHostPort(row.Swap.TunnelAddress,strconv.Itoa(int(row.Swap.TunnelPort)))})
  }
 }
 sort.Slice(probes,func(i,k int)bool{
  if probes[i].InboundID==probes[k].InboundID{return probes[i].Kind<probes[k].Kind}
  return probes[i].InboundID<probes[k].InboundID
 })
 return probes,nil
}

// Report only records TCP port reachability. It NEVER claims Xray validation,
// tunnel data-plane success, or subscription publication. Retry of an identical
// report is idempotent; a conflicting reply cannot overwrite recorded evidence.
func (j *Journal) Report(e Evidence)(Operation,error){
 j.mu.Lock();defer j.mu.Unlock()
 op,ok:=j.state.Operations[e.OperationID]
 if !ok{return Operation{},ErrNotFound}
 if op.Phase==Cancelled{return Operation{},ErrCancelled}
 if e.ManifestSHA256!=op.Preview.ManifestSHA256||e.NewNodeID!=op.NewNodeID||
   e.InstallationID!=op.InstallationID{return Operation{},ErrMismatch}
 wanted,err:=Expect(op.Preview)
 if err!=nil{return Operation{},err}
 if len(e.Probes)!=len(wanted){return Operation{},ErrMismatch}
 answer:=append([]Probe(nil),e.Probes...)
 sort.Slice(answer,func(i,k int)bool{
  if answer[i].InboundID==answer[k].InboundID{return answer[i].Kind<answer[k].Kind}
  return answer[i].InboundID<answer[k].InboundID
 })
 allOK:=true
 for i,proof:=range answer{
  expect:=wanted[i]
  if proof.InboundID!=expect.InboundID||proof.Kind!=expect.Kind||
   proof.Address!=expect.Address||len(proof.Failure)>64{return Operation{},ErrMismatch}
  if proof.Reachable {
   if proof.Failure!=""{return Operation{},ErrInvalid}
  }else{
   allOK=false
   switch proof.Failure{
   case "timeout","refused","dns","unreachable","cancelled","network","internal":
   default:return Operation{},ErrInvalid
   }
  }
 }
 e.Probes=answer
 if op.Phase!=Pending{
  if op.Evidence!=nil{
   a,_:=json.Marshal(*op.Evidence);b,_:=json.Marshal(e)
   if bytes.Equal(a,b){return copyOperation(op),nil}
  }
  return Operation{},ErrConflict
 }
 current:=copyOperation(op)
 current.Evidence=&e
 current.Revision++
 current.UpdatedAt=j.clock().UTC()
 current.Phase=ProbeFailed
 if allOK {current.Phase=TCPChecked}
 next:=clone(j.state);next.Operations[op.ID]=current
 if err:=j.commit(next);err!=nil{return Operation{},err}
 return copyOperation(current),nil
}

// Cancel does not stop, delete or restart an old or new Xray process.
func (j *Journal) Cancel(id string,expectedRevision uint64)(Operation,error){
 j.mu.Lock();defer j.mu.Unlock()
 op,ok:=j.state.Operations[id]
 if !ok{return Operation{},ErrNotFound}
 // A lost cancellation response may be replayed with its original revision.
 // Cancelled is terminal: no later probe can advance or replace this receipt.
 if op.Phase==Cancelled&&op.Revision>1&&expectedRevision==op.Revision-1{return copyOperation(op),nil}
 if op.Revision!=expectedRevision{return Operation{},ErrConflict}
 if op.Phase==Cancelled{return copyOperation(op),nil}
 next:=clone(j.state);op=copyOperation(op)
 op.Phase=Cancelled;op.Revision++;op.UpdatedAt=j.clock().UTC()
 next.Operations[id]=op
 if err:=j.commit(next);err!=nil{return Operation{},err}
 return copyOperation(op),nil
}
