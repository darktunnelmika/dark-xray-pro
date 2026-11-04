//go:build !linux

package telegrambot

import "context"

// The disabled surface remains portable; credential handling and lifetime
// filesystem locks are supported only in the explicitly enabled Linux LAB.
type Worker struct{}

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
