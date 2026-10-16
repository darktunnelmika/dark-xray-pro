package usagecheckpoint

import("bytes";"encoding/json";"fmt";"io";"strconv";"strings")
// Only explicitly mapped user directions are billable. Missing/unknown user
// counters fail closed; aggregate inbound/outbound counters are not recharged.
func ParseStats(raw []byte,bindings []Binding)([]Usage,error){
 if len(raw)==0||len(raw)>MaxStatsBytes||len(bindings)==0||len(bindings)>MaxBindings{return nil,ErrInvalid}
 dec:=json.NewDecoder(bytes.NewReader(raw));t,err:=dec.Token();if err!=nil||t!=json.Delim('{'){return nil,ErrInvalid}
 var entries []json.RawMessage;found:=false
 for dec.More(){k,e:=dec.Token();if e!=nil||k!="stat"||found{return nil,ErrInvalid};found=true;if err=dec.Decode(&entries);err!=nil{return nil,ErrInvalid}}
 if t,err=dec.Token();err!=nil||t!=json.Delim('}'){return nil,ErrInvalid};var extra any;if !isEOF(dec.Decode(&extra)){return nil,ErrInvalid}
 if !found||len(entries)>MaxBindings*2+4096{return nil,ErrIncomplete}
 mapped:=make(map[string]int,len(bindings));seenIDs:=map[string]bool{};users:=make([]Usage,len(bindings));seen:=make([][2]bool,len(bindings))
 for i,b:=range bindings{if !idPattern.MatchString(b.ClientID)||!emailPattern.MatchString(b.Email)||seenIDs[b.ClientID]{return nil,ErrInvalid};if _,ok:=mapped[b.Email];ok{return nil,ErrInvalid};mapped[b.Email]=i;seenIDs[b.ClientID]=true;users[i].ClientID=b.ClientID}
 for _,encoded:=range entries{
  entry,e:=strictStat(encoded);if e!=nil{return nil,e}
  if !strings.HasPrefix(entry.Name,"user>>>"){if strings.HasPrefix(entry.Name,"inbound>>>")||strings.HasPrefix(entry.Name,"outbound>>>"){continue};return nil,ErrInvalid}
  parts:=strings.Split(entry.Name,">>>");if len(parts)!=4||parts[0]!="user"||parts[2]!="traffic"{return nil,ErrInvalid}
  i,ok:=mapped[parts[1]];if !ok{return nil,fmt.Errorf("unregistered user counter: %w",ErrEpoch)}
  direction:=0;if parts[3]=="downlink"{direction=1}else if parts[3]!="uplink"{return nil,ErrInvalid}
  if seen[i][direction]{return nil,ErrConflict};seen[i][direction]=true;text:=string(entry.Value)
  if len(text)==0{text="0"}else if text[0]=='"'{var value string;if json.Unmarshal(entry.Value,&value)!=nil{return nil,ErrInvalid};text=value}
  if len(text)==0||len(text)>19{return nil,ErrInvalid};for _,c:=range text{if c<'0'||c>'9'{return nil,ErrInvalid}}
  if len(text)>1&&text[0]=='0'{return nil,ErrInvalid};value,e:=strconv.ParseInt(text,10,64);if e!=nil{return nil,ErrOverflow}
  if direction==0{users[i].Uplink=value}else{users[i].Downlink=value}
 }
 for i,s:=range seen{if !s[0]||!s[1]{return nil,ErrIncomplete};if _,err=add(users[i].Uplink,users[i].Downlink);err!=nil{return nil,err}}
 return users,nil
}
func isEOF(err error)bool{return err==io.EOF}
type rawStat struct{Name string;Value json.RawMessage}
func strictStat(raw []byte)(rawStat,error){
 var stat rawStat;dec:=json.NewDecoder(bytes.NewReader(raw));token,err:=dec.Token();if err!=nil||token!=json.Delim('{'){return stat,ErrInvalid};seen:=map[string]bool{}
 for dec.More(){token,err=dec.Token();if err!=nil{return stat,ErrInvalid};field,ok:=token.(string);if !ok||seen[field]{return stat,ErrInvalid};seen[field]=true
  switch field{case "name":err=dec.Decode(&stat.Name);case "value":err=dec.Decode(&stat.Value);default:return stat,ErrInvalid};if err!=nil{return stat,ErrInvalid}}
 if token,err=dec.Token();err!=nil||token!=json.Delim('}')||!seen["name"]||stat.Name==""{return stat,ErrInvalid};var extra any;if !isEOF(dec.Decode(&extra)){return stat,ErrInvalid};return stat,nil
}
