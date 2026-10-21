//go:build linux

package usageledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func probeTestLedger(t *testing.T) (*sql.DB, string, *Ledger, ProbeIdentity, uc.Epoch) {
	t.Helper()
	ctx := context.Background()
	db, path := testDB(t)
	if _, err := db.Exec("PRAGMA user_version=8"); err != nil {
		t.Fatal(err)
	}
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	account(t, db, l, "customer", true)
	if err = l.EnableProbeAccounting(ctx); err != nil {
		t.Fatal(err)
	}
	p := ProbeIdentity{ID: "system-probe-one", UUID: "11111111-1111-4111-8111-111111111111", NodeID: "node-one", InstallationID: strings.Repeat("a", 32), InboundID: 7612, EventID: "probe-identity-fixture-0001"}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.CreateProbeIdentityTx(ctx, tx, p); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	e := uc.Epoch{ID: strings.Repeat("b", 32), NodeID: p.NodeID, InstallationID: p.InstallationID, KernelBootID: "11111111-1111-4111-8111-111111111111", PID: 12345, ProcessStartTicks: 77, ConfigSHA: strings.Repeat("c", 64)}
	for _, id := range []string{"customer", p.ID} {
		email, err := uc.CounterEmail(id)
		if err != nil {
			t.Fatal(err)
		}
		e.Bindings = append(e.Bindings, uc.Binding{ClientID: id, Email: email, Billable: id == "customer"})
	}
	e, _, err = uc.CanonicalEpoch(e)
	if err != nil {
		t.Fatal(err)
	}
	return db, path, l, p, e
}

func probeTestReport(t *testing.T, e uc.Epoch, sequence uint64, final bool, users ...uc.Usage) uc.Report {
	t.Helper()
	_, binding, err := uc.CanonicalEpoch(e)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ClientID < users[j].ClientID })
	r := uc.Report{Version: 1, EpochID: e.ID, BindingSHA: binding, Sequence: sequence, Quiesced: final, Users: users}
	raw, _ := json.Marshal(r)
	h := sha256.Sum256(raw)
	r.SHA = hex.EncodeToString(h[:])
	return r
}

func TestSystemProbeAccountingMixedEpochNeverDebitsCustomer(t *testing.T) {
	ctx := context.Background()
	db, _, l, p, e := probeTestLedger(t)
	if err := l.RegisterEpoch(ctx, e); err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err := db.QueryRow("SELECT value FROM native_config_generation").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	zero := probeTestReport(t, e, 1, false, uc.Usage{ClientID: "customer"}, uc.Usage{ClientID: p.ID})
	if _, err := l.Accept(ctx, principal(e), zero); err != nil {
		t.Fatal(err)
	}
	r := probeTestReport(t, e, 2, true, uc.Usage{ClientID: "customer", Uplink: 11, Downlink: 22}, uc.Usage{ClientID: p.ID, Uplink: 1 << 20, Downlink: 2 << 20})
	ack, err := l.Accept(ctx, principal(e), r)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []uc.Report{zero, r, r} {
		if _, err = l.Accept(ctx, principal(e), old); err != nil {
			t.Fatal("exact replay", err)
		}
	}
	actual, err := l.Account(ctx, "customer")
	if err != nil || actual.Total != 33 || !actual.Eligible {
		t.Fatal("system bytes reached customer quota", actual, err)
	}
	probe, err := l.ProbeUsage(ctx, p.ID)
	if err != nil || probe.Total != 3<<20 || probe.Uplink != 1<<20 || probe.Downlink != 2<<20 || probe.Holds != 0 {
		t.Fatal("probe bytes lost or duplicated", probe, err)
	}
	if _, err = l.Account(ctx, p.ID); !errors.Is(err, ErrMissing) {
		t.Fatal("system identity became financial account", err)
	}
	proof, err := l.ConfirmSettlement(ctx, principal(e), ack)
	if err != nil || !proof.Sealed {
		t.Fatal("mixed final not sealed", err)
	}
	var count, after int64
	if err = db.QueryRow("SELECT COUNT(*) FROM clients").Scan(&count); err != nil || count != 1 {
		t.Fatal("fake customer created", count, err)
	}
	if err = db.QueryRow("SELECT value FROM native_config_generation").Scan(&after); err != nil || after != generation {
		t.Fatal("probe traffic changed quota generation", generation, after, err)
	}
	for _, q := range []string{"DELETE FROM pro_probe_deltas", "UPDATE pro_probe_accounts SET uuid='changed'", "DELETE FROM pro_probe_accounts"} {
		if _, err = db.Exec(q); err == nil {
			t.Fatal("system audit/identity mutable")
		}
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), p.UUID) {
		t.Fatal("private system UUID serialized")
	}
}

func TestSystemProbeAccountingCommitLossRestartAndConcurrentReplay(t *testing.T) {
	for _, point := range []string{"after_totals", "before_commit", "after_commit"} {
		t.Run(point, func(t *testing.T) {
			ctx := context.Background()
			db, path, l, p, e := probeTestLedger(t)
			if err := l.RegisterEpoch(ctx, e); err != nil {
				t.Fatal(err)
			}
			r := probeTestReport(t, e, 1, true, uc.Usage{ClientID: "customer", Uplink: 7, Downlink: 9}, uc.Usage{ClientID: p.ID, Uplink: 101, Downlink: 202})
			l.checkpoint = func(at string) error {
				if at == point {
					return errors.New("injected commit loss")
				}
				return nil
			}
			if _, err := l.Accept(ctx, principal(e), r); err == nil {
				t.Fatal("unconfirmed commit acknowledged")
			}
			l.checkpoint = nil
			wantC, wantP := int64(0), int64(0)
			if point == "after_commit" {
				wantC, wantP = 16, 303
			}
			c, err := l.Account(ctx, "customer")
			if err != nil || c.Total != wantC {
				t.Fatal("partial customer commit", c, err)
			}
			a, err := l.ProbeUsage(ctx, p.ID)
			if err != nil || a.Total != wantP {
				t.Fatal("partial probe commit", a, err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			fresh := connectDB(t, path)
			one, err := Open(ctx, fresh)
			if err != nil {
				t.Fatal(err)
			}
			two, err := Open(ctx, connectDB(t, path))
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					ledger := one
					if i%2 == 1 {
						ledger = two
					}
					_, err := ledger.Accept(ctx, principal(e), r)
					errs <- err
				}(i)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			c, err = one.Account(ctx, "customer")
			if err != nil || c.Total != 16 {
				t.Fatal("customer replay duplicated", c, err)
			}
			a, err = one.ProbeUsage(ctx, p.ID)
			if err != nil || a.Total != 303 {
				t.Fatal("probe replay duplicated", a, err)
			}
		})
	}
}

func TestSystemProbeAccountingScopeRoleAndIdentityCollisions(t *testing.T) {
	ctx := context.Background()
	db, _, l, p, e := probeTestLedger(t)
	for _, mutate := range []func(*uc.Epoch){func(e *uc.Epoch) { e.NodeID = "other-node" }, func(e *uc.Epoch) { e.InstallationID = strings.Repeat("f", 32) }, func(e *uc.Epoch) {
		for i := range e.Bindings {
			if e.Bindings[i].ClientID == p.ID {
				e.Bindings[i].Billable = true
			}
		}
	}} {
		bad := e
		bad.Bindings = append([]uc.Binding(nil), e.Bindings...)
		mutate(&bad)
		if err := l.RegisterEpoch(ctx, bad); !errors.Is(err, ErrAuthority) {
			t.Fatal("system scope or role substituted", err)
		}
	}
	for _, change := range []func(*ProbeIdentity){func(p *ProbeIdentity) { p.ID = "customer" }, func(p *ProbeIdentity) { p.UUID = "uuid-customer" }, func(p *ProbeIdentity) { p.InboundID++ }} {
		bad := p
		change(&bad)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = l.CreateProbeIdentityTx(ctx, tx, bad)
		tx.Rollback()
		if err == nil {
			t.Fatal("identity collision accepted")
		}
	}
	for _, q := range []string{fmt.Sprintf("UPDATE clients SET uuid='%s' WHERE id='customer'", p.UUID), fmt.Sprintf("UPDATE clients SET id='%s' WHERE id='customer'", p.ID)} {
		if _, err := db.Exec(q); err == nil {
			t.Fatal("customer adopted system identity")
		}
	}
}

func TestSystemProbeAccountingUncertainTailAndOverflowStayAtomic(t *testing.T) {
	ctx := context.Background()
	db, _, l, p, e := probeTestLedger(t)
	if err := l.RegisterEpoch(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkEpochUncertain(ctx, principal(e), e.ID, "Unknown final customer and system bytes"); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkEpochUncertain(ctx, principal(e), e.ID, "Same uncertain event replay"); err != nil {
		t.Fatal(err)
	}
	r := probeTestReport(t, e, 1, true, uc.Usage{ClientID: "customer", Uplink: 5, Downlink: 6}, uc.Usage{ClientID: p.ID, Uplink: 10, Downlink: 20})
	ack, err := l.Accept(ctx, principal(e), r)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := l.ProbeUsage(ctx, p.ID)
	if err != nil || probe.Total != 30 || probe.Holds != 1 {
		t.Fatal("late sample forgave system tail", probe, err)
	}
	if _, err = l.ConfirmSettlement(ctx, principal(e), ack); err == nil {
		t.Fatal("uncertain system epoch sealed")
	}
	// Independent fixture: overflow in the system ledger must also roll back
	// the customer debit and receipt in this same mixed report transaction.
	db2, _, l2, p2, e2 := probeTestLedger(t)
	if err = l2.RegisterEpoch(ctx, e2); err != nil {
		t.Fatal(err)
	}
	if _, err = db2.Exec("UPDATE pro_probe_accounts SET uplink=? WHERE probe_id=?", int64(math.MaxInt64), p2.ID); err != nil {
		t.Fatal(err)
	}
	r2 := probeTestReport(t, e2, 1, false, uc.Usage{ClientID: "customer", Uplink: 5}, uc.Usage{ClientID: p2.ID, Uplink: 1})
	if _, err = l2.Accept(ctx, principal(e2), r2); !errors.Is(err, uc.ErrOverflow) {
		t.Fatal("system overflow accepted", err)
	}
	c, err := l2.Account(ctx, "customer")
	if err != nil || c.Total != 0 {
		t.Fatal("customer debit escaped failed system transaction", c, err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM pro_probe_holds").Scan(&n); err != nil || n != 1 {
		t.Fatal("system hold duplicated", n, err)
	}
}

func TestSystemProbeAccountingSchemaOptInAndDamageRefusal(t *testing.T) {
	ctx := context.Background()
	db, _ := testDB(t)
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.EnableProbeAccounting(ctx); !errors.Is(err, ErrSchema) {
		t.Fatal("v7 was implicitly upgraded", err)
	}
	if _, err = db.Exec("PRAGMA user_version=8"); err != nil {
		t.Fatal(err)
	}
	if err = l.EnableProbeAccounting(ctx); err != nil {
		t.Fatal(err)
	}
	if err = l.EnableProbeAccounting(ctx); err != nil {
		t.Fatal("exact opt-in replay", err)
	}
	if _, err = Open(ctx, db); err != nil {
		t.Fatal("valid v9 rejected", err)
	}
	if _, err = db.Exec("PRAGMA user_version=8"); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, db); !errors.Is(err, ErrSchema) {
		t.Fatal("probe schema silently read as v8", err)
	}
	if _, err = db.Exec("PRAGMA user_version=9"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DROP TRIGGER pro_probe_customer_insert"); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, db); !errors.Is(err, ErrSchema) {
		t.Fatal("damaged system schema accepted", err)
	}
}

func TestSystemProbeAccountingMissingCountersAndMigrationFailure(t *testing.T) {
	ctx := context.Background()
	db, _, l, p, e := probeTestLedger(t)
	if err := l.RegisterEpoch(ctx, e); err != nil {
		t.Fatal(err)
	}
	r := probeTestReport(t, e, 1, false, uc.Usage{ClientID: "customer", Uplink: 7})
	if _, err := l.Accept(ctx, principal(e), r); !errors.Is(err, uc.ErrIncomplete) {
		t.Fatal("missing system counter was invented as zero", err)
	}
	c, err := l.Account(ctx, "customer")
	if err != nil || c.Total != 0 {
		t.Fatal("partial counter set charged customer", c, err)
	}
	a, err := l.ProbeUsage(ctx, p.ID)
	if err != nil || a.Total != 0 {
		t.Fatal("partial counter set advanced system", a, err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM pro_usage_receipts").Scan(&n); err != nil || n != 0 {
		t.Fatal("incomplete receipt committed", n, err)
	}
	db2, _ := testDB(t)
	if _, err = db2.Exec("PRAGMA user_version=8"); err != nil {
		t.Fatal(err)
	}
	l2, err := Open(ctx, db2)
	if err != nil {
		t.Fatal(err)
	}
	l2.checkpoint = func(at string) error {
		if at == "before_commit" {
			return errors.New("migration storage failure")
		}
		return nil
	}
	if err = l2.EnableProbeAccounting(ctx); err == nil {
		t.Fatal("failed migration committed")
	}
	l2.checkpoint = nil
	if err = db2.QueryRow("PRAGMA user_version").Scan(&n); err != nil || n != 8 {
		t.Fatal("partial schema version", n, err)
	}
	if err = db2.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_probe_*'").Scan(&n); err != nil || n != 0 {
		t.Fatal("partial probe schema survived rollback", n, err)
	}
}
