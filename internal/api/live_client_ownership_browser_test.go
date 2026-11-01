//go:build linux

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

// Real compiled panel, existing representative cookie sessions and a disposable
// SQLite ledger. The browser cannot inject API responses or authentication.
// Native usage is synthetic, registered and accepted by the actual ledger;
// no external Node, customer traffic or production service is involved.
func TestRealBrowserClientOwnership(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_OWNERSHIP_BROWSER") != "1" {
		t.Skip("explicit compiled-browser ownership acceptance only")
	}
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_OWNERSHIP_BROWSER_SCRIPT")
	if !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("absolute compiled UI and browser script paths required")
	}
	for _, path := range []string{filepath.Join(webRoot, "index.html"), script} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("compiled UI and browser script must exist", path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	s, metered, _, epoch := readyUsageServer(t)
	const source = "usage-rep"
	const target = "ownership-browser-target"
	const targetPassword = "synthetic-ownership-browser-target-password"
	const addedVolume = int64(1 << 20)
	if _, err := s.business.CreateRepresentative(ctx, target, "Ownership destination"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "ownership-browser-target-credit-0001", RepresentativeID: target, VolumeDeltaBytes: 10 << 20, UnlimitedDeltaUnits: 20, Reason: "Synthetic ownership browser credit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, target, target, targetPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableClientOwnershipV1Lab(ctx); err != nil {
		t.Fatal(err)
	}
	before := map[string]business.Client{metered.ID: metered}
	for _, id := range []string{"ownership-batch-second", "ownership-group-first", "ownership-group-archived"} {
		c, err := s.business.CreateClient(ctx, business.NewClient{EventID: "ownership-browser-create-" + id, ClientID: id, RepresentativeID: source, Name: "Synthetic " + id, DurationDays: 30, VolumeBytes: 100, IPCount: 2})
		if err != nil {
			t.Fatal(err)
		}
		if id == "ownership-group-archived" {
			c, err = s.business.SetClientState(ctx, business.SetClientState{EventID: "ownership-browser-archive-0001", ClientID: id, Action: "archive", Reason: "Preserve archived group member"})
			if err != nil {
				t.Fatal(err)
			}
		}
		before[id] = c
	}
	for _, group := range []business.NewGroup{{ID: "ownership-source-group", RepresentativeID: source, Name: "Complete source group"}, {ID: "ownership-target-group", RepresentativeID: target, Name: "Destination group"}} {
		if _, err := s.business.CreateGroup(ctx, group); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{metered.ID, "ownership-batch-second", "ownership-group-first", "ownership-group-archived"}
	if _, err := s.business.AssignGroupMembers(ctx, "ownership-source-group", source, ids); err != nil {
		t.Fatal(err)
	}
	box, err := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "outbox"), Epoch: epoch, Key: bytes.Repeat([]byte{3}, 32), Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	report, err := box.Observe([]uc.Usage{{ClientID: metered.ID, Uplink: 20, Downlink: 25}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, report); err != nil {
		t.Fatal(err)
	}
	// Issue a real encrypted subscription credential independently of traffic
	// publication. Ownership must preserve it while denying its former manager.
	for _, enable := range []func(context.Context) error{s.business.EnableSystemProbeAccounting, s.business.EnableMeteredHealthChallenges, s.business.EnableMeteredHealthQueue, s.business.EnableMeteredHealthRounds, s.business.EnableLabSubscriptions} {
		if err := enable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.MutateLabSubscription(ctx, s.inboundKey, source, metered.ID, business.SubscriptionTokenMutation{EventID: "ownership-browser-subscription-0001", Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	secretBefore, err := s.business.RevealLabSubscription(ctx, s.inboundKey, source, metered.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := s.business.ListEvents(ctx, source, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	sourceBefore, err := s.business.Representative(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", s.BrowserHandler())
	mux.Handle("/", webui.Directory(webRoot))
	ui := httptest.NewServer(mux)
	defer ui.Close()
	controlPath := filepath.Join(t.TempDir(), "ownership-browser-control.json")
	control, err := json.Marshal(map[string]string{"username": ownerUsername, "password": ownerPassword, "source_username": source, "source_password": repPassword, "target_username": target, "target_password": targetPassword})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controlPath, control, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+ui.URL, "DARK_PRO_QA_CONTROL="+controlPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := string(output)
		for _, password := range []string{ownerPassword, repPassword, targetPassword} {
			message = strings.ReplaceAll(message, password, "[synthetic test password]")
		}
		t.Fatalf("real ownership browser failed: %v\n%s", err, message)
	}
	if !bytes.Contains(output, []byte("REAL_CLIENT_OWNERSHIP_BROWSER_PASS\n")) {
		t.Fatal("browser omitted complete ownership acceptance marker")
	}
	for id, expected := range before {
		expected.RepresentativeID = target
		if id == metered.ID {
			expected.DurationDays += 1
			expected.ExpiresUnix += 24 * 60 * 60
			expected.VolumeBytes += addedVolume
			expected.ChargeVolumeBytes += addedVolume
		}
		actual, err := s.business.Client(ctx, id)
		if err != nil || actual != expected {
			t.Fatal("ownership or explicit renewal changed unexpected client fields", id, err)
		}
	}
	usage, err := s.business.ClientUsage(ctx, metered.ID)
	if err != nil || usage.Uplink != 20 || usage.Downlink != 25 || usage.Total != 45 || !usage.Known || usage.Holds != 0 || usage.RepresentativeID != target {
		t.Fatal("real browser transfer/renewal changed usage", err)
	}
	sourceAfter, err := s.business.Representative(ctx, source)
	if err != nil || sourceAfter != sourceBefore {
		t.Fatal("source credit changed during ownership/target renewal", err)
	}
	targetAfter, err := s.business.Representative(ctx, target)
	if err != nil || targetAfter.VolumeCreditBytes != 9<<20 || targetAfter.UnlimitedCreditUnits != 20 {
		t.Fatal("destination debit must equal only the explicit renewal", err)
	}
	eventsAfter, err := s.business.ListEvents(ctx, source, 50, 0)
	if err != nil || !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatal("original payer financial receipts changed", err)
	}
	secretAfter, err := s.business.RevealLabSubscription(ctx, s.inboundKey, target, metered.ID, 1)
	if err != nil || secretAfter != secretBefore {
		t.Fatal("existing encrypted subscription credential changed", err)
	}
	if _, err := s.business.RevealLabSubscription(ctx, s.inboundKey, source, metered.ID, 1); !errors.Is(err, business.ErrNotFound) {
		t.Fatal("previous representative retained subscription management access", err)
	}
	sourceMembers, err := s.business.ListGroupMembers(ctx, "ownership-source-group", source, 50, 0)
	if err != nil || len(sourceMembers) != 0 {
		t.Fatal("source group retained transferred members", err)
	}
	targetMembers, err := s.business.ListGroupMembers(ctx, "ownership-target-group", target, 50, 0)
	if err != nil || len(targetMembers) != 4 {
		t.Fatal("target group omitted transferred or archived members", err)
	}
	t.Log("REAL_CLIENT_OWNERSHIP_BROWSER_PASS")
}
