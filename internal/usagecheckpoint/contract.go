package usagecheckpoint

// CanonicalEpoch validates a trusted supervised registration, not authority.
func CanonicalEpoch(e Epoch)(Epoch,string,error){n,err:=normalize(e);if err!=nil{return Epoch{},"",err};return n,bindingSHA(n),nil}
func VerifyReport(e Epoch,r Report)error{n,err:=normalize(e);if err!=nil{return err};return validateReport(n,r)}
