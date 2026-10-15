package business

import("context";"encoding/json";"errors")
// An identical retry returns the existing identity AND its current lifecycle state.
func(s *Store)replayInbound(ctx context.Context,key []byte,in InboundInput)(NativeInbound,bool,error){
 got,err:=s.NativeInbound(ctx,key,in.ID)
 if errors.Is(err,ErrNotFound){return NativeInbound{},false,nil}
 if err!=nil{return NativeInbound{},false,err}
 a,_:=json.Marshal(got.InboundInput);b,_:=json.Marshal(in)
 if string(a)!=string(b){return NativeInbound{},false,ErrConflict}
 return got,true,nil
}
