//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Synthetic signed node claim tests transaction boundaries. Real process and
// network evidence are covered by TestRealImportedQuarantineAdmissionAndTraffic.
func syntheticAdmissionFixture(t *testing.T) (ownershipFixture, DarkRestoreQuarantinePlan, uc.Epoch, string) {
	t.Helper()
	ctx := context.Background()
	f, ev, key, _ := preparedCutoverFixture(t)
	input := NativeInboundPlanInput{EventID: "admission-negative-profile-0001", Profile: nativeInput(), Reason: "Synthetic admission guard", Nodes: []NativeInboundPlanNode{{NodeID: "negative-node", InstallationID: strings.Repeat("a", 32), ListenIP: "127.0.0.1", DirectPort: 29283, DirectHosts: []string{"127.0.0.1"}}}}
	input.Profile.ID = 504
	if _, err := f.s.SaveNativeInboundPlan(ctx, key, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, ev.ClientID, DarkRestorePreparedDeliveryInputV1{EventID: "admission-negative-link-0001", InboundID: 504, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"}); err != nil {
		t.Fatal(err)
	}
	p, err := f.s.DarkRestoreQuarantinePlan(ctx, key, f.owner.Token, ev.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	sha, _ := p.Digest()
	pub, signer, _ := ed25519.GenerateKey(nil)
	now := time.Now()
	lease, err := mx.SignLease(bytes.Repeat([]byte{3}, 64), mx.Lease{NodeID: p.NodeID, InstallationID: p.InstallationID, Sequence: 1, ConfigSHA: p.ConfigSHA, ManifestSHA: p.ManifestSHA, Issued: now.Add(-time.Second).UnixNano(), Expires: now.Add(30 * time.Second).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	birth := DarkRestoreQuarantineBirth{PlanSHA: sha, Lease: lease, Process: uc.ProcessToken{BootID: "11111111-1111-1111-1111-111111111111", PID: 1234, StartTicks: 4567}, ObservedUnixNano: now.UnixNano()}
	birth.Signature = ed25519.Sign(signer, birth.signingBytes())
	registered, err := f.s.RegisterDarkRestoreBirth(ctx, key, f.owner.Token, ev.ClientID, sha, pub, lease, birth)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, registered.Epoch, sha
}
func TestDarkRestoreAdmissionAuthorizationAndRollback(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x61}, 32)
	for _, mode := range []string{"success", "rollback", "changed quota", "representative denied"} {
		t.Run(mode, func(t *testing.T) {
			f, p, epoch, sha := syntheticAdmissionFixture(t)
			principal := usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}
			box, err := uc.Open(uc.Options{Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32), Epoch: epoch, Create: true})
			if err != nil {
				t.Fatal(err)
			}
			defer box.Close()
			report, err := box.Observe([]uc.Usage{{ClientID: p.ClientID}}, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.usage.AcceptChecked(ctx, principal, report, f.s.CheckDarkRestoreAdmission); err == nil {
				t.Fatal("unapproved zero received authority")
			}
			if mode == "representative denied" {
				if _, err = f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.rep.Token, p.ClientID, sha, "negative-admit-rep-0001"); err == nil {
					t.Fatal("representative authorized traffic")
				}
				return
			}
			if mode == "rollback" {
				if _, err = f.s.db.Exec("CREATE TEMP TRIGGER synthetic_admit_failure BEFORE UPDATE OF deployment_state ON clients BEGIN SELECT RAISE(ABORT,'synthetic admission failure'); END"); err != nil {
					t.Fatal(err)
				}
				if _, err = f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, p.ClientID, sha, "negative-admit-fail-0001"); err == nil {
					t.Fatal("failed state update admitted")
				}
				c, err := f.s.Client(ctx, p.ClientID)
				if err != nil || c.DeploymentState != "disabled" {
					t.Fatal("partial state", err)
				}
				var n int
				if err = f.s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_native_admission_v1*'").Scan(&n); err != nil || n != 0 {
					t.Fatal("orphan grant schema", err)
				}
				return
			}
			a, err := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, p.ClientID, sha, "negative-admit-owner-0001")
			if err != nil {
				t.Fatal(err)
			}
			replay, err := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, p.ClientID, sha, "negative-admit-owner-0001")
			if err != nil || a.Registered.Epoch.ID != replay.Registered.Epoch.ID {
				t.Fatal("lost-reply replay", err)
			}
			if mode == "changed quota" {
				if _, err = f.s.ExtendClientScoped(ctx, ExtendClient{EventID: "negative-admit-renew-0001", ClientID: p.ClientID, AdditionalVolumeBytes: 20, Reason: "Invalidate initial admission"}, f.repIDForTest(t, p.ClientID)); err != nil {
					t.Fatal(err)
				}
				if _, err = f.s.usage.AcceptChecked(ctx, principal, report, f.s.CheckDarkRestoreAdmission); err == nil {
					t.Fatal("stale zero report opened listener")
				}
				return
			}
			for i := 0; i < 2; i++ {
				if _, err = f.s.usage.AcceptChecked(ctx, principal, report, f.s.CheckDarkRestoreAdmission); err != nil {
					t.Fatal("valid initial zero/replay", err)
				}
			}
			var n int
			if err = f.s.db.QueryRow("SELECT COUNT(*) FROM pro_usage_receipts WHERE epoch_id=?", epoch.ID).Scan(&n); err != nil || n != 1 {
				t.Fatal("duplicate initial receipt", err, n)
			}
		})
	}
}
func (f ownershipFixture) repIDForTest(t *testing.T, id string) string {
	t.Helper()
	c, err := f.s.Client(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return c.RepresentativeID
}

func TestDarkRestoreNativeAdmissionEvidenceProjectionV1(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x61}, 32)
	f, plan, epoch, planSHA := syntheticAdmissionFixture(t)
	if _, err := f.s.ProjectDarkRestoreNativeAdmissionEvidenceV1(ctx, f.owner.Token, plan.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("birth alone projected admission evidence", err)
	}
	if _, err := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, plan.ClientID, planSHA, "evidence-admit-owner-0001"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ProjectDarkRestoreNativeAdmissionEvidenceV1(ctx, f.owner.Token, plan.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("owner grant without accepted zero projected evidence", err)
	}
	box, err := uc.Open(uc.Options{Directory: t.TempDir() + "/outbox", Key: bytes.Repeat([]byte{9}, 32), Epoch: epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	report, err := box.Observe([]uc.Usage{{ClientID: plan.ClientID}}, false)
	if err != nil {
		t.Fatal(err)
	}
	principal := usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}
	if _, err = f.s.usage.AcceptChecked(ctx, principal, report, f.s.CheckDarkRestoreAdmission); err != nil {
		t.Fatal(err)
	}
	evidence, err := f.s.ProjectDarkRestoreNativeAdmissionEvidenceV1(ctx, f.owner.Token, plan.ClientID)
	if err != nil || !evidence.InitialZeroReceiptRecorded || evidence.NativeApplyRecorded ||
		evidence.ServingHealthRecorded || evidence.AuthorizesMutation {
		t.Fatal("conservative evidence projection", evidence, err)
	}
	replay, err := f.s.ProjectDarkRestoreNativeAdmissionEvidenceV1(ctx, f.owner.Token, plan.ClientID)
	if err != nil || !reflect.DeepEqual(evidence, replay) {
		t.Fatal("projection replay changed", err)
	}
	if _, err = f.s.ProjectDarkRestoreNativeAdmissionEvidenceV1(ctx, f.rep.Token, plan.ClientID); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative projected owner evidence", err)
	}
	pins := DarkRestoreNativeAdmissionPinsV1{
		ClientID: evidence.ClientID, TransitionSHA: evidence.TransitionSHA,
		PreparedContentSHA: evidence.PreparedContentSHA, PreparedAuthoritySHA: evidence.PreparedAuthoritySHA,
		PlanSHA: evidence.PlanSHA, BirthReceiptSHA: evidence.BirthReceiptSHA,
		AdmissionEventID: evidence.AdmissionEventID, AdmissionReceiptSHA: evidence.AdmissionReceiptSHA,
		EpochID: evidence.EpochID, InitialReportSHA: evidence.InitialReportSHA,
	}
	pins.DocumentSHA, err = cutoverSnapshotDigest("DARK-PRO-NATIVE-ADMISSION-EVIDENCE-v1", evidence)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := ValidateDarkRestoreNativeAdmissionEvidenceV1(evidence, pins)
	if err != nil || !validation.InitialZeroReceiptRecorded || validation.NativeApplyRecorded ||
		validation.ServingHealthRecorded || validation.AuthorizesMutation ||
		validation.EvidenceSHA != pins.DocumentSHA {
		t.Fatal("independent validation", validation, err)
	}
	badPins := pins
	badPins.InitialReportSHA = strings.Repeat("0", 64)
	if _, err = ValidateDarkRestoreNativeAdmissionEvidenceV1(evidence, badPins); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("foreign report pin accepted", err)
	}
	badEvidence := evidence
	badEvidence.NativeApplyRecorded = true
	badPins = pins
	badPins.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-ADMISSION-EVIDENCE-v1", badEvidence)
	if _, err = ValidateDarkRestoreNativeAdmissionEvidenceV1(badEvidence, badPins); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("admission evidence promoted itself to native apply", err)
	}
}
