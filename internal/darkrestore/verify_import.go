package darkrestore

import (
	"context"
	"time"
)

// VerifyImportReview rebuilds the entire review from a SHA-pinned private
// snapshot. A browser-provided digest or internally consistent JSON is not
// proof of source authenticity. No network or legacy HUB access is performed.
func VerifyImportReview(ctx context.Context, path string, proposed Review) error {
	expected, err := CanonicalReviewSHA256(proposed)
	if err != nil {
		return ErrInspection
	}
	rebuilt, err := BuildReview(ctx, path, proposed.SourceSHA256, time.Unix(proposed.AsOfUnix, 0))
	if err != nil {
		return err
	}
	actual, err := CanonicalReviewSHA256(rebuilt)
	if err != nil || expected != actual {
		return ErrInspection
	}
	return nil
}
