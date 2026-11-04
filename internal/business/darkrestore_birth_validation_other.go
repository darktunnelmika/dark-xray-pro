//go:build !linux

package business

import (
	"context"
	"database/sql"
)

func validateDarkRestoreBirths(ctx context.Context, tx *sql.Tx) error {
	present, err := birthSchema(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return ErrUnavailable
	}
	return nil
}
