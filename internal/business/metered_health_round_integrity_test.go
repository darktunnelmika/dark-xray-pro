package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func TestMeteredHealthRoundAuditRejectsMalformedPersistedEvidence(t *testing.T) {
	ctx := context.Background()
	f := healthApplied(t, func(s *Store, _ []byte, _, _ mx.TransactionPoll) {
		if err := s.EnableMeteredHealthQueue(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.EnableMeteredHealthRounds(ctx); err != nil {
			t.Fatal(err)
		}
	})
	r := queuedHealthRound(t, f, 351)
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	q, err := f.s.healthCurrent(ctx, tx, f.key, f.in.ID, r.Members[0].InboundID, r.Created)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	q.ID, q.EndpointSHA, q.Issued, q.Expires = routeTestID(352), r.EndpointSHA, r.Created+int64(time.Millisecond), r.Expires
	q, err = mx.SignProtocolHealthRequest(routeTestToken, q)
	if err != nil {
		t.Fatal(err)
	}
	p := signedHealthProofAt(q, time.Unix(0, q.Issued+int64(time.Millisecond)))
	intentSHA := fingerprint("metered-health-intent-v1", struct {
		Op       string
		Inbound  uint64
		Endpoint string
	}{r.HandoverID, q.InboundID, r.EndpointSHA})
	type storedEvidence struct {
		request          mx.ProtocolHealthRequest
		receipt          mx.ProtocolHealthReceipt
		requestRaw       string
		receiptRaw       string
		intentSHA        string
		challengeExpires int64
		accepted         int64
		withReceipt      bool
	}
	base := storedEvidence{request: q, receipt: p, intentSHA: intentSHA, challengeExpires: q.Expires, accepted: p.Completed + 1, withReceipt: true}
	insert := func(t *testing.T, tx *sql.Tx, v storedEvidence) {
		t.Helper()
		requestRaw, e := json.Marshal(v.request)
		if e != nil {
			t.Fatal(e)
		}
		if v.requestRaw != "" {
			requestRaw = []byte(v.requestRaw)
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO pro_health_challenges(id,intent_id,handover_id,intent_sha,request,expires) VALUES(?,?,?,?,?,?)`, q.ID, r.Members[0].IntentID, r.HandoverID, v.intentSHA, string(requestRaw), v.challengeExpires); e != nil {
			t.Fatal("insert retained challenge", e)
		}
		if !v.withReceipt {
			return
		}
		receiptRaw, e := json.Marshal(v.receipt)
		if e != nil {
			t.Fatal(e)
		}
		if v.receiptRaw != "" {
			receiptRaw = []byte(v.receiptRaw)
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO pro_health_round_receipts(challenge_id,round_id,inbound_id,receipt,accepted) VALUES(?,?,?,?,?)`, q.ID, r.ID, q.InboundID, string(receiptRaw), v.accepted); e != nil {
			t.Fatal("insert retained receipt", e)
		}
	}
	// Exercise damaged retained JSON without removing immutable/schema triggers.
	// Relational scope alone cannot establish that those bodies describe the
	// challenge and member named by their rows.
	cases := []struct {
		name   string
		mutate func(*storedEvidence)
		valid  bool
	}{
		{"valid-pending-challenge", func(v *storedEvidence) { v.withReceipt = false }, true},
		{"valid-accepted-evidence", func(*storedEvidence) {}, true},
		{"malformed-pending-request", func(v *storedEvidence) { v.requestRaw, v.withReceipt = "{", false }, false},
		{"request-body-id", func(v *storedEvidence) { v.request.ID = routeTestID(353) }, false},
		{"request-body-handover", func(v *storedEvidence) { v.request.HandoverID = routeTestID(353) }, false},
		{"request-body-node", func(v *storedEvidence) { v.request.NodeID = "other-node" }, false},
		{"request-body-installation", func(v *storedEvidence) { v.request.InstallationID = routeTestID(353) }, false},
		{"request-body-inbound", func(v *storedEvidence) { v.request.InboundID++ }, false},
		{"request-body-port", func(v *storedEvidence) { v.request.Port++ }, false},
		{"request-body-endpoint", func(v *storedEvidence) { v.request.EndpointSHA = strings.Repeat("b", 64) }, false},
		{"request-body-runtime-binding", func(v *storedEvidence) { v.request.RuntimeInstance = routeTestID(353) }, false},
		{"request-issued-before-round", func(v *storedEvidence) { v.request.Issued = r.Created - 1 }, false},
		{"request-deadline-shortened", func(v *storedEvidence) { v.request.Expires-- }, false},
		{"challenge-deadline-differs", func(v *storedEvidence) { v.challengeExpires-- }, false},
		{"challenge-intent-fingerprint", func(v *storedEvidence) { v.intentSHA = strings.Repeat("b", 64) }, false},
		{"request-signature-shape", func(v *storedEvidence) { v.request.Signature = strings.Repeat("x", 64) }, false},
		{"malformed-receipt", func(v *storedEvidence) { v.receiptRaw = "{" }, false},
		{"receipt-version", func(v *storedEvidence) { v.receipt.Version++ }, false},
		{"receipt-request-hash", func(v *storedEvidence) { v.receipt.RequestSHA = strings.Repeat("b", 64) }, false},
		{"receipt-response-hash", func(v *storedEvidence) { v.receipt.ResponseSHA = strings.Repeat("b", 64) }, false},
		{"receipt-completed-before-issued", func(v *storedEvidence) { v.receipt.Completed = q.Issued - 1 }, false},
		{"receipt-completed-after-acceptance", func(v *storedEvidence) { v.receipt.Completed = v.accepted + 1 }, false},
		{"receipt-completed-at-deadline", func(v *storedEvidence) { v.receipt.Completed = q.Expires }, false},
		{"receipt-signature-shape", func(v *storedEvidence) { v.receipt.Signature = strings.Repeat("x", 64) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, e := f.s.db.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback()
			v := base
			tc.mutate(&v)
			insert(t, tx, v)
			row, e := readHealthRound(ctx, tx, r.ID)
			if !tc.valid {
				if !errors.Is(e, ErrUnavailable) {
					t.Fatal("damaged retained evidence was readable", e)
				}
				return
			}
			if e != nil || len(row.Members) != 1 || row.Members[0].ChallengeID != q.ID {
				t.Fatal("valid retained evidence rejected", e)
			}
			want := "pending"
			if v.withReceipt {
				want = "accepted"
				if row.Members[0].Accepted != v.accepted {
					t.Fatal("audit changed historical acceptance")
				}
			}
			if row.Members[0].State != want {
				t.Fatal("incorrect retained member state", row.Members[0].State)
			}
		})
	}

	// The public owner audit must propagate the integrity failure, including
	// after expiry, rather than displaying a successful historical member.
	tx, err = f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	bad := base
	bad.receiptRaw = "{}"
	insert(t, tx, bad)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.MeteredHandoverAudit(ctx, f.key, r.HandoverID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("public audit presented damaged evidence", err)
	}
}
