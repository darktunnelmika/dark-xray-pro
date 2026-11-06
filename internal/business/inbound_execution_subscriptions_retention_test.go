package business

// Publication availability is independent of temporary node readiness.
// Every database, identity and counter below belongs to opened(t)'s t.TempDir.
import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func publicationStateDigest(t *testing.T, f *executionFixture) string {
	t.Helper()
	records := []any{}
	for _, table := range []string{
		"clients", "pro_usage_accounts", "pro_usage_eligibility",
		"inbound_execution_heads", "inbound_execution_nodes",
		"inbound_execution_epochs", "inbound_execution_usage_receipts",
		"inbound_execution_operations", "inbound_execution_subscription_heads",
		"inbound_execution_subscription_credentials",
	} {
		rows, err := f.s.db.Query("SELECT * FROM " + table + " ORDER BY 1")
		if err != nil {
			t.Fatal(table, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		values := [][]any{}
		for rows.Next() {
			row := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range row {
				ptrs[i] = &row[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			for i, value := range row {
				if raw, ok := value.([]byte); ok {
					row[i] = bytes.Clone(raw)
				}
			}
			values = append(values, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, struct {
			Table   string
			Columns []string
			Rows    [][]any
		}{table, columns, values})
	}
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return ix.BytesSHA(raw)
}

func TestManagedInboundSubscriptionRetainsAppliedRoutesAcrossStaleNode(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	loaded, err := f.s.NativeInboundPlan(ctx, f.key, 501)
	if err != nil {
		t.Fatal(err)
	}
	plan := copiedPlan(f.plan)
	plan.EventID = "audit-subscription-two-nodes-0001"
	plan.ExpectedPlanRevision = loaded.PlanRevision
	plan.ExpectedInboundRevision = loaded.InboundRevision
	plan.Profile = loaded.Profile.InboundInput
	plan.Nodes = append(plan.Nodes, NativeInboundPlanNode{
		NodeID: "remote-b", InstallationID: strings.Repeat("b", 32),
		ListenIP: "198.51.100.11", DirectPort: 21185,
		DirectHosts: []string{"b.example.org"}, TunnelHosts: []string{},
	})
	if _, err = f.s.SaveNativeInboundPlan(ctx, f.key, plan); err != nil {
		t.Fatal(err)
	}
	f.command(t, "audit-subscription-activate-0001", "activate")
	bundles := []ix.Bundle{}
	epochs := []ix.EpochInput{}
	for i, node := range plan.Nodes {
		bundle, ok, err := f.s.InboundExecutionBundle(ctx, f.key, node.NodeID, node.InstallationID)
		if err != nil || !ok {
			t.Fatal("bundle unavailable", i, err)
		}
		epoch := f.birth(t, bundle, "audit-subscription-epoch-"+node.NodeID)
		f.ready(t, bundle, epoch)
		bundles, epochs = append(bundles, bundle), append(epochs, epoch)
	}
	if _, err = f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID,
		SubscriptionTokenMutation{EventID: "audit-subscription-issue-0001", Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	secret, err := f.s.RevealInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri")
	if err != nil || len(initial.Routes) != 2 ||
		!strings.Contains(string(initial.Content), "a.example.org:21185") ||
		!strings.Contains(string(initial.Content), "b.example.org:21185") ||
		!strings.Contains(string(initial.Content), f.client.UUID) {
		t.Fatal("two applied routes were not initially published", len(initial.Routes), err)
	}
	if !initial.RuntimeReady || !initial.PathVerified || initial.ReadinessExpiresUnixNano <= time.Now().UnixNano() || initial.ExpiresUnixNano <= initial.ReadinessExpiresUnixNano {
		t.Fatal("document and readiness deadlines are not independent")
	}
	before := publicationStateDigest(t, f)
	a, b := bundles[0], bundles[1]
	var originalA, originalB []byte
	if err = f.s.db.QueryRow(`SELECT observation_enc FROM inbound_execution_nodes WHERE node_id=?`, a.NodeID).Scan(&originalA); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT observation_enc FROM inbound_execution_nodes WHERE node_id=?`, b.NodeID).Scan(&originalB); err != nil {
		t.Fatal(err)
	}
	stale := executionObservation(a, epochs[0])
	stale.ObservedUnixNano = time.Now().Add(-16 * time.Second).UnixNano()
	enc, err := sealPlanValue(f.key, executionAAD("observation", a.NodeID+"/"+a.InstallationID, a.Generation), stale)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`UPDATE inbound_execution_nodes SET observation_enc=? WHERE node_id=?`, enc, a.NodeID); err != nil {
		t.Fatal(err)
	}

	// B still satisfies the exact health/lease and accounting-freshness gates.
	var healthy ix.Observation
	if err = openPlanValue(f.key, executionAAD("observation", b.NodeID+"/"+b.InstallationID, b.Generation), originalB, &healthy); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixNano()
	if healthy.Generation != b.Generation || healthy.ConfigSHA != b.ConfigSHA || healthy.ManifestSHA != b.ManifestSHA ||
		!healthy.ProcessReady || healthy.State != "active" || healthy.LeaseExpires <= now ||
		healthy.ObservedUnixNano > now || now-healthy.ObservedUnixNano > int64(15*time.Second) ||
		healthy.AccountingState != "accepted" || healthy.HealthState != "healthy" {
		t.Fatal("control node B is not healthy")
	}
	tx, err := executionTx(f.s, ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executionAccountingFreshUntil(ctx, tx, b.NodeID, b.InstallationID, b.Generation, epochs[1].EpochID, now)
	tx.Rollback()
	if err != nil {
		t.Fatal("control node B accounting is stale", err)
	}
	var up, down, baseline, holds, eligible int64
	if err = f.s.db.QueryRow(`SELECT uplink,downlink,baseline_complete,holds FROM pro_usage_accounts WHERE client_id=?`, f.client.ID).Scan(&up, &down, &baseline, &holds); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, f.client.ID).Scan(&eligible); err != nil {
		t.Fatal(err)
	}
	if up != 0 || down != 0 || baseline != 1 || holds != 0 || eligible != 1 {
		t.Fatal("authorization/accounting control changed")
	}

	staleBeforeRead := publicationStateDigest(t, f)
	retained, err := f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri")
	if err != nil || len(retained.Routes) != 2 || !bytes.Equal(retained.Content, initial.Content) || retained.SHA256 != initial.SHA256 || retained.ExpiresUnixNano != initial.ExpiresUnixNano || retained.RuntimeReady || retained.PathVerified || retained.ReadinessExpiresUnixNano != 0 {
		t.Fatal("stale node withdrew or misrepresented previously applied routes", len(retained.Routes), err)
	}
	view, err := f.s.InboundExecutionSubscription(ctx, f.key, "owner", f.client.ID)
	if err != nil || !view.Available || view.RouteCount != 2 || view.RuntimeReady || view.PathVerified || view.ReadinessExpiresUnixNano != 0 || view.ExpiresUnixNano != initial.ExpiresUnixNano {
		t.Fatal("credential availability was conflated with runtime readiness", err)
	}
	if publicationStateDigest(t, f) != staleBeforeRead {
		t.Fatal("subscription read mutated execution/accounting/identity state")
	}

	// Restore only the observation bytes; do not queue a command, rotate a token,
	// create an epoch, submit usage, or request any process restart.
	if _, err = f.s.db.Exec(`UPDATE inbound_execution_nodes SET observation_enc=? WHERE node_id=?`, originalA, a.NodeID); err != nil {
		t.Fatal(err)
	}
	restored, err := f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri")
	if err != nil || len(restored.Routes) != 2 || !bytes.Equal(restored.Content, initial.Content) || restored.SHA256 != initial.SHA256 || !restored.RuntimeReady || !restored.PathVerified {
		t.Fatal("fresh observation did not restore the same published identities", len(restored.Routes), err)
	}
	sameSecret, err := f.s.RevealInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if err != nil || sameSecret.Token != secret.Token {
		t.Fatal("subscription credential changed", err)
	}
	if publicationStateDigest(t, f) != before {
		t.Fatal("restored state differs in identity, generations, epochs, receipts or account")
	}
	t.Log("RETAINED: published_routes=2 stale_A_seconds=16 healthy_B=true eligible=1 holds=0; identical_document_routes=2 readiness=false; observation_recovery_only=true same_URI_UUID_token=true; execution_generations_epochs_receipts_and_account_unchanged=true")
}
