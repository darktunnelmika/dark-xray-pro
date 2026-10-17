// SPDX-License-Identifier: MPL-2.0
package inbound

import (
 "context"
 "net"
 "sync"
 "testing"
 "time"

 "github.com/xtls/xray-core/common/protocol"
 "github.com/xtls/xray-core/common/uuid"
 "github.com/xtls/xray-core/proxy/vless"
)

func proHandler(t *testing.T, email string) (*Handler,*protocol.MemoryUser) {
 t.Helper(); h:=&Handler{validator:new(vless.MemoryValidator)}
 u:=&protocol.MemoryUser{Email:email,Account:&vless.MemoryAccount{ID:protocol.NewID(uuid.New())}}
 if err:=h.AddUser(context.Background(),u);err!=nil{t.Fatal(err)}
 return h,h.validator.GetByEmail(email)
}
func proSession(t *testing.T,h *Handler,u *protocol.MemoryUser)(context.Context,func(),net.Conn){
 t.Helper();server,peer:=net.Pipe();t.Cleanup(func(){server.Close();peer.Close()})
 ctx,release,err:=h.trackUserSession(context.Background(),u,server);if err!=nil{t.Fatal(err)}
 t.Cleanup(release);return ctx,release,peer
}
func proCancelled(t *testing.T,ctx context.Context){t.Helper();select{case <-ctx.Done():case <-time.After(time.Second):t.Fatal("session did not cancel")}}
func proSocketClosed(t *testing.T,peer net.Conn){t.Helper();peer.SetReadDeadline(time.Now().Add(time.Second));_,err:=peer.Read(make([]byte,1));if err==nil{t.Fatal("closed user still readable")};if n,ok:=err.(net.Error);ok&&n.Timeout(){t.Fatal("stream not actually closed")}}

func TestPROUserRemovalClosesAllOwnStreamsOnly(t *testing.T){
 h,a:=proHandler(t,"a@pro.invalid"); b:=&protocol.MemoryUser{Email:"b@pro.invalid",Account:&vless.MemoryAccount{ID:protocol.NewID(uuid.New())}}
 if err:=h.AddUser(context.Background(),b);err!=nil{t.Fatal(err)};b=h.validator.GetByEmail(b.Email)
 a1,_,peer1:=proSession(t,h,a);a2,_,peer2:=proSession(t,h,a);b1,_,_:=proSession(t,h,b)
 if err:=h.RemoveUser(context.Background(),"A@pro.invalid");err!=nil{t.Fatal(err)}
 proCancelled(t,a1);proCancelled(t,a2);proSocketClosed(t,peer1);proSocketClosed(t,peer2)
 select{case <-b1.Done():t.Fatal("unrelated user cancelled");default:}
 if h.validator.GetByEmail(a.Email)!=nil||h.validator.GetByEmail(b.Email)!=b{t.Fatal("wrong validator mutation")}
}
func TestPROReaddCannotAdmitOldHandshakeOrLoseNewSession(t *testing.T){
 h,old:=proHandler(t,"same@pro.invalid");oldCtx,release,oldPeer:=proSession(t,h,old)
 if err:=h.RemoveUser(context.Background(),old.Email);err!=nil{t.Fatal(err)}
 // Even an in-process caller reusing the exact MemoryUser must get a fresh generation.
 if err:=h.AddUser(context.Background(),old);err!=nil{t.Fatal(err)};fresh:=h.validator.GetByEmail(old.Email)
 if fresh==old{t.Fatal("AddUser reused revoked admission identity")}
 server,peer:=net.Pipe();defer server.Close();defer peer.Close()
 if _,done,err:=h.trackUserSession(context.Background(),old,server);err==nil{done();t.Fatal("stale handshake revived")}
 next,_,newPeer:=proSession(t,h,fresh);release();release()
 proCancelled(t,oldCtx);proSocketClosed(t,oldPeer)
 select{case <-next.Done():t.Fatal("old cleanup cancelled fresh session");default:}
 if err:=h.RemoveUser(context.Background(),fresh.Email);err!=nil{t.Fatal(err)};proCancelled(t,next);proSocketClosed(t,newPeer)
}
func TestPROSameEmailOnAnotherInboundIsIndependent(t *testing.T){
 left,a:=proHandler(t,"shared@pro.invalid");right,b:=proHandler(t,"shared@pro.invalid")
 x,_,p:=proSession(t,left,a);y,_,_:=proSession(t,right,b)
 if err:=left.RemoveUser(context.Background(),a.Email);err!=nil{t.Fatal(err)}
 proCancelled(t,x);proSocketClosed(t,p);select{case <-y.Done():t.Fatal("cross-inbound revocation");default:}
}
type proBlockingClose struct{net.Conn;entered,unblock chan struct{};once sync.Once}
func(c *proBlockingClose)Close()error{c.once.Do(func(){close(c.entered)});<-c.unblock;return c.Conn.Close()}
func TestPROBlockedCloseDoesNotHoldAdmissionLock(t *testing.T){
 h,a:=proHandler(t,"slow@pro.invalid");server,peer:=net.Pipe();defer peer.Close()
 conn:=&proBlockingClose{Conn:server,entered:make(chan struct{}),unblock:make(chan struct{})};defer close(conn.unblock)
 ctx,release,err:=h.trackUserSession(context.Background(),a,conn);if err!=nil{t.Fatal(err)};defer release()
 removed:=make(chan error,1);go func(){removed<-h.RemoveUser(context.Background(),a.Email)}()
 select{case err:=<-removed:if err!=nil{t.Fatal(err)};case <-time.After(time.Second):t.Fatal("RemoveUser waited on stream Close")}
 proCancelled(t,ctx);select{case <-conn.entered:case <-time.After(time.Second):t.Fatal("Close was not called")}
 if err:=h.AddUser(context.Background(),a);err!=nil{t.Fatal("blocked close retained registry lock",err)}
}
func TestPROConcurrentAdmissionRemovalAndShutdown(t *testing.T){
 for i:=0;i<100;i++{
  h,a:=proHandler(t,"race@pro.invalid");server,peer:=net.Pipe();var ctx context.Context;var release func();var err error
  var wg sync.WaitGroup;wg.Add(2)
  go func(){defer wg.Done();ctx,release,err=h.trackUserSession(context.Background(),a,server)}()
  go func(){defer wg.Done();_=h.RemoveUser(context.Background(),a.Email)}();wg.Wait()
  if err==nil{proCancelled(t,ctx);release()};server.Close();peer.Close()
  h.stopUserSessions();if err:=h.AddUser(context.Background(),a);err==nil{t.Fatal("closed inbound admitted a user")}
 }
}
