package business

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// Startup-only capability: schema restoration alone never enables preparation,
// probes or delivery. Consumer tables are installed without endpoint policy.
func (s *Store) EnableInitialApplyHealth(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	for _, enable := range []func(context.Context) error{
		s.EnableSystemProbeAccounting, s.EnableMeteredHealthChallenges,
		s.EnableMeteredHealthQueue, s.EnableMeteredHealthRounds, s.EnableLabSubscriptions,
		s.usage.EnableInitialApplyHealth,
	} {
		if err := enable(ctx); err != nil {
			return err
		}
	}
	s.initialApplyHealthEnabled = true
	return nil
}

func (s *Store) initialHealthTx(ctx context.Context, key []byte) (*sql.Tx, error) {
	if !s.initialApplyHealthEnabled {
		return nil, ErrUnavailable
	}
	tx, err := s.subscriptionTx(ctx, key)
	if err != nil {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || !usageledger.HasInitialApplyHealthSchema(version) {
		tx.Rollback()
		return nil, ErrUnavailable
	}
	return tx, nil
}

type initialPreparationRow struct {
	InitialApplyHealthPreparation
	ActorID, PromotionSHA, ProbeID, Fingerprint string
}

func initialPreparationAAD(inbound uint64, event string) string {
	return promotionAAD(inbound, event) + ":initial-health-preparation-v1"
}

func readInitialPreparation(ctx context.Context, tx *sql.Tx, key []byte, inbound uint64) (initialPreparationRow, error) {
	var row initialPreparationRow
	var enc []byte
	row.InboundID = inbound
	err := tx.QueryRowContext(ctx, `SELECT event_id,actor_id,promotion_event_id,promotion_sha,probe_id,fingerprint,created,receipt_enc FROM pro_initial_preparations WHERE inbound_id=?`, inbound).Scan(&row.EventID, &row.ActorID, &row.PromotionEventID, &row.PromotionSHA, &row.ProbeID, &row.Fingerprint, &row.CreatedUnixNano, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, err
	}
	var stored initialPreparationRow
	if err = openPlanValue(key, initialPreparationAAD(inbound, row.EventID), enc, &stored); err != nil {
		return row, err
	}
	promotion, sha, err := readNativeInboundPromotion(ctx, tx, key, inbound)
	if err != nil {
		return row, err
	}
	if promotion == nil {
		return row, ErrUnavailable
	}
	row.NodeID, row.InstallationID, row.Prepared = promotion.NodeID, promotion.InstallationID, true
	if !reflect.DeepEqual(row, stored) || row.PromotionSHA != sha || row.PromotionEventID != promotion.EventID || row.CreatedUnixNano <= 0 || !eventRE.MatchString(row.EventID) || !idRE.MatchString(row.ActorID) || !routeSHA.MatchString(row.Fingerprint) {
		return row, ErrUnavailable
	}
	probe, err := usageledger.ProbeIdentityTx(ctx, tx, row.NodeID, row.InstallationID, inbound)
	if err != nil || probe.ID != row.ProbeID {
		return row, ErrUnavailable
	}
	want := fingerprint("initial-health-preparation-v1", struct {
		ActorID      string
		InboundID    uint64
		Input        InitialApplyHealthPreparationInput
		PromotionSHA string
	}{row.ActorID, inbound, InitialApplyHealthPreparationInput{row.EventID, row.PromotionEventID}, sha})
	if want != row.Fingerprint {
		return row, ErrUnavailable
	}
	return row, nil
}

func initialPreparationPossible(ctx context.Context, tx *sql.Tx, promotion *NativeInboundPlanPromotionReceipt) error {
	ch, serial, err := meteredRouteObservation(ctx, tx, promotion.NodeID)
	if err != nil {
		return err
	}
	status, o := ch.Status, ch.Status.Observed
	if !ch.ObservationFresh || ch.InstallationID != promotion.InstallationID || serial != 0 || status.Serial != 0 || !routeID.MatchString(status.Instance) || status.Usage == nil || *status.Usage != (mx.MeteredCondition{}) || status.StagedID != "" || status.CurrentStageID != "" || status.PreviousStageID != "" || status.InterruptedCommandSHA != "" || mx.Condition(o) != (mx.RuntimeCondition{}) || o.PreviousSHA != "" || o.LeaseExpires != 0 || o.Usage != nil || o.Phase != "awaiting_fresh_lease" || o.DataPlaneVerified || o.SubscriptionPublished {
		return ErrConflict
	}
	for _, query := range []string{`SELECT COUNT(*) FROM native_metered_route_commands WHERE node_id=?`, `SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=?`, `SELECT COUNT(*) FROM native_metered_handover_nodes WHERE node_id=?`} {
		var n int
		if err = tx.QueryRowContext(ctx, query, promotion.NodeID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return nil
}

func requireInitialPreparationForStage(ctx context.Context, tx *sql.Tx, key []byte, node string) error {
	var inbound uint64
	err := tx.QueryRowContext(ctx, `SELECT inbound_id FROM native_inbound_plan_promotions WHERE node_id=?`, node).Scan(&inbound)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = readInitialPreparation(ctx, tx, key, inbound)
	if errors.Is(err, ErrNotFound) {
		return ErrConflict
	}
	return err
}

func initialEventUnused(ctx context.Context, tx *sql.Tx, event string) error {
	for _, table := range []string{"pro_initial_preparations", "pro_initial_snapshots", "pro_subscription_snapshots", "pro_subscription_token_events", "pro_consumer_manifests"} {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE event_id=?`, event).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return nil
}

func (s *Store) PrepareInitialApplyHealth(ctx context.Context, key []byte, actorID string, inbound uint64, in InitialApplyHealthPreparationInput) (InitialApplyHealthPreparation, error) {
	var out InitialApplyHealthPreparation
	if inbound == 0 || !eventRE.MatchString(in.EventID) || !eventRE.MatchString(in.PromotionEventID) {
		return out, ErrInvalid
	}
	tx, err := s.initialHealthTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = consumerManifestOwner(ctx, tx, actorID); err != nil {
		return out, err
	}
	promotion, sha, err := readNativeInboundPromotion(ctx, tx, key, inbound)
	if err != nil {
		return out, err
	}
	if promotion == nil || promotion.EventID != in.PromotionEventID {
		return out, ErrConflict
	}
	fp := fingerprint("initial-health-preparation-v1", struct {
		ActorID      string
		InboundID    uint64
		Input        InitialApplyHealthPreparationInput
		PromotionSHA string
	}{actorID, inbound, in, sha})
	prior, err := readInitialPreparation(ctx, tx, key, inbound)
	if err == nil {
		if prior.Fingerprint != fp {
			return out, ErrConflict
		}
		prior.Replayed = true
		return prior.InitialApplyHealthPreparation, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if err = initialEventUnused(ctx, tx, in.EventID); err != nil {
		return out, err
	}
	if err = initialHealthIDUnused(ctx, tx, in.EventID); err != nil {
		return out, err
	}
	if err = initialPreparationPossible(ctx, tx, promotion); err != nil {
		return out, err
	}
	probe, err := s.provisionMeteredProbeTx(ctx, tx, key, promotion.NodeID, promotion.InstallationID, inbound)
	if err != nil {
		return out, err
	}
	row := initialPreparationRow{InitialApplyHealthPreparation: InitialApplyHealthPreparation{EventID: in.EventID, PromotionEventID: in.PromotionEventID, InboundID: inbound, NodeID: promotion.NodeID, InstallationID: promotion.InstallationID, Prepared: true, CreatedUnixNano: time.Now().UnixNano()}, ActorID: actorID, PromotionSHA: sha, ProbeID: probe.ID, Fingerprint: fp}
	enc, err := sealPlanValue(key, initialPreparationAAD(inbound, in.EventID), row)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_initial_preparations(inbound_id,event_id,actor_id,promotion_event_id,promotion_sha,probe_id,fingerprint,created,receipt_enc) VALUES(?,?,?,?,?,?,?,?,?)`, inbound, in.EventID, actorID, in.PromotionEventID, sha, probe.ID, fp, row.CreatedUnixNano, enc); err != nil {
		return out, err
	}
	// Verify the prepared candidate in the same transaction. A probe never
	// makes an otherwise empty or ineligible customer route executable.
	if _, err = meteredRouteCandidate(ctx, tx, key, promotion.NodeID, promotion.InstallationID, []RouteDraftRef{{InboundID: inbound, InboundRevision: promotion.InboundRevision, DraftRevision: promotion.DraftRevision}}); err != nil {
		return out, err
	}
	return row.InitialApplyHealthPreparation, tx.Commit()
}

type initialApplySnapshot struct {
	meteredHealthSnapshot
	promotion   NativeInboundPlanPromotionReceipt
	preparation initialPreparationRow
}

func (s *Store) loadInitialApplySnapshot(ctx context.Context, tx *sql.Tx, key []byte, inbound uint64, now int64) (initialApplySnapshot, error) {
	var out initialApplySnapshot
	prep, err := readInitialPreparation(ctx, tx, key, inbound)
	if err != nil {
		return out, err
	}
	promotion, sha, err := readNativeInboundPromotion(ctx, tx, key, inbound)
	if err != nil || promotion == nil {
		return out, ErrUnavailable
	}
	ch, err := handoverIdleChannel(ctx, tx, promotion.NodeID)
	if err != nil {
		return out, err
	}
	var applyID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM native_metered_route_commands WHERE node_id=? AND installation_id=? AND serial=? AND action='apply'`, promotion.NodeID, promotion.InstallationID, ch.Status.Serial).Scan(&applyID); err != nil {
		return out, ErrConflict
	}
	state, err := s.loadAppliedHealthSnapshot(ctx, tx, key, promotion.NodeID, promotion.InstallationID, ch.Status.CurrentStageID, applyID, now)
	if err != nil {
		return out, err
	}
	a, c, ack := state.apply, state.apply.payload.Command, state.apply.view.Receipt
	if a.payload.HandoverID != "" || c.Usage == nil || *c.Usage != (mx.MeteredCondition{}) || ack.Metered.PriorSettlement != (uc.Receipt{}) || c.Expected != (mx.RuntimeCondition{}) || len(a.payload.Request.Drafts) != 1 || a.payload.Request.Drafts[0] != (RouteDraftRef{InboundID: inbound, InboundRevision: promotion.InboundRevision, DraftRevision: promotion.DraftRevision}) || len(a.payload.Candidate.Listeners) != 1 {
		return out, ErrConflict
	}
	l := a.payload.Candidate.Listeners[0]
	if l.InboundID != inbound || l.Kind != "direct" || l.NodePort < 1024 || a.payload.Candidate.ManifestSHA256 != c.Lease.ManifestSHA {
		return out, ErrConflict
	}
	var epochs, reservations int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=?`, promotion.NodeID).Scan(&epochs); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_metered_handover_nodes WHERE node_id=?`, promotion.NodeID).Scan(&reservations); err != nil {
		return out, err
	}
	if epochs != 1 || reservations != 0 {
		return out, ErrConflict
	}
	if err = meteredReceiptLedger(ctx, tx, a, *ack); err != nil {
		return out, err
	}
	state.base.Version = mx.InitialApplyProtocolHealthVersion
	// Fixed, authenticated applied authority; the digest excludes a round's
	// nonce/deadline and all mutable accounting counters.
	state.base.InitialApplySHA = fingerprint("promoted-initial-apply-v1", struct {
		Promotion                    NativeInboundPlanPromotionReceipt
		PromotionSHA, PreparationSHA string
		Runtime                      mx.ProtocolHealthRequest
		Generation                   uint64
		ConfigSHA, ManifestSHA       string
	}{*promotion, sha, prep.Fingerprint, state.base, a.payload.Generation, a.payload.Candidate.SHA256, a.payload.Candidate.ManifestSHA256})
	if _, err = state.request(ctx, tx, inbound); err != nil {
		return out, err
	}
	out.meteredHealthSnapshot, out.promotion, out.preparation = state, *promotion, prep
	return out, nil
}

func (s *Store) InitialApplyHealth(ctx context.Context, key []byte, actorID string, inbound uint64, endpoint mx.ProtocolHealthEndpoint) (InitialApplyHealthState, error) {
	out := InitialApplyHealthState{Scope: InitialApplyHealthScope, InboundID: inbound, ClientIDs: []string{}, Blockers: []InitialApplyHealthBlocker{}, ServerTimeMillis: time.Now().UnixMilli()}
	if inbound == 0 || !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.initialHealthTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = consumerManifestOwner(ctx, tx, actorID); err != nil {
		return out, err
	}
	promotion, _, err := readNativeInboundPromotion(ctx, tx, key, inbound)
	if err != nil {
		return out, err
	}
	if promotion == nil {
		return out, ErrNotFound
	}
	out.PromotionEventID, out.NodeID, out.InstallationID = promotion.EventID, promotion.NodeID, promotion.InstallationID
	out.InboundRevision, out.DraftRevision = promotion.InboundRevision, promotion.DraftRevision
	out.ClientIDs = append(out.ClientIDs, promotion.ClientIDs...)
	if err = tx.QueryRowContext(ctx, `SELECT direct_port FROM native_inbound_plan_promotions WHERE inbound_id=?`, inbound).Scan(&out.DirectPort); err != nil {
		return out, err
	}
	prep, err := readInitialPreparation(ctx, tx, key, inbound)
	if errors.Is(err, ErrNotFound) {
		out.CanPrepare = initialPreparationPossible(ctx, tx, promotion) == nil
		code := "preparation_required"
		if !out.CanPrepare {
			code = "preparation_requires_fresh_unstaged_runtime"
		}
		out.Blockers = append(out.Blockers, InitialApplyHealthBlocker{Field: "preparation", Code: code})
		return out, tx.Commit()
	}
	if err != nil {
		return out, err
	}
	out.Prepared, out.Preparation = true, &prep.InitialApplyHealthPreparation
	state, err := s.loadInitialApplySnapshot(ctx, tx, key, inbound, time.Now().UnixNano())
	if err != nil {
		if !routeReviewBlocked(err) && !errors.Is(err, ErrNotFound) {
			return out, err
		}
		out.Blockers = append(out.Blockers, InitialApplyHealthBlocker{Field: "apply", Code: "current_first_apply_required"})
	} else {
		q, e := state.request(ctx, tx, inbound)
		if e != nil {
			return out, e
		}
		if e = mx.ValidateProtocolHealthSupport(state.apply.payload.Candidate, q, endpoint); e != nil {
			out.Blockers = append(out.Blockers, InitialApplyHealthBlocker{Field: "network", Code: "protocol_probe_unsupported"})
		} else {
			out.Eligible = true
			out.InitialApplySHA, out.ApplyCommandID, out.StageID, out.EpochID = state.base.InitialApplySHA, state.base.ApplyCommandID, state.base.StageID, state.epoch.ID
		}
	}
	var roundID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM pro_initial_rounds WHERE inbound_id=? ORDER BY created DESC,rowid DESC LIMIT 1`, inbound).Scan(&roundID)
	if err == nil {
		row, e := readInitialHealthRound(ctx, tx, roundID)
		if e != nil {
			return out, e
		}
		v := s.assessInitialHealthRound(ctx, tx, key, row, &endpoint, time.Now().UnixNano())
		out.LatestRound = &v
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	return out, tx.Commit()
}
