package usagewire

import (
 "strings"
 "testing"
)
func TestStrictReportWireRejectsDuplicateAndUnknownKeys(t *testing.T){
 type Payload struct{Epoch string `json:"epoch_id"`;Number string `json:"sequence"`}
 for _,raw:=range []string{
  `{"epoch_id":"a","epoch_id":"b","sequence":"1"}`,
  `{"epoch_id":"a","EPOCH_ID":"b","sequence":"1"}`,
  `{"epoch_id":"a","unknown":true,"sequence":"1"}`,
  `{"epoch_id":"a","sequence":"1"} {}`,
  `{"epoch_id":"a","sequence":1}`,
  `{"epoch_id":[],"sequence":"1"}`,
  strings.Repeat("[",15)+strings.Repeat("]",15),
 }{var output Payload;if e:=Decode([]byte(raw),&output);e==nil{t.Fatalf("ambiguous wire accepted: %s",raw)}}
 var out Payload;if e:=Decode([]byte(`{"epoch_id":"valid","sequence":"9007199254740993"}`),&out);e!=nil||out.Number!="9007199254740993"{t.Fatal("exact int64 wire altered",e)}
}
