//go:build !linux

package business

import (
	"context"
	"database/sql"
)

func validateDarkRestoreAdmissions(ctx context.Context, tx *sql.Tx) error {
	present, err := admissionSchema(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return ErrUnavailable
	}
	return nil
}
