package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"sync"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// Freshness is a disposable projection of an already committed observation.
// It never contains usage, grants a lease, changes an epoch, or resolves a hold.
// Every entry is bound to the exact encrypted durable predecessor. A command,
// state transition, restore, or new Store therefore cannot reuse another state.
const executionObservationFreshnessLimit = 1024

type executionFreshObservation struct {
	base  [32]byte
	value ix.Observation
}

type executionObservationGate struct {
	token chan struct{}
	users int
}

type executionObservationFreshness struct {
	mu     sync.Mutex
	values map[string]executionFreshObservation
	gates  map[string]*executionObservationGate
}

type executionObservationContextKey struct{}

func observationIdentity(in ix.Observation) string { return in.NodeID + "/" + in.InstallationID }

func (s *Store) withObservationFreshness(ctx context.Context) context.Context {
	return context.WithValue(ctx, executionObservationContextKey{}, &s.observationFreshness)
}

// Gates exist only while calls are in flight; an untrusted node ID cannot grow
// a permanent lock map. Waiting for one node never holds the global cache lock.
func (c *executionObservationFreshness) lock(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.gates == nil {
		c.gates = make(map[string]*executionObservationGate)
	}
	g := c.gates[id]
	if g == nil {
		g = &executionObservationGate{token: make(chan struct{}, 1)}
		g.token <- struct{}{}
		c.gates[id] = g
	}
	g.users++
	c.mu.Unlock()
	release := func() {
		c.mu.Lock()
		g.users--
		if g.users == 0 {
			delete(c.gates, id)
		}
		c.mu.Unlock()
	}
	select {
	case <-g.token:
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
	unlock := func() { g.token <- struct{}{}; release() }
	if err := ctx.Err(); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

func freshExecutionObservation(ctx context.Context, raw []byte, durable ix.Observation) ix.Observation {
	c, _ := ctx.Value(executionObservationContextKey{}).(*executionObservationFreshness)
	if c == nil {
		return durable
	}
	c.mu.Lock()
	v, ok := c.values[observationIdentity(durable)]
	c.mu.Unlock()
	if ok && v.base == sha256.Sum256(raw) && v.value.ObservedUnixNano >= durable.ObservedUnixNano {
		return v.value
	}
	return durable
}

func (c *executionObservationFreshness) put(raw []byte, in ix.Observation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = make(map[string]executionFreshObservation)
	}
	id := observationIdentity(in)
	if _, exists := c.values[id]; !exists && len(c.values) >= executionObservationFreshnessLimit {
		var oldest string
		var at int64
		for k, v := range c.values {
			if oldest == "" || v.value.ObservedUnixNano < at {
				oldest, at = k, v.value.ObservedUnixNano
			}
		}
		delete(c.values, oldest)
	}
	c.values[id] = executionFreshObservation{base: sha256.Sum256(raw), value: in}
}

func observationRefreshable(in ix.Observation) bool {
	if in.InitialEpoch != nil || in.ErrorCode != "" {
		return false
	}
	if in.State == "stopped" {
		return !in.ProcessReady && in.EpochID == "" && in.HealthState == "stopped" && in.AccountingState == "settled"
	}
	return (in.State == "active" || in.State == "ready") && in.ProcessReady && in.EpochID != "" &&
		(in.HealthState == "healthy" || in.HealthState == "engine_ready") && in.AccountingState == "accepted"
}

func observationStateSHA(in ix.Observation) string {
	in.ObservedUnixNano, in.LeaseExpires = 0, 0
	return ix.Digest(in)
}

// Only unchanged freshness may bypass the writer. The first observation and
// every semantic transition still use the original FULL-durability transaction.
// Read-only validation here does not authorize a core; its independent Poll and
// Usage paths continue to require the durable accounting writer.
func (s *Store) refreshExecutionObservation(ctx context.Context, key []byte, in ix.Observation) (bool, error) {
	if !observationRefreshable(in) || len(key) != 32 || s.usage == nil || s.readOnly == nil {
		return false, nil
	}
	tx, err := s.readSnapshot(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback()
	enabled, err := executionSchemaPresent(ctx, tx)
	if err != nil {
		return true, err
	}
	if !enabled {
		return false, nil
	}
	var desired, observed uint64
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT desired_generation,observed_generation,observation_enc FROM inbound_execution_nodes WHERE node_id=? AND installation_id=?`, in.NodeID, in.InstallationID).Scan(&desired, &observed, &raw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if desired != in.Generation || observed != in.Generation || len(raw) == 0 {
		return false, nil
	}
	var old ix.Observation
	if openPlanValue(key, executionAAD("observation", in.NodeID+"/"+in.InstallationID, observed), raw, &old) != nil {
		return true, ErrUnavailable
	}
	old = freshExecutionObservation(ctx, raw, old)
	if in.ObservedUnixNano < old.ObservedUnixNano || in.ObservedUnixNano == old.ObservedUnixNano && ix.Digest(in) != ix.Digest(old) {
		return true, ErrConflict
	}
	if !observationRefreshable(old) || observationStateSHA(in) != observationStateSHA(old) {
		return false, nil
	}
	b, err := readExecutionBundle(ctx, tx, key, in.NodeID, in.InstallationID, in.Generation)
	if err != nil {
		return true, err
	}
	if b.OperationID != in.OperationID || b.ConfigSHA != in.ConfigSHA || b.ManifestSHA != in.ManifestSHA {
		return true, ErrConflict
	}
	if in.State == "stopped" {
		if len(b.Listeners) > 0 || len(b.Components) > 0 {
			return false, nil
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbound_execution_epochs WHERE node_id=? AND state!='sealed'`, in.NodeID).Scan(&n); err != nil {
			return true, err
		}
		if n != 0 {
			return true, ErrConflict
		}
	} else {
		now := time.Now()
		if in.LeaseExpires <= now.UnixNano() || in.LeaseExpires > now.Add(35*time.Second).UnixNano() {
			return true, ErrConflict
		}
		if err := executionBundleAuthority(ctx, tx, key, b); err != nil {
			return true, err
		}
		if _, err := executionAccountingFreshUntil(ctx, tx, in.NodeID, in.InstallationID, in.Generation, in.EpochID, now.UnixNano()); err != nil {
			return true, err
		}
	}
	if err := tx.Commit(); err != nil {
		return true, err
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	s.observationFreshness.put(raw, in)
	return true, nil
}
