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

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func TestDarkRestoreSystemProbeRoundIsDurableAndReadOnly(t *testing.T) {
	ctx := context.Background()
	f, plan, registered, listenerSigner, planSHA := syntheticAdmissionSignedProbeFixture(t)
	key := bytes.Repeat([]byte{0x61}, 32)
	nodeKey := bytes.Repeat([]byte{0x5a}, 64)
	beforeBalances := ownershipBalances(t, f.s)
	var err error
	if _, err = f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, plan.ClientID, planSHA, "system-probe-admit-0001"); err != nil {
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
	expectedInitial, err := initialAdmissionReport(registered.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	report, err := box.Observe(expectedInitial.Users, false)
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
	if _, err = f.s.RegisterDarkRestoreListenerApply(ctx, registered.Epoch.NodeID, registered.Epoch.InstallationID, planSHA, listener); err != nil {
		t.Fatal(err)
	}
	beforeClient, err := f.s.Client(ctx, plan.ClientID)
	if err != nil {
		t.Fatal(err)
	}

	endpoint := healthEndpoint()
	manual := mx.ProtocolHealthRequest{
		Version: mx.InitialApplyProtocolHealthVersion, ID: strings.Repeat("1", 32),
		NodeID: plan.NodeID, InstallationID: plan.InstallationID,
		RuntimeInstance: strings.Repeat("2", 32), StageID: strings.Repeat("3", 32),
		ApplyCommandID: strings.Repeat("4", 32), InboundID: plan.Prepared.InboundID,
		Port: 29283, CredentialSHA: plan.SystemProbeCredentialSHA,
		SystemProbeID: plan.SystemProbeID, EndpointSHA: endpoint.SHA(),
		InitialApplySHA: listener.Digest(),
	}
	if err = mx.ValidateProtocolHealthSupport(plan.candidate, manual, endpoint); err != nil {
		t.Fatalf("fixture renderer rejected: %v listeners=%+v probe=%s credential=%s", err, plan.candidate.Listeners, plan.SystemProbeID, plan.SystemProbeCredentialSHA)
	}
	input := DarkRestoreSystemProbeInputV1{EventID: "system-probe-intent-0001", PlanSHA: planSHA}
	if _, err = f.s.QueueDarkRestoreSystemProbeV1(ctx, key, f.rep.Token, plan.ClientID, input, endpoint); err == nil {
		t.Fatal("representative queued probe")
	}
	var n int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_system_probe_v1_*'").Scan(&n); err != nil || n != 0 {
		t.Fatal("denied request left schema", n, err)
	}
	queued, err := f.s.QueueDarkRestoreSystemProbeV1(ctx, key, f.owner.Token, plan.ClientID, input, endpoint)
	if err != nil || queued.State != "pending" || queued.ServingHealth || queued.AuthorizesMutation {
		t.Fatal("queue", queued, err)
	}
	if _, err = f.s.ProjectDarkRestoreSystemProbeV1(ctx, f.rep.Token, plan.ClientID, planSHA); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative projected owner probe", err)
	}
	projected, err := f.s.ProjectDarkRestoreSystemProbeV1(ctx, f.owner.Token, plan.ClientID, planSHA)
	if err != nil || projected.State != "pending" || projected.ServingHealth || projected.AuthorizesMutation ||
		projected.ChallengeID != queued.ChallengeID {
		t.Fatal("pending projection", projected, err)
	}
	replay, err := f.s.QueueDarkRestoreSystemProbeV1(ctx, key, f.owner.Token, plan.ClientID, input, endpoint)
	if err != nil || !reflect.DeepEqual(queued, replay) {
		t.Fatal("intent replay", replay, err)
	}
	conflict := input
	conflict.EventID = "system-probe-intent-0002"
	if _, err = f.s.QueueDarkRestoreSystemProbeV1(ctx, key, f.owner.Token, plan.ClientID, conflict, endpoint); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting intent", err)
	}

	epochSHA, err := mx.MeteredEpochSHA(registered.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	state := mx.DarkRestoreSystemProbeState{
		Expected: mx.RuntimeCondition{
			Highwater:     registered.Birth.Lease.Sequence,
			SavedSequence: registered.Birth.Lease.Sequence, SavedSHA: plan.ConfigSHA,
			ActiveSequence: registered.Birth.Lease.Sequence, ActiveSHA: plan.ConfigSHA,
			ProcessReady: true,
		},
		Usage: mx.MeteredCondition{
			EpochID: registered.Epoch.ID, EpochSHA: epochSHA,
			AdmissionSHA: plan.PreparedAdmissionSHA,
		},
		LeaseExpires: registered.Birth.Lease.Expires,
	}
	badState := state
	badState.Expected.ActiveSHA = strings.Repeat("f", 64)
	if _, _, err = f.s.PollDarkRestoreSystemProbeV1(ctx, key, nodeKey, plan.NodeID, plan.InstallationID, planSHA, badState, endpoint); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale runtime issued probe", err)
	}
	if _, err = f.s.db.Exec("CREATE TEMP TRIGGER fail_system_probe_request BEFORE INSERT ON darkrestore_system_probe_v1_request BEGIN SELECT RAISE(ABORT,'synthetic request failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.PollDarkRestoreSystemProbeV1(ctx, key, nodeKey, plan.NodeID, plan.InstallationID, planSHA, state, endpoint); err == nil {
		t.Fatal("failed request write accepted")
	}
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM darkrestore_system_probe_v1_request").Scan(&n); err != nil || n != 0 {
		t.Fatal("failed request left row", n, err)
	}
	if _, err = f.s.db.Exec("DROP TRIGGER fail_system_probe_request"); err != nil {
		t.Fatal(err)
	}
	request, found, err := f.s.PollDarkRestoreSystemProbeV1(ctx, key, nodeKey, plan.NodeID, plan.InstallationID, planSHA, state, endpoint)
	if err != nil || !found || len(request.Signature) != 64 || request.Issued <= listener.AppliedUnixNano ||
		request.Expires <= time.Now().UnixNano() {
		t.Fatal("issue", found, err)
	}
	requestReplay, found, err := f.s.PollDarkRestoreSystemProbeV1(ctx, key, nodeKey, plan.NodeID, plan.InstallationID, planSHA, state, endpoint)
	if err != nil || !found || !bytes.Equal(systemProbeJSON(request), systemProbeJSON(requestReplay)) {
		t.Fatal("request replay changed", found, err)
	}
	receipt := signedServingHealthReceipt(t, nodeKey, request, time.Now().UnixNano())
	if !validDarkRestoreSystemProbeReceiptAt(nodeKey, request, receipt, time.Unix(0, request.Expires+1)) {
		t.Fatal("saved receipt could not replay after challenge expiry")
	}
	if validDarkRestoreSystemProbeReceiptAt(nodeKey, request, receipt, time.Unix(0, receipt.Completed-1)) {
		t.Fatal("future receipt accepted")
	}
	wrong := receipt
	wrong.ResponseSHA = strings.Repeat("e", 64)
	if _, err = f.s.RegisterDarkRestoreSystemProbeV1(ctx, nodeKey, plan.NodeID, plan.InstallationID, planSHA, endpoint, wrong); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("foreign response accepted", err)
	}
	evidence, err := f.s.RegisterDarkRestoreSystemProbeV1(ctx, nodeKey, plan.NodeID, plan.InstallationID, planSHA, endpoint, receipt)
	if err != nil || evidence.Scope != DarkRestoreServingHealthEvidenceScopeV1 ||
		evidence.NativeApplyRecorded || evidence.AuthorizesMutation {
		t.Fatal("record", evidence, err)
	}
	recorded, err := f.s.QueueDarkRestoreSystemProbeV1(ctx, key, f.owner.Token, plan.ClientID, input, endpoint)
	if err != nil || recorded.State != "recorded" || !recorded.ServingHealth || recorded.AuthorizesMutation {
		t.Fatal("recorded status", recorded, err)
	}
	projected, err = f.s.ProjectDarkRestoreSystemProbeV1(ctx, f.owner.Token, plan.ClientID, planSHA)
	if err != nil || projected.State != "recorded" || !projected.ServingHealth || projected.AuthorizesMutation {
		t.Fatal("recorded projection", projected, err)
	}
	evidenceReplay, err := f.s.RegisterDarkRestoreSystemProbeV1(ctx, nodeKey, plan.NodeID, plan.InstallationID, planSHA, endpoint, receipt)
	if err != nil || !reflect.DeepEqual(evidence, evidenceReplay) {
		t.Fatal("receipt replay", err)
	}
	for _, table := range []string{"intent", "request"} {
		for _, query := range []string{"DELETE FROM darkrestore_system_probe_v1_" + table, "UPDATE darkrestore_system_probe_v1_" + table + " SET id=id"} {
			if _, err = f.s.db.Exec(query); err == nil {
				t.Fatal("mutable round", table)
			}
		}
	}
	if !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
		t.Fatal("wallet changed")
	}
	afterClient, err := f.s.Client(ctx, plan.ClientID)
	if err != nil || !reflect.DeepEqual(beforeClient, afterClient) || afterClient.DeploymentState != "pending_node_apply" {
		t.Fatal("probe mutated client", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("backup", err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal("reopen", err)
	}
	defer reopened.Close()
	evidenceReplay, err = reopened.RegisterDarkRestoreSystemProbeV1(ctx, nodeKey, plan.NodeID, plan.InstallationID, planSHA, endpoint, receipt)
	if err != nil || !reflect.DeepEqual(evidence, evidenceReplay) {
		t.Fatal("durable replay", err)
	}
	if _, err = reopened.db.Exec("DROP TRIGGER darkrestore_system_probe_v1_request_no_update"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(request)
	if _, err = reopened.db.Exec("UPDATE darkrestore_system_probe_v1_request SET request_json=?", raw); err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	if _, err = ValidateOfflineBackup(ctx, f.path); err == nil {
		t.Fatal("altered probe schema admitted")
	}
}
