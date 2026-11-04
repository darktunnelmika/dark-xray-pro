//go:build !linux

package business

import (
	"context"
	"database/sql"
)

func validateDarkRestoreListeners(ctx context.Context, tx *sql.Tx) error {
	present, err := listenerApplySchema(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return ErrUnavailable
	}
	return nil
}
