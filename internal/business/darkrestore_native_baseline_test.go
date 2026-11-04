package business

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func TestDarkRestoreNativePreparationFailsClosed(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "native-baseline-fixture-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableDarkRestoreCutoverV1(ctx, f.owner.Token); err != nil {
		t.Fatal(err)
	}
	sha, id := intent.Review.SourceSHA256, intent.Review.Clients[0].SourceID
	before, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, id)
	if err != nil {
		t.Fatal(err)
	}
	native, pins := businessSignedNativeBaseline(t, before, strings.Repeat("1", 64))
	value, _, _, _, err := native.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projectAttestedNativeBaselineV1(before, native, pins, time.Now().UnixNano())
	if err != nil || !projected.NativeBaselineComplete || projected.AuthorizesMutation || projected.StateSHA == before.StateSHA {
		t.Fatal("positive native projection failed", projected, err)
	}
	for _, test := range []struct {
		name   string
		change func(*DarkRestoreCutoverSnapshotV1)
	}{
		{"foreign UUID", func(v *DarkRestoreCutoverSnapshotV1) { v.ClientUUID = "different" }},
		{"foreign identity", func(v *DarkRestoreCutoverSnapshotV1) { v.ClientID = "different" }},
		{"foreign receipt", func(v *DarkRestoreCutoverSnapshotV1) { v.ImportReceiptSHA = strings.Repeat("e", 64) }},
		{"stale state", func(v *DarkRestoreCutoverSnapshotV1) { v.StateSHA = strings.Repeat("e", 64) }},
		{"stale native state", func(v *DarkRestoreCutoverSnapshotV1) { v.NativeBaselineSHA = strings.Repeat("e", 64) }},
		{"uncovered lifetime", func(v *DarkRestoreCutoverSnapshotV1) { v.ClientCreatedUnix-- }},
		{"unknown legacy unlimited", func(v *DarkRestoreCutoverSnapshotV1) { v.PlanKind = "unlimited" }},
		{"already complete", func(v *DarkRestoreCutoverSnapshotV1) { v.NativeBaselineComplete = true }},
		{"archived", func(v *DarkRestoreCutoverSnapshotV1) { v.Archived = true }},
		{"not quarantined", func(v *DarkRestoreCutoverSnapshotV1) { v.DeploymentState = "pending_node_apply" }},
		{"missing account", func(v *DarkRestoreCutoverSnapshotV1) { v.NativeAccountPresent = false }},
		{"hold", func(v *DarkRestoreCutoverSnapshotV1) { n := int64(1); v.NativeHolds = &n }},
		{"counter drift", func(v *DarkRestoreCutoverSnapshotV1) { n := int64(1); v.NativeUplink = &n }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := before
			test.change(&changed)
			if _, err := projectAttestedNativeBaselineV1(changed, native, pins, time.Now().UnixNano()); !errors.Is(err, ErrDarkRestoreCutover) {
				t.Fatal("unsafe projection accepted", err)
			}
		})
	}
	for _, now := range []int64{value.MeasuredThroughNano - 1, value.FenceUntilNano, value.FenceUntilNano + 1} {
		if _, err := projectAttestedNativeBaselineV1(before, native, pins, now); !errors.Is(err, ErrDarkRestoreCutover) {
			t.Fatal("future/expired measurement accepted", err)
		}
	}
	for _, test := range []struct {
		name   string
		change func(*darkrestore.NativeBaselineV1)
	}{
		{"lifetime gap", func(v *darkrestore.NativeBaselineV1) { v.CoverageFromNano++ }},
		{"unbounded fence", func(v *darkrestore.NativeBaselineV1) { v.FenceUntilNano = v.MeasuredThroughNano + int64(6*time.Minute) }},
		{"expired fence", func(v *darkrestore.NativeBaselineV1) { v.FenceUntilNano = v.MeasuredThroughNano + 1 }},
		{"future measurement", func(v *darkrestore.NativeBaselineV1) {
			v.MeasuredThroughNano += int64(time.Minute)
			v.FenceUntilNano = v.MeasuredThroughNano + int64(time.Minute)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			receipt, expected := businessSignedNativeBaseline(t, before, strings.Repeat("1", 64), test.change)
			if _, err := projectAttestedNativeBaselineV1(before, receipt, expected, time.Now().UnixNano()); !errors.Is(err, ErrDarkRestoreCutover) {
				t.Fatal("unsafe signed coverage interval accepted", err)
			}
		})
	}
	wrong := pins
	wrong.MeasurementSHA = strings.Repeat("e", 64)
	if _, err := projectAttestedNativeBaselineV1(before, native, wrong, time.Now().UnixNano()); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("untrusted pins accepted", err)
	}
	if _, err := projectAttestedNativeBaselineV1(before, darkrestore.VerifiedNativeBaselineV1{}, pins, time.Now().UnixNano()); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("unverified baseline accepted", err)
	}
	if _, err = f.s.PrepareDarkRestoreNativeBaselineV1(ctx, f.rep.Token, sha, id, native, pins); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative prepared owner-only proof", err)
	}
	still, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, id)
	if err != nil || still.NativeBaselineComplete || still.StateSHA != before.StateSHA {
		t.Fatal("negative cases changed destination", err)
	}
}
