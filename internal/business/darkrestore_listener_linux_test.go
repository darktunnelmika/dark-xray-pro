//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func TestDarkRestoreListenerApplyReceiptBoundaries(t *testing.T) {
	ctx := context.Background()
	f, p, r, signer, sha := syntheticAdmissionSignedFixture(t)
	before := ownershipBalances(t, f.s)
	clientBefore, err := f.s.Client(ctx, p.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := initialAdmissionReportSHA(r.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	a := DarkRestoreListenerApply{Version: 1, PlanSHA: sha, EpochID: r.Epoch.ID, CoreSHA: r.Birth.CoreSHA, InitialReportSHA: initial, Process: r.Birth.Process, AppliedUnixNano: time.Now().UnixNano()}
	sign := func(v *DarkRestoreListenerApply) { v.Signature = ed25519.Sign(signer, v.signingBytes()) }
	sign(&a)
	if !a.verify(r) {
		t.Fatal("fixture")
	}
	if _, err = f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, a); err == nil {
		t.Fatal("birth without admission accepted")
	}
	key := bytes.Repeat([]byte{0x61}, 32)
	if _, err = f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, p.ClientID, sha, "listener-apply-admit-0001"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, a); err == nil {
		t.Fatal("missing durable zero accepted")
	}
	var n int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_listener_apply_v1*'").Scan(&n); err != nil || n != 0 {
		t.Fatal("failed receipt left partial schema", n, err)
	}
	box, err := uc.Open(uc.Options{Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32), Epoch: r.Epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	report, err := box.Observe([]uc.Usage{{ClientID: p.ClientID}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.usage.AcceptChecked(ctx, usageledger.Principal{NodeID: r.Epoch.NodeID, InstallationID: r.Epoch.InstallationID}, report, f.s.CheckDarkRestoreAdmission); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RegisterDarkRestoreListenerApply(ctx, "foreign-node", r.Epoch.InstallationID, sha, a); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("foreign node", err)
	}
	tests := []struct {
		name string
		edit func(*DarkRestoreListenerApply)
	}{
		{"core", func(v *DarkRestoreListenerApply) { v.CoreSHA = strings.Repeat("d", 64) }},
		{"process", func(v *DarkRestoreListenerApply) { v.Process.StartTicks++ }},
		{"epoch", func(v *DarkRestoreListenerApply) { v.EpochID = strings.Repeat("d", 32) }},
		{"zero report", func(v *DarkRestoreListenerApply) { v.InitialReportSHA = strings.Repeat("d", 64) }},
		{"before birth", func(v *DarkRestoreListenerApply) { v.AppliedUnixNano = r.Birth.ObservedUnixNano }},
		{"after lease", func(v *DarkRestoreListenerApply) { v.AppliedUnixNano = r.Birth.Lease.Expires }},
		{"future", func(v *DarkRestoreListenerApply) { v.AppliedUnixNano = time.Now().Add(time.Second).UnixNano() }},
		{"version", func(v *DarkRestoreListenerApply) { v.Version++ }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bad := a
			tc.edit(&bad)
			sign(&bad)
			if _, err := f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, bad); err == nil {
				t.Fatal("invalid attestation accepted")
			}
		})
	}
	bad := a
	bad.Signature = append([]byte(nil), a.Signature...)
	bad.Signature[0] ^= 1
	if _, err = f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, bad); err == nil {
		t.Fatal("bad signature accepted")
	}
	// Ensure receipt creation is transactional even when the INSERT fails.
	for _, ddl := range darkRestoreListenerDDL {
		if _, err = f.s.db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.s.db.Exec("CREATE TEMP TRIGGER fail_listener_apply BEFORE INSERT ON darkrestore_listener_apply_v1 BEGIN SELECT RAISE(ABORT,'synthetic write failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, a); err == nil {
		t.Fatal("insert failure accepted")
	}
	if _, err = f.s.db.Exec("DROP TRIGGER fail_listener_apply"); err != nil {
		t.Fatal(err)
	}
	digest, err := f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, a)
	if err != nil || digest != a.Digest() {
		t.Fatal(digest, err)
	}
	for i := 0; i < 2; i++ {
		got, err := f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, a)
		if err != nil || got != digest {
			t.Fatal("replay", err)
		}
	}
	changed := a
	changed.AppliedUnixNano++
	sign(&changed)
	if _, err = f.s.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting replay", err)
	}
	for _, query := range []string{"UPDATE darkrestore_listener_apply_v1 SET receipt_json=receipt_json", "DELETE FROM darkrestore_listener_apply_v1"} {
		if _, err = f.s.db.Exec(query); err == nil {
			t.Fatal("receipt mutable")
		}
	}
	evidence, err := f.s.ProjectDarkRestoreNativeAdmissionEvidenceV1(ctx, f.owner.Token, p.ClientID)
	if err != nil || !evidence.ListenerApplyRecorded || evidence.ListenerApplyReceiptSHA != digest || evidence.NativeApplyRecorded || evidence.ServingHealthRecorded || evidence.AuthorizesMutation {
		t.Fatal(evidence, err)
	}
	pins := DarkRestoreNativeAdmissionPinsV1{ClientID: evidence.ClientID, TransitionSHA: evidence.TransitionSHA, PreparedContentSHA: evidence.PreparedContentSHA, PreparedAuthoritySHA: evidence.PreparedAuthoritySHA, PlanSHA: evidence.PlanSHA, BirthReceiptSHA: evidence.BirthReceiptSHA, AdmissionEventID: evidence.AdmissionEventID, AdmissionReceiptSHA: evidence.AdmissionReceiptSHA, EpochID: evidence.EpochID, InitialReportSHA: evidence.InitialReportSHA, ListenerApplyRecorded: true, ListenerApplyReceiptSHA: digest}
	pins.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-ADMISSION-EVIDENCE-v1", evidence)
	if v, err := ValidateDarkRestoreNativeAdmissionEvidenceV1(evidence, pins); err != nil || !v.ListenerApplyRecorded {
		t.Fatal(v, err)
	}
	pins.ListenerApplyReceiptSHA = strings.Repeat("0", 64)
	if _, err = ValidateDarkRestoreNativeAdmissionEvidenceV1(evidence, pins); err == nil {
		t.Fatal("foreign independent apply pin accepted")
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("wallet changed")
	}
	clientAfter, err := f.s.Client(ctx, p.ClientID)
	if err != nil || clientAfter.UUID != clientBefore.UUID || clientAfter.ExpiresUnix != clientBefore.ExpiresUnix || clientAfter.DeploymentState != "pending_node_apply" {
		t.Fatal("receipt changed client activation", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("offline backup", err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal("reopen", err)
	}
	defer reopened.Close()
	if got, err := reopened.RegisterDarkRestoreListenerApply(ctx, r.Epoch.NodeID, r.Epoch.InstallationID, sha, a); err != nil || got != digest {
		t.Fatal("durable replay", err)
	}
	var saved []byte
	if err = reopened.db.QueryRow("SELECT receipt_json FROM darkrestore_listener_apply_v1").Scan(&saved); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(a)
	if !bytes.Equal(saved, raw) {
		t.Fatal("receipt changed across reopen")
	}
}
