package business

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func tgExtensionSetup(t *testing.T) tgCommerceFixture {
	t.Helper()
	f := tgCommerceSetup(t, true)
	ctx := context.Background()
	for _, enable := range []func(context.Context) error{f.s.EnableTelegramSupportV1, f.s.EnableTelegramCampaignsV1, f.s.EnableTelegramRewardsV1} {
		if err := enable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func tgExtensionCustomer(t *testing.T, f tgCommerceFixture, user int64) TelegramActor {
	t.Helper()
	a := TelegramActor{BotID: f.store.BotID, UserID: user, ChatID: user}
	if _, err := f.s.TelegramStart(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func tgExtensionOtherStore(t *testing.T, f tgCommerceFixture, user int64) TelegramActor {
	t.Helper()
	_, err := f.s.ConfigureTelegramStore(context.Background(), f.owner.Token, TelegramStoreRegistration{EventID: "extension-other-store-0001", ID: "shop-b", BotID: 101, RepresentativeID: "owner", Name: "Other store", ManagerUserID: 1002, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a := TelegramActor{BotID: 101, UserID: user, ChatID: user}
	if _, err = f.s.TelegramStart(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func tgExtensionAdmin(t *testing.T, f tgCommerceFixture, user int64, permissions ...string) TelegramActor {
	t.Helper()
	a := tgExtensionCustomer(t, f, user)
	invite, err := f.s.InviteTelegramAdmin(context.Background(), f.manager, TelegramAdminInvitationRequest{EventID: fmt.Sprintf("extension-admin-invite-%d", user), UserID: user, Permissions: permissions})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptTelegramAdmin(context.Background(), a, invite.ID); err != nil {
		t.Fatal(err)
	}
	return a
}

func tgExtensionRevoke(t *testing.T, f tgCommerceFixture, a TelegramActor) {
	t.Helper()
	admins, err := f.s.ListTelegramAdmins(context.Background(), f.manager)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range admins {
		if member.UserID == a.UserID {
			_, err = f.s.UpdateTelegramAdmin(context.Background(), f.manager, TelegramAdminChange{EventID: fmt.Sprintf("extension-admin-revoke-%d", a.UserID), UserID: a.UserID, ExpectedRevision: member.Revision, Permissions: member.Permissions, Enabled: false})
			if err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("invited admin missing")
}

func TestTelegramExtensionsExplicitSchemaAndTamperDetection(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	for _, verify := range []func(context.Context) error{s.VerifyTelegramSupportV1, s.VerifyTelegramCampaignsV1, s.VerifyTelegramRewardsV1} {
		if err := verify(ctx); err != nil {
			t.Fatal("old native database rejected", err)
		}
	}
	for _, enable := range []func(context.Context) error{s.EnableTelegramSupportV1, s.EnableTelegramCampaignsV1, s.EnableTelegramRewardsV1} {
		if err := enable(ctx); !errors.Is(err, ErrUnavailable) {
			t.Fatal("extension enabled without core", err)
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'telegram_*'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("implicit schema", count, err)
	}
	f := tgExtensionSetup(t)
	for _, enable := range []func(context.Context) error{f.s.EnableTelegramSupportV1, f.s.EnableTelegramCampaignsV1, f.s.EnableTelegramRewardsV1} {
		if err := enable(ctx); err != nil {
			t.Fatal("enable replay", err)
		}
	}
	for _, verify := range []func(context.Context) error{f.s.VerifyTelegramSupportV1, f.s.VerifyTelegramCampaignsV1, f.s.VerifyTelegramRewardsV1} {
		if err := verify(ctx); err != nil {
			t.Fatal("complete schema", err)
		}
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER telegram_support_messages_no_update`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.VerifyTelegramSupportV1(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("mutable history accepted", err)
	}
	if err := f.s.EnableTelegramSupportV1(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("damaged history repaired silently", err)
	}
}

func TestTelegramSupportOwnershipServiceBindingAndDurableReplies(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	stranger := tgExtensionCustomer(t, f, 2002)
	otherBot := tgExtensionOtherStore(t, f, f.buyer.UserID)
	order := tgCommercePaidOrder(t, f, "support-service-0001", f.product.ID, "")
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := TelegramTicketCreateV1{EventID: "support-create-ticket-0001", ClientID: receipt.Client.ID, Subject: "Connection help", Text: "Please check my service", Attachment: &TelegramAttachmentV1{Kind: "photo", FileID: "AgAC_test_photo_id"}}
	if _, err = f.s.TelegramCreateTicketV1(ctx, stranger, req); err == nil {
		t.Fatal("other customer's service bound")
	}
	ticket, err := f.s.TelegramCreateTicketV1(ctx, f.buyer, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.TelegramCreateTicketV1(ctx, f.buyer, req)
	if err != nil || !replay.Replayed || replay.Ticket.ID != ticket.Ticket.ID || replay.Message.ID != ticket.Message.ID {
		t.Fatal("ticket duplicated", replay, err)
	}
	changed := req
	changed.Text = "Different request"
	if _, err = f.s.TelegramCreateTicketV1(ctx, f.buyer, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("event rebound", err)
	}
	for _, actor := range []TelegramActor{stranger, otherBot} {
		if _, err = f.s.TelegramTicketMessagesV1(ctx, actor, ticket.Ticket.ID, 20, 0); !errors.Is(err, ErrNotFound) {
			t.Fatal("ticket privacy", err)
		}
		if _, err = f.s.TelegramReplyTicketV1(ctx, actor, TelegramTicketReplyV1{EventID: "support-cross-reply-0001", TicketID: ticket.Ticket.ID, Text: "Forged"}); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross ticket reply", err)
		}
	}
	admin := tgExtensionAdmin(t, f, 3001, "support")
	replyReq := TelegramTicketReplyV1{EventID: "support-admin-reply-0001", TicketID: ticket.Ticket.ID, Text: "Please import the current subscription"}
	reply, err := f.s.TelegramReplyTicketV1(ctx, admin, replyReq)
	if err != nil || !reply.Message.FromSupport || reply.Ticket.Status != "waiting_customer" {
		t.Fatal("support response", reply, err)
	}
	if _, err = f.s.TelegramSetTicketStateV1(ctx, admin, TelegramTicketChangeV1{EventID: "support-bad-assignee-0001", TicketID: ticket.Ticket.ID, ExpectedRevision: reply.Ticket.Revision, Status: "open", AssignedTelegramUserID: stranger.UserID}); err == nil {
		t.Fatal("unprivileged assignee")
	}
	tgExtensionRevoke(t, f, admin)
	if _, err = f.s.TelegramReplyTicketV1(ctx, admin, replyReq); err == nil {
		t.Fatal("revoked admin can replay confidential reply")
	}
	if _, err = f.s.TelegramTicketMessagesV1(ctx, admin, ticket.Ticket.ID, 20, 0); err == nil {
		t.Fatal("revoked admin reads ticket")
	}
	closed, err := f.s.TelegramSetTicketStateV1(ctx, f.buyer, TelegramTicketChangeV1{EventID: "support-close-ticket-0001", TicketID: ticket.Ticket.ID, ExpectedRevision: reply.Ticket.Revision, Status: "closed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.TelegramReplyTicketV1(ctx, f.buyer, TelegramTicketReplyV1{EventID: "support-closed-reply-0001", TicketID: ticket.Ticket.ID, Text: "New message"}); !errors.Is(err, ErrConflict) {
		t.Fatal("closed ticket appended", err)
	}
	if _, err = f.s.TelegramSetTicketStateV1(ctx, f.buyer, TelegramTicketChangeV1{EventID: "support-stale-change-0001", TicketID: ticket.Ticket.ID, ExpectedRevision: 1, Status: "open"}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision", err)
	}
	if _, err = f.s.TelegramSetTicketStateV1(ctx, f.buyer, TelegramTicketChangeV1{EventID: "support-reopen-ticket-0001", TicketID: ticket.Ticket.ID, ExpectedRevision: closed.Ticket.Revision, Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`UPDATE telegram_support_messages SET body='changed'`); err == nil {
		t.Fatal("ticket history overwritten")
	}
	if _, err = f.s.db.Exec(`DELETE FROM telegram_support_messages`); err == nil {
		t.Fatal("ticket history erased")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	messages, err := s.TelegramTicketMessagesV1(ctx, f.buyer, ticket.Ticket.ID, 20, 0)
	if err != nil || len(messages) != 2 || messages[0].Text != req.Text || messages[1].Text != replyReq.Text {
		t.Fatal("persistent conversation", messages, err)
	}
	list, err := s.TelegramTicketsV1(ctx, stranger, TelegramTicketFilterV1{})
	if err != nil || len(list) != 0 {
		t.Fatal("customer sees other tickets", list, err)
	}
}

func TestTelegramSupportFailedAuditRollsBackMessageAndTicket(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	if _, err := f.s.db.Exec(`CREATE TRIGGER test_reject_support_audit BEFORE INSERT ON telegram_support_events BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.TelegramCreateTicketV1(ctx, f.buyer, TelegramTicketCreateV1{EventID: "support-audit-fail-0001", Subject: "Help", Text: "Message"})
	if err == nil {
		t.Fatal("failed audit ignored")
	}
	var tickets, messages int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_support_tickets`).Scan(&tickets); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_support_messages`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if tickets != 0 || messages != 0 {
		t.Fatal("partial support mutation", tickets, messages)
	}
}
