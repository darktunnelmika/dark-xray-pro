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

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

// This acceptance uses the compiled panel, real cookie/CSRF handlers and a
// disposable SQLite Store. No browser responses, identities or balances are
// mocked, and there are no external Nodes, bots or customer traffic.
func TestRealBrowserSellerManagementV2(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_SELLER_BROWSER") != "1" {
		t.Skip("explicit compiled-browser seller acceptance only")
	}
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_SELLER_BROWSER_SCRIPT")
	if !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("absolute compiled UI and browser script paths required")
	}
	for _, path := range []string{filepath.Join(webRoot, "index.html"), script} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("compiled UI and browser script must exist", path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s := enabledOwner(t)
	const repID = "browser-seller-rep"
	const repUsername = "browser-seller-login"
	const repPassword = "synthetic-seller-browser-password"
	if _, err := s.business.CreateRepresentative(ctx, repID, "Browser Seller Representative"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
		EventID: "seller-browser-initial-credit-0001", RepresentativeID: repID,
		VolumeDeltaBytes: 100 * 1024 * 1024, UnlimitedDeltaUnits: 20, Reason: "Synthetic browser acceptance",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, repID, repUsername, repPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerQuotaV2Lab(ctx); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", s.BrowserHandler())
	mux.Handle("/", webui.Directory(webRoot))
	ui := httptest.NewServer(mux)
	defer ui.Close()
	controlPath := filepath.Join(t.TempDir(), "seller-browser-control.json")
	control, err := json.Marshal(map[string]string{
		"username": ownerUsername, "password": ownerPassword,
		"representative_username": repUsername, "representative_password": repPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controlPath, control, 0600); err != nil {
		t.Fatal(err)
	}
	browser := exec.CommandContext(ctx, "node", script)
	browser.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+ui.URL, "DARK_PRO_QA_CONTROL="+controlPath)
	output, err := browser.CombinedOutput()
	if err != nil {
		message := strings.ReplaceAll(strings.ReplaceAll(string(output), ownerPassword, "[synthetic owner password]"), repPassword, "[synthetic representative password]")
		t.Fatalf("real seller browser acceptance failed: %v\n%s", err, message)
	}
	if !bytes.Contains(output, []byte("REAL_SELLER_MANAGEMENT_BROWSER_PASS\n")) {
		t.Fatal("browser omitted the complete seller acceptance marker")
	}
	rep, err := s.business.Representative(ctx, repID)
	if err != nil || rep.VolumeCreditBytes != 91*1024*1024 || rep.UnlimitedCreditUnits != 19 {
		t.Fatal("representative free credit did not match real browser transfers", rep, err)
	}
	seller, err := s.business.SellerV2(ctx, repID, "browser-seller")
	if err != nil || seller.Enabled || seller.Revision != 1 || seller.ReservedBytes != 9*1024*1024 || seller.ReservedUnits != 1 || seller.TelegramUserID != 123456789 {
		t.Fatal("real seller identity, state and reserve were not preserved", seller, err)
	}
	owner, err := s.business.Login(ctx, ownerUsername, ownerPassword)
	if err != nil {
		t.Fatal(err)
	}
	permission, err := s.business.SellerPermissionV2(ctx, owner.Token, repID)
	if err != nil || permission.ManageSellers || permission.Revision != 2 {
		t.Fatal("owner revoke did not persist", permission, err)
	}
	if rep.VolumeCreditBytes+seller.ReservedBytes != 100*1024*1024 || rep.UnlimitedCreditUnits+seller.ReservedUnits != 20 {
		t.Fatal("browser workflow changed total credit")
	}
	t.Log("REAL_SELLER_MANAGEMENT_BROWSER_PASS")
}
