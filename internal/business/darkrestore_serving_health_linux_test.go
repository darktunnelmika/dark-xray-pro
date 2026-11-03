//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func signedServingHealthReceipt(t *testing.T, key []byte, q mx.ProtocolHealthRequest, completed int64) mx.ProtocolHealthReceipt {
	t.Helper()
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	body := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
	p := mx.ProtocolHealthReceipt{
		Version: q.Version, RequestSHA: admissionBytesSHA(raw),
		ResponseSHA: hex.EncodeToString(body[:]), Completed: completed,
	}
	unsigned, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v4\x00"))
	mac.Write(unsigned)
	p.Signature = hex.EncodeToString(mac.Sum(nil))
	return p
}

func TestDarkRestoreServingHealthStoreIsImmutableAndReadOnly(t *testing.T) {
	ctx := context.Background()
	f, plan, registered, listenerSigner, planSHA := syntheticAdmissionSignedFixture(t)
	beforeBalances := ownershipBalances(t, f.s)
	beforeClient, err := f.s.Client(ctx, plan.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x61}, 32)
	if _, err = f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, plan.ClientID, planSHA, "serving-health-admit-0001"); err != nil {
		t.Fatal(err)
	}
	box, err := uc.Open(uc.Options{
		Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32),
		Epoch: registered.Epoch, Create: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	report, err := box.Observe([]uc.Usage{{ClientID: plan.ClientID}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.usage.AcceptChecked(ctx, usageledger.Principal{
		NodeID: registered.Epoch.NodeID, InstallationID: registered.Epoch.InstallationID,
	}, report, f.s.CheckDarkRestoreAdmission); err != nil {
		t.Fatal(err)
	}

	initial, err := initialAdmissionReportSHA(registered.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	applied := registered.Birth.ObservedUnixNano + 1
	if applied >= time.Now().UnixNano() {
		time.Sleep(time.Millisecond)
	}
	listener := DarkRestoreListenerApply{
		Version: 1, PlanSHA: planSHA, EpochID: registered.Epoch.ID,
		CoreSHA: registered.Birth.CoreSHA, InitialReportSHA: initial,
		Process: registered.Birth.Process, AppliedUnixNano: applied,
	}
	listener.Signature = ed25519.Sign(listenerSigner, listener.signingBytes())
	listenerSHA, err := f.s.RegisterDarkRestoreListenerApply(ctx, registered.Epoch.NodeID, registered.Epoch.InstallationID, planSHA, listener)
	if err != nil {
		t.Fatal(err)
	}

	agentKey := bytes.Repeat([]byte{0x5a}, 64)
	endpoint := healthEndpoint()
	issued, completed := applied+100, applied+200
	expires, recorded := applied+800, applied+300
	if expires > registered.Birth.Lease.Expires {
		t.Fatal("synthetic lease too short")
	}
	q, err := mx.SignProtocolHealthRequest(agentKey, mx.ProtocolHealthRequest{
		Version: mx.InitialApplyProtocolHealthVersion, ID: strings.Repeat("1", 32),
		NodeID: plan.NodeID, InstallationID: plan.InstallationID,
		RuntimeInstance: strings.Repeat("2", 32), StageID: strings.Repeat("3", 32),
		ApplyCommandID: strings.Repeat("4", 32), InboundID: plan.Prepared.InboundID,
		Port: 24503, CredentialSHA: strings.Repeat("5", 64),
		SystemProbeID: "darkrestore-system-probe", EndpointSHA: endpoint.SHA(),
		InitialApplySHA: listenerSHA,
		Expected: mx.RuntimeCondition{
			ProcessReady: true, ActiveSequence: registered.Birth.Lease.Sequence,
			ActiveSHA: plan.ConfigSHA,
		},
		Usage: mx.MeteredCondition{
			EpochID: registered.Epoch.ID, EpochSHA: strings.Repeat("7", 64),
			AdmissionSHA: strings.Repeat("8", 64),
		},
		Issued: issued, Expires: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := signedServingHealthReceipt(t, agentKey, q, completed)
	clock := func() time.Time { return time.Unix(0, recorded) }
	if _, err = f.s.recordDarkRestoreServingHealthV1(ctx, "invalid-owner-token", plan.ClientID, agentKey, endpoint, q, receipt, clock); err == nil {
		t.Fatal("unauthorized receipt accepted")
	}
	for _, ddl := range darkRestoreServingHealthDDL {
		if _, err = f.s.db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.s.db.Exec("CREATE TEMP TRIGGER fail_serving_health BEFORE INSERT ON darkrestore_serving_health_v1 BEGIN SELECT RAISE(ABORT,'synthetic write failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.recordDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, endpoint, q, receipt, clock); err == nil {
		t.Fatal("failed write accepted")
	}
	var failedRows int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM darkrestore_serving_health_v1").Scan(&failedRows); err != nil || failedRows != 0 {
		t.Fatal("failed write left evidence", failedRows, err)
	}
	if _, err = f.s.db.Exec("DROP TRIGGER fail_serving_health"); err != nil {
		t.Fatal(err)
	}
	foreign := q
	foreign.NodeID = "foreign-health-node"
	foreign, err = mx.SignProtocolHealthRequest(agentKey, foreign)
	if err != nil {
		t.Fatal(err)
	}
	foreignReceipt := signedServingHealthReceipt(t, agentKey, foreign, completed)
	if _, err = f.s.recordDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, endpoint, foreign, foreignReceipt, clock); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("foreign node accepted", err)
	}
	evidence, err := f.s.recordDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, endpoint, q, receipt, clock)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ListenerApplyReceiptSHA != listenerSHA || evidence.NativeApplyRecorded || evidence.AuthorizesMutation {
		t.Fatal("authority invented", evidence)
	}
	replayed, err := f.s.recordDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, endpoint, q, receipt, clock)
	if err != nil || !reflect.DeepEqual(replayed, evidence) {
		t.Fatal("replay", err)
	}
	pins, err := servingHealthPins(evidence)
	if err != nil {
		t.Fatal(err)
	}
	review, err := f.s.ReviewDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, pins)
	if err != nil || !review.ServingHealthRecorded || review.NativeApplyRecorded || review.AuthorizesMutation {
		t.Fatal("review", review, err)
	}
	badPins := pins
	badPins.AdmissionEvidenceSHA = strings.Repeat("0", 64)
	if _, err = f.s.ReviewDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, badPins); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("foreign pin accepted", err)
	}
	badRequest := q
	badRequest.StageID = strings.Repeat("9", 32)
	badRequest, err = mx.SignProtocolHealthRequest(agentKey, badRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.recordDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, endpoint, badRequest, receipt, clock); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting replay accepted", err)
	}
	for _, query := range []string{
		"UPDATE darkrestore_serving_health_v1 SET evidence_json=evidence_json",
		"DELETE FROM darkrestore_serving_health_v1",
	} {
		if _, err = f.s.db.Exec(query); err == nil {
			t.Fatal("serving evidence mutable")
		}
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateDarkRestoreServingHealth(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
		t.Fatal("wallet changed")
	}
	afterClient, err := f.s.Client(ctx, plan.ClientID)
	if err != nil || afterClient.UUID != beforeClient.UUID || afterClient.ExpiresUnix != beforeClient.ExpiresUnix ||
		afterClient.DeploymentState != "pending_node_apply" {
		t.Fatal("client mutated", afterClient, err)
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("dark restore validation", err)
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
	review, err = reopened.ReviewDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, pins)
	if err != nil || !review.ServingHealthRecorded || review.AuthorizesMutation {
		t.Fatal("durable review", review, err)
	}
	replayed, err = reopened.RecordDarkRestoreServingHealthV1(ctx, f.owner.Token, plan.ClientID, agentKey, endpoint, q, receipt)
	if err != nil || !reflect.DeepEqual(replayed, evidence) {
		t.Fatal("durable replay", err)
	}
}
