//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestCustomerConnectionSOCKSRequiresAttemptCredentials(t *testing.T) {
	const username = "customer-connection"
	password := strings.Repeat("a", 64)
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for n := 0; n < 3; n++ {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				greeting := make([]byte, 3)
				if _, e = io.ReadFull(c, greeting); e != nil {
					return
				}
				if !bytes.Equal(greeting, []byte{5, 1, 2}) {
					c.Write([]byte{5, 255})
					return
				}
				c.Write([]byte{5, 2})
				header := make([]byte, 2)
				if _, e = io.ReadFull(c, header); e != nil || header[0] != 1 {
					return
				}
				user := make([]byte, int(header[1]))
				if _, e = io.ReadFull(c, user); e != nil {
					return
				}
				size := make([]byte, 1)
				if _, e = io.ReadFull(c, size); e != nil {
					return
				}
				pass := make([]byte, int(size[0]))
				if _, e = io.ReadFull(c, pass); e != nil {
					return
				}
				if string(user) != username || string(pass) != password {
					c.Write([]byte{1, 1})
					return
				}
				c.Write([]byte{1, 0})
				connect := make([]byte, 10)
				if _, e = io.ReadFull(c, connect); e != nil {
					return
				}
				c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
			}()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, e := protocolHealthSOCKS(ctx, listener.Addr().String(), "11.0.0.1:443"); e == nil {
		c.Close()
		t.Fatal("anonymous client accepted")
	}
	if c, e := dialProtocolHealthSOCKS(ctx, listener.Addr().String(), "11.0.0.1:443", username, strings.Repeat("b", 64)); e == nil {
		c.Close()
		t.Fatal("foreign attempt accepted")
	}
	c, e := dialProtocolHealthSOCKS(ctx, listener.Addr().String(), "11.0.0.1:443", username, password)
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	<-done
	if _, e = dialProtocolHealthSOCKS(ctx, "invalid", "11.0.0.1:443", username, ""); e == nil {
		t.Fatal("partial credentials accepted")
	}
}
