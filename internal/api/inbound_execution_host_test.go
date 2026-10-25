package api

import (
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
)

func TestManagedHostPreviewUsesFreshExactInstallationAndPreservesStop(t *testing.T) {
	s := &Server{inboundExecution: &inboundExecutionService{}}
	installation := "11111111111111111111111111111111"
	other := "22222222222222222222222222222222"
	host := &inboundexec.HostCapabilities{Version: 1, NetworkAdmin: true, ChildNetworkAdmin: true, TUNDevice: true, IPv6: true, SystemDNSControl: true, TUNDNSActivation: true}
	input := inboundexec.Preview{Valid: true, ReviewSHA: "review", Action: "activate", Targets: []inboundexec.TargetState{{NodeID: "usa-pro-01", InstallationID: installation, State: "queued", Listeners: []inboundexec.Listener{{InboundID: 10, HostRequirements: []string{inboundexec.HostTUN}}}}}}
	s.recordExecutionCapabilities("usa-pro-01", other, inboundruntime.Capabilities{Version: 1, Host: host})
	if got := s.executionHostPreview(input); got.Valid || got.ReviewSHA != "" || len(got.Blockers) != 1 || got.Blockers[0].NodeID != "usa-pro-01" || got.Blockers[0].InboundID != 10 {
		t.Fatal("another installation established execution authority", got)
	}
	s.recordExecutionCapabilities("usa-pro-01", installation, inboundruntime.Capabilities{Version: 1, Host: host})
	if got := s.executionHostPreview(input); !got.Valid || got.Targets[0].Host == nil || got.Targets[0].HostReportedAt <= 0 {
		t.Fatal("fresh scoped prerequisites did not reach owner review", got)
	}
	key := "usa-pro-01/" + installation
	report := s.inboundExecution.capabilities[key]
	report.ReceivedAt = time.Now().Add(-16 * time.Second)
	s.inboundExecution.capabilities[key] = report
	if got := s.executionHostPreview(input); got.Valid {
		t.Fatal("stale host report admitted a new start")
	}
	input.Action = "stop"
	if got := s.executionHostPreview(input); !got.Valid || got.ReviewSHA != "review" {
		t.Fatal("loss of host privilege prevented stop")
	}
	input.Action = "activate"
	input.Targets[0].Listeners[0].HostRequirements = nil
	if got := s.executionHostPreview(input); !got.Valid {
		t.Fatal("ordinary userspace listener gained unrelated requirements")
	}
}
