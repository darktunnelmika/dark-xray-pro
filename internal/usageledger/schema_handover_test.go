package usageledger

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestReviewedBusinessSchemaVersions(t *testing.T) {
	for _, version := range []int{7, 8, 9} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db, _ := testDB(t)
			if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
				t.Fatal(err)
			}
			_, err := Open(context.Background(), db)
			if version == 9 {
				if !errors.Is(err, ErrSchema) {
					t.Fatal("unreviewed schema accepted", err)
				}
				return
			}
			if err != nil {
				t.Fatal("reviewed schema rejected", err)
			}
		})
	}
}
