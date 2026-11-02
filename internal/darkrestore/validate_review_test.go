package darkrestore

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func validatedReviewFixture(t *testing.T) Review {
	t.Helper()
	path, _ := sourceFixture(t, 3)
	r, err := BuildReview(context.Background(), path, fixtureSHA(t, path), time.Unix(1780000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePrivateReview(r); err != nil {
		t.Fatal("scanner output rejected", err)
	}
	return r
}

func TestImportReviewRejectsTamperedAccountingAndUnsafeFlags(t *testing.T) {
	base := validatedReviewFixture(t)
	cases := map[string]func(*Review){
		"total":                  func(r *Review) { r.Summary.TotalConsumedBytes = "0" },
		"group history":          func(r *Review) { r.Groups[0].HistoricalBytes = "0" },
		"client history":         func(r *Review) { r.Clients[0].HistoricalBytes = "0001" },
		"import enabled":         func(r *Review) { r.ImportEnabled = true },
		"source hash":            func(r *Review) { r.SourceSHA256 = "unverified" },
		"archive reason removed": func(r *Review) { r.Clients[0].ReviewReasons = nil },
		"unknown status":         func(r *Review) { r.Clients[0].Status = "active" },
		"duplicate identity":     func(r *Review) { r.Clients[1].SourceID = r.Clients[0].SourceID },
		"unknown reason":         func(r *Review) { r.Clients[0].ReviewReasons = []string{"unknown_reason"} },
	}
	raw, _ := json.Marshal(base)
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var r Review
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			mutate(&r)
			if err := ValidatePrivateReview(r); err == nil {
				t.Fatal("unsafe review accepted")
			}
			if _, err := CanonicalReviewSHA256(r); err == nil {
				t.Fatal("unsafe review fingerprinted")
			}
		})
	}
}

func TestImportReviewFingerprintStableAndSensitiveToIdentity(t *testing.T) {
	r := validatedReviewFixture(t)
	a, err := CanonicalReviewSHA256(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalReviewSHA256(r)
	if err != nil || a != b || len(a) != 64 {
		t.Fatal("unstable digest", err)
	}
	r.Clients[1].Label = "Changed synthetic label"
	c, err := CanonicalReviewSHA256(r)
	if err != nil || a == c {
		t.Fatal("review change unbound", err)
	}
}
