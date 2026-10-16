package usagecheckpoint
import("crypto/sha256";"encoding/hex")
// CounterEmail is identical to the native renderer's opaque per-client email.
func CounterEmail(clientID string)(string,error){if !idPattern.MatchString(clientID){return "",ErrInvalid};h:=sha256.Sum256([]byte(clientID));return hex.EncodeToString(h[:16])+"@pro.invalid",nil}
type VolumeGate struct{Known bool;Remaining int64;Exhausted bool;MayAuthorize bool}
// This decision helper alone does not cut off a connection or prevent overshoot.
func VolumeStatus(limit,consumed int64,complete bool)(VolumeGate,error){if limit<=0||consumed<0{return VolumeGate{},ErrInvalid};if !complete{return VolumeGate{},nil};if consumed>=limit{return VolumeGate{Known:true,Exhausted:true},nil};return VolumeGate{Known:true,Remaining:limit-consumed,MayAuthorize:true},nil}
