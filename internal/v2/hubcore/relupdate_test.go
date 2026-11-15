package hubcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestHubReleaseUpdate(t *testing.T) {
	hubName, nodeName := assetNames()
	bin := []byte("\x7fELF fake")
	sum := sha256.Sum256(bin)
	sums := "deadbeef" + strings.Repeat("0", 56) + "  " + hubName + "\n" + hex.EncodeToString(sum[:]) + "  " + nodeName + "\n"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			a := func(n string) string {
				return fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/dl/%s"}`, n, srv.URL, n)
			}
			fmt.Fprintf(w, `[{"tag_name":"v9.0.0","prerelease":true,"assets":[%s,%s]},
				{"tag_name":"v9.1.0","assets":[%s]},
				{"tag_name":"v8.2.0","published_at":"2026-11-01T00:00:00Z","body":"notes","assets":[%s,%s,%s]},
				{"tag_name":"v8.10.0","draft":true,"assets":[%s,%s]}]`,
				a(hubName), a("SHA256SUMS"), a(hubName), a(hubName), a(nodeName), a("SHA256SUMS"), a(hubName), a("SHA256SUMS"))
		case "/dl/SHA256SUMS":
			w.Write([]byte(sums))
		default:
			w.Write(bin)
		}
	}))
	defer srv.Close()
	old := hubReleasesURL
	hubReleasesURL = srv.URL + "/releases"
	defer func() { hubReleasesURL = old }()
	rel, err := LatestHubRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v8.2.0" || !rel.Newer || rel.Notes != "notes" || rel.Published != "2026-11-01" {
		t.Fatalf("%+v (arch %s)", rel, runtime.GOARCH)
	}
	s, _ := newStore(t)
	// The HUB binary's checksum does not match: nothing may be installed.
	if _, _, _, err := s.InstallHubRelease(context.Background()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal("checksum mismatch must stop the update:", err)
	}
	if semver("v2.10.0", "v2.9.9") != 1 || semver("v2.7.0", "v2.7.0") != 0 || semver("v1.0.0", "v2.0.0") != -1 {
		t.Fatal("semver")
	}
}
