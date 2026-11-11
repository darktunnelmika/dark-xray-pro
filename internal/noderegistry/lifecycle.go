package noderegistry

import (
	"reflect"
	"time"
)

type LifecycleInput struct {
	EventID          string `json:"event_id"`
	InstallationID   string `json:"installation_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	Action           string `json:"action"`
}
type LifecycleState struct {
	Status       string `json:"status"`
	Revision     uint64 `json:"revision"`
	PendingEvent string `json:"pending_event,omitempty"`
}
type LifecycleReceipt struct {
	LifecycleInput
	NodeID     string         `json:"node_id"`
	ActorID    string         `json:"actor_id"`
	State      LifecycleState `json:"state"`
	Completed  bool           `json:"completed"`
	InboundIDs []uint64       `json:"inbound_ids"`
	At         time.Time      `json:"at"`
}

func (r *Registry) Lifecycle(id string) LifecycleState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return lifecycleState(r.state, id)
}
func lifecycleState(s diskState, id string) LifecycleState {
	if v, ok := s.Lifecycle[id]; ok {
		return v
	}
	return LifecycleState{Status: "active", Revision: 1}
}
func (r *Registry) DeploymentAllowed(id, install string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.state.Bindings[id]
	v := lifecycleState(r.state, id)
	return ok && b.Info.InstallationID == install && v.Status == "active" && v.PendingEvent == ""
}
func (r *Registry) BeginLifecycle(id, actor string, in LifecycleInput) (LifecycleReceipt, error) {
	if !idPattern.MatchString(id) || !validName(actor) || !hex32Pattern.MatchString(in.EventID) || !hex32Pattern.MatchString(in.InstallationID) || in.ExpectedRevision == 0 || (in.Action != "disable" && in.Action != "remove" && in.Action != "enable") {
		return LifecycleReceipt{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.state.Bindings[id]
	if !ok || b.Info.InstallationID != in.InstallationID {
		return LifecycleReceipt{}, ErrNodeNotFound
	}
	if old, ok := r.state.LifecycleEvents[in.EventID]; ok {
		if old.NodeID != id || old.ActorID != actor || !reflect.DeepEqual(old.LifecycleInput, in) {
			return LifecycleReceipt{}, ErrNameConflict
		}
		return old, nil
	}
	if len(r.state.LifecycleEvents) >= 10000 {
		return LifecycleReceipt{}, ErrTooMany
	}
	old := lifecycleState(r.state, id)
	if old.Revision != in.ExpectedRevision || old.PendingEvent != "" || old.Status == "removed" || (in.Action == "enable" && old.Status != "disabled") || (in.Action == "disable" && old.Status != "active") {
		return LifecycleReceipt{}, ErrNameConflict
	}
	status := "disabled"
	if in.Action == "remove" {
		status = "removed"
	}
	// Enable stays fenced until detach recovery has completed. It never restores
	// saved inbound membership and a removed installation cannot be enabled.
	next := clone(r.state)
	state := LifecycleState{Status: status, Revision: old.Revision + 1, PendingEvent: in.EventID}
	next.Lifecycle[id] = state
	receipt := LifecycleReceipt{LifecycleInput: in, NodeID: id, ActorID: actor, State: state, At: r.now().UTC(), InboundIDs: []uint64{}}
	next.LifecycleEvents[in.EventID] = receipt
	if err := r.saveLocked(next); err != nil {
		return LifecycleReceipt{}, err
	}
	return receipt, nil
}
func (r *Registry) FinishLifecycle(event string, inbounds []uint64) (LifecycleReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.state.LifecycleEvents[event]
	if !ok {
		return LifecycleReceipt{}, ErrInvalid
	}
	if old.Completed {
		return old, nil
	}
	state := lifecycleState(r.state, old.NodeID)
	if state.PendingEvent != event {
		return LifecycleReceipt{}, ErrNameConflict
	}
	next := clone(r.state)
	state.PendingEvent = ""
	if old.Action == "enable" {
		state.Status = "active"
	}
	next.Lifecycle[old.NodeID] = state
	old.State = state
	old.Completed = true
	old.InboundIDs = append([]uint64{}, inbounds...)
	next.LifecycleEvents[event] = old
	if err := r.saveLocked(next); err != nil {
		return LifecycleReceipt{}, err
	}
	return old, nil
}
func validateLifecycle(s diskState) error {
	if len(s.LifecycleEvents) > 10000 {
		return ErrUnsafeState
	}
	for id, v := range s.Lifecycle {
		if _, ok := s.Bindings[id]; !ok || v.Revision < 2 || (v.Status != "active" && v.Status != "disabled" && v.Status != "removed") {
			return ErrUnsafeState
		}
		if v.PendingEvent != "" {
			e, ok := s.LifecycleEvents[v.PendingEvent]
			if !ok || e.Completed || e.NodeID != id || e.State != v {
				return ErrUnsafeState
			}
		}
	}
	for event, e := range s.LifecycleEvents {
		b, ok := s.Bindings[e.NodeID]
		if !ok || event != e.EventID || !hex32Pattern.MatchString(event) || e.InstallationID != b.Info.InstallationID || !validName(e.ActorID) || e.At.IsZero() || e.State.Revision != e.ExpectedRevision+1 || (e.Action != "disable" && e.Action != "remove" && e.Action != "enable") {
			return ErrUnsafeState
		}
	}
	return nil
}

func (r *Registry) LifecycleDetails(id string) (LifecycleState, *LifecycleReceipt) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := lifecycleState(r.state, id)
	if state.PendingEvent == "" {
		return state, nil
	}
	p := r.state.LifecycleEvents[state.PendingEvent]
	return state, &p
}
