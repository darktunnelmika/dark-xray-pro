package api

import (
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// observedNodeChannels returns evidence, never an activation or delivery grant.
// The remaining observation lifetime lets browsers expire the same evidence
// without comparing their wall clocks with the HUB clock.
func observedNodeChannels(channel ix.TargetState, now time.Time) ownerNode {
	status := ownerNode{
		NodeID: channel.NodeID, CoreState: channel.HealthState, AccountingState: channel.AccountingState,
		ExecutionInstallation: channel.InstallationID,
	}
	if channel.ReportedAt <= 0 {
		return status
	}
	reported := time.Unix(0, channel.ReportedAt).UTC()
	status.ExecutionReportedAt = &reported
	const maxAge = 15 * time.Second
	remaining := maxAge - now.Sub(reported)
	if reported.After(now.Add(time.Second)) || remaining <= 0 {
		return status
	}
	if remaining > maxAge {
		remaining = maxAge
	}
	status.ExecutionFreshForMS = remaining.Milliseconds()
	status.ExecutionCurrent = status.ExecutionFreshForMS > 0
	return status
}
