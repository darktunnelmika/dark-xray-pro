//go:build !linux

package business

import (
	"context"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func (s *Store) CheckDarkRestoreAdmission(ctx context.Context, q usageledger.AdmissionQuery, p usageledger.Principal, r uc.Report) error {
	present, err := birthSchema(ctx, q)
	if err != nil {
		return err
	}
	if present {
		return usageledger.ErrAuthority
	}
	return nil
}
