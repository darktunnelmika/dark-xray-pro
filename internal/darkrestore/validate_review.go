package darkrestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"regexp"
	"sort"
)

var privateReviewSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)

var importReviewReason = map[string]bool{
	"source_disabled_do_not_reactivate":             true,
	"source_verification_not_proven":                true,
	"uuid_not_verified":                             true,
	"duplicate_core_uuid":                           true,
	"legacy_unlimited_requires_plan_policy":         true,
	"no_finite_expiry":                              true,
	"expired_requires_explicit_renewal":             true,
	"archived_history_must_not_be_recreated_active": true,
	"promoted_identity_must_not_be_imported_twice":  true,
	"historical_counter_exceeds_signed_64bit":       true,
}

func reviewDecimal(value string) (*big.Int, bool) {
	if value == "" || len(value) > 30 || value[0] == '+' || value[0] == '-' ||
		(len(value) > 1 && value[0] == '0') {
		return nil, false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	n := new(big.Int)
	if _, ok := n.SetString(value, 10); !ok || n.Sign() < 0 {
		return nil, false
	}
	return n, true
}

// ValidatePrivateReview revalidates the browser/offline review as data,
// independently of the SQLite scanner which originally produced it.
// It is intentionally stricter than ReadPrivateReview because a real import
// quote must never trust client-side sums, ordering, flags or unknown reasons.
func ValidatePrivateReview(p Review) error {
	if p.Version != ReviewVersion || p.Mode != "offline_operator_review_only" ||
		p.Status != "ready_for_review" || p.ImportEnabled || p.AsOfUnix <= 0 ||
		!privateReviewSHA.MatchString(p.SourceSHA256) || p.SourceSizeBytes < 16 ||
		len(p.Groups) == 0 || len(p.Groups) > MaxReviewClients ||
		len(p.Clients) == 0 || len(p.Clients) > MaxReviewClients ||
		p.Summary.Status != "ready_for_manual_review" ||
		p.Summary.Subscriptions != int64(len(p.Clients)) ||
		p.Summary.Groups != int64(len(p.Groups)) || len(p.Summary.Findings) != 0 {
		return ErrInspection
	}
	type aggregate struct {
		count, archived, promoted int64
		usage                     *big.Int
	}
	groups := make(map[string]*aggregate, len(p.Groups))
	groupRows := make(map[string]ReviewGroup, len(p.Groups))
	lastGroup := ""
	for _, g := range p.Groups {
		if !safeLabel(g.SourceGroupID) || !safeLabel(g.Name) || g.SourceGroupID <= lastGroup ||
			g.Count < 0 || g.Archived < 0 || g.Promoted < 0 ||
			g.Archived > g.Count || g.Promoted > g.Count {
			return ErrInspection
		}
		usage, ok := reviewDecimal(g.HistoricalBytes)
		if !ok {
			return ErrInspection
		}
		groups[g.SourceGroupID] = &aggregate{usage: big.NewInt(0)}
		groupRows[g.SourceGroupID] = g
		_ = usage
		lastGroup = g.SourceGroupID
	}
	seenUUID := make(map[string]bool, len(p.Clients))
	lastClient := ""
	total := big.NewInt(0)
	for _, c := range p.Clients {
		agg, ok := groups[c.SourceGroupID]
		if !ok || !safeLabel(c.SourceID) || !safeLabel(c.Label) || c.SourceID <= lastClient ||
			c.ExpiryUnix < 0 || c.ExpiryUnix > maxExpireUnix {
			return ErrInspection
		}
		switch c.Status {
		case "requires_manual_review":
		case "archive_history_only":
			agg.archived++
		case "skip_already_promoted":
			agg.promoted++
			if containsReviewReason(c.ReviewReasons, "archived_history_must_not_be_recreated_active") {
				agg.archived++
			}
		default:
			return ErrInspection
		}
		quota, ok := reviewDecimal(c.QuotaBytes)
		if !ok {
			return ErrInspection
		}
		switch c.QuotaKind {
		case "volume":
			if quota.Sign() <= 0 {
				return ErrInspection
			}
		case "legacy_unlimited":
			if quota.Sign() != 0 {
				return ErrInspection
			}
		default:
			return ErrInspection
		}
		historical, ok := reviewDecimal(c.HistoricalBytes)
		if !ok {
			return ErrInspection
		}
		if c.UUID != "" {
			if !uuidPattern.MatchString(c.UUID) {
				return ErrInspection
			}
			if seenUUID[c.UUID] && !containsReviewReason(c.ReviewReasons, "duplicate_core_uuid") {
				return ErrInspection
			}
			seenUUID[c.UUID] = true
		}
		lastReason := ""
		for _, reason := range c.ReviewReasons {
			if !importReviewReason[reason] || reason <= lastReason {
				return ErrInspection
			}
			lastReason = reason
		}
		if c.UUID == "" && !containsReviewReason(c.ReviewReasons, "uuid_not_verified") {
			return ErrInspection
		}
		if c.QuotaKind == "legacy_unlimited" && !containsReviewReason(c.ReviewReasons, "legacy_unlimited_requires_plan_policy") {
			return ErrInspection
		}
		if c.ExpiryUnix == 0 && !containsReviewReason(c.ReviewReasons, "no_finite_expiry") {
			return ErrInspection
		}
		if c.Status == "archive_history_only" && !containsReviewReason(c.ReviewReasons, "archived_history_must_not_be_recreated_active") {
			return ErrInspection
		}
		if c.Status == "skip_already_promoted" && !containsReviewReason(c.ReviewReasons, "promoted_identity_must_not_be_imported_twice") {
			return ErrInspection
		}
		agg.count++
		agg.usage.Add(agg.usage, historical)
		total.Add(total, historical)
		lastClient = c.SourceID
	}
	for id, actual := range groups {
		want := groupRows[id]
		if actual.count != want.Count || actual.archived != want.Archived || actual.promoted != want.Promoted ||
			actual.usage.String() != want.HistoricalBytes {
			return ErrInspection
		}
	}
	if p.Summary.TotalConsumedBytes != total.String() {
		return ErrInspection
	}
	return nil
}

func containsReviewReason(reasons []string, want string) bool {
	i := sort.SearchStrings(reasons, want)
	return i < len(reasons) && reasons[i] == want
}

func CanonicalReviewSHA256(p Review) (string, error) {
	if err := ValidatePrivateReview(p); err != nil {
		return "", err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", ErrInspection
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
