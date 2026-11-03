//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDarkRestoreCustomerConnectionDurabilityAndBoundaries(t *testing.T) {
	ctx := context.Background()
	f, p, r, signer, plan := syntheticAdmissionSignedFixture(t)
	key := bytes.Repeat([]byte{0x61}, 32)
	nodeKey := bytes.Repeat([]byte{3}, 64)
	before := ownershipBalances(t, f.s)
	if _, e := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, p.ClientID, plan, "connection-admit-0001"); e != nil {
		t.Fatal(e)
	}
	box, e := uc.Open(uc.Options{Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32), Epoch: r.Epoch, Create: true})
	if e != nil {
		t.Fatal(e)
	}
	defer box.Close()
	report, e := box.Observe([]uc.Usage{{ClientID: p.ClientID}}, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.usage.AcceptChecked(ctx, usageledger.Principal{NodeID: r.Epoch.NodeID, InstallationID: r.Epoch.InstallationID}, report, f.s.CheckDarkRestoreAdmission); e != nil {
		t.Fatal(e)
	}
	initial, _ := initialAdmissionReportSHA(r.Epoch)
	l := DarkRestoreListenerApply{Version: 1, PlanSHA: plan, EpochID: r.Epoch.ID, CoreSHA: r.Birth.CoreSHA, InitialReportSHA: initial, Process: r.Birth.Process, AppliedUnixNano: time.Now().UnixNano()}
	l.Signature = ed25519.Sign(signer, l.signingBytes())
	if _, e = f.s.RegisterDarkRestoreListenerApply(ctx, r.Plan.NodeID, r.Plan.InstallationID, plan, l); e != nil {
		t.Fatal(e)
	}
	endpoint := mx.ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "example.com", CertificateSHA: strings.Repeat("c", 64)}
	in := DarkRestoreConnectionInput{EventID: "connection-intent-0001", PlanSHA: plan}
	if _, e = f.s.QueueDarkRestoreConnection(ctx, key, f.owner.Token, p.ClientID, in, endpoint); !errors.Is(e, ErrInvalid) {
		t.Fatal("consent", e)
	}
	var n int
	f.s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_connection_v1_*'").Scan(&n)
	if n != 0 {
		t.Fatal("denied request left schema")
	}
	in.AcknowledgeUsage = true
	if _, e = f.s.QueueDarkRestoreConnection(ctx, key, f.rep.Token, p.ClientID, in, endpoint); e == nil {
		t.Fatal("representative queued")
	}
	queued, e := f.s.QueueDarkRestoreConnection(ctx, key, f.owner.Token, p.ClientID, in, endpoint)
	if e != nil || queued.State != "pending" || queued.CustomerConnectionVerified {
		t.Fatal(queued, e)
	}
	replay, e := f.s.QueueDarkRestoreConnection(ctx, key, f.owner.Token, p.ClientID, in, endpoint)
	if e != nil || !reflect.DeepEqual(queued, replay) {
		t.Fatal("replayed intent", e)
	}
	conflict := in
	conflict.EventID = "connection-intent-0002"
	if _, e = f.s.QueueDarkRestoreConnection(ctx, key, f.owner.Token, p.ClientID, conflict, endpoint); !errors.Is(e, ErrConflict) {
		t.Fatal("conflicting intent", e)
	}
	state := mx.CustomerConnectionState{Expected: mx.RuntimeCondition{Highwater: 1, SavedSequence: 1, SavedSHA: p.ConfigSHA, ActiveSequence: 1, ActiveSHA: p.ConfigSHA, ProcessReady: true}, Usage: mx.MeteredCondition{EpochID: r.Epoch.ID, EpochSHA: admissionBytesSHA(connectionJSON(r.Epoch)), AdmissionSHA: strings.Repeat("d", 64)}, LeaseExpires: time.Now().Add(20 * time.Second).UnixNano()}
	if _, _, e = f.s.PollDarkRestoreConnection(ctx, key, nodeKey, "foreign-node", r.Plan.InstallationID, plan, state, endpoint); e == nil {
		t.Fatal("foreign node polled")
	}
	foreignState := state
	foreignState.Usage.EpochSHA = strings.Repeat("e", 64)
	if _, _, e = f.s.PollDarkRestoreConnection(ctx, key, nodeKey, r.Plan.NodeID, r.Plan.InstallationID, plan, foreignState, endpoint); e == nil {
		t.Fatal("foreign epoch issued")
	}
	q, found, e := f.s.PollDarkRestoreConnection(ctx, key, nodeKey, r.Plan.NodeID, r.Plan.InstallationID, plan, state, endpoint)
	if e != nil || !found || !mx.VerifyCustomerConnectionChallenge(nodeKey, q, time.Now().UnixNano()) {
		t.Fatal("issue", found, e)
	}
	q2, _, e := f.s.PollDarkRestoreConnection(ctx, key, nodeKey, r.Plan.NodeID, r.Plan.InstallationID, plan, state, endpoint)
	if e != nil || !bytes.Equal(connectionJSON(q), connectionJSON(q2)) {
		t.Fatal("poll changed nonce", e)
	}
	body := "DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"
	receipt := mx.CustomerConnectionReceipt{Challenge: q, Completed: time.Now().UnixNano(), ResponseSHA: admissionBytesSHA([]byte(body))}
	sign := func(v *mx.CustomerConnectionReceipt) {
		v.Signature = nil
		v.Signature = ed25519.Sign(signer, append([]byte("DARK-PRO-CUSTOMER-CONNECTION-RECEIPT-v1\x00"), connectionJSON(*v)...))
	}
	sign(&receipt)
	if !receipt.Verify(q) {
		t.Fatal("synthetic signed receipt")
	}
	if _, e = f.s.RegisterDarkRestoreConnection(ctx, nodeKey, "foreign", r.Plan.InstallationID, plan, receipt); e == nil {
		t.Fatal("foreign node posted proof")
	}
	wrong := receipt
	wrong.ResponseSHA = strings.Repeat("e", 64)
	sign(&wrong)
	if _, e = f.s.RegisterDarkRestoreConnection(ctx, nodeKey, r.Plan.NodeID, r.Plan.InstallationID, plan, wrong); e == nil {
		t.Fatal("foreign response accepted")
	}
	digest, e := f.s.RegisterDarkRestoreConnection(ctx, nodeKey, r.Plan.NodeID, r.Plan.InstallationID, plan, receipt)
	if e != nil {
		t.Fatal(e)
	}
	for j := 0; j < 2; j++ {
		got, e := f.s.RegisterDarkRestoreConnection(ctx, nodeKey, r.Plan.NodeID, r.Plan.InstallationID, plan, receipt)
		if e != nil || got != digest {
			t.Fatal("receipt replay", e)
		}
	}
	status, e := f.s.DarkRestoreConnectionStatus(ctx, f.owner.Token, p.ClientID)
	if e != nil || !status.CustomerConnectionVerified || status.AuthorizesMutation || status.ReceiptSHA != digest {
		t.Fatal(status, e)
	}
	if _, e = f.s.DarkRestoreConnectionStatus(ctx, f.rep.Token, p.ClientID); e == nil {
		t.Fatal("representative read proof")
	}
	for _, table := range []string{"intent", "request", "receipt"} {
		for _, query := range []string{"DELETE FROM darkrestore_connection_v1_" + table, "UPDATE darkrestore_connection_v1_" + table + " SET id=id"} {
			if _, e = f.s.db.Exec(query); e == nil {
				t.Fatal("mutable evidence", table)
			}
		}
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("wallet changed")
	}
	client, e := f.s.Client(ctx, p.ClientID)
	if e != nil || client.DeploymentState != "pending_node_apply" {
		t.Fatal("test activated service", e)
	}
	if e = f.s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = ValidateOfflineBackup(ctx, f.path); e != nil {
		t.Fatal("backup", e)
	}
	reopened, e := Open(f.path)
	if e != nil {
		t.Fatal("reopen", e)
	}
	defer reopened.Close()
	got, e := reopened.RegisterDarkRestoreConnection(ctx, nodeKey, r.Plan.NodeID, r.Plan.InstallationID, plan, receipt)
	if e != nil || got != digest {
		t.Fatal("durable receipt replay", e)
	}
	// Offline validation checks exact append-only schema and Ed25519 body.
	if _, e = reopened.db.Exec("DROP TRIGGER darkrestore_connection_v1_receipt_no_update"); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(wrong)
	if _, e = reopened.db.Exec("UPDATE darkrestore_connection_v1_receipt SET receipt_json=?", raw); e != nil {
		t.Fatal(e)
	}
	reopened.Close()
	if _, e = ValidateOfflineBackup(ctx, f.path); e == nil {
		t.Fatal("altered proof schema admitted")
	}
}
