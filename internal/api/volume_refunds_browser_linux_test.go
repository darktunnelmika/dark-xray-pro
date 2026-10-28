//go:build linux

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

func TestRealBrowserVolumeRefunds(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_VOLUME_REFUND_BROWSER") != "1" {
		t.Skip("explicit real compiled browser volume refund acceptance")
	}
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_VOLUME_REFUND_BROWSER_SCRIPT")
	if !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("absolute compiled UI and test script paths required")
	}
	for _, file := range []string{filepath.Join(webRoot, "index.html"), script} {
		if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
			t.Fatal("compiled browser acceptance input missing", err)
		}
	}
	s, client := volumeRefundServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	go s.RunVolumeRefunds(ctx)
	defer func() {
		cancel()
		stop, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		s.WaitVolumeRefunds(stop)
	}()
	mux := http.NewServeMux()
	mux.Handle("/api/", s.BrowserHandler())
	mux.Handle("/", webui.Directory(webRoot))
	ui := httptest.NewServer(mux)
	defer ui.Close()
	controlPath := filepath.Join(t.TempDir(), "refund-browser-control.json")
	raw, err := json.Marshal(map[string]string{"username": "refund-rep", "password": repPassword})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controlPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+ui.URL, "DARK_PRO_QA_CONTROL="+controlPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real refund browser failed: %v\n%s", err, strings.ReplaceAll(string(output), repPassword, "[synthetic password]"))
	}
	if !bytes.Contains(output, []byte("REAL_VOLUME_REFUND_BROWSER_PASS\n")) {
		t.Fatal("real refund browser acceptance marker missing")
	}
	rep, err := s.business.Representative(ctx, client.RepresentativeID)
	if err != nil || rep.VolumeCreditBytes != 1000 || rep.UnlimitedCreditUnits != 8 {
		t.Fatal("browser refund changed token or credit conservation", rep, err)
	}
	c, err := s.business.Client(ctx, client.ID)
	if err != nil || c.UUID != client.UUID || !c.Archived || c.VolumeBytes != 0 || c.ChargeVolumeBytes != 100 {
		t.Fatal("browser refund lost historical identity or entitlement", c, err)
	}
	history, err := s.business.VolumeRefundHistory(ctx, client.RepresentativeID, 25, 0)
	if err != nil || len(history) != 1 || history[0].RefundedBytes != 100 {
		t.Fatal("browser refund missing immutable exact history", history, err)
	}
	t.Log("REAL_VOLUME_REFUND_BROWSER_PASS")
}
