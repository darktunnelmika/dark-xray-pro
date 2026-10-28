package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Seller orders use the existing reserved quota and native client/usage store.
// This core has no startup, HTTP, Telegram, payment or runtime activation wiring.
type SellerClientCreateV1 struct {
	EventID          string `json:"event_id"`
	RepresentativeID string `json:"representative_id"`
	SellerID         string `json:"seller_id"`
	ClientID         string `json:"client_id"`
	Name             string `json:"name"`
	DurationDays     int64  `json:"duration_days"`
	IPCount          int64  `json:"ip_count"`
	VolumeBytes      int64  `json:"volume_bytes,string"`
	Unlimited        bool   `json:"unlimited"`
	Reason           string `json:"reason"`
}

type SellerClientExtendV1 struct {
	EventID               string `json:"event_id"`
	RepresentativeID      string `json:"representative_id"`
	SellerID              string `json:"seller_id"`
	ClientID              string `json:"client_id"`
	AdditionalDays        int64  `json:"additional_days"`
	AdditionalVolumeBytes int64  `json:"additional_volume_bytes,string"`
	Reason                string `json:"reason"`
}

// The receipt is an immutable snapshot, not a fresh client or balance read.
// All potentially large quantities remain exact in JSON/browser consumers.
type SellerOrderClientV1 struct {
	ID                    string `json:"id"`
	RepresentativeID      string `json:"representative_id"`
	Name                  string `json:"name"`
	UUID                  string `json:"uuid"`
	PlanKind              string `json:"plan_kind"`
	IPCount               int64  `json:"ip_count"`
	VolumeBytes           int64  `json:"volume_bytes,string"`
	DurationDays          int64  `json:"duration_days"`
	ChargedVolumeBytes    int64  `json:"charged_volume_bytes,string"`
	ChargedUnlimitedUnits int64  `json:"charged_unlimited_units,string"`
	ExpiresUnix           int64  `json:"expires_unix"`
	CreatedUnix           int64  `json:"created_unix"`
	DeploymentState       string `json:"deployment_state"`
	Archived              bool   `json:"archived"`
}

type SellerOrderReceiptV1 struct {
	EventID             string               `json:"event_id"`
	ActorUserID         string               `json:"actor_user_id"`
	RepresentativeID    string               `json:"representative_id"`
	SellerID            string               `json:"seller_id"`
	Action              string               `json:"action"`
	Client              SellerOrderClientV1  `json:"client"`
	PreviousClient      *SellerOrderClientV1 `json:"previous_client,omitempty"`
	VolumeDebitBytes    int64                `json:"volume_debit_bytes,string"`
	UnlimitedDebitUnits int64                `json:"unlimited_debit_units,string"`
	ReservedBytesBefore int64                `json:"reserved_bytes_before,string"`
	ReservedBytesAfter  int64                `json:"reserved_bytes_after,string"`
	ReservedUnitsBefore int64                `json:"reserved_units_before,string"`
	ReservedUnitsAfter  int64                `json:"reserved_units_after,string"`
	CreatedUnix         int64                `json:"created_unix"`
	Replayed            bool                 `json:"replayed"`
}

type sellerOrderIntentV1 struct {
	Create *SellerClientCreateV1 `json:"create,omitempty"`
	Extend *SellerClientExtendV1 `json:"extend,omitempty"`
}

type sellerOrderIdentityV1 struct{ event, representative, seller, client, action, reason string }

func (i sellerOrderIntentV1) identity() (sellerOrderIdentityV1, error) {
	if (i.Create == nil) == (i.Extend == nil) {
		return sellerOrderIdentityV1{}, ErrInvalid
	}
	var k sellerOrderIdentityV1
	if c := i.Create; c != nil {
		if !validClient(NewClient{EventID: c.EventID, ClientID: c.ClientID, RepresentativeID: c.RepresentativeID,
			Name: c.Name, DurationDays: c.DurationDays, IPCount: c.IPCount, VolumeBytes: c.VolumeBytes, Unlimited: c.Unlimited}) {
			return k, ErrInvalid
		}
		k = sellerOrderIdentityV1{c.EventID, c.RepresentativeID, c.SellerID, c.ClientID, "create", c.Reason}
	} else {
		e := i.Extend
		if !positiveAdditional(ExtendClient{EventID: e.EventID, ClientID: e.ClientID, AdditionalDays: e.AdditionalDays,
			AdditionalVolumeBytes: e.AdditionalVolumeBytes, Reason: e.Reason}) {
			return k, ErrInvalid
		}
		k = sellerOrderIdentityV1{e.EventID, e.RepresentativeID, e.SellerID, e.ClientID, "extend", e.Reason}
	}
	if !idRE.MatchString(k.representative) || k.representative == "owner" || !idRE.MatchString(k.seller) || !validName(k.reason, 200) {
		return sellerOrderIdentityV1{}, ErrInvalid
	}
	return k, nil
}

func sellerOrderFingerprintV1(actor string, intent sellerOrderIntentV1) string {
	return fingerprint("seller-order-v1", struct {
		Actor  string
		Intent sellerOrderIntentV1
	}{actor, intent})
}

var sellerOrderSchemaV1 = []string{
	`CREATE TABLE seller_order_meta_v1(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`CREATE TABLE seller_order_clients_v1(
 client_id TEXT PRIMARY KEY REFERENCES clients(id), representative_id TEXT NOT NULL,
 seller_id TEXT NOT NULL, client_uuid TEXT NOT NULL UNIQUE REFERENCES clients(uuid),
 create_event_id TEXT NOT NULL UNIQUE REFERENCES seller_order_receipts_v1(event_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(representative_id,seller_id) REFERENCES representative_sellers_v2(representative_id,id))`,
	`CREATE TABLE seller_order_receipts_v1(
 event_id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),
 actor_user_id TEXT NOT NULL REFERENCES auth_users(id),
 representative_id TEXT NOT NULL, seller_id TEXT NOT NULL,
 client_id TEXT NOT NULL REFERENCES seller_order_clients_v1(client_id),
 action TEXT NOT NULL CHECK(action IN('create','extend')),
 volume_debit_bytes INTEGER NOT NULL CHECK(typeof(volume_debit_bytes)='integer' AND volume_debit_bytes BETWEEN 0 AND 1000000000000000000),
 unlimited_debit_units INTEGER NOT NULL CHECK(typeof(unlimited_debit_units)='integer' AND unlimited_debit_units BETWEEN 0 AND 1000000000),
 reserved_bytes_before INTEGER NOT NULL CHECK(typeof(reserved_bytes_before)='integer' AND reserved_bytes_before BETWEEN 0 AND 1000000000000000000),
 reserved_bytes_after INTEGER NOT NULL CHECK(typeof(reserved_bytes_after)='integer' AND reserved_bytes_after BETWEEN 0 AND 1000000000000000000),
 reserved_units_before INTEGER NOT NULL CHECK(typeof(reserved_units_before)='integer' AND reserved_units_before BETWEEN 0 AND 1000000000),
 reserved_units_after INTEGER NOT NULL CHECK(typeof(reserved_units_after)='integer' AND reserved_units_after BETWEEN 0 AND 1000000000),
 request_json BLOB NOT NULL, receipt_json BLOB NOT NULL, created_unix INTEGER NOT NULL,
 CHECK(reserved_bytes_after=reserved_bytes_before-volume_debit_bytes),
 CHECK(reserved_units_after=reserved_units_before-unlimited_debit_units),
 CHECK(volume_debit_bytes=0 OR unlimited_debit_units=0),
 FOREIGN KEY(representative_id,seller_id) REFERENCES representative_sellers_v2(representative_id,id))`,
	`CREATE UNIQUE INDEX seller_order_creation_once_v1 ON seller_order_receipts_v1(client_id) WHERE action='create'`,
	`CREATE INDEX seller_order_client_history_v1 ON seller_order_receipts_v1(client_id,event_id)`,
	`CREATE TRIGGER seller_order_clients_no_update_v1 BEFORE UPDATE ON seller_order_clients_v1 BEGIN SELECT RAISE(ABORT,'seller client funding is immutable'); END`,
	`CREATE TRIGGER seller_order_clients_no_delete_v1 BEFORE DELETE ON seller_order_clients_v1 BEGIN SELECT RAISE(ABORT,'seller client funding is immutable'); END`,
	`CREATE TRIGGER seller_order_receipts_no_update_v1 BEFORE UPDATE ON seller_order_receipts_v1 BEGIN SELECT RAISE(ABORT,'seller orders are immutable'); END`,
	`CREATE TRIGGER seller_order_receipts_no_delete_v1 BEFORE DELETE ON seller_order_receipts_v1 BEGIN SELECT RAISE(ABORT,'seller orders are immutable'); END`,
	`CREATE TRIGGER seller_order_meta_no_update_v1 BEFORE UPDATE ON seller_order_meta_v1 BEGIN SELECT RAISE(ABORT,'seller order policy is immutable'); END`,
	`CREATE TRIGGER seller_order_meta_no_delete_v1 BEFORE DELETE ON seller_order_meta_v1 BEGIN SELECT RAISE(ABORT,'seller order policy is immutable'); END`,
}

// EnableSellerOrdersV1 is an explicit local migration, never called by startup.
// Existing provenance remains readable after reopen without enabling any API.
func (s *Store) EnableSellerOrdersV1(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = sellerManagementConfiguredV2(ctx, tx); err != nil {
		return err
	}
	present, err := sellerOrderSchemaStateV1(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return tx.Commit()
	}
	for _, ddl := range sellerOrderSchemaV1 {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO seller_order_meta_v1(id,version) VALUES(1,1)`); err != nil {
		return err
	}
	return tx.Commit()
}

func sellerOrderSchemaStateV1(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'seller_order_*'`).Scan(&n); err != nil {
		return false, ErrUnavailable
	}
	if n == 0 {
		return false, nil
	}
	if n != len(sellerOrderSchemaV1) {
		return false, ErrUnavailable
	}
	var version int
	if err := q.QueryRowContext(ctx, `SELECT version FROM seller_order_meta_v1 WHERE id=1`).Scan(&version); err != nil || version != 1 {
		return false, ErrUnavailable
	}
	for _, ddl := range sellerOrderSchemaV1 {
		fields := strings.Fields(ddl)
		at := 2
		if fields[1] == "UNIQUE" {
			at = 3
		}
		name := strings.SplitN(fields[at], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&actual); err != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(fields, " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

// Old paths retain their behavior when no seller-order schema exists. With it,
// a claimed order nonce cannot become a separate credit or seller mutation.
func rejectSellerOrderEventV1(ctx context.Context, tx *sql.Tx, eventID string) error {
	present, err := sellerOrderSchemaStateV1(ctx, tx)
	if err != nil || !present {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM seller_order_receipts_v1 WHERE event_id=?`, eventID).Scan(&n); err != nil {
		return ErrUnavailable
	}
	if n != 0 {
		return ErrConflict
	}
	return nil
}

func newSellerOrderEventV1(ctx context.Context, tx *sql.Tx, eventID string) error {
	for _, table := range []string{"events", "client_operations", "client_profile_edits", "representative_control_events", "catalog_events", "catalog_policy_events", "seller_quota_events_v2", "representative_seller_permission_events_v2", "seller_state_events_v2"} {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE event_id=?", eventID).Scan(&n); err != nil {
			return ErrUnavailable
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return nil
}

func authorizeSellerOrderV1(ctx context.Context, tx *sql.Tx, token string, k sellerOrderIdentityV1) (string, SellerQuotaV2, error) {
	actor, err := authorizeSellerAccessV2(ctx, tx, token, k.representative, true, false)
	if err != nil {
		return "", SellerQuotaV2{}, err
	}
	var role string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM auth_users WHERE id=?`, actor).Scan(&role); err != nil {
		return "", SellerQuotaV2{}, ErrUnavailable
	}
	if role != "owner" {
		control, e := readRepresentativeControl(ctx, tx, k.representative)
		if e != nil {
			return "", SellerQuotaV2{}, e
		}
		if control.LoginSuspended || k.action == "create" && !control.Permissions.CreateClients || k.action == "extend" && !control.Permissions.RenewClients {
			return "", SellerQuotaV2{}, ErrSellerPermissionDenied
		}
	}
	rep, seller, err := sellerCurrentV2(ctx, tx, k.representative, k.seller)
	if err != nil {
		return "", SellerQuotaV2{}, err
	}
	if rep.Kind != "representative" || !rep.Enabled || !seller.Enabled {
		return "", SellerQuotaV2{}, ErrSellerPermissionDenied
	}
	return actor, seller, nil
}

func sellerOrderClientSnapshotV1(c Client) SellerOrderClientV1 {
	return SellerOrderClientV1{c.ID, c.RepresentativeID, c.Name, c.UUID, c.PlanKind, c.IPCount, c.VolumeBytes, c.DurationDays,
		c.ChargeVolumeBytes, c.ChargeUnlimitedUnits, c.ExpiresUnix, c.CreatedUnix, c.DeploymentState, c.Archived}
}

func (s *Store) CreateSellerClientV1(ctx context.Context, token string, req SellerClientCreateV1) (SellerOrderReceiptV1, error) {
	return s.executeSellerOrderV1(ctx, token, sellerOrderIntentV1{Create: &req})
}
func (s *Store) ExtendSellerClientV1(ctx context.Context, token string, req SellerClientExtendV1) (SellerOrderReceiptV1, error) {
	return s.executeSellerOrderV1(ctx, token, sellerOrderIntentV1{Extend: &req})
}

func (s *Store) executeSellerOrderV1(ctx context.Context, token string, intent sellerOrderIntentV1) (SellerOrderReceiptV1, error) {
	var out SellerOrderReceiptV1
	k, err := intent.identity()
	if err != nil {
		return out, err
	}
	if s.usage == nil {
		return out, ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	present, err := sellerOrderSchemaStateV1(ctx, tx)
	if err != nil || !present {
		return out, ErrUnavailable
	}
	actor, seller, err := authorizeSellerOrderV1(ctx, tx, token, k)
	if err != nil {
		return out, err
	}
	hash := sellerOrderFingerprintV1(actor, intent)
	record, err := readSellerOrderRecordV1(ctx, tx, k.event)
	if err == nil {
		if record.fingerprint != hash {
			return out, ErrConflict
		}
		if _, err = verifiedSellerOrderV1(ctx, tx, record); err != nil {
			return out, err
		}
		current, err := readClient(ctx, tx, k.client)
		if err != nil {
			return out, ErrUnavailable
		}
		if _, _, err = sellerClientFundingV1(ctx, tx, current); err != nil {
			return out, err
		}
		record.receipt.Replayed = true
		return record.receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err = newSellerOrderEventV1(ctx, tx, k.event); err != nil {
		return out, err
	}
	now := time.Now().UTC().Unix()
	var c Client
	var bytes, units int64
	var previous *SellerOrderClientV1
	if req := intent.Create; req != nil {
		if _, e := readClient(ctx, tx, k.client); e == nil {
			return out, ErrConflict
		} else if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		plan := "volume"
		bytes = req.VolumeBytes
		if req.Unlimited {
			plan = "unlimited"
			units = (req.DurationDays*req.IPCount + 29) / 30
		}
		uuid, e := randomUUID()
		if e != nil {
			return out, e
		}
		c = Client{ID: k.client, RepresentativeID: k.representative, Name: req.Name, UUID: uuid, PlanKind: plan, IPCount: req.IPCount,
			VolumeBytes: req.VolumeBytes, DurationDays: req.DurationDays, ChargeVolumeBytes: bytes, ChargeUnlimitedUnits: units,
			CreatedUnix: now, ExpiresUnix: now + req.DurationDays*86400, DeploymentState: "pending_node_apply"}
	} else {
		c, err = readClient(ctx, tx, k.client)
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		if err != nil {
			return out, err
		}
		if c.RepresentativeID != k.representative {
			return out, ErrSellerPermissionDenied
		}
		if err = verifySellerClientBindingV1(ctx, tx, c, k.seller); err != nil {
			return out, err
		}
		// Verify provenance before accepting more money against a damaged identity.
		if _, _, err = sellerClientFundingV1(ctx, tx, c); err != nil {
			return out, err
		}
		before := sellerOrderClientSnapshotV1(c)
		previous = &before
		change, e := calculateBulkExtension(c, intent.Extend.AdditionalDays, intent.Extend.AdditionalVolumeBytes, now)
		if e != nil {
			return out, e
		}
		c, bytes, units = change.Client, change.VolumeCost, change.UnitsCost
	}
	if seller.ReservedBytes < bytes || seller.ReservedUnits < units {
		return out, ErrInsufficient
	}
	out = SellerOrderReceiptV1{EventID: k.event, ActorUserID: actor, RepresentativeID: k.representative, SellerID: k.seller, Action: k.action,
		Client: sellerOrderClientSnapshotV1(c), PreviousClient: previous, VolumeDebitBytes: bytes, UnlimitedDebitUnits: units,
		ReservedBytesBefore: seller.ReservedBytes, ReservedBytesAfter: seller.ReservedBytes - bytes,
		ReservedUnitsBefore: seller.ReservedUnits, ReservedUnitsAfter: seller.ReservedUnits - units, CreatedUnix: now}
	if _, err = tx.ExecContext(ctx, `UPDATE representative_sellers_v2 SET reserved_bytes=?,reserved_units=? WHERE representative_id=? AND id=?`,
		out.ReservedBytesAfter, out.ReservedUnitsAfter, k.representative, k.seller); err != nil {
		return SellerOrderReceiptV1{}, err
	}
	if intent.Create != nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO clients("+clientColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			c.ID, c.RepresentativeID, c.Name, c.UUID, c.PlanKind, c.IPCount, c.VolumeBytes, c.DurationDays, c.ChargeVolumeBytes, c.ChargeUnlimitedUnits, c.CreatedUnix, c.ExpiresUnix, c.DeploymentState, 0)
		if err != nil {
			return SellerOrderReceiptV1{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO seller_order_clients_v1(client_id,representative_id,seller_id,client_uuid,create_event_id) VALUES(?,?,?,?,?)`, c.ID, c.RepresentativeID, k.seller, c.UUID, k.event); err != nil {
			return SellerOrderReceiptV1{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(event_id,fingerprint,kind,representative_id,object_id,reason,volume_delta_bytes,unlimited_delta_units,created_unix)
 VALUES(?,?,'client_created',?,?,?,0,0,?)`, k.event, hash, k.representative, k.client, k.reason, now); err != nil {
			return SellerOrderReceiptV1{}, err
		}
		if err = s.seedNewUsage(ctx, tx, c); err != nil {
			return SellerOrderReceiptV1{}, err
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE clients SET duration_days=?,volume_bytes=?,charged_volume_bytes=?,charged_unlimited_units=?,expires_unix=? WHERE id=?`,
			c.DurationDays, c.VolumeBytes, c.ChargeVolumeBytes, c.ChargeUnlimitedUnits, c.ExpiresUnix, c.ID); err != nil {
			return SellerOrderReceiptV1{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO client_operations(event_id,fingerprint,client_id,representative_id,kind,reason,added_days,added_volume_bytes,volume_credit_debit,unlimited_credit_debit,created_unix)
 VALUES(?,?,?,?,'extend',?,?,?,0,0,?)`, k.event, hash, k.client, k.representative, k.reason, intent.Extend.AdditionalDays, intent.Extend.AdditionalVolumeBytes, now); err != nil {
			return SellerOrderReceiptV1{}, err
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return SellerOrderReceiptV1{}, err
	}
	request, err := json.Marshal(intent)
	if err != nil {
		return SellerOrderReceiptV1{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO seller_order_receipts_v1(event_id,fingerprint,actor_user_id,representative_id,seller_id,client_id,action,
 volume_debit_bytes,unlimited_debit_units,reserved_bytes_before,reserved_bytes_after,reserved_units_before,reserved_units_after,request_json,receipt_json,created_unix)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, k.event, hash, actor, k.representative, k.seller, k.client, k.action, bytes, units,
		out.ReservedBytesBefore, out.ReservedBytesAfter, out.ReservedUnitsBefore, out.ReservedUnitsAfter, request, raw, now); err != nil {
		return SellerOrderReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return SellerOrderReceiptV1{}, err
	}
	return out, nil
}
