package hubcore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func TestPanelRestore(t *testing.T) {
	src, _ := newStore(t)
	ctx := context.Background()
	seed(t, src)
	if err := src.SetAdmin(ctx, "boss", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if err := src.WriteBackup(ctx, t.TempDir(), &plain); err != nil {
		t.Fatal(err)
	}
	enc, err := src.EncryptedBackup(ctx, "backup pass")
	if err != nil {
		t.Fatal(err)
	}

	live, _ := newStore(t)
	if err := live.SetAdmin(ctx, "other", "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := live.StageRestore(ctx, []byte("not a backup"), ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("junk: %v", err)
	}
	if _, err := live.StageRestore(ctx, enc, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("encrypted without password")
	}
	if _, err := live.StageRestore(ctx, enc, "wrong"); !errors.Is(err, ErrInvalid) {
		t.Fatal("wrong password")
	}
	p, err := live.StageRestore(ctx, enc, "backup pass")
	if err != nil || p.Clients != 2 || p.Nodes != 1 || p.Logins != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := live.StageRestore(ctx, plain.Bytes(), ""); err != nil || !live.HasPendingRestore() {
		t.Fatal(err)
	}
	restarted := make(chan bool, 1)
	SetRestartHook(func() { restarted <- true })
	defer SetRestartHook(func() { os.Exit(0) })
	if err := live.ApplyRestore(); err != nil {
		t.Fatal(err)
	}
	<-restarted
	path := live.path
	live.Close()
	if ok, err := ApplyPendingRestore(path); !ok || err != nil {
		t.Fatal(ok, err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if list, _ := r.Clients(ctx); len(list) != 2 {
		t.Fatal("restored store has the wrong clients")
	}
	if _, err := r.Login(ctx, "boss", "correct horse battery"); err != nil {
		t.Fatal("restored logins", err)
	}
	if ok, _ := ApplyPendingRestore(path); ok {
		t.Fatal("pending restore applied twice")
	}
}
