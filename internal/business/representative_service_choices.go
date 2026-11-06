package business

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"slices"
)

type RepresentativeServiceChoice struct {
	InboundID         uint64 `json:"inbound_id"`
	Name              string `json:"name"`
	GrantRevision     uint64 `json:"grant_revision"`
	ExecutionRevision uint64 `json:"execution_revision"`
	PlanRevision      uint64 `json:"plan_revision"`
	InboundRevision   uint64 `json:"inbound_revision"`
	Assigned          bool   `json:"assigned"`
	Applied           bool   `json:"applied"`
	Ready             bool   `json:"ready"`
}
type RepresentativeServiceChoices struct {
	ClientID string                        `json:"client_id"`
	Services []RepresentativeServiceChoice `json:"services"`
}

func (s *Store) RepresentativeClientServices(ctx context.Context, key []byte, actor, client string) (RepresentativeServiceChoices, error) {
	out := RepresentativeServiceChoices{ClientID: client, Services: []RepresentativeServiceChoice{}}
	tx, e := executionTx(s, ctx, key)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	c, e := subscriptionActor(ctx, tx, actor, client, true, false)
	if e != nil {
		return out, e
	}
	var role, rep string
	if e = tx.QueryRowContext(ctx, `SELECT role,representative_id FROM auth_users WHERE id=?`, actor).Scan(&role, &rep); e != nil {
		return out, e
	}
	if role != "representative" || rep != c.RepresentativeID || c.Archived {
		return out, ErrUnauthenticated
	}
	rows, e := tx.QueryContext(ctx, `SELECT g.inbound_id,i.name,g.revision FROM representative_service_grants g JOIN native_inbounds i ON i.id=g.inbound_id WHERE g.representative_id=? AND g.enabled=1 AND NOT EXISTS(SELECT 1 FROM representative_service_grants newer WHERE newer.representative_id=g.representative_id AND newer.inbound_id=g.inbound_id AND newer.revision>g.revision) ORDER BY g.inbound_id LIMIT 257`, rep)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v RepresentativeServiceChoice
		if e = rows.Scan(&v.InboundID, &v.Name, &v.GrantRevision); e != nil {
			rows.Close()
			return out, e
		}
		out.Services = append(out.Services, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Services) > 256 {
		return RepresentativeServiceChoices{}, ErrUnavailable
	}
	for i := range out.Services {
		v := &out.Services[i]
		state, e := inboundExecutionState(ctx, tx, key, v.InboundID)
		if e != nil {
			return RepresentativeServiceChoices{}, e
		}
		v.ExecutionRevision = state.Revision
		v.PlanRevision = state.PlanRevision
		v.InboundRevision = state.InboundRevision
		v.Assigned = slices.Contains(state.ClientIDs, client)
		v.Applied = slices.Contains(state.EffectiveClientIDs, client)
		v.Ready = state.Enabled && !state.OwnerResumeRequired && !state.PendingChanges && state.DesiredState == "active" && state.ObservedState == "active" && state.AccountingMode == ix.AccountingPerClient
	}
	return out, tx.Commit()
}
