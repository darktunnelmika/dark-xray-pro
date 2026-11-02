package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

const maxDarkRestoreImportClients = darkrestore.MaxReviewClients

type DarkRestoreGroupMappingV1 struct {
	SourceGroupID    string `json:"source_group_id"`
	RepresentativeID string `json:"representative_id"`
}
type DarkRestoreImportIntentV1 struct {
	Review         darkrestore.Review          `json:"review"`
	Mappings       []DarkRestoreGroupMappingV1 `json:"mappings"`
	ImportSafeOnly bool                        `json:"import_safe_only"`
}
type DarkRestoreImportCommitV1 struct {
	DarkRestoreImportIntentV1
	EventID   string `json:"event_id"`
	QuoteHash string `json:"quote_hash"`
	QuoteUnix int64  `json:"quote_unix"`
}
type DarkRestoreReasonCountV1 struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}
type DarkRestoreGroupQuoteV1 struct {
	SourceGroupID    string `json:"source_group_id"`
	SourceName       string `json:"source_name"`
	RepresentativeID string `json:"representative_id"`
	TargetGroupID    string `json:"target_group_id"`
	TargetGroupName  string `json:"target_group_name"`
	SourceCount      int64  `json:"source_count"`
	Importable       int64  `json:"importable"`
	Archived         int64  `json:"archived"`
	Disabled         int64  `json:"disabled"`
	SkippedPromoted  int64  `json:"skipped_promoted"`
	Blocked          int64  `json:"blocked"`
	HistoricalBytes  string `json:"historical_bytes"`
}
type DarkRestoreImportQuoteV1 struct {
	SourceSHA256      string                     `json:"source_sha256"`
	ReviewSHA256      string                     `json:"review_sha256"`
	QuoteHash         string                     `json:"quote_hash"`
	QuoteUnix         int64                      `json:"quote_unix"`
	Total             int64                      `json:"total"`
	Importable        int64                      `json:"importable"`
	Archived          int64                      `json:"archived"`
	Disabled          int64                      `json:"disabled"`
	SkippedPromoted   int64                      `json:"skipped_promoted"`
	Blocked           int64                      `json:"blocked"`
	BlockedReasons    []DarkRestoreReasonCountV1 `json:"blocked_reasons"`
	Groups            []DarkRestoreGroupQuoteV1  `json:"groups"`
	CreditChanged     bool                       `json:"credit_changed"`
	UsageReset        bool                       `json:"usage_reset"`
	XrayChanged       bool                       `json:"xray_changed"`
	ImportedUsageMode string                     `json:"imported_usage_mode"`
}
type DarkRestoreImportReceiptV1 struct {
	EventID           string                     `json:"event_id"`
	ActorUserID       string                     `json:"actor_user_id"`
	SourceSHA256      string                     `json:"source_sha256"`
	ReviewSHA256      string                     `json:"review_sha256"`
	Count             int64                      `json:"count"`
	Archived          int64                      `json:"archived"`
	Disabled          int64                      `json:"disabled"`
	SkippedPromoted   int64                      `json:"skipped_promoted"`
	Blocked           int64                      `json:"blocked"`
	BlockedReasons    []DarkRestoreReasonCountV1 `json:"blocked_reasons"`
	Groups            []DarkRestoreGroupQuoteV1  `json:"groups"`
	CreatedUnix       int64                      `json:"created_unix"`
	Replayed          bool                       `json:"replayed"`
	CreditChanged     bool                       `json:"credit_changed"`
	UsageReset        bool                       `json:"usage_reset"`
	XrayChanged       bool                       `json:"xray_changed"`
	UsageBaselineMade bool                       `json:"usage_baseline_made"`
}

type darkRestoreImportProofV1 struct {
	SourceSHA256   string                      `json:"source_sha256"`
	ReviewSHA256   string                      `json:"review_sha256"`
	AsOfUnix       int64                       `json:"as_of_unix"`
	Mappings       []DarkRestoreGroupMappingV1 `json:"mappings"`
	ImportSafeOnly bool                        `json:"import_safe_only"`
}
type darkRestoreTargetSnapshotV1 struct {
	Representative Representative `json:"representative"`
	GroupID        string         `json:"group_id"`
	GroupName      string         `json:"group_name"`
	Group          *Group         `json:"group,omitempty"`
}
type darkRestorePlanRowV1 struct {
	Client     darkrestore.ReviewClient
	ClientID   string
	Mapping    DarkRestoreGroupMappingV1
	GroupID    string
	GroupName  string
	Duration   int64
	Blocked    []string
	Promoted   bool
	Archived   bool
	Disabled   bool
	Historical int64
	Quota      int64
}

func darkRestoreClientID(sourceSHA, sourceID string) string {
	sum := sha256.Sum256([]byte("darkrestore-client-v1\x00" + sourceSHA + "\x00" + sourceID))
	return "drc-" + hex.EncodeToString(sum[:12])
}
func darkRestoreTargetGroup(sourceSHA, sourceGroup, sourceName string) (string, string) {
	sum := sha256.Sum256([]byte("darkrestore-group-v1\x00" + sourceSHA + "\x00" + sourceGroup))
	suffix := hex.EncodeToString(sum[:4])
	id := "drg-" + hex.EncodeToString(sum[:12])
	name := strings.TrimSpace(sourceName)
	runes := []rune(name)
	if len(runes) > 67 {
		name = string(runes[:67])
	}
	name = strings.TrimSpace(name) + " · " + suffix
	return id, name
}
func canonicalDarkRestoreIntent(input DarkRestoreImportIntentV1) (DarkRestoreImportIntentV1, string, error) {
	if err := darkrestore.ValidatePrivateReview(input.Review); err != nil {
		return input, "", ErrInvalid
	}
	reviewSHA, err := darkrestore.CanonicalReviewSHA256(input.Review)
	if err != nil || len(input.Mappings) != len(input.Review.Groups) || len(input.Mappings) == 0 {
		return input, "", ErrInvalid
	}
	input.Mappings = append([]DarkRestoreGroupMappingV1{}, input.Mappings...)
	sort.Slice(input.Mappings, func(i, j int) bool { return input.Mappings[i].SourceGroupID < input.Mappings[j].SourceGroupID })
	for i, mapping := range input.Mappings {
		if !idRE.MatchString(mapping.RepresentativeID) || mapping.SourceGroupID != input.Review.Groups[i].SourceGroupID {
			return input, "", ErrInvalid
		}
		if i > 0 && mapping.SourceGroupID == input.Mappings[i-1].SourceGroupID {
			return input, "", ErrInvalid
		}
	}
	return input, reviewSHA, nil
}
func darkRestoreDuration(review darkrestore.ReviewClient, asOf int64) (int64, bool) {
	if review.ExpiryUnix <= 0 {
		return 0, false
	}
	if review.ExpiryUnix <= asOf {
		return 1, true
	}
	seconds := review.ExpiryUnix - asOf
	days := (seconds + 86399) / 86400
	return days, days >= 1 && days <= MaxDays
}
func darkRestoreBlockingReasons(c darkrestore.ReviewClient, quota int64, quotaOK, durationOK, historicalOK, duplicateUUID bool) []string {
	if c.Status == "skip_already_promoted" {
		return nil
	}
	block := map[string]bool{}
	for _, reason := range c.ReviewReasons {
		switch reason {
		case "source_verification_not_proven", "uuid_not_verified", "duplicate_core_uuid",
			"legacy_unlimited_requires_plan_policy", "no_finite_expiry", "historical_counter_exceeds_signed_64bit":
			block[reason] = true
		}
	}
	if duplicateUUID {
		block["duplicate_core_uuid"] = true
	}
	if c.UUID == "" {
		block["uuid_not_verified"] = true
	}
	if c.QuotaKind != "volume" {
		block["legacy_unlimited_requires_plan_policy"] = true
	}
	if !quotaOK || quota <= 0 || quota > MaxVolumeCredit {
		block["legacy_quota_exceeds_pro_limit"] = true
	}
	if !durationOK {
		block["legacy_expiry_exceeds_pro_duration"] = true
	}
	if !historicalOK {
		block["historical_counter_exceeds_signed_64bit"] = true
	}
	out := make([]string, 0, len(block))
	for reason := range block {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}
func darkRestoreHasReason(c darkrestore.ReviewClient, reason string) bool {
	i := sort.SearchStrings(c.ReviewReasons, reason)
	return i < len(c.ReviewReasons) && c.ReviewReasons[i] == reason
}

func quotedDarkRestoreTx(ctx context.Context, tx *sql.Tx, actor string, input DarkRestoreImportIntentV1, reviewSHA string, at int64) (DarkRestoreImportQuoteV1, []darkRestorePlanRowV1, error) {
	out := DarkRestoreImportQuoteV1{
		SourceSHA256: input.Review.SourceSHA256, ReviewSHA256: reviewSHA, QuoteUnix: at,
		Total: int64(len(input.Review.Clients)), Groups: make([]DarkRestoreGroupQuoteV1, 0, len(input.Mappings)),
		BlockedReasons: []DarkRestoreReasonCountV1{}, ImportedUsageMode: "legacy_history_separate_current_usage_unknown_until_cutover",
	}
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return out, nil, ErrUnavailable
	}
	if imported, e := darkRestoreSourceAlreadyImported(ctx, tx, input.Review.SourceSHA256); e != nil {
		return out, nil, e
	} else if imported {
		return out, nil, ErrConflict
	}
	existingIDs := map[string]bool{}
	existingUUIDs := map[string]bool{}
	rows, err := tx.QueryContext(ctx, "SELECT id,uuid FROM clients")
	if err != nil {
		return out, nil, err
	}
	for rows.Next() {
		var id, uuid string
		if err = rows.Scan(&id, &uuid); err != nil {
			rows.Close()
			return out, nil, err
		}
		existingIDs[id], existingUUIDs[uuid] = true, true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, nil, err
	}
	rows.Close()
	reviewUUIDs := map[string]int{}
	for _, c := range input.Review.Clients {
		if c.UUID != "" {
			reviewUUIDs[c.UUID]++
		}
	}
	mapping := make(map[string]DarkRestoreGroupMappingV1, len(input.Mappings))
	groupQuote := make(map[string]*DarkRestoreGroupQuoteV1, len(input.Mappings))
	targets := make([]darkRestoreTargetSnapshotV1, 0, len(input.Mappings))
	plannedGroups := map[string]bool{}
	for i, m := range input.Mappings {
		source := input.Review.Groups[i]
		rep, e := readRep(ctx, tx, m.RepresentativeID)
		if errors.Is(e, sql.ErrNoRows) {
			return out, nil, ErrNotFound
		}
		if e != nil {
			return out, nil, e
		}
		if !rep.Enabled || (rep.Kind != "representative" && rep.Kind != "owner") {
			return out, nil, ErrConflict
		}
		groupID, groupName := darkRestoreTargetGroup(input.Review.SourceSHA256, m.SourceGroupID, source.Name)
		if plannedGroups[groupID] {
			return out, nil, ErrConflict
		}
		plannedGroups[groupID] = true
		var existing *Group
		g, e := readGroup(ctx, tx, groupID)
		if e == nil {
			if g.RepresentativeID != rep.ID || g.Name != groupName {
				return out, nil, ErrConflict
			}
			existing = &g
		} else if !errors.Is(e, sql.ErrNoRows) {
			return out, nil, e
		} else {
			var sameName int
			if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM client_groups WHERE representative_id=? AND name=?", rep.ID, groupName).Scan(&sameName); e != nil || sameName != 0 {
				if e == nil {
					e = ErrConflict
				}
				return out, nil, e
			}
		}
		mapping[m.SourceGroupID] = m
		q := DarkRestoreGroupQuoteV1{SourceGroupID: source.SourceGroupID, SourceName: source.Name,
			RepresentativeID: rep.ID, TargetGroupID: groupID, TargetGroupName: groupName,
			SourceCount: source.Count, HistoricalBytes: source.HistoricalBytes}
		out.Groups = append(out.Groups, q)
		groupQuote[source.SourceGroupID] = &out.Groups[len(out.Groups)-1]
		targets = append(targets, darkRestoreTargetSnapshotV1{Representative: rep, GroupID: groupID, GroupName: groupName, Group: existing})
	}
	reasons := map[string]int64{}
	plannedClients := map[string]bool{}
	plan := make([]darkRestorePlanRowV1, 0, len(input.Review.Clients))
	for _, c := range input.Review.Clients {
		m := mapping[c.SourceGroupID]
		g := groupQuote[c.SourceGroupID]
		if m.SourceGroupID == "" || g == nil {
			return out, nil, ErrInvalid
		}
		row := darkRestorePlanRowV1{Client: c, ClientID: darkRestoreClientID(input.Review.SourceSHA256, c.SourceID),
			Mapping: m, GroupID: g.TargetGroupID, GroupName: g.TargetGroupName}
		if plannedClients[row.ClientID] {
			return out, nil, ErrConflict
		}
		plannedClients[row.ClientID] = true
		row.Promoted = c.Status == "skip_already_promoted"
		row.Archived = c.Status == "archive_history_only"
		row.Disabled = row.Archived || c.ExpiryUnix <= at || darkRestoreHasReason(c, "source_disabled_do_not_reactivate")
		quota, qErr := strconv.ParseInt(c.QuotaBytes, 10, 64)
		row.Quota = quota
		historical, hErr := strconv.ParseInt(c.HistoricalBytes, 10, 64)
		row.Historical = historical
		duration, durationOK := darkRestoreDuration(c, at)
		row.Duration = duration
		row.Blocked = darkRestoreBlockingReasons(c, row.Quota, qErr == nil, durationOK, hErr == nil, c.UUID != "" && reviewUUIDs[c.UUID] > 1)
		if row.Promoted {
			out.SkippedPromoted++
			g.SkippedPromoted++
			plan = append(plan, row)
			continue
		}
		if existingIDs[row.ClientID] || c.UUID != "" && existingUUIDs[c.UUID] {
			return out, nil, ErrConflict
		}
		if len(row.Blocked) != 0 {
			out.Blocked++
			g.Blocked++
			for _, reason := range row.Blocked {
				reasons[reason]++
			}
			plan = append(plan, row)
			continue
		}
		out.Importable++
		g.Importable++
		if row.Archived {
			out.Archived++
			g.Archived++
		}
		if row.Disabled {
			out.Disabled++
			g.Disabled++
		}
		plan = append(plan, row)
	}
	for reason, count := range reasons {
		out.BlockedReasons = append(out.BlockedReasons, DarkRestoreReasonCountV1{Reason: reason, Count: count})
	}
	sort.Slice(out.BlockedReasons, func(i, j int) bool { return out.BlockedReasons[i].Reason < out.BlockedReasons[j].Reason })
	proof := darkRestoreImportProofV1{SourceSHA256: input.Review.SourceSHA256, ReviewSHA256: reviewSHA, AsOfUnix: input.Review.AsOfUnix, Mappings: input.Mappings, ImportSafeOnly: input.ImportSafeOnly}
	out.QuoteHash = fingerprint("darkrestore-import-quote-v1", struct {
		Actor   string
		Proof   darkRestoreImportProofV1
		Targets []darkRestoreTargetSnapshotV1
		At      int64
	}{actor, proof, targets, at})
	return out, plan, nil
}

func (s *Store) QuoteDarkRestoreImportV1(ctx context.Context, token string, input DarkRestoreImportIntentV1) (DarkRestoreImportQuoteV1, error) {
	intent, reviewSHA, err := canonicalDarkRestoreIntent(input)
	if err != nil {
		return DarkRestoreImportQuoteV1{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DarkRestoreImportQuoteV1{}, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return DarkRestoreImportQuoteV1{}, err
	}
	quote, _, err := quotedDarkRestoreTx(ctx, tx, actor, intent, reviewSHA, time.Now().Unix())
	return quote, err
}
