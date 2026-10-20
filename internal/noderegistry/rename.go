package noderegistry

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrNameConflict = errors.New("node name revision or event conflict")
var ErrNodeNotFound = errors.New("paired node not found")

// RenameRequest is a compare-and-swap against one immutable installation.
// EventID must be retained across an ambiguous/lost-response retry.
type RenameRequest struct {
	EventID          string `json:"event_id"`
	InstallationID   string `json:"installation_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	Name             string `json:"name"`
}

type NameReceipt struct {
	RenameRequest
	NodeID       string    `json:"node_id"`
	ActorID      string    `json:"actor_id"`
	PreviousName string    `json:"previous_name"`
	Revision     uint64    `json:"revision"`
	At           time.Time `json:"at"`
}

type NameState struct {
	Node       NodeInfo      `json:"node"`
	Revision   uint64        `json:"revision"`
	AuditCount int           `json:"audit_count"`
	Recent     []NameReceipt `json:"recent"`
}

func validRename(x RenameRequest) bool {
	return hex32Pattern.MatchString(x.EventID) && hex32Pattern.MatchString(x.InstallationID) &&
		x.ExpectedRevision > 0 && x.ExpectedRevision <= 10000 && validName(x.Name) && x.Name == strings.TrimSpace(x.Name)
}

func nameHistory(s diskState, id string) []NameReceipt {
	rows := make([]NameReceipt, 0)
	for _, v := range s.Renames {
		if v.NodeID == id {
			rows = append(rows, v)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Revision < rows[j].Revision })
	return rows
}

func validateRenames(s diskState) error {
	if len(s.Renames) > 10000 {
		return ErrUnsafeState
	}
	groups := make(map[string][]NameReceipt)
	for key, x := range s.Renames {
		b, ok := s.Bindings[x.NodeID]
		if !ok || key != x.EventID || !validRename(x.RenameRequest) || !validName(x.ActorID) ||
			!validName(x.PreviousName) || x.PreviousName == x.Name || x.At.IsZero() ||
			x.InstallationID != b.Info.InstallationID || x.Revision != x.ExpectedRevision+1 {
			return ErrUnsafeState
		}
		groups[x.NodeID] = append(groups[x.NodeID], x)
	}
	for id, rows := range groups {
		sort.Slice(rows, func(i, j int) bool { return rows[i].Revision < rows[j].Revision })
		for i, x := range rows {
			if x.ExpectedRevision != uint64(i+1) || (i > 0 && x.PreviousName != rows[i-1].Name) {
				return ErrUnsafeState
			}
		}
		if rows[len(rows)-1].Name != s.Bindings[id].Info.Name {
			return ErrUnsafeState
		}
	}
	return nil
}

// NameState returns a bounded audit tail, never authentication secrets. Revision
// starts at one for legacy paired nodes; it does not reset on A -> B -> A.
func (r *Registry) NameState(id string) (NameState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.state.Bindings[id]
	if !ok {
		return NameState{}, ErrNodeNotFound
	}
	rows := nameHistory(r.state, id)
	count := len(rows)
	if count > 20 {
		rows = rows[count-20:]
	}
	return NameState{Node: b.Info, Revision: uint64(count + 1), AuditCount: count, Recent: rows}, nil
}

// Rename changes only display metadata. No token, identity, pairing timestamp,
// route, subscription, client or accounting record can be mutated here.
func (r *Registry) Rename(id, actor string, x RenameRequest) (NameReceipt, error) {
	if !idPattern.MatchString(id) || !validName(actor) || !validRename(x) {
		return NameReceipt{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.state.Renames[x.EventID]; ok {
		if prior.NodeID != id || prior.ActorID != actor || prior.RenameRequest != x {
			return NameReceipt{}, ErrNameConflict
		}
		// Re-establish durability in case the original response failed after
		// rename but before directory sync. This does not append another event.
		if err := r.saveLocked(clone(r.state)); err != nil {
			return NameReceipt{}, err
		}
		return prior, nil // historical receipt, not a claim about current name
	}
	b, ok := r.state.Bindings[id]
	if !ok {
		return NameReceipt{}, ErrNodeNotFound
	}
	if b.Info.InstallationID != x.InstallationID || x.ExpectedRevision != uint64(len(nameHistory(r.state, id))+1) {
		return NameReceipt{}, ErrNameConflict
	}
	if b.Info.Name == x.Name {
		return NameReceipt{}, ErrInvalid
	}
	if len(r.state.Renames) >= 10000 {
		return NameReceipt{}, ErrTooMany
	}
	receipt := NameReceipt{RenameRequest: x, NodeID: id, ActorID: actor, PreviousName: b.Info.Name, Revision: x.ExpectedRevision + 1, At: r.now().UTC()}
	if receipt.At.IsZero() {
		return NameReceipt{}, ErrInvalid
	}
	next := clone(r.state)
	b.Info.Name = x.Name
	next.Bindings[id] = b
	next.Renames[x.EventID] = receipt
	if err := r.saveLocked(next); err != nil {
		return NameReceipt{}, err
	}
	return receipt, nil
}
