//go:build linux

package business_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

func TestPreparedDownloadInOwnerBrowser(t *testing.T) {
	dist := os.Getenv("DARK_PRO_TEST_WEB_DIST")
	if dist == "" {
		t.Skip("requires compiled UI and Chromium")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store, id, key, pins, password := business.BrowserPreparedFixture(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	reg, err := noderegistry.Open(filepath.Join(dir, "nodes.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := api.NewServer(telemetry.NewStore(time.Minute, time.Now), reg, strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, setup := range []func() error{func() error { return srv.EnableBusinessLab(store) }, func() error { return srv.EnableNativeInbounds(key) }, func() error { return srv.EnableClientOwnershipV1Lab(ctx) }, func() error {
		return srv.StageDarkRestoreLocalHandoff(ctx, pins.Path, pins.SHA, pins.PublicPath, pins.PublicSHA)
	}} {
		if err := setup(); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", srv.BrowserHandler())
	mux.Handle("/", webui.Directory(dist))
	server := httptest.NewServer(mux)
	defer server.Close()
	before, err := store.ListRepresentatives(ctx, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	client, err := store.Client(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", "../../web/tests/live-prepared-download.mjs")
	cmd.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+server.URL, "DARK_PRO_TEST_PASSWORD="+password, "DARK_PRO_TEST_UUID="+client.UUID)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	}
	after, err := store.ListRepresentatives(ctx, 100, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("balances changed", err)
	}
	current, err := store.Client(ctx, id)
	if err != nil || current.DeploymentState != "disabled" {
		t.Fatal("client state", err)
	}
}
