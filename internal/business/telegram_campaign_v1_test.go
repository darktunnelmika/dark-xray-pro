package business

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func tgCampaignConsent(t *testing.T, f tgCommerceFixture, a TelegramActor, tag string, enabled bool) {
	t.Helper()
	if _, err := f.s.TelegramSetMarketingConsentV1(context.Background(), a, "campaign-consent-"+tag, enabled); err != nil {
		t.Fatal(err)
	}
}

func tgCampaignDraft(t *testing.T, f tgCommerceFixture, a TelegramActor, tag string) TelegramCampaignV1 {
	t.Helper()
	r, err := f.s.TelegramSaveCampaignV1(context.Background(), a, TelegramCampaignSaveV1{EventID: "campaign-draft-" + tag, Title: "Service announcement", Content: TelegramCampaignContentV1{Text: "A new plan is available", Buttons: [][]TelegramCampaignButtonV1{{{Text: "Details", URL: "https://example.invalid/plans", Style: "primary"}}}}, Audience: "all"})
	if err != nil {
		t.Fatal(err)
	}
	return r.Campaign
}

func tgCampaignApprove(t *testing.T, f tgCommerceFixture, a TelegramActor, c TelegramCampaignV1, tag string) TelegramCampaignV1 {
	t.Helper()
	preview, err := f.s.TelegramPreviewCampaignV1(context.Background(), a, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.s.TelegramApproveCampaignV1(context.Background(), a, TelegramCampaignApprovalV1{EventID: "campaign-approve-" + tag, CampaignID: c.ID, ExpectedRevision: c.Revision, PreviewID: preview.ID, Digest: preview.Digest})
	if err != nil {
		t.Fatal(err)
	}
	return r.Campaign
}

func TestTelegramCampaignApprovalBindsContentConsentAudienceAndPermissions(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	drafter := tgExtensionAdmin(t, f, 3001, "broadcast_draft")
	second := tgExtensionCustomer(t, f, 2002)
	tgExtensionCustomer(t, f, 2003) // Joining the bot alone is not marketing consent.
	other := tgExtensionOtherStore(t, f, f.buyer.UserID)
	tgCampaignConsent(t, f, f.buyer, "first-enable-0001", true)
	tgCampaignConsent(t, f, second, "second-enable-0001", true)
	tgCampaignConsent(t, f, other, "other-store-enable-0001", true)
	c := tgCampaignDraft(t, f, drafter, "snapshot-0001")
	preview, err := f.s.TelegramPreviewCampaignV1(ctx, drafter, c.ID)
	if err != nil || preview.RecipientCount != 2 {
		t.Fatal("unconsented/cross-store audience", preview, err)
	}
	approval := TelegramCampaignApprovalV1{EventID: "campaign-approval-snapshot-0001", CampaignID: c.ID, ExpectedRevision: c.Revision, PreviewID: preview.ID, Digest: preview.Digest}
	if _, err = f.s.TelegramApproveCampaignV1(ctx, drafter, approval); err == nil {
		t.Fatal("draft permission can send")
	}
	tgCampaignConsent(t, f, second, "second-revoke-0001", false)
	if _, err = f.s.TelegramApproveCampaignV1(ctx, f.manager, approval); !errors.Is(err, ErrConflict) {
		t.Fatal("changed audience reused approval", err)
	}
	tgCampaignConsent(t, f, second, "second-enable-0002", true)
	preview, err = f.s.TelegramPreviewCampaignV1(ctx, f.manager, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := f.s.TelegramSaveCampaignV1(ctx, drafter, TelegramCampaignSaveV1{EventID: "campaign-edit-snapshot-0001", ID: c.ID, ExpectedRevision: c.Revision, Title: c.Title, Content: TelegramCampaignContentV1{Text: "Updated plan announcement"}, Audience: "all"})
	if err != nil {
		t.Fatal(err)
	}
	approval.PreviewID, approval.Digest = preview.ID, preview.Digest
	approval.ExpectedRevision = edit.Campaign.Revision
	if _, err = f.s.TelegramApproveCampaignV1(ctx, f.manager, approval); !errors.Is(err, ErrConflict) {
		t.Fatal("changed content reused approval", err)
	}
	preview, err = f.s.TelegramPreviewCampaignV1(ctx, f.manager, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	approval.PreviewID, approval.Digest = preview.ID, preview.Digest
	approved, err := f.s.TelegramApproveCampaignV1(ctx, f.manager, approval)
	if err != nil || approved.Campaign.RecipientCount != 2 {
		t.Fatal(approved, err)
	}
	replay, err := f.s.TelegramApproveCampaignV1(ctx, f.manager, approval)
	if err != nil || !replay.Replayed {
		t.Fatal("approval retry", replay, err)
	}
	tgCampaignConsent(t, f, second, "second-revoke-after-approval", false)
	clock := time.Now().UTC().Add(time.Second)
	delivery, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock)
	if err != nil || !ok || delivery.ChatID != f.buyer.UserID {
		t.Fatal("revoked recipient claimed", delivery, ok, err)
	}
	if _, _, err = f.s.TelegramClaimCampaignDeliveryV1(ctx, other.BotID, clock); err != nil {
		t.Fatal(err)
	}
	if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, TelegramCampaignDeliveryResultV1{DeliveryID: delivery.DeliveryID, AttemptID: delivery.AttemptID, Outcome: "sent", MessageID: 71}, clock); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock.Add(time.Second)); err != nil || ok {
		t.Fatal("duplicate or revoked send", ok, err)
	}
	report, err := f.s.TelegramCampaignReportV1(ctx, f.manager, c.ID)
	if err != nil || report.Sent != 1 || report.Skipped != 1 || report.Pending != 0 || report.Campaign.State != "completed" {
		t.Fatal("wrong durable accounting", report, err)
	}
	if _, err = f.s.TelegramCampaignReportV1(ctx, other, c.ID); err == nil {
		t.Fatal("cross-store report disclosed")
	}
}

func TestTelegramCampaignRechecksRevocationAndSupportsReviewedResume(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	sender := tgExtensionAdmin(t, f, 3002, "broadcast_send")
	tgCampaignConsent(t, f, f.buyer, "revocation-0001", true)
	c := tgCampaignDraft(t, f, f.manager, "revocation-0001")
	c = tgCampaignApprove(t, f, sender, c, "revocation-0001")
	tgExtensionRevoke(t, f, sender)
	clock := time.Now().UTC().Add(time.Second)
	if _, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock); err == nil || ok {
		t.Fatal("revoked approval sent", ok, err)
	}
	pause, err := f.s.TelegramControlCampaignV1(ctx, f.manager, TelegramCampaignControlV1{EventID: "campaign-pause-revocation-0001", CampaignID: c.ID, ExpectedRevision: c.Revision, Action: "pause"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock); err != nil || ok {
		t.Fatal("paused campaign sent", ok, err)
	}
	resume := TelegramCampaignControlV1{EventID: "campaign-resume-revocation-0001", CampaignID: c.ID, ExpectedRevision: pause.Campaign.Revision, Action: "resume", ApprovalDigest: "wrong"}
	if _, err = f.s.TelegramControlCampaignV1(ctx, f.manager, resume); !errors.Is(err, ErrConflict) {
		t.Fatal("unreviewed resume", err)
	}
	resume.ApprovalDigest = c.ApprovalDigest
	resumed, err := f.s.TelegramControlCampaignV1(ctx, f.manager, resume)
	if err != nil {
		t.Fatal(err)
	}
	d, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock)
	if err != nil || !ok {
		t.Fatal("current approver cannot resume", ok, err)
	}
	if _, err = f.s.TelegramControlCampaignV1(ctx, f.manager, TelegramCampaignControlV1{EventID: "campaign-cancel-inflight-0001", CampaignID: c.ID, ExpectedRevision: resumed.Campaign.Revision, Action: "cancel"}); err != nil {
		t.Fatal(err)
	}
	if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, TelegramCampaignDeliveryResultV1{DeliveryID: d.DeliveryID, AttemptID: d.AttemptID, Outcome: "rate_limited", ErrorCode: 429, RetryAfterSeconds: 2}, clock); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock.Add(3*time.Second)); err != nil || ok {
		t.Fatal("cancelled request retried", ok, err)
	}
	report, err := f.s.TelegramCampaignReportV1(ctx, f.manager, c.ID)
	if err != nil || report.Cancelled != 1 || report.Campaign.State != "cancelled" {
		t.Fatal(report, err)
	}
}

func TestTelegramCampaignSafeRateLimitRetryAndUnknownCrashRecovery(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	tgCampaignConsent(t, f, f.buyer, "retry-0001", true)
	c := tgCampaignApprove(t, f, f.manager, tgCampaignDraft(t, f, f.manager, "retry-0001"), "retry-0001")
	clock := time.Now().UTC().Add(time.Second)
	d, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, TelegramCampaignDeliveryResultV1{DeliveryID: d.DeliveryID, AttemptID: d.AttemptID, Outcome: "permanent_failure", ErrorCode: 500}, clock); !errors.Is(err, ErrInvalid) {
		t.Fatal("ambiguous server failure treated as definite", err)
	}
	rate := TelegramCampaignDeliveryResultV1{DeliveryID: d.DeliveryID, AttemptID: d.AttemptID, Outcome: "rate_limited", ErrorCode: 429, RetryAfterSeconds: 5}
	if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, rate, clock); err != nil {
		t.Fatal(err)
	}
	if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, rate, clock); err != nil {
		t.Fatal("duplicate definite outcome", err)
	}
	if _, ok, err = f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock.Add(4*time.Second)); err != nil || ok {
		t.Fatal("retry_after ignored", ok, err)
	}
	retry, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock.Add(5*time.Second))
	if err != nil || !ok || retry.DeliveryID != d.DeliveryID || retry.AttemptID == d.AttemptID {
		t.Fatal("safe attempt journal", retry, ok, err)
	}
	unknown := TelegramCampaignDeliveryResultV1{DeliveryID: retry.DeliveryID, AttemptID: retry.AttemptID, Outcome: "unknown", ErrorCode: 500}
	if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, unknown, clock.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock.Add(10*time.Second)); err != nil || ok {
		t.Fatal("unknown result automatically retried", ok, err)
	}
	report, err := f.s.TelegramCampaignReportV1(ctx, f.manager, c.ID)
	if err != nil || report.Unknown != 1 {
		t.Fatal(report, err)
	}
	crash := tgCampaignApprove(t, f, f.manager, tgCampaignDraft(t, f, f.manager, "crash-0001"), "crash-0001")
	inflight, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock.Add(11*time.Second))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, ok, err = reopened.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock.Add(132*time.Second)); err != nil || ok {
		t.Fatal("crash outcome silently retried", ok, err)
	}
	report, err = reopened.TelegramCampaignReportV1(ctx, f.manager, crash.ID)
	if err != nil || report.Unknown != 1 || report.Dispatching != 0 {
		t.Fatal("lost dispatch journal", report, err)
	}
	late := TelegramCampaignDeliveryResultV1{DeliveryID: inflight.DeliveryID, AttemptID: inflight.AttemptID, Outcome: "sent", MessageID: 999}
	if err = reopened.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, late, clock.Add(133*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatal("unknown overwritten by late unreviewed result", err)
	}
}

func TestTelegramCampaignApprovedContentTamperingFailsClosed(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	tgCampaignConsent(t, f, f.buyer, "tamper-0001", true)
	c := tgCampaignApprove(t, f, f.manager, tgCampaignDraft(t, f, f.manager, "tamper-0001"), "tamper-0001")
	if _, err := f.s.db.Exec(`UPDATE telegram_campaign_drafts SET content_json=? WHERE id=?`, []byte(`{"text":"Unreviewed replacement"}`), c.ID); err != nil {
		return // The immutable approved-content trigger rejected the mutation.
	}
	if _, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, time.Now().UTC().Add(time.Second)); !errors.Is(err, ErrUnavailable) || ok {
		t.Fatal("changed approved content sent", ok, err)
	}
}

func TestTelegramCampaignBoundedRetryAndDefiniteFailures(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	tgCampaignConsent(t, f, f.buyer, "bounded-retry-0001", true)
	c := tgCampaignApprove(t, f, f.manager, tgCampaignDraft(t, f, f.manager, "bounded-retry-0001"), "bounded-retry-0001")
	clock := time.Now().UTC().Add(time.Second)
	for attempt := 0; attempt < 8; attempt++ {
		d, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock)
		if err != nil || !ok {
			t.Fatal(fmt.Sprintf("attempt %d", attempt+1), ok, err)
		}
		if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, TelegramCampaignDeliveryResultV1{DeliveryID: d.DeliveryID, AttemptID: d.AttemptID, Outcome: "rate_limited", ErrorCode: 429, RetryAfterSeconds: 2}, clock); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(2 * time.Second)
	}
	if _, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, clock); err != nil || ok {
		t.Fatal("unbounded retry", ok, err)
	}
	r, err := f.s.TelegramCampaignReportV1(ctx, f.manager, c.ID)
	if err != nil || r.Failed != 1 || r.Campaign.State != "completed" {
		t.Fatal(r, err)
	}
}
