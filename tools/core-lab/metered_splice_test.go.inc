// SPDX-License-Identifier: MPL-2.0
package proxy

import (
 "bytes"
 "context"
 "errors"
 "io"
 "testing"
)
func TestPROMeteredRawBatchesAndExactTail(t *testing.T){
 for _,size:=range []int{0,1,int(proSpliceBatch),int(proSpliceBatch)*3+17}{
  input:=bytes.Repeat([]byte{42},size);var output bytes.Buffer;var total int64;calls:=0
  err:=proCopyMeteredRaw(context.Background(),&output,bytes.NewReader(input),func(n int64){if n<=0||n>proSpliceBatch{t.Fatal("invalid reported progress")};total+=n;calls++})
  if err!=nil||total!=int64(size)||!bytes.Equal(output.Bytes(),input){t.Fatalf("copy/count mismatch size=%d total=%d err=%v",size,total,err)}
  if calls!=(size+int(proSpliceBatch)-1)/int(proSpliceBatch){t.Fatal("progress only reported after whole connection")}
 }
}
type proPartialFailure struct{}
func(proPartialFailure)ReadFrom(r io.Reader)(int64,error){_,_=io.ReadFull(r,make([]byte,10));return 7,io.ErrClosedPipe}
func TestPROMeteredRawRetainsPartialWriteOnFailure(t *testing.T){
 var total int64;err:=proCopyMeteredRaw(context.Background(),proPartialFailure{},bytes.NewReader(make([]byte,100)),func(n int64){total+=n})
 if !errors.Is(err,io.ErrClosedPipe)||total!=7{t.Fatalf("partial progress lost or read bytes charged as written: total=%d err=%v",total,err)}
}
func TestPROMeteredRawCancellationDoesNotConsumeNextBatch(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel();var out bytes.Buffer;var total int64
 err:=proCopyMeteredRaw(ctx,&out,bytes.NewReader(make([]byte,proSpliceBatch*3)),func(n int64){total+=n;cancel()})
 if !errors.Is(err,context.Canceled)||total!=proSpliceBatch||int64(out.Len())!=total{t.Fatal("cancelled copy kept consuming",err,total)}
}
