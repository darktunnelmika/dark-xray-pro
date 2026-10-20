//go:build linux

package usageledger

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestUsageSettlementRequiresExactSealedCursorAndPreservesUnknownTail(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "sealed", true: "uncertain"}[uncertain], func(t *testing.T) {
			ctx := context.Background()
			db, path := testDB(t)
			l, err := Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			account(t, db, l, "customer", true)
			// Pure ledger authority fixture; no local process evidence is needed.
			epoch := uc.Epoch{ID: strings.Repeat("b", 32), NodeID: "node-one", InstallationID: strings.Repeat("a", 32), ConfigSHA: strings.Repeat("c", 64), KernelBootID: "00000000-0000-0000-0000-000000000001", PID: 123, ProcessStartTicks: 456, Bindings: []uc.Binding{{ClientID: "customer", Email: "customer@pro.invalid", Billable: true}}}
			epoch.Bindings[0].Email, _ = uc.CounterEmail("customer")
			if err = l.RegisterEpoch(ctx, epoch); err != nil {
				t.Fatal(err)
			}
			box, err := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "box"), Key: bytes.Repeat([]byte{1}, 32), Epoch: epoch, Create: true})
			if err != nil {
				t.Fatal(err)
			}
			defer box.Close()
			r, err := box.Observe([]uc.Usage{{ClientID: "customer", Uplink: 17, Downlink: 23}}, false)
			if err != nil {
				t.Fatal(err)
			}
			ack, err := l.Accept(ctx, principal(epoch), r)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = l.ConfirmSettlement(ctx, principal(epoch), ack); !errors.Is(err, ErrConflict) {
				t.Fatal("ordinary receipt settled", err)
			}
			if uncertain {
				if err = l.MarkEpochUncertain(ctx, principal(epoch), epoch.ID, "test unknown tail"); err != nil {
					t.Fatal(err)
				}
			}
			final, err := box.Observe(r.Users, true)
			if err != nil {
				t.Fatal(err)
			}
			terminal, err := l.Accept(ctx, principal(epoch), final)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = l.ConfirmSettlement(ctx, principal(epoch), ack); !errors.Is(err, ErrConflict) {
				t.Fatal("stale receipt settled", err)
			}
			wrong := terminal
			wrong.SHA = strings.Repeat("f", 64)
			if _, err = l.ConfirmSettlement(ctx, principal(epoch), wrong); !errors.Is(err, ErrConflict) {
				t.Fatal("wrong hash settled", err)
			}
			foreign := principal(epoch)
			foreign.InstallationID = strings.Repeat("e", 32)
			if _, err = l.ConfirmSettlement(ctx, foreign, terminal); !errors.Is(err, ErrAuthority) {
				t.Fatal("foreign principal settled", err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			l, err = Open(ctx, connectDB(t, path))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				proof, e := l.ConfirmSettlement(ctx, principal(epoch), terminal)
				if uncertain {
					if !errors.Is(e, ErrConflict) {
						t.Fatal("quiesced receipt forgave unknown tail", e)
					}
				} else if e != nil || !proof.Sealed || proof.Receipt != terminal {
					t.Fatal("durable exact settlement missing", e)
				}
			}
			a, err := l.Account(ctx, "customer")
			if err != nil || a.Total != 40 || (a.Holds == 1) != uncertain {
				t.Fatal("confirmation changed accounting", a, err)
			}
		})
	}
}
