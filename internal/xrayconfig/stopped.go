package xrayconfig

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "net"
)

// A zero-listener revision is an explicit revocation, not a missing task.
// Its entire configuration is fixed: no API, reverse proxy or other listener
// can be hidden in what is presented as a stopped Node configuration.
const stoppedConfig = `{"log":{"loglevel":"none"},"inbounds":[],"outbounds":[{"tag":"deny","protocol":"blackhole"}]}`

func IsStoppedCandidate(c Candidate) bool {
 return c.NoListeners && len(c.Listeners)==0 && bytes.Equal(c.Config,[]byte(stoppedConfig))
}

func RenderStopped(node, installation, listen string)(Candidate,error){
 ip:=net.ParseIP(listen)
 if node==""||len(node)>128||len(installation)!=32||ip==nil||ip.IsUnspecified()||ip.IsMulticast(){return Candidate{},ErrInvalid}
 if _,err:=hex.DecodeString(installation);err!=nil{return Candidate{},ErrInvalid}
 raw:=[]byte(stoppedConfig);digest:=sha256.Sum256(raw)
 binding,_:=json.Marshal(struct{Node,Installation,Listen,ConfigSHA,Mode string}{
  node,installation,ip.String(),hex.EncodeToString(digest[:]),"no-listeners-v1",
 })
 manifest:=sha256.Sum256(binding)
 return Candidate{Config:raw,SHA256:hex.EncodeToString(digest[:]),ManifestSHA256:hex.EncodeToString(manifest[:]),
  Listeners:[]Listener{},NoListeners:true,RequiresXrayTest:true,RequiresDataPlaneProof:true,RequiresSubscriptionCommit:true},nil
}

func IsStoppedHash(hash string)bool{h:=sha256.Sum256([]byte(stoppedConfig));return hash==hex.EncodeToString(h[:])}
