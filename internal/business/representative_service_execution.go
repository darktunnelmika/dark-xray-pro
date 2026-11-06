package business

import (
	"context"
	"database/sql"
	"errors"
	cs "github.com/darktunnelmika/dark-xray-pro/internal/clientservices"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// No full member list or infrastructure action can be supplied by a delegate.
type RepresentativeServiceChange struct {
	EventID           string `json:"event_id"`
	ClientID          string `json:"client_id"`
	InboundID         uint64 `json:"inbound_id"`
	GrantRevision     uint64 `json:"grant_revision"`
	ExecutionRevision uint64 `json:"execution_revision"`
	PlanRevision      uint64 `json:"plan_revision"`
	InboundRevision   uint64 `json:"inbound_revision"`
	Add               bool   `json:"add"`
	Reason            string `json:"reason"`
	ReviewSHA         string `json:"review_sha,omitempty"`
}

// Never expose owner execution state or other customers to representatives.
type RepresentativeServiceResult struct {
	ClientID    string `json:"client_id"`
	InboundID   uint64 `json:"inbound_id"`
	Valid       bool   `json:"valid"`
	ReviewSHA   string `json:"review_sha,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	State       string `json:"state,omitempty"`
	Replayed    bool   `json:"replayed"`
}

func representativeServiceAuthority(ctx context.Context, tx *sql.Tx, actor string, in RepresentativeServiceChange) (Client, RepresentativeServiceGrant, error) {
	c, e := subscriptionActor(ctx, tx, actor, in.ClientID, true, false)
	if e != nil {
		return c, RepresentativeServiceGrant{}, e
	}
	var role, rep string
	if e = tx.QueryRowContext(ctx, `SELECT role,representative_id FROM auth_users WHERE id=?`, actor).Scan(&role, &rep); e != nil {
		return c, RepresentativeServiceGrant{}, e
	}
	if role != "representative" || rep != c.RepresentativeID || c.Archived {
		return c, RepresentativeServiceGrant{}, ErrUnauthenticated
	}
	g, e := serviceGrantTx(ctx, tx, rep, in.InboundID)
	if e != nil {
		return c, g, e
	}
	if !g.Enabled || g.Revision <= 0 || uint64(g.Revision) != in.GrantRevision {
		return c, g, ErrConflict
	}
	return c, g, nil
}

// Preview and admission share authorization and use a single transaction.
func (s *Store) ChangeRepresentativeService(ctx context.Context, key []byte, actor string, in RepresentativeServiceChange, commit bool, hostGate func(ix.Preview) bool) (RepresentativeServiceResult, error) {
	out := RepresentativeServiceResult{ClientID: in.ClientID, InboundID: in.InboundID}
	tx, e := executionTx(s, ctx, key)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	c, g, e := representativeServiceAuthority(ctx, tx, actor, in)
	if e != nil {
		return out, e
	}
	fp := fingerprint("representative-service-command-v1", struct {
		Actor string
		Input RepresentativeServiceChange
	}{actor, in})
	if commit {
		if !routeSHA.MatchString(in.ReviewSHA) {
			return out, ErrInvalid
		}
		var prior string
		e = tx.QueryRowContext(ctx, `SELECT fingerprint FROM inbound_execution_events WHERE event_id=?`, in.EventID).Scan(&prior)
		if e == nil {
			if prior != fp {
				return out, ErrConflict
			}
			op, e := readExecutionOperation(ctx, tx, key, in.InboundID, in.EventID)
			if e != nil {
				return out, e
			}
			out.Valid = true
			out.OperationID = op.ID
			out.State = op.State
			out.Replayed = true
			return out, tx.Commit()
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if e = executionEventAvailable(ctx, tx, in.EventID); e != nil {
			return out, e
		}
	}
	state, e := inboundExecutionState(ctx, tx, key, in.InboundID)
	if e != nil {
		return out, e
	}
	ready := state.Enabled && !state.OwnerResumeRequired && !state.PendingChanges && state.DesiredState == "active" && state.ObservedState == "active" && state.AccountingMode == ix.AccountingPerClient
	members, e := cs.Membership(c.RepresentativeID, cs.Snapshot{Grant: cs.Grant{RepresentativeID: g.RepresentativeID, InboundID: g.InboundID, Revision: uint64(g.Revision), Enabled: g.Enabled}, RepresentativeEnabled: true, CanManageClients: true, ClientID: c.ID, ClientRepresentativeID: c.RepresentativeID, ClientArchived: c.Archived, InboundID: state.InboundID, ExecutionRevision: state.Revision, Ready: ready, Members: state.ClientIDs}, cs.Change{ClientID: in.ClientID, InboundID: in.InboundID, ExpectedGrantRevision: in.GrantRevision, ExpectedExecutionRevision: in.ExecutionRevision, Add: in.Add})
	if e != nil {
		return out, ErrConflict
	}
	command, e := normalizeExecutionCommand(ix.CommandInput{EventID: in.EventID, Action: "apply", ExpectedRevision: in.ExecutionRevision, ExpectedPlanRevision: in.PlanRevision, ExpectedInboundRevision: in.InboundRevision, ClientIDs: members, AccountingMode: ix.AccountingPerClient, Reason: in.Reason})
	if e != nil {
		return out, e
	}
	a, e := assessAuthorizedInboundExecution(ctx, tx, key, actor, in.InboundID, command)
	if e != nil {
		return out, e
	}
	// Apply the same fresh host capability gate as the owner API. A missing
	// gate can only admit ordinary userspace listeners, never privileged ones.
	if hostGate != nil {
		if !hostGate(a.preview) {
			return out, ErrConflict
		}
	} else {
		for _, target := range a.preview.Targets {
			for _, listener := range target.Listeners {
				if len(listener.HostRequirements) > 0 {
					return out, ErrUnavailable
				}
			}
		}
	}
	// Bind review to the grant generation and exact single-client intent too.
	review := in
	review.ReviewSHA = ""
	digest := fingerprint("representative-service-review-v1", struct {
		Actor           string
		Input           RepresentativeServiceChange
		ExecutionReview string
	}{actor, review, a.preview.ReviewSHA})
	out.Valid = a.preview.Valid
	if !commit {
		if out.Valid {
			out.ReviewSHA = digest
		}
		return out, tx.Commit()
	}
	if !out.Valid || in.ReviewSHA != digest {
		return out, ErrConflict
	}
	command.ReviewSHA = a.preview.ReviewSHA
	op, e := commitExecutionAssessment(ctx, tx, key, actor, in.InboundID, command, a, fp)
	if e != nil {
		return out, e
	}
	out.OperationID = op.ID
	out.State = op.State
	return out, tx.Commit()
}
