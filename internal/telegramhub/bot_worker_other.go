//go:build !linux

package telegramhub

import "context"

// OwnerReportWorker is unavailable outside Linux. Keeping the disabled startup
// surface portable prevents an opt-in LAB feature from changing other builds.
type OwnerReportWorker struct{}

func OpenLABOwnerReportBot(options LABOwnerReportBotOptions, _ RepresentativeReporter) (*OwnerReportWorker, error) {
	if options.EnableFlag == "" || options.EnableFlag == "0" {
		return nil, nil
	}
	return nil, ErrBotUnsupported
}

func (*OwnerReportWorker) Run(context.Context) error  { return ErrBotUnsupported }
func (*OwnerReportWorker) Wait(context.Context) error { return ErrBotUnsupported }
func (*OwnerReportWorker) Close() error               { return nil }
