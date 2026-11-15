package hubcore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeSSH accepts one password and answers an exec request with a fixed
// output, recording the command.
func fakeSSH(t *testing.T, password string, got chan<- string) string {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if string(p) == password {
			return nil, nil
		}
		return nil, ssh.ErrNoAuth
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					ch, creqs, _ := nc.Accept()
					go func() {
						for r := range creqs {
							if r.Type == "exec" {
								n := binary.BigEndian.Uint32(r.Payload[:4])
								got <- string(r.Payload[4 : 4+n])
								_ = r.Reply(true, nil)
								_, _ = ch.Write([]byte("installing dark-node\nnode is running\n"))
								_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
								ch.Close()
								return
							}
							_ = r.Reply(false, nil)
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestSSHInstall(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	got := make(chan string, 1)
	addr := fakeSSH(t, "s3cret", got)
	host, port, _ := net.SplitHostPort(addr)
	p := 0
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	url := func(tok string) string { return "https://hub.example/install/" + tok }
	id, err := s.StartSSHInstall(ctx, SSHInstall{NodeID: "fr-1", Name: "France", Host: host, Port: p, Password: "s3cret"}, url)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case cmd := <-got:
		if !strings.HasPrefix(cmd, "curl -fsSL https://hub.example/install/") || !strings.HasSuffix(cmd, "| bash") {
			t.Fatal("command", cmd)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no command ran")
	}
	var j SSHJobView
	for i := 0; i < 50; i++ {
		j, _ = SSHInstallJob(id)
		if j.Status != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if j.Status != "done" || !strings.Contains(strings.Join(j.Lines, "\n"), "node is running") {
		t.Fatalf("%+v", j)
	}
	var x int
	if s.ro.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id='fr-1'`).Scan(&x) != nil {
		t.Fatal("node not registered")
	}
	for _, l := range j.Lines {
		if strings.Contains(l, "s3cret") {
			t.Fatal("the password must never appear in the log")
		}
	}
	bad, _ := s.StartSSHInstall(ctx, SSHInstall{NodeID: "fr-2", Host: host, Port: p, Password: "wrong"}, url)
	for i := 0; i < 50; i++ {
		j, _ = SSHInstallJob(bad)
		if j.Status != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if j.Status != "failed" {
		t.Fatalf("wrong password must fail: %+v", j)
	}
}
