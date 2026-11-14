package nodeagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestXrayUpdateTask(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "xray")
	os.WriteFile(bin, []byte("#!/bin/sh\necho 'Xray 26.0.0 (Xray, Penetrates Everything.)'\n"), 0o755)
	if v := XrayVersion(bin); v != "26.0.0" {
		t.Fatalf("version %q", v)
	}
	args := func(sum string) json.RawMessage {
		b, _ := json.Marshal(map[string]string{"name": "xray@v26.1.1-amd64", "sha256": sum})
		return b
	}
	broken := []byte("#!/bin/sh\necho nope\n")
	if _, err := XrayUpdateTask(fakeFetch{broken}, bin)(context.Background(), args(shaOf(broken))); err == nil {
		t.Fatal("binary that reports no version accepted")
	}
	good := []byte("#!/bin/sh\necho 'Xray 26.1.1 (Xray, Penetrates Everything.)'\n")
	v, err := XrayUpdateTask(fakeFetch{good}, bin)(context.Background(), args(shaOf(good)))
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := v.(Restart); !ok || r.Value.(map[string]string)["to"] != "26.1.1" {
		t.Fatalf("result %#v", v)
	}
	if XrayVersion(bin) != "26.1.1" || XrayVersion(bin+".prev") != "26.0.0" {
		t.Fatal("binary not replaced or previous not kept")
	}
}
