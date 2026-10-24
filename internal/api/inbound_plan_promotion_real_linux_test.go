//go:build linux

package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// The active-traffic acceptance uses the same owner APIs as the creation form.
// In particular, neither helper seeds targets, bindings, route drafts or epochs.
func saveMeteredTrafficCreationPlan(t *testing.T, s *Server, profile business.InboundInput, port uint16) business.NativeInboundPlanReceipt {
	t.Helper()
	input := business.NativeInboundPlanInput{
		EventID: "active-promotion-create-0001", Profile: profile,
		Nodes: []business.NativeInboundPlanNode{{
			NodeID: "route-qa", InstallationID: installID, ListenIP: "127.0.0.1",
			DirectPort: port, DirectHosts: []string{"127.0.0.1"},
		}},
		Reason: "Review saved loopback Direct plan before metered promotion",
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	base := "/api/v1/workspace/native-inbound-plans"
	previewResponse := browserRequest(t, s.Handler(), "POST", base+"/preview", input, cookie, csrf, "https://"+browserHost)
	var preview business.NativeInboundPlanPreview
	if previewResponse.Code != http.StatusOK || json.Unmarshal(previewResponse.Body.Bytes(), &preview) != nil ||
		!preview.Valid || !preview.TopologyValidated || !preview.TopologyValidationOnly ||
		preview.RuntimeChanged || preview.Plan.RuntimeApply || preview.Plan.Profile.Enabled || len(preview.Topology.Nodes) != 1 {
		t.Fatal("owner creation preview did not remain a paused single-node plan", previewResponse.Code)
	}
	created := browserRequest(t, s.Handler(), "POST", base, input, cookie, csrf, "https://"+browserHost)
	var saved business.NativeInboundPlanReceipt
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &saved) != nil ||
		saved.Replayed || saved.RuntimeChanged || saved.ClientsDeleted || saved.CreditChanged || saved.UsageReset || saved.SubscriptionPublished ||
		saved.Plan.PlanRevision != 1 || saved.Plan.InboundRevision != 1 || saved.Plan.Profile.Revision != 1 ||
		saved.Plan.Profile.Enabled || saved.Plan.Profile.BoundClients != 0 || saved.Plan.Profile.PublicKey == "" ||
		!saved.Plan.Editable || saved.Plan.RuntimeApply || !reflect.DeepEqual(saved.Plan.Profile.InboundInput, profile) {
		t.Fatal("owner creation save changed identity or materialized runtime state", created.Code)
	}
	targets, err := s.business.ListInboundTargets(context.Background())
	if err != nil || len(targets) != 0 {
		t.Fatal("saving the creation plan materialized targets before promotion", err)
	}
	return saved
}

func promoteMeteredTrafficCreationPlan(t *testing.T, s *Server, saved business.NativeInboundPlanReceipt, customers []business.Client, cookie *http.Cookie, csrf string) uint64 {
	t.Helper()
	ids := make([]string, 0, len(customers))
	for _, customer := range customers {
		ids = append(ids, customer.ID)
	}
	input := business.NativeInboundPlanPromotionInput{
		EventID: "active-promotion-commit-0001", ExpectedPlanRevision: saved.Plan.PlanRevision,
		ExpectedInboundRevision: saved.Plan.InboundRevision, ClientIDs: ids,
		Reason: "Promote the reviewed saved plan into the reserved metered channel",
	}
	expectedIDs := append([]string(nil), ids...)
	sort.Strings(expectedIDs)
	path := fmt.Sprintf("/api/v1/workspace/native-inbound-plans/%d/promotion", saved.Plan.Profile.ID)
	response := browserRequest(t, s.Handler(), "POST", path+"/preview", input, cookie, csrf, "https://"+browserHost)
	var preview business.NativeInboundPlanPromotionPreview
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &preview) != nil ||
		!preview.Eligible || len(preview.Blockers) != 0 || len(preview.ReviewSHA256) != 64 ||
		preview.Scope != business.NativeInboundPlanPromotionScope || preview.InboundID != saved.Plan.Profile.ID ||
		preview.PlanRevision != saved.Plan.PlanRevision || preview.InboundRevision != saved.Plan.InboundRevision ||
		preview.NodeID != "route-qa" || preview.InstallationID != installID ||
		preview.DirectPort != saved.Plan.Nodes[0].DirectPort || !reflect.DeepEqual(preview.DirectHosts, saved.Plan.Nodes[0].DirectHosts) ||
		!reflect.DeepEqual(preview.ClientIDs, expectedIDs) || preview.RuntimeChanged || preview.SubscriptionPublished {
		t.Fatal("owner promotion preview did not produce a concrete review digest", response.Code)
	}
	if digest, err := hex.DecodeString(preview.ReviewSHA256); err != nil || len(digest) != 32 {
		t.Fatal("invalid promotion review digest")
	}
	before, err := s.business.NativeInbound(context.Background(), s.inboundKey, saved.Plan.Profile.ID)
	if err != nil || before.Enabled || before.BoundClients != 0 || before.Revision != saved.Plan.InboundRevision {
		t.Fatal("promotion preview changed the paused inbound", err)
	}
	targets, err := s.business.ListInboundTargets(context.Background())
	if err != nil || len(targets) != 0 {
		t.Fatal("promotion preview created a target", err)
	}
	input.ReviewSHA256 = preview.ReviewSHA256
	confirmed := nativeInboundPlanPromotionWire{NativeInboundPlanPromotionInput: input, Confirmed: true}
	response = browserRequest(t, s.Handler(), "POST", path, confirmed, cookie, csrf, "https://"+browserHost)
	var committed business.NativeInboundPlanPromotionReceipt
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &committed) != nil ||
		committed.Replayed || !committed.Promoted || committed.RuntimeChanged || committed.SubscriptionPublished ||
		committed.EventID != input.EventID || committed.InboundID != saved.Plan.Profile.ID ||
		committed.PlanRevision != saved.Plan.PlanRevision || committed.SourceInboundRevision != saved.Plan.InboundRevision ||
		committed.InboundRevision != saved.Plan.InboundRevision+1 || committed.DraftRevision != 1 ||
		committed.NodeID != "route-qa" || committed.InstallationID != installID ||
		committed.ReviewSHA256 != preview.ReviewSHA256 || !reflect.DeepEqual(committed.ClientIDs, expectedIDs) {
		t.Fatal("confirmed owner promotion failed", response.Code)
	}
	// A lost owner reply must return the same immutable receipt without a
	// second materialization, even though the saved plan is now frozen.
	replayed := browserRequest(t, s.Handler(), "POST", path, confirmed, cookie, csrf, "https://"+browserHost)
	var replay business.NativeInboundPlanPromotionReceipt
	if replayed.Code != http.StatusOK || json.Unmarshal(replayed.Body.Bytes(), &replay) != nil || !replay.Replayed {
		t.Fatal("owner promotion could not replay its committed receipt", replayed.Code)
	}
	replay.Replayed = false
	if !reflect.DeepEqual(committed, replay) {
		t.Fatal("promotion retry changed the immutable receipt")
	}
	current, err := s.business.NativeInbound(context.Background(), s.inboundKey, saved.Plan.Profile.ID)
	if err != nil || !current.Enabled || current.Archived || current.Revision != committed.InboundRevision ||
		current.BoundClients != len(customers) || current.PublicKey != saved.Plan.Profile.PublicKey ||
		!reflect.DeepEqual(current.InboundInput, saved.Plan.Profile.InboundInput) {
		t.Fatal("promotion lost the saved profile identity or selected client bindings", err)
	}
	targets, err = s.business.ListInboundTargets(context.Background())
	if err != nil || len(targets) != 1 || targets[0].InboundID != current.ID || targets[0].NodeID != "route-qa" ||
		targets[0].InstallationID != installID || targets[0].ListenIP != "127.0.0.1" ||
		targets[0].DirectPort != saved.Plan.Nodes[0].DirectPort || targets[0].SwapPort != 0 {
		t.Fatal("promotion did not materialize exactly the saved Direct target", err)
	}
	return current.Revision
}
