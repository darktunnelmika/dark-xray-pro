//go:build !linux

package business

import (
	"context"
	"database/sql"
)

func validateDarkRestoreServingHealth(context.Context, *sql.Tx) error { return nil }
