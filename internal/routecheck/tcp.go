// Package routecheck performs bounded TCP preflight on explicitly selected
// replacement routes. Port reachability is NOT Xray protocol/data-plane proof.
package routecheck

import (
 "context"
 "errors"
 "net"
 "os"
 "strings"
 "syscall"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
)

type Dialer interface {
 DialContext(context.Context,string,string)(net.Conn,error)
}

type Result struct {
 Evidence replacement.Evidence
 AllTCPReachable bool
}

// Run sends no user traffic and never mutates network routes or Xray state.
// All attempted destinations originate from the strictly validated manifest.
func Run(ctx context.Context, operation replacement.Operation, dial Dialer)(Result,error){
 expected,err:=replacement.Expect(operation.Preview)
 if err!=nil{return Result{},err}
 if operation.Phase!=replacement.Pending||operation.CanActivate||
  operation.NewNodeID==""||operation.InstallationID==""{
  return Result{},replacement.ErrInvalid
 }
 if dial==nil{dial=&net.Dialer{Timeout:1200*time.Millisecond}}
 answer:=make([]replacement.Probe,0,len(expected))
 all:=true
 for _,item:=range expected{
  proof:=item
  limited,cancel:=context.WithTimeout(ctx,1200*time.Millisecond)
  conn,err:=dial.DialContext(limited,"tcp",proof.Address)
  cancel()
  if conn!=nil {_=conn.Close()}
  if err==nil {
   proof.Reachable=true
  } else {
   proof.Failure=classify(err)
   all=false
  }
  answer=append(answer,proof)
 }
 return Result{
  Evidence:replacement.Evidence{
   OperationID:operation.ID,ManifestSHA256:operation.Preview.ManifestSHA256,
   NewNodeID:operation.NewNodeID,InstallationID:operation.InstallationID,
   Probes:answer,
  },
  AllTCPReachable:all,
 },nil
}

func classify(err error)string{
 if errors.Is(err,context.Canceled){return "cancelled"}
 if errors.Is(err,context.DeadlineExceeded){return "timeout"}
 var n net.Error
 if errors.As(err,&n)&&n.Timeout(){return "timeout"}
 var dns *net.DNSError
 if errors.As(err,&dns){return "dns"}
 if errors.Is(err,syscall.ECONNREFUSED){return "refused"}
 if errors.Is(err,syscall.ENETUNREACH)||errors.Is(err,syscall.EHOSTUNREACH){
  return "unreachable"
 }
 if errors.Is(err,os.ErrDeadlineExceeded){return "timeout"}
 if strings.Contains(err.Error(),"network is unreachable"){return "unreachable"}
 return "network"
}
