//go:build linux

package managedxray

import (
 "encoding/json"
 "strconv"
 "strings"
)

// A successful revocation must not automatically resurrect an earlier port
// or UUID when the new core exits. Explicit Rollback still requires a NEW
// higher, signed revision. A failed restrictive Apply stops rather than
// falling back to the credentials the HUB has just revoked.
func removesAuthority(before,after *savedRevision)bool{
 if before==nil{return false}
 read:=func(raw []byte)(map[string]map[string]bool,bool){
  var doc struct{Inbounds []struct{Listen string `json:"listen"`;Port uint16 `json:"port"`;Settings struct{Clients []struct{ID string `json:"id"`} `json:"clients"`} `json:"settings"`} `json:"inbounds"`}
  if json.Unmarshal(raw,&doc)!=nil{return nil,false}
  result:=map[string]map[string]bool{}
  for _,in:=range doc.Inbounds{
   endpoint:=in.Listen+":"+strconv.Itoa(int(in.Port));users:=map[string]bool{}
   for _,user:=range in.Settings.Clients{users[strings.ToLower(user.ID)]=true};result[endpoint]=users
  }
  return result,true
 }
 old,ok:=read(before.Raw);if !ok{return true};next,ok:=read(after.Raw);if !ok{return true}
 for endpoint,users:=range old{
  allowed,exists:=next[endpoint];if !exists{return true}
  for uuid:=range users{if !allowed[uuid]{return true}}
 }
 return false
}
