package hubcore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestXrayDistAndUpdate(t *testing.T) {
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("xray")
	w.Write([]byte("#!/bin/sh\necho Xray 26.1.1\n"))
	w2, _ := zw.Create("geoip.dat")
	w2.Write([]byte("x"))
	zw.Close()
	sum := sha256.Sum256(zbuf.Bytes())
	tamper := false
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".zip.dgst"):
			if tamper {
				rw.Write([]byte("SHA2-256= 00"))
				return
			}
			rw.Write([]byte("MD5= abc\nSHA2-256= " + hex.EncodeToString(sum[:]) + "\n"))
		case strings.HasSuffix(r.URL.Path, "/Xray-linux-64.zip"):
			rw.Write(zbuf.Bytes())
		default:
			http.NotFound(rw, r)
		}
	}))
	defer srv.Close()
	old := xrayDownloadURL
	xrayDownloadURL = srv.URL + "/"
	defer func() { xrayDownloadURL = old }()

	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if _, err := s.StartXrayUpdate(ctx, "de-1", "v26.1.1"); err == nil || !strings.Contains(err.Error(), "architecture") {
		t.Fatalf("node without reported arch: %v", err)
	}
	s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1", XrayVersion: "26.0.0", Arch: "amd64"})
	if ns, _ := s.Nodes(ctx); ns[0].XrayVersion != "26.0.0" || ns[0].Arch != "amd64" {
		t.Fatalf("node %+v", ns[0])
	}
	if _, err := s.StartXrayUpdate(ctx, "de-1", "latest"); err == nil {
		t.Fatal("bad version accepted")
	}
	tamper = true
	if _, err := s.StartXrayUpdate(ctx, "de-1", "v26.1.1"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered release accepted: %v", err)
	}
	tamper = false
	id, err := s.StartXrayUpdate(ctx, "de-1", "v26.1.1")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1"})
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != id || resp.Tasks[0].Kind != wire.TaskXrayUpdate {
		t.Fatalf("tasks %+v", resp.Tasks)
	}
	var a map[string]string
	json.Unmarshal(resp.Tasks[0].Args, &a)
	p, ok := s.distFile(a["name"])
	if !ok || a["name"] != "xray@v26.1.1-amd64" || cachedSHA(p) != a["sha256"] {
		t.Fatalf("dist %v %v %s", a, ok, p)
	}
	if _, ok := s.distFile("xray@v26.1.1-amd64/../../x"); ok {
		t.Fatal("traversal through dist name")
	}
}

func TestXrayOverlay(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if err := s.PutXrayOverlay(ctx, json.RawMessage(`{"inbounds":[]}`)); err == nil {
		t.Fatal("managed section accepted")
	}
	if err := s.PutXrayOverlay(ctx, json.RawMessage(`[1]`)); err == nil {
		t.Fatal("non-object accepted")
	}
	if err := s.PutXrayOverlay(ctx, json.RawMessage(`{"log":{"loglevel":"info"},"policy":{"levels":{"0":{"connIdle":120,"statsUserUplink":false}}}}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ := s.RenderNode(ctx, "de-1")
	var c struct {
		Log    map[string]any `json:"log"`
		Policy struct {
			Levels map[string]map[string]any `json:"levels"`
			System map[string]any            `json:"system"`
		} `json:"policy"`
	}
	json.Unmarshal(raw, &c)
	l0 := c.Policy.Levels["0"]
	if c.Log["loglevel"] != "info" || l0["connIdle"] != float64(120) || l0["statsUserUplink"] != true || c.Policy.System["statsInboundUplink"] != true {
		t.Fatalf("merged %s", raw)
	}
}
