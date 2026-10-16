package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// EnableUsageLedger must run before serving requests. It never initializes
// existing clients to zero. Once its private schema exists, Store.Open reloads
// it automatically; omitting a startup flag cannot bypass an exhausted quota.
func (s *Store) EnableUsageLedger(ctx context.Context) error {
	if s.usage != nil {
		return nil
	}
	ledger, err := usageledger.Open(ctx, s.db)
	if err != nil {
		return err
	}
	s.usage = ledger
	return nil
}
func (s *Store) UsageLedger() *usageledger.Ledger { return s.usage }
func (s *Store) restoreUsageLedger(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_usage_*'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	return s.EnableUsageLedger(ctx)
}
func (s *Store) seedNewUsage(ctx context.Context, tx *sql.Tx, c Client) error {
	if s.usage == nil {
		return nil
	}
	sum := sha256.Sum256([]byte("new-native-usage:" + c.ID + ":" + c.UUID))
	return s.usage.InitializeAccountTx(ctx, tx, usageledger.Seed{ClientID: c.ID, EventID: "new-usage-" + hex.EncodeToString(sum[:]), Complete: true, Reason: "New PRO identity, before first activation"})
}

// Missing historical data is explicitly unknown/ineligible, never guessed zero.
func (s *Store) ClientUsage(ctx context.Context, id string) (usageledger.Account, error) {
	if s.usage == nil {
		return usageledger.Account{}, ErrUnavailable
	}
	c, err := s.Client(ctx, id)
	if err != nil {
		return usageledger.Account{}, err
	}
	a, err := s.usage.Account(ctx, id)
	if errors.Is(err, usageledger.ErrMissing) {
		return usageledger.Account{ClientID: c.ID, RepresentativeID: c.RepresentativeID, Plan: c.PlanKind, Limit: c.VolumeBytes, Known: false, Eligible: false}, nil
	}
	return a, err
}

// UsageBirth is measured by the trusted supervisor. There is deliberately NO
// public epoch/seed endpoint. A usage report cannot choose client mappings,
// decide whether bytes are billable, or declare its own known-zero baseline.
type UsageBirth struct {
	EpochID      string
	KernelBootID string
	PID          int
	StartTicks   uint64
}

func (s *Store) RegisterNativeUsageEpoch(ctx context.Context, c NodeConfig, birth UsageBirth) (uc.Epoch, error) {
	if s.usage == nil {
		return uc.Epoch{}, ErrUnavailable
	}
	digest := sha256.Sum256(c.Candidate.Config)
	if c.ConfigSHA != hex.EncodeToString(digest[:]) || c.ConfigSHA != c.Candidate.SHA256 || c.ManifestSHA != c.Candidate.ManifestSHA256 {
		return uc.Epoch{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return uc.Epoch{}, err
	}
	defer tx.Rollback()
	var seq, generation, currentGeneration uint64
	var sha, manifest string
	var expiry int64
	err = tx.QueryRowContext(ctx, "SELECT sequence,sha,manifest,generation,expires FROM native_runtime_delivery WHERE node_id=? AND installation_id=?", c.NodeID, c.InstallationID).Scan(&seq, &sha, &manifest, &generation, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return uc.Epoch{}, ErrNotFound
	}
	if err != nil {
		return uc.Epoch{}, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT value FROM native_config_generation WHERE id=1").Scan(&currentGeneration); err != nil {
		return uc.Epoch{}, err
	}
	if seq != c.Sequence || sha != c.ConfigSHA || manifest != c.ManifestSHA || generation != c.Generation || generation != currentGeneration || expiry <= time.Now().UnixNano() {
		return uc.Epoch{}, ErrConflict
	}
	var config struct {
		Inbounds []struct {
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					UUID  string `json:"id"`
					Email string `json:"email"`
				} `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(c.Candidate.Config, &config) != nil {
		return uc.Epoch{}, ErrInvalid
	}
	epoch := uc.Epoch{NodeID: c.NodeID, InstallationID: c.InstallationID, ID: birth.EpochID, ConfigSHA: c.ConfigSHA, KernelBootID: birth.KernelBootID, PID: birth.PID, ProcessStartTicks: birth.StartTicks}
	seen := map[string]string{}
	for _, in := range config.Inbounds {
		if in.Protocol != "vless" {
			return uc.Epoch{}, ErrInvalid
		}
		for _, u := range in.Settings.Clients {
			var id string
			if err = tx.QueryRowContext(ctx, "SELECT id FROM clients WHERE uuid=?", u.UUID).Scan(&id); err != nil {
				return uc.Epoch{}, ErrConflict
			}
			email, e := uc.CounterEmail(id)
			if e != nil || email != u.Email {
				return uc.Epoch{}, ErrConflict
			}
			if prior, exists := seen[id]; exists {
				if prior != u.Email {
					return uc.Epoch{}, ErrConflict
				}
				continue
			}
			seen[id] = u.Email
			// A native VLESS receiver TERMINATES customer traffic, including Swap
			// destinations. This does not label a separate transit server billable.
			epoch.Bindings = append(epoch.Bindings, uc.Binding{ClientID: id, Email: email, Billable: true})
		}
	}
	canonical, _, err := uc.CanonicalEpoch(epoch)
	if err != nil {
		return uc.Epoch{}, err
	}
	if err = s.usage.RegisterEpochTx(ctx, tx, canonical); err != nil {
		return uc.Epoch{}, err
	}
	if err = tx.Commit(); err != nil {
		return uc.Epoch{}, err
	}
	return canonical, nil
}
