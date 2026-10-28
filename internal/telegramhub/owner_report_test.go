package telegramhub

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

type reportReaderFunc func(context.Context, string) (business.OwnerRepLedgerReport, error)

func (f reportReaderFunc) OwnerRepresentativeLedgerReport(ctx context.Context, id string) (business.OwnerRepLedgerReport, error) {
	return f(ctx, id)
}

func ownerConfig() OwnerReportConfig {
	return OwnerReportConfig{OwnerUserID: 900001, OwnerChatID: 900001, BotUsername: "DarkLabHubBot"}
}

func ownerMessage() OwnerReportMessage {
	return OwnerReportMessage{UpdateID: 100, MessageID: 200, SenderID: 900001,
		ChatID: 900001, ChatType: "private", Text: "/rep_credit repA"}
}

func validReport() business.OwnerRepLedgerReport {
	return business.OwnerRepLedgerReport{
		RepresentativeID: "repA", DisplayName: "<b>Alice & Bob</b>", Enabled: true,
		FreeVolumeBytes: "1", FreeUnlimitedUnits: "80", SellerQuotaState: "enabled",
		SellerCount: 2, ReservedVolumeBytes: "9007199254740993", ReservedUnlimitedUnits: "20",
		BalanceSemantics: "authoritative_representative_free_credit_not_total_sales_or_live_traffic",
	}
}

func TestOwnerReportRejectsInvalidServerConfiguration(t *testing.T) {
	reader := reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		t.Fatal("invalid configuration read a ledger")
		return business.OwnerRepLedgerReport{}, nil
	})
	for _, config := range []OwnerReportConfig{
		{}, {OwnerUserID: -1, OwnerChatID: 2}, {OwnerUserID: 1, OwnerChatID: -2},
		{OwnerUserID: 1, OwnerChatID: 2, BotUsername: "@DarkLabHubBot"},
		{OwnerUserID: 1, OwnerChatID: 2, BotUsername: "bot\nother"},
		{OwnerUserID: 1, OwnerChatID: 2, BotUsername: strings.Repeat("b", 33)},
	} {
		if _, err := NewOwnerReportHandler(config, reader); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("invalid config accepted: %+v %v", config, err)
		}
	}
	if _, err := NewOwnerReportHandler(ownerConfig(), nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("nil reader accepted", err)
	}
	var handler *OwnerReportHandler
	if reply, err := handler.Prepare(context.Background(), ownerMessage()); !errors.Is(err, ErrInvalidConfig) || reply != (PreparedOwnerReply{}) {
		t.Fatal("nil handler produced a reply", reply, err)
	}
}

func TestOwnerReportAuthenticatesBeforeCommandOrLedgerAccess(t *testing.T) {
	h, err := NewOwnerReportHandler(ownerConfig(), reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		t.Fatal("unauthorized input reached the ledger")
		return business.OwnerRepLedgerReport{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*OwnerReportMessage){
		"foreign sender":       func(m *OwnerReportMessage) { m.SenderID++ },
		"foreign private chat": func(m *OwnerReportMessage) { m.ChatID++ },
		"group":                func(m *OwnerReportMessage) { m.ChatType = "group" },
		"supergroup":           func(m *OwnerReportMessage) { m.ChatType = "supergroup" },
		"channel":              func(m *OwnerReportMessage) { m.ChatType = "channel" },
		"missing chat type":    func(m *OwnerReportMessage) { m.ChatType = "" },
		"bot sender":           func(m *OwnerReportMessage) { m.SenderIsBot = true },
		"on behalf of chat":    func(m *OwnerReportMessage) { m.SenderChatID = 123 },
		"forward":              func(m *OwnerReportMessage) { m.Forwarded = true },
		"edit":                 func(m *OwnerReportMessage) { m.Edited = true },
		"via bot":              func(m *OwnerReportMessage) { m.ViaBot = true },
		"invalid update":       func(m *OwnerReportMessage) { m.UpdateID = -1 },
		"missing message":      func(m *OwnerReportMessage) { m.MessageID = 0 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := ownerMessage()
			m.Text = strings.Repeat("x", MaxOwnerReportCommandBytes+1)
			change(&m)
			reply, err := h.Prepare(context.Background(), m)
			if !errors.Is(err, ErrUnauthorized) || reply != (PreparedOwnerReply{}) {
				t.Fatalf("authorization did not fail first: %+v %v", reply, err)
			}
		})
	}
}

func TestOwnerReportParsesOnlyBoundedScopedCommands(t *testing.T) {
	calls := 0
	h, _ := NewOwnerReportHandler(ownerConfig(), reportReaderFunc(func(_ context.Context, id string) (business.OwnerRepLedgerReport, error) {
		calls++
		if id != "repA" {
			t.Fatalf("unexpected representative: %q", id)
		}
		return validReport(), nil
	}))
	for _, command := range []string{"/rep_credit repA", "/rep_credit@DarkLabHubBot repA", "  /rep_credit@darklabhubbot   repA  "} {
		m := ownerMessage()
		m.Text = command
		if _, err := h.Prepare(context.Background(), m); err != nil {
			t.Fatalf("valid command rejected: %q %v", command, err)
		}
	}
	if calls != 3 {
		t.Fatal("accepted command count", calls)
	}
	for _, command := range []string{
		"", "/rep_credit", "/rep_credit repA other", "/rep_credit owner", "/rep_credit ../repA",
		"/rep_credit@OtherBot repA", "/rep_credit@ repA", "/rep_credit@@DarkLabHubBot repA",
		"/REP_CREDIT repA", "rep_credit repA", "/rep_credit repA\n", "/rep_credit\trepA",
		"/rep_credit\u00a0repA", "/rep_credit repA\x00", "/rep_credit repA\u202e",
		"/rep_credit " + strings.Repeat("a", 65), strings.Repeat("x", MaxOwnerReportCommandBytes+1),
		string([]byte{0xff}),
	} {
		m := ownerMessage()
		m.Text = command
		reply, err := h.Prepare(context.Background(), m)
		if !errors.Is(err, ErrInvalidCommand) || reply != (PreparedOwnerReply{}) {
			t.Fatalf("invalid command accepted: %q %+v %v", command, reply, err)
		}
	}
	if calls != 3 {
		t.Fatal("invalid command reached ledger", calls)
	}
	config := ownerConfig()
	config.BotUsername = ""
	h, _ = NewOwnerReportHandler(config, reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		return validReport(), nil
	}))
	m := ownerMessage()
	if _, err := h.Prepare(context.Background(), m); err != nil {
		t.Fatal("unmentioned command rejected without username", err)
	}
	m.Text = "/rep_credit@DarkLabHubBot repA"
	if reply, err := h.Prepare(context.Background(), m); !errors.Is(err, ErrInvalidCommand) || reply != (PreparedOwnerReply{}) {
		t.Fatal("mention accepted without configured bot identity", reply, err)
	}
}

func TestOwnerReportPreservesExactMoneyEscapesHTMLAndBindsReply(t *testing.T) {
	h, _ := NewOwnerReportHandler(ownerConfig(), reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		return validReport(), nil
	}))
	reply, err := h.Prepare(context.Background(), ownerMessage())
	if err != nil {
		t.Fatal(err)
	}
	if reply.OwnerUserID != 900001 || reply.ChatID != 900001 || reply.UpdateID != 100 ||
		reply.ReplyToMessageID != 200 || reply.RepresentativeID != "repA" || reply.ParseMode != "HTML" {
		t.Fatal("reply escaped its authenticated scope", reply)
	}
	for _, want := range []string{"&lt;b&gt;Alice &amp; Bob&lt;/b&gt;", "≈ 0.00 GiB (1 بایت)", "9007199254740993 بایت", "80 واحد", "20 واحد"} {
		if !strings.Contains(reply.Text, want) {
			t.Fatalf("missing exact or escaped field %q: %s", want, reply.Text)
		}
	}
	if strings.Contains(reply.Text, "<b>Alice & Bob</b>") || len(reply.Text) > 4096 {
		t.Fatal("unsafe or oversized reply", reply.Text)
	}
}

func TestOwnerReportFailsClosedForUnavailableOrMismatchedReports(t *testing.T) {
	type failureCase struct {
		name   string
		report business.OwnerRepLedgerReport
		err    error
		want   error
	}
	cases := []failureCase{
		{"missing", business.OwnerRepLedgerReport{}, business.ErrNotFound, ErrReportNotFound},
		{"unavailable", validReport(), errors.New("private SQL path and token must not leak"), ErrReportUnavailable},
	}
	foreign := validReport()
	foreign.RepresentativeID = "anotherRep"
	cases = append(cases, failureCase{"foreign report", foreign, nil, ErrReportUnavailable})
	invalid := validReport()
	invalid.ReservedVolumeBytes = "-1"
	cases = append(cases, failureCase{"invalid amount", invalid, nil, ErrReportUnavailable})
	invalid = validReport()
	invalid.DisplayName = "injected\nstatus"
	cases = append(cases, failureCase{"invalid name", invalid, nil, ErrReportUnavailable})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, _ := NewOwnerReportHandler(ownerConfig(), reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
				return c.report, c.err
			}))
			reply, err := h.Prepare(context.Background(), ownerMessage())
			if err != c.want || reply != (PreparedOwnerReply{}) {
				t.Fatalf("failure produced content or leaked source error: %+v %v", reply, err)
			}
		})
	}
}

func TestOwnerReportHonorsCancellationBeforeAndAfterRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	h, _ := NewOwnerReportHandler(ownerConfig(), reportReaderFunc(func(got context.Context, _ string) (business.OwnerRepLedgerReport, error) {
		calls++
		if got != ctx {
			t.Fatal("reader did not receive caller context")
		}
		cancel()
		return validReport(), nil
	}))
	if reply, err := h.Prepare(ctx, ownerMessage()); !errors.Is(err, context.Canceled) || reply != (PreparedOwnerReply{}) || calls != 1 {
		t.Fatal("cancelled read produced a reply", reply, err, calls)
	}
	if reply, err := h.Prepare(ctx, ownerMessage()); !errors.Is(err, context.Canceled) || reply != (PreparedOwnerReply{}) || calls != 1 {
		t.Fatal("already cancelled command reached the ledger", reply, err, calls)
	}
}

func TestOwnerReportReplayPreparesFreshDataWithoutClaimingDelivery(t *testing.T) {
	calls := 0
	h, _ := NewOwnerReportHandler(ownerConfig(), reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		calls++
		r := validReport()
		if calls > 1 {
			r.FreeUnlimitedUnits = "79"
		}
		return r, nil
	}))
	first, err := h.Prepare(context.Background(), ownerMessage())
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Prepare(context.Background(), ownerMessage())
	if err != nil || calls != 2 || first.UpdateID != second.UpdateID || reflect.DeepEqual(first, second) ||
		!strings.Contains(second.Text, "79 واحد") {
		t.Fatal("duplicate input was mistaken for a cached sent receipt", first, second, err)
	}
}
