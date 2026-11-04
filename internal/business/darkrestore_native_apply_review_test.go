package business

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDarkRestoreNativeApplyOwnerReviewCommitRecovery(t *testing.T) {
	ctx := context.Background()
	f, _, result, _, _ := preparedNativeApplyAcceptanceFixture(t)
	defer f.s.Close()
	in := DarkRestoreNativeApplyReviewInputV1{EventID: "owner-apply-review-0001", ClientID: f.evidence.ClientID, InboundID: 802, RoundID: result.round.ID}
	before, _ := f.s.Client(ctx, in.ClientID)
	balances := ownershipBalances(t, f.s)
	if _, e := f.s.PreviewDarkRestoreNativeApplyReviewV1(ctx, f.key, f.rep.Token, in); !errors.Is(e, ErrOwnershipDenied) {
		t.Fatal("representative preview", e)
	}
	if _, e := f.s.DarkRestoreNativeApplyAcceptanceReceipt(ctx, f.owner.Token, in.ClientID); !errors.Is(e, ErrNotFound) {
		t.Fatal("invented receipt", e)
	}
	p, e := f.s.PreviewDarkRestoreNativeApplyReviewV1(ctx, f.key, f.owner.Token, in)
	if e != nil || p.AuthorizesMutation || p.Evidence.ClientID != in.ClientID || !routeSHA.MatchString(p.ReviewSHA) {
		t.Fatal("preview", p, e)
	}
	if _, e = f.s.DarkRestoreNativeApplyAcceptanceReceipt(ctx, f.owner.Token, in.ClientID); !errors.Is(e, ErrNotFound) {
		t.Fatal("preview wrote acceptance", e)
	}
	if _, e = f.s.CommitDarkRestoreNativeApplyReviewV1(ctx, f.key, f.owner.Token, in, strings.Repeat("f", 64)); !errors.Is(e, ErrConflict) {
		t.Fatal("wrong review", e)
	}
	r, e := f.s.CommitDarkRestoreNativeApplyReviewV1(ctx, f.key, f.owner.Token, in, p.ReviewSHA)
	if e != nil || !r.NativeApplyRecorded || r.Replayed || r.SubscriptionDelivered || r.SubscriptionActivated || r.AuthorizesFurtherMutation || r.EvidenceSHA != p.EvidenceSHA {
		t.Fatal("commit", r, e)
	}
	replay, e := f.s.CommitDarkRestoreNativeApplyReviewV1(ctx, f.key, f.owner.Token, in, p.ReviewSHA)
	if e != nil || !replay.Replayed {
		t.Fatal("replay", e)
	}
	replay.Replayed = false
	if !reflect.DeepEqual(replay, r) {
		t.Fatal("changed replay")
	}
	got, e := f.s.DarkRestoreNativeApplyAcceptanceReceipt(ctx, f.owner.Token, in.ClientID)
	if e != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("readback", e)
	}
	if _, e = f.s.DarkRestoreNativeApplyAcceptanceReceipt(ctx, f.rep.Token, in.ClientID); !errors.Is(e, ErrOwnershipDenied) {
		t.Fatal("representative recovery", e)
	}
	after, _ := f.s.Client(ctx, in.ClientID)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(balances, ownershipBalances(t, f.s)) {
		t.Fatal("changed client or balance")
	}
}
func TestDarkRestoreNativeApplyOwnerReviewRejectsDrift(t *testing.T) {
	f, _, result, _, _ := preparedNativeApplyAcceptanceFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	in := DarkRestoreNativeApplyReviewInputV1{EventID: "owner-apply-drift-0001", ClientID: f.evidence.ClientID, InboundID: 802, RoundID: result.round.ID}
	p, e := f.s.PreviewDarkRestoreNativeApplyReviewV1(ctx, f.key, f.owner.Token, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.db.Exec("UPDATE clients SET archived=1 WHERE id=?", in.ClientID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.CommitDarkRestoreNativeApplyReviewV1(ctx, f.key, f.owner.Token, in, p.ReviewSHA); e == nil {
		t.Fatal("stale review recorded")
	}
	if _, e = f.s.DarkRestoreNativeApplyAcceptanceReceipt(ctx, f.owner.Token, in.ClientID); !errors.Is(e, ErrNotFound) {
		t.Fatal("stale review wrote receipt", e)
	}
}

func ApplyAcceptanceHTTPFixture(t *testing.T) (*Store, string, []byte, PreparedHandoffFiles, MeteredRouteNode, string, string) {
	var files PreparedHandoffFiles
	f := preparedPromotionWithFiles(t, &files)
	p, in := f.reviewed(t)
	if _, err := f.s.CommitDarkRestorePreparedPromotionV1(context.Background(), f.key, f.owner.Token, p.ReviewSHA256, in); err != nil {
		t.Fatal(err)
	}
	result := completePreparedNativeApply(t, &f)
	return f.s, f.evidence.ClientID, f.key, files, f.node, ownershipPassword, result.round.ID
}
