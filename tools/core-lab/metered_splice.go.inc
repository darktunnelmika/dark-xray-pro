// SPDX-License-Identifier: MPL-2.0
package proxy

import (
 "context"
 "fmt"
 "io"
)

// Bound a live user's unreported ReadFrom tail instead of waiting for an entire
// long-lived Vision connection to end. net.TCPConn.ReadFrom recognizes a
// *io.LimitedReader and still uses Linux splice where supported. This does NOT
// make periodic accounting an exact byte-by-byte or distributed hard limit.
const proSpliceBatch int64 = 32 * 1024

func proCopyMeteredRaw(ctx context.Context, dst io.ReaderFrom, src io.Reader, observed func(int64)) error {
 for {
  if err:=ctx.Err();err!=nil{return err}
  limited:=&io.LimitedReader{R:src,N:proSpliceBatch}
  n,err:=dst.ReadFrom(limited)
  if n<0||n>proSpliceBatch||limited.N<0||limited.N>proSpliceBatch||n>proSpliceBatch-limited.N{return fmt.Errorf("invalid bounded raw-copy result")}
  // Account partial progress even when close/cancellation/write failure ends
  // this call. Never credit the requested batch rather than actual progress.
  if n>0{observed(n)}
  if err!=nil{if err==io.EOF{return nil};return err}
  if n<proSpliceBatch{return nil}
 }
}
