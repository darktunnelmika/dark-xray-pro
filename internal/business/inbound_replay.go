package business

import("context";"database/sql";"encoding/json";"errors")
// A lost create response must not rotate REALITY keys or change the profile.
func(s *Store)replayInbound(ctx context.Context,key []byte,in InboundInput)(NativeInbound,bool,error){
 var name,pub string;var enc []byte
 e:=s.db.QueryRowContext(ctx,"SELECT name,profile_enc,public_key FROM native_inbounds WHERE id=?",in.ID).Scan(&name,&enc,&pub)
 if errors.Is(e,sql.ErrNoRows){return NativeInbound{},false,nil};if e!=nil{return NativeInbound{},false,e}
 p,e:=openProfile(key,enc,in.ID);if e!=nil{return NativeInbound{},false,e}
 got:=InboundInput{ID:in.ID,Name:name,Transport:p.Transport,GRPCService:p.GRPCService,RealityTarget:p.RealityTarget,ServerNames:p.ServerNames,ShortIDs:p.ShortIDs}
 a,_:=json.Marshal(got);b,_:=json.Marshal(in);if string(a)!=string(b){return NativeInbound{},false,ErrConflict}
 var count int;if e=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM native_inbound_clients WHERE inbound_id=?",in.ID).Scan(&count);e!=nil{return NativeInbound{},false,e}
 return NativeInbound{InboundInput:got,PublicKey:pub,BoundClients:count},true,nil
}
