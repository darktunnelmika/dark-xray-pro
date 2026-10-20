//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type meteredTestAuthority struct {
	check    func(context.Context, managedxray.TransactionCommand, xrayconfig.Candidate) error
	register func(context.Context, managedxray.TransactionCommand, xrayconfig.Candidate, uc.ProcessToken) (uc.Epoch, error)
}

func (a meteredTestAuthority) Check(ctx context.Context, c managedxray.TransactionCommand, v xrayconfig.Candidate) error {
	return a.check(ctx, c, v)
}
func (a meteredTestAuthority) Register(ctx context.Context, c managedxray.TransactionCommand, v xrayconfig.Candidate, b uc.ProcessToken) (uc.Epoch, error) {
	return a.register(ctx, c, v, b)
}

func testMeteredTransactionOutcome(t *testing.T, ctx context.Context, s *Server, o managedxray.Options, cfg business.NodeConfig, receiver *httptest.Server, mode string, customers []business.Client, port uint16, origin *httptest.Server, finalReport *atomic.Pointer[uc.Report]) {
	t.Helper()
	cert := sha256.Sum256(receiver.Certificate().Raw)
	sender, err := uc.NewSender(uc.SenderOptions{Endpoint: receiver.URL, NodeID: cfg.NodeID, InstallationID: installID, Token: nodeToken, PinSHA256: hex.EncodeToString(cert[:]), Client: receiver.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	closed := func() {
		t.Helper()
		c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 100*time.Millisecond)
		if e == nil {
			c.Close()
			t.Fatal("customer listener remained open at transaction barrier")
		}
	}
	c1 := cfg.Candidate
	c2 := c1
	var doc map[string]any
	if json.Unmarshal(c1.Config, &doc) != nil {
		t.Fatal("fixture candidate")
	}
	// Keep the strict no-log admission grammar. A second REALITY short ID is
	// a distinct valid same-port revision; the existing client still uses aa11.
	doc["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["shortIds"] = []string{"aa11", "bb22"}
	c2.Config, _ = json.Marshal(doc)
	digest := sha256.Sum256(c2.Config)
	c2.SHA256 = hex.EncodeToString(digest[:])
	digest = sha256.Sum256([]byte(c1.ManifestSHA256 + c2.SHA256))
	c2.ManifestSHA256 = hex.EncodeToString(digest[:])
	if c1.SHA256 == c2.SHA256 {
		t.Fatal("replacement must have distinct bytes")
	}
	if _, _, _, e := xrayconfig.UsageBootstrap(c2); e != nil {
		t.Fatal("replacement fixture violates admission grammar", e)
	}
	allowed := map[string]bool{managedxray.TransactionPayloadSHA(c1): true, managedxray.TransactionPayloadSHA(c2): true}
	var births []uc.ProcessToken
	var epochs []uc.Epoch
	var registrations int
	var revoked, uncertain bool
	authority := meteredTestAuthority{}
	authority.check = func(ctx context.Context, c managedxray.TransactionCommand, v xrayconfig.Candidate) error {
		if !allowed[managedxray.TransactionPayloadSHA(v)] {
			return business.ErrConflict
		}
		if mode == "transaction-revoked-after-seal" && finalReport.Load() != nil && !revoked {
			revoked = true
			if _, e := s.business.SetClientState(ctx, business.SetClientState{EventID: "revoke-during-drain", ClientID: customers[0].ID, Action: "disable", Reason: "Disposable final settlement revocation test"}); e != nil {
				return e
			}
		}
		for _, customer := range customers {
			u, e := s.business.ClientUsage(ctx, customer.ID)
			if e != nil {
				return e
			}
			client, e := s.business.Client(ctx, customer.ID)
			if e != nil || !u.Eligible || client.Archived || client.DeploymentState == "disabled" {
				return business.ErrConflict
			}
		}
		// Introduce uncertainty after the last eligibility read, as a competing
		// HUB writer could. The exact final confirmation must still reject it.
		if mode == "transaction-unknown-tail" && c.Action == "apply" && len(epochs) == 1 && !uncertain {
			uncertain = true
			return s.business.UsageLedger().MarkEpochUncertain(ctx, usageledger.Principal{NodeID: cfg.NodeID, InstallationID: installID}, epochs[0].ID, "competing uncertainty after authority check")
		}
		return nil
	}
	authority.register = func(ctx context.Context, c managedxray.TransactionCommand, v xrayconfig.Candidate, birth uc.ProcessToken) (uc.Epoch, error) {
		registrations++
		closed()
		if len(epochs) > 0 {
			final := finalReport.Load()
			if final == nil || final.EpochID != epochs[len(epochs)-1].ID || !final.Quiesced {
				t.Fatal("new birth registered before exact old final report")
			}
			if e := uc.VerifyEpochStopped(epochs[len(epochs)-1]); e != nil {
				t.Fatal("old birth still alive at new registration", e)
			}
			if e := sender.ConfirmSettlement(ctx, epochs[len(epochs)-1], uc.Receipt{EpochID: final.EpochID, Sequence: final.Sequence, SHA: final.SHA}); e != nil {
				t.Fatal("new birth preceded HUB seal", e)
			}
		}
		if mode == "transaction-register-failed" && registrations == 2 {
			return uc.Epoch{}, business.ErrUnavailable
		}
		next := cfg
		next.Sequence, next.Candidate, next.ConfigSHA, next.ManifestSHA = c.RuntimeSequence, v, v.SHA256, v.ManifestSHA256
		if e := s.business.RecordNativeDelivery(ctx, next, c.Lease); e != nil {
			return uc.Epoch{}, e
		}
		epoch, e := s.business.RegisterNativeUsageEpoch(ctx, next, business.UsageBirth{EpochID: fmt.Sprintf("%032x", c.RuntimeSequence), KernelBootID: birth.BootID, PID: birth.PID, StartTicks: birth.StartTicks})
		if e == nil {
			births, epochs = append(births, birth), append(epochs, epoch)
		}
		return epoch, e
	}
	m := managedxray.MeteredTransactionOptions{Authority: authority, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 8 * time.Second, MaxPendingAge: 8 * time.Second}
	r, err := managedxray.OpenMeteredTransactions(o, m)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { r.Close() }()
	command := func(action string, serial, sequence uint64, stage string, candidate xrayconfig.Candidate) managedxray.TransactionCommand {
		t.Helper()
		status := r.Status()
		now := time.Now()
		c := managedxray.TransactionCommand{Version: 2, ID: fmt.Sprintf("%032x", serial), Serial: serial, Action: action, StageID: stage, RuntimeInstance: status.Instance,
			NodeID: o.NodeID, InstallationID: o.InstallationID, RuntimeSequence: sequence, BinarySHA: o.BinarySHA, PayloadSHA: managedxray.TransactionPayloadSHA(candidate),
			Expected: managedxray.Condition(status.Observed), Usage: status.Usage, Issued: now.UnixNano(), Expires: now.Add(time.Minute).UnixNano(), InterruptedCommandSHA: status.InterruptedCommandSHA}
		if action == "stage" {
			c.StageID = c.ID
		} else {
			c.InterruptedCommandSHA = ""
			c.Lease = liveUsageLease(t, o, sequence, candidate)
		}
		c, e := managedxray.SignTransactionCommand(o.LeaseKey, c)
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	execute := func(c managedxray.TransactionCommand, candidate xrayconfig.Candidate, outcome string) managedxray.TransactionReceipt {
		t.Helper()
		ack, e := r.ExecuteMetered(ctx, c, candidate)
		if e != nil || ack.Outcome != outcome || !managedxray.VerifyMeteredTransactionReceipt(o.LeaseKey, c, ack) || managedxray.VerifyTransactionReceipt(o.LeaseKey, c, ack) {
			t.Fatal("metered transaction", c.Action, ack.Outcome, e)
		}
		return ack
	}
	s1 := command("stage", 1, cfg.Sequence, "", c1)
	execute(s1, c1, "staged")
	closed()
	a1 := command("apply", 2, cfg.Sequence, s1.ID, c1)
	ack1 := execute(a1, xrayconfig.Candidate{}, "applied")
	if replay := execute(a1, xrayconfig.Candidate{}, "applied"); !reflect.DeepEqual(replay, ack1) || registrations != 1 {
		t.Fatal("lost ACK retry changed receipt or birth")
	}
	profiles, err := s.business.ListInbounds(ctx, bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	stream := func() {
		t.Helper()
		client := activeTCPClient(t, ctx, o.Binary, customers[0].UUID, profiles[0].PublicKey, port, origin)
		response, e := client.Get(origin.URL)
		if e != nil {
			t.Fatal("active stream", e)
		}
		t.Cleanup(func() { response.Body.Close() })
		if _, e = io.CopyN(io.Discard, response.Body, 64<<10); e != nil {
			t.Fatal(e)
		}
		go func() { _, _ = io.Copy(io.Discard, response.Body) }()
	}
	stream()
	if mode == "transaction-lost-ack-restart" {
		r.Close()
		r, err = managedxray.OpenMeteredTransactions(o, m)
		if err != nil {
			t.Fatal(err)
		}
		if replay := execute(a1, xrayconfig.Candidate{}, "applied"); !reflect.DeepEqual(replay, ack1) || registrations != 1 || r.Status().Observed.ProcessReady || r.Status().Observed.Usage.Settled {
			t.Fatal("historical receipt resumed a stopped epoch")
		}
		closed()
		return
	}
	s2 := command("stage", 3, cfg.Sequence+1, "", c2)
	execute(s2, c2, "staged")
	if p, e := uc.Process(births[0].PID); e != nil || p != births[0] || !r.Status().Observed.ProcessReady {
		t.Fatal("Stage interrupted active epoch", e)
	}
	a2 := command("apply", 4, cfg.Sequence+1, s2.ID, c2)
	stale := a2
	u := *stale.Usage
	u.EpochSHA = fmt.Sprintf("%064x", 99)
	stale.Usage = &u
	stale, _ = managedxray.SignTransactionCommand(o.LeaseKey, stale)
	if _, e := r.ExecuteMetered(ctx, stale, xrayconfig.Candidate{}); !errors.Is(e, managedxray.ErrTransaction) || registrations != 1 || !r.Status().Observed.ProcessReady {
		t.Fatal("stale epoch command disturbed active child", e)
	}
	ack2, err := r.ExecuteMetered(ctx, a2, xrayconfig.Candidate{})
	if mode != "transaction-apply-rollback" {
		if err == nil || ack2.Outcome != "failed_stopped" || !managedxray.VerifyMeteredTransactionReceipt(o.LeaseKey, a2, ack2) || r.Status().Observed.ProcessReady || len(epochs) != 1 {
			t.Fatal("failed transition admitted new epoch", mode, err, ack2.Outcome)
		}
		closed()
		if replay, e := r.ExecuteMetered(ctx, a2, xrayconfig.Candidate{}); e == nil || !reflect.DeepEqual(replay, ack2) {
			t.Fatal("failed receipt replay retried activation")
		}
		if mode == "transaction-lost-seal" {
			r.Close()
			r, err = managedxray.OpenMeteredTransactions(o, m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = r.ReplayMeteredUsage(ctx); err != nil || !r.Status().Observed.Usage.Settled {
				t.Fatal("exact seal recovery", err)
			}
			closed()
			fresh := command("stage", 5, cfg.Sequence+1, "", c2)
			execute(fresh, c2, "staged")
			execute(command("apply", 6, cfg.Sequence+1, fresh.ID, c2), xrayconfig.Candidate{}, "applied")
		}
		if mode == "transaction-unknown-tail" {
			a, e := s.business.ClientUsage(ctx, customers[0].ID)
			if e != nil || a.Holds != 1 || a.Eligible || r.Status().Observed.Usage.Settled {
				t.Fatal("unknown tail cleared", a, e)
			}
		}
		return
	}
	if err != nil || ack2.Outcome != "applied" || !managedxray.VerifyMeteredTransactionReceipt(o.LeaseKey, a2, ack2) || len(epochs) != 2 || births[0] == births[1] {
		t.Fatal("real replacement", err, ack2.Outcome)
	}
	final1 := finalReport.Load()
	if final1 == nil || ack2.Metered.PriorSettlement != (uc.Receipt{EpochID: final1.EpochID, Sequence: final1.Sequence, SHA: final1.SHA}) {
		t.Fatal("ACK missing exact prior settlement")
	}
	for _, u := range final1.Users {
		a, e := s.business.ClientUsage(ctx, u.ClientID)
		if e != nil || a.Total != u.Uplink+u.Downlink || a.Holds != 0 || (u.ClientID == customers[0].ID && u.Downlink < 64<<10) || (u.ClientID == customers[1].ID && a.Total != 0) {
			t.Fatal("replacement lost or invented final bytes", u, a, e)
		}
	}
	stream()
	rollback := command("rollback", 5, cfg.Sequence+2, s1.ID, c1)
	rolled := execute(rollback, xrayconfig.Candidate{}, "rolled_back")
	if len(epochs) != 3 || rolled.Metered.PriorSettlement.EpochID != epochs[1].ID || rolled.Metered.After.EpochID != epochs[2].ID || rolled.Observed.ActiveSHA != c1.SHA256 {
		t.Fatal("rollback reused old epoch or lacked exact settlement")
	}
	final2 := finalReport.Load()
	for i, u := range final2.Users {
		a, e := s.business.ClientUsage(ctx, u.ClientID)
		if e != nil || a.Total != u.Uplink+u.Downlink+final1.Users[i].Uplink+final1.Users[i].Downlink || a.Holds != 0 {
			t.Fatal("rollback double billed or lost final bytes", a, e)
		}
	}
	// Revocation between owner review and a later rollback must reject before
	// any drain or new registration, even with otherwise valid fresh authority.
	denied := command("rollback", 6, cfg.Sequence+3, s2.ID, c2)
	if _, e := s.business.SetClientState(ctx, business.SetClientState{EventID: "revoke-before-rollback", ClientID: customers[0].ID, Action: "disable", Reason: "Disposable rollback revocation test"}); e != nil {
		t.Fatal(e)
	}
	if _, e := r.ExecuteMetered(ctx, denied, xrayconfig.Candidate{}); e == nil || registrations != 3 || r.Status().Serial != 5 {
		t.Fatal("rollback restored revoked authority")
	}
	t.Log("METERED TRANSACTIONS: offline stage, live TCP drain, exact prior seal, new zero gate, monotonic rollback, real ledger totals and revoked rollback rejection")
}
