//go:build linux

package business

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func TestDarkRestoreNativeClosedCycleEvidenceV1(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x61}, 32)
	f, plan, epoch, planSHA := syntheticAdmissionFixture(t)
	if _, err := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, plan.ClientID, planSHA, "closed-cycle-admit-0001"); err != nil {
		t.Fatal(err)
	}
	box, err := uc.Open(uc.Options{Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32), Epoch: epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	principal := usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}
	initial, err := box.Observe([]uc.Usage{{ClientID: plan.ClientID}}, false)
	if err != nil {
		t.Fatal(err)
	}
	initialReceipt, err := f.s.usage.AcceptChecked(ctx, principal, initial, f.s.CheckDarkRestoreAdmission)
	if err != nil {
		t.Fatal(err)
	}
	if err = box.Acknowledge(initialReceipt); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.owner.Token, plan.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("open epoch projected as a closed cycle", err)
	}
	final, err := box.Observe([]uc.Usage{{ClientID: plan.ClientID, Uplink: 5, Downlink: 7}}, true)
	if err != nil {
		t.Fatal(err)
	}
	finalReceipt, err := f.s.usage.AcceptChecked(ctx, principal, final, f.s.CheckDarkRestoreAdmission)
	if err != nil {
		t.Fatal(err)
	}
	if err = box.Acknowledge(finalReceipt); err != nil {
		t.Fatal(err)
	}
	evidence, err := f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.owner.Token, plan.ClientID)
	if err != nil ||
		evidence.FinalSequence != final.Sequence ||
		evidence.FinalReportSHA != final.SHA ||
		evidence.CycleUplink != 5 || evidence.CycleDownlink != 7 ||
		!evidence.FinalReceiptRecorded || !evidence.SealedCycleRecorded || !evidence.ObservedTrafficRecorded ||
		evidence.NativeApplyRecorded || evidence.ServingHealthRecorded || evidence.AuthorizesMutation {
		t.Fatal("conservative closed-cycle projection", evidence, err)
	}
	replay, err := f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.owner.Token, plan.ClientID)
	if err != nil || !reflect.DeepEqual(evidence, replay) {
		t.Fatal("read-only projection replay changed", err)
	}
	if _, err = f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.rep.Token, plan.ClientID); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative projected private closed-cycle evidence", err)
	}
	pins := DarkRestoreNativeClosedCyclePinsV1{
		ClientID:                 evidence.ClientID,
		AdmissionEvidenceSHA:     evidence.AdmissionEvidenceSHA,
		TransitionSHA:            evidence.TransitionSHA,
		PreparedContentSHA:       evidence.PreparedContentSHA,
		PreparedAuthoritySHA:     evidence.PreparedAuthoritySHA,
		PlanSHA:                  evidence.PlanSHA,
		EpochID:                  evidence.EpochID,
		FinalSequence:            evidence.FinalSequence,
		FinalReportSHA:           evidence.FinalReportSHA,
		FinalReceiptReceivedUnix: evidence.FinalReceiptReceivedUnix,
		CycleUplink:              evidence.CycleUplink,
		CycleDownlink:            evidence.CycleDownlink,
	}
	pins.DocumentSHA, err = cutoverSnapshotDigest("DARK-PRO-NATIVE-CLOSED-CYCLE-EVIDENCE-v1", evidence)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := ValidateDarkRestoreNativeClosedCycleEvidenceV1(evidence, pins)
	if err != nil || validation.EvidenceSHA != pins.DocumentSHA ||
		!validation.FinalReceiptRecorded || !validation.SealedCycleRecorded || !validation.ObservedTrafficRecorded ||
		validation.NativeApplyRecorded || validation.ServingHealthRecorded || validation.AuthorizesMutation {
		t.Fatal("independent closed-cycle validation", validation, err)
	}
	badPins := pins
	badPins.FinalReportSHA = strings.Repeat("0", 64)
	if _, err = ValidateDarkRestoreNativeClosedCycleEvidenceV1(evidence, badPins); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("foreign terminal receipt pin accepted", err)
	}
	badEvidence := evidence
	badEvidence.AuthorizesMutation = true
	badPins = pins
	badPins.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-CLOSED-CYCLE-EVIDENCE-v1", badEvidence)
	if _, err = ValidateDarkRestoreNativeClosedCycleEvidenceV1(badEvidence, badPins); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("closed-cycle history self-authorized mutation", err)
	}
	if _, err = f.s.db.Exec("UPDATE pro_usage_epochs SET state='open' WHERE epoch_id=?", epoch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.owner.Token, plan.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("unsealed tampering projected as closed cycle", err)
	}
}

func TestDarkRestoreClosedCycleSeparatesCustomerAndProbe(t *testing.T) {
	ctx := context.Background()
	f, plan, registered, _, planSHA := syntheticAdmissionSignedProbeFixture(t)
	if _, err := f.s.AuthorizeDarkRestoreAdmission(ctx, bytes.Repeat([]byte{0x61}, 32), f.owner.Token, plan.ClientID, planSHA, "closed-dual-round-0001"); err != nil {
		t.Fatal(err)
	}
	epoch := registered.Epoch
	initial, err := initialAdmissionReport(epoch)
	if err != nil {
		t.Fatal(err)
	}
	box, err := uc.Open(uc.Options{Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32), Epoch: epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	for _, final := range []bool{false, true} {
		users := append([]uc.Usage(nil), initial.Users...)
		if final {
			for i := range users {
				if users[i].ClientID == plan.ClientID {
					users[i].Uplink, users[i].Downlink = 5, 7
				} else {
					users[i].Uplink, users[i].Downlink = 101, 203
				}
			}
		}
		report, err := box.Observe(users, final)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := f.s.usage.AcceptChecked(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, report, f.s.CheckDarkRestoreAdmission)
		if err != nil {
			t.Fatal(err)
		}
		if err = box.Acknowledge(receipt); err != nil {
			t.Fatal(err)
		}
	}
	v, err := f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.owner.Token, plan.ClientID)
	if err != nil || !v.SealedCycleRecorded || v.CycleUplink != 5 || v.CycleDownlink != 7 || v.AuthorizesMutation {
		t.Fatal("customer settlement with separate nonbillable traffic", v, err)
	}
	if _, err = f.s.db.Exec("UPDATE pro_usage_epochs SET cursor_json=json_set(CAST(cursor_json AS TEXT),'$.Users[0].uplink_bytes','6') WHERE epoch_id=?", epoch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.owner.Token, plan.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("altered terminal counters accepted", err)
	}
}

func TestDarkRestoreNativeClosedCycleRequiresObservedTrafficV1(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x61}, 32)
	f, plan, epoch, planSHA := syntheticAdmissionFixture(t)
	if _, err := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, plan.ClientID, planSHA, "closed-cycle-zero-admit-0001"); err != nil {
		t.Fatal(err)
	}
	box, err := uc.Open(uc.Options{Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32), Epoch: epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	principal := usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}
	initial, err := box.Observe([]uc.Usage{{ClientID: plan.ClientID}}, false)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := f.s.usage.AcceptChecked(ctx, principal, initial, f.s.CheckDarkRestoreAdmission)
	if err != nil || box.Acknowledge(receipt) != nil {
		t.Fatal("initial zero", err)
	}
	final, err := box.Observe([]uc.Usage{{ClientID: plan.ClientID}}, true)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = f.s.usage.AcceptChecked(ctx, principal, final, f.s.CheckDarkRestoreAdmission)
	if err != nil || box.Acknowledge(receipt) != nil {
		t.Fatal("terminal zero", err)
	}
	if _, err = f.s.ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx, f.owner.Token, plan.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("zero-traffic cycle claimed observed execution", err)
	}
}
