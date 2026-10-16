// Package usagewire rejects ambiguous, duplicate or oversized JSON structures.
package usagewire
import("bytes";"encoding/json";"errors";"io";"strings")
var ErrJSON=errors.New("invalid or ambiguous usage JSON")
func Decode(raw []byte,dst any)error{
 d:=json.NewDecoder(bytes.NewReader(raw));d.UseNumber();if e:=walk(d,0);e!=nil{return ErrJSON};if _,e:=d.Token();e!=io.EOF{return ErrJSON}
 d=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields();if e:=d.Decode(dst);e!=nil{return ErrJSON};return nil
}
func walk(d *json.Decoder,depth int)error{
 if depth>12{return ErrJSON};token,e:=d.Token();if e!=nil{return e};delimiter,ok:=token.(json.Delim);if !ok{return nil}
 switch delimiter{
 case '{':seen:=map[string]bool{};for d.More(){key,e:=d.Token();if e!=nil{return e};k,ok:=key.(string);if !ok||len(k)>256||len(seen)>=64||seen[strings.ToLower(k)]{return ErrJSON};seen[strings.ToLower(k)]=true;if e=walk(d,depth+1);e!=nil{return e}};end,e:=d.Token();if e!=nil||end!=json.Delim('}'){return ErrJSON}
 case '[':count:=0;for d.More(){count++;if count>20000{return ErrJSON};if e:=walk(d,depth+1);e!=nil{return e}};end,e:=d.Token();if e!=nil||end!=json.Delim(']'){return ErrJSON}
 default:return ErrJSON
 };return nil
}
