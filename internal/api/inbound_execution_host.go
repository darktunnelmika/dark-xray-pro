package api

import (
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
)

type executionCapabilityReport struct {
	Capabilities inboundruntime.Capabilities
	ReceivedAt   time.Time
}

func (s *Server) recordExecutionCapabilities(node, installation string, c inboundruntime.Capabilities) {
	copy := c
	copy.Binaries = make(map[string]string, len(c.Binaries))
	for k, v := range c.Binaries {
		copy.Binaries[k] = v
	}
	if c.Host != nil {
		h := *c.Host
		copy.Host = &h
	}
	x := s.inboundExecution
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.capabilities == nil {
		x.capabilities = map[string]executionCapabilityReport{}
	}
	x.capabilities[node+"/"+installation] = executionCapabilityReport{Capabilities: copy, ReceivedAt: time.Now()}
}

// Host prerequisites are intentionally transient and installation scoped. A
// prior Agent process, another installation or a stale report cannot establish
// that this target is able to mutate its host network now.
func (s *Server) executionHostTargets(targets []inboundexec.TargetState) ([]inboundexec.TargetState, []inboundexec.Blocker) {
	out := append([]inboundexec.TargetState{}, targets...)
	all := []inboundexec.Blocker{}
	if s.inboundExecution == nil {
		return out, all
	}
	now := time.Now()
	x := s.inboundExecution
	x.mu.Lock()
	defer x.mu.Unlock()
	for i := range out {
		target := &out[i]
		target.Blockers = append([]inboundexec.Blocker{}, target.Blockers...)
		report, exists := x.capabilities[target.NodeID+"/"+target.InstallationID]
		fresh := exists && !report.ReceivedAt.After(now) && now.Sub(report.ReceivedAt) <= 15*time.Second
		if exists && report.Capabilities.Host != nil {
			h := *report.Capabilities.Host
			target.Host, target.HostReportedAt = &h, report.ReceivedAt.UnixNano()
		}
		seen := map[string]bool{}
		for _, listener := range target.Listeners {
			for _, blocker := range inboundexec.HostRequirementBlockers(listener.HostRequirements, report.Capabilities.Host, fresh) {
				blocker.NodeID, blocker.InboundID = target.NodeID, listener.InboundID
				key := inboundexec.Digest(blocker)
				if seen[key] {
					continue
				}
				seen[key] = true
				target.Blockers = append(target.Blockers, blocker)
				all = append(all, blocker)
			}
		}
		if len(seen) > 0 && target.State != "stopped" && target.State != "archived" {
			target.State = "blocked"
		}
	}
	return out, all
}

func (s *Server) executionHostState(state inboundexec.State) inboundexec.State {
	var blockers []inboundexec.Blocker
	state.Targets, blockers = s.executionHostTargets(state.Targets)
	state.Blockers = append(state.Blockers, blockers...)
	return state
}

func (s *Server) executionHostPreview(p inboundexec.Preview) inboundexec.Preview {
	var blockers []inboundexec.Blocker
	p.State = s.executionHostState(p.State)
	// Stop/archive must remain possible when the target loses a privilege or
	// device. The Agent's final receipt and cleanup still gate its successor.
	if p.Action == "stop" || p.Action == "archive" {
		return p
	}
	p.Targets, blockers = s.executionHostTargets(p.Targets)
	if len(blockers) > 0 {
		p.Blockers = append(p.Blockers, blockers...)
		p.Valid, p.ReviewSHA = false, ""
	}
	return p
}
