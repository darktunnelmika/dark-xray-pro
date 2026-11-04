//go:build !linux

package telegrambot

import "context"

// The disabled surface remains portable; credential handling and lifetime
// filesystem locks are supported only in the explicitly enabled Linux LAB.
type Worker struct{}
type Manager struct{}

func CheckLABConfig() (int, error) { return 0, ErrConfig }

func Open(options Options, _ Backend) (*Worker, error) {
	if options.EnableFlag == "" || options.EnableFlag == "0" {
		return nil, nil
	}
	return nil, ErrConfig
}
func (*Worker) Run(context.Context) error                         { return ErrState }
func (*Worker) Wait(context.Context) error                        { return ErrState }
func (*Worker) Close() error                                      { return nil }
func (*Worker) Status() []BotStatus                               { return nil }
func (*Worker) Statuses() []BotStatus                             { return nil }
func ValidateOfflineBackup(context.Context, string, []byte) error { return ErrConfig }

func OpenManager(options Options, _ Backend) (*Manager, error) {
	if options.EnableFlag == "" || options.EnableFlag == "0" {
		return nil, nil
	}
	return nil, ErrConfig
}
func (*Manager) Run(context.Context) error  { return ErrState }
func (*Manager) Wait(context.Context) error { return ErrState }
func (*Manager) Close() error               { return nil }
func (*Manager) Statuses() []BotStatus      { return nil }
func (*Manager) Management() ManagementStatus {
	return ManagementStatus{Connections: []ManagedConnection{}}
}
func (*Manager) Verify(context.Context, string) (BotIdentity, error) {
	return BotIdentity{}, ErrState
}
func (*Manager) Check(context.Context, int64) (BotIdentity, error) {
	return BotIdentity{}, ErrState
}
func (*Manager) Configure(context.Context, int64, string, int64) (ManagementStatus, error) {
	return ManagementStatus{}, ErrState
}
func (*Manager) ConfigureAuthorized(context.Context, int64, string, int64, func(context.Context) error) (ManagementStatus, error) {
	return ManagementStatus{}, ErrState
}
