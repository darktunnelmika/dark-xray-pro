//go:build !linux

package business

import (
	"context"
	"database/sql"
)

func validateDarkRestoreConnections(ctx context.Context, tx *sql.Tx) error {
	present, e := connectionSchema(ctx, tx)
	if e != nil {
		return e
	}
	if present {
		return ErrUnavailable
	}
	return nil
}
