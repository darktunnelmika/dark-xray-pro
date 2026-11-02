package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	restorefixture "github.com/darktunnelmika/dark-xray-pro/testdata/darkrestore"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

func stagedRestoreAPI(t *testing.T) ownershipAPI {
	t.Helper()
	f := newOwnershipAPI(t)
	if len(f.s.inboundKey) == 0 {
		if err := f.s.EnableNativeInbounds(make([]byte, 32)); err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "approved.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = db.Exec(restorefixture.SQL); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(blob)
	if err = f.s.StageDarkRestoreImport(context.Background(), path, hex.EncodeToString(sum[:]), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	f.h = f.s.BrowserHandler()
	return f
}
func TestDarkRestoreStageBrowserAccessAndReceipt(t *testing.T) {
	f := stagedRestoreAPI(t)
	from := "https://" + browserHost
	req := restoreQuoteRequest{ReviewSHA: f.s.darkRestoreStage.digest, Mappings: []business.DarkRestoreGroupMappingV1{{SourceGroupID: "synthetic-group", RepresentativeID: "transfer-target"}}}
	for _, p := range []struct {
		method, route string
		body          any
	}{{"GET", "/stage", nil}, {"POST", "/quote", req}, {"POST", "/commit", req}, {"GET", "/receipts/restore-synthetic-0001", nil}} {
		if w := browserRequest(t, f.h, p.method, restoreBase+p.route, p.body, f.source, f.sourceCSRF, from); w.Code != 403 {
			t.Fatal("representative accessed import", p.route, w.Code)
		}
		if w := browserRequest(t, f.s.AgentHandler(), p.method, restoreBase+p.route, p.body, f.owner, f.ownerCSRF, from); w.Code == 200 {
			t.Fatal("agent accessed import")
		}
	}
	for _, a := range []struct{ csrf, origin string }{{"", from}, {f.ownerCSRF, "https://unrelated.invalid"}} {
		if w := browserRequest(t, f.h, "POST", restoreBase+"/quote", req, f.owner, a.csrf, a.origin); w.Code != 403 {
			t.Fatal("csrf/origin gate", w.Code)
		}
	}
	if w := browserRequest(t, f.h, "POST", restoreBase+"/quote", map[string]any{"review_sha256": req.ReviewSHA, "mappings": req.Mappings, "path": "/private"}, f.owner, f.ownerCSRF, from); w.Code != 422 {
		t.Fatal("browser-supplied source accepted", w.Code)
	}
	w := browserRequest(t, f.h, "POST", restoreBase+"/quote", req, f.owner, f.ownerCSRF, from)
	var q business.DarkRestoreImportQuoteV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil || q.Importable != 1 {
		t.Fatal("quote", w.Code)
	}
	commit := restoreCommitRequest{restoreQuoteRequest: req, QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix, EventID: "restore-synthetic-0001"}
	for i := 0; i < 2; i++ {
		w = browserRequest(t, f.h, "POST", restoreBase+"/commit", commit, f.owner, f.ownerCSRF, from)
		var receipt business.DarkRestoreImportReceiptV1
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Count != 1 || receipt.Replayed != (i == 1) {
			t.Fatal("commit/replay", w.Code)
		}
	}
	w = browserRequest(t, f.h, "GET", restoreBase+"/receipts/"+commit.EventID, nil, f.owner, "", from)
	if w.Code != 200 || !strings.Contains(w.Body.String(), commit.EventID) {
		t.Fatal("receipt recovery", w.Code)
	}
}

func TestDarkRestoreLiveBrowser(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_DARKRESTORE_BROWSER") != "1" {
		t.Skip("compiled browser acceptance")
	}
	f := stagedRestoreAPI(t)
	root := os.Getenv("DARK_PRO_TEST_WEB_DIST")
	if !filepath.IsAbs(root) {
		t.Fatal("absolute build path required")
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", f.h)
	mux.Handle("/", webui.Directory(root))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../web/tests/live-darkrestore-import.mjs")
	cmd.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+srv.URL, "DARK_PRO_TEST_OWNER="+ownerUsername, "DARK_PRO_TEST_PASSWORD="+ownerPassword)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
}
