package main

import (
	"context"

	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

type usageRecoveryRuntime interface {
	UsageStatus() managedxray.UsageObservationStatus
	ReplayObservedUsage(context.Context, *uc.Sender) (int, error)
}

// Used in the actual Agent synchronization loop before requesting a new
// candidate. Even a successful historical replay is NOT a final usage receipt
// or permission to resume the previous listener; the HUB's hold remains.
func replayUsageBeforeSync(ctx context.Context, r usageRecoveryRuntime, sender *uc.Sender) (bool, error) {
	status := r.UsageStatus()
	if !status.Required || status.Running {
		return false, nil
	}
	_, err := r.ReplayObservedUsage(ctx, sender)
	return true, err
}
