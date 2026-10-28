//go:build linux

package backupacceptance_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"github.com/darktunnelmika/dark-xray-pro/internal/probackup"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type economicFixture struct {
	root, path string
	lock       *hubstate.Lock
	inventory  hubstate.Inventory
	token      string
	creates    []business.SellerClientCreateV1
	extends    []business.SellerClientExtendV1
	orders     []business.SellerOrderReceiptV1
	refund     business.VolumeExpiryRefundRequest
	refunded   business.VolumeExpiryRefundReceipt
	principal  usageledger.Principal
	report     uc.Report
	usage      uc.Receipt
	state      economicState
}

type economicState struct {
	Representatives []business.Representative
	Sellers         []business.SellerQuotaV2
	Clients         []business.Client
	Usage           []usageledger.Account
}

func require(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func captureEconomicState(t *testing.T, s *business.Store) economicState {
	t.Helper()
	ctx := context.Background()
	var state economicState
	for _, id := range []string{"backup-rep-a", "backup-rep-b"} {
		r, err := s.Representative(ctx, id)
		require(t, err)
		state.Representatives = append(state.Representatives, r)
	}
	for _, id := range []string{"seller-alice", "seller-bob"} {
		r, err := s.SellerV2(ctx, "backup-rep-a", id)
		require(t, err)
		state.Sellers = append(state.Sellers, r)
	}
	for _, id := range []string{"backup-volume", "backup-unlimited", "backup-unknown"} {
		c, err := s.Client(ctx, id)
		require(t, err)
		state.Clients = append(state.Clients, c)
		u, err := s.ClientUsage(ctx, id)
		require(t, err)
		state.Usage = append(state.Usage, u)
	}
	return state
}

func newEconomicFixture(t *testing.T) economicFixture {
	t.Helper()
	ctx := context.Background()
	f := economicFixture{root: t.TempDir()}
	require(t, os.Chmod(f.root, 0700))
	f.path = filepath.Join(f.root, "business.sqlite")
	var err error
	f.lock, err = hubstate.Acquire(f.root)
	require(t, err)
	t.Cleanup(func() { _ = f.lock.Close() })
	s, err := business.Open(f.path)
	require(t, err)
	t.Cleanup(func() { _ = s.Close() })
	const password = "synthetic-offline-backup-fixture-password"
	_, err = s.BootstrapOwner(ctx, "backup-owner", password)
	require(t, err)
	for _, id := range []string{"backup-rep-a", "backup-rep-b"} {
		_, err = s.CreateRepresentative(ctx, id, "Fixture "+id)
		require(t, err)
		credit := int64(1000)
		if id == "backup-rep-b" {
			credit = 9007199254741010 // after a 17-byte sale: exactly 2^53+1
		}
		_, err = s.AdjustCredits(ctx, business.CreditChange{EventID: "backup-credit-" + id, RepresentativeID: id, VolumeDeltaBytes: credit, UnlimitedDeltaUnits: 100, Reason: "Synthetic exact native credit"})
		require(t, err)
	}
	// A historical customer predating trusted usage seeding must remain unknown
	// after backup; restoring an absent meter as known zero would be unsafe.
	_, err = s.CreateClient(ctx, business.NewClient{EventID: "backup-unknown-create-0001", ClientID: "backup-unknown", RepresentativeID: "backup-rep-b", Name: "Unmeasured historical customer", DurationDays: 30, IPCount: 1, VolumeBytes: 17})
	require(t, err)
	for _, enable := range []func(context.Context) error{s.EnableUsageLedger, s.EnableSellerQuotaV2, s.EnableVolumeExpiryRefunds, s.EnableSellerOrdersV1, s.EnableInboundExecution} {
		require(t, enable(ctx))
	}
	_, err = s.ProvisionRepresentativeLogin(ctx, "backup-rep-a", "backup-representative", password)
	require(t, err)
	owner, err := s.Login(ctx, "backup-owner", password)
	require(t, err)
	_, err = s.UpdateSellerPermissionV2(ctx, owner.Token, business.SellerPermissionChangeV2{EventID: "backup-seller-permission-0001", RepresentativeID: "backup-rep-a", ManageSellers: true, Reason: "Synthetic scoped seller permission"})
	require(t, err)
	rep, err := s.Login(ctx, "backup-representative", password)
	require(t, err)
	f.token = rep.Token
	for i, id := range []string{"seller-alice", "seller-bob"} {
		_, err = s.CreateSellerV2As(ctx, rep.Token, business.CreateSellerV2{EventID: "backup-create-" + id, RepresentativeID: "backup-rep-a", SellerID: id, Name: id, TelegramUserID: int64(10001 + i)})
		require(t, err)
	}
	_, err = s.TransferSellerQuotaV2As(ctx, rep.Token, business.SellerQuotaTransferV2{EventID: "backup-reserve-alice-0001", RepresentativeID: "backup-rep-a", SellerID: "seller-alice", Action: "allocate", Bytes: 1000, Units: 100, Reason: "Allocate synthetic reserve"})
	require(t, err)
	f.creates = []business.SellerClientCreateV1{
		{EventID: "backup-order-volume-create", RepresentativeID: "backup-rep-a", SellerID: "seller-alice", ClientID: "backup-volume", Name: "Measured volume customer", DurationDays: 30, IPCount: 2, VolumeBytes: 100, Reason: "Synthetic volume purchase"},
		{EventID: "backup-order-unlimited-create", RepresentativeID: "backup-rep-a", SellerID: "seller-alice", ClientID: "backup-unlimited", Name: "Legacy formula customer", DurationDays: 60, IPCount: 3, Unlimited: true, Reason: "Synthetic unlimited purchase"},
	}
	for _, input := range f.creates {
		r, err := s.CreateSellerClientV1(ctx, rep.Token, input)
		require(t, err)
		f.orders = append(f.orders, r)
	}
	f.extends = []business.SellerClientExtendV1{
		{EventID: "backup-order-volume-renew", RepresentativeID: "backup-rep-a", SellerID: "seller-alice", ClientID: "backup-volume", AdditionalDays: 15, AdditionalVolumeBytes: 50, Reason: "Synthetic volume renewal"},
		{EventID: "backup-order-unlimited-renew-1", RepresentativeID: "backup-rep-a", SellerID: "seller-alice", ClientID: "backup-unlimited", AdditionalDays: 15, Reason: "Synthetic partial month one"},
		{EventID: "backup-order-unlimited-renew-2", RepresentativeID: "backup-rep-a", SellerID: "seller-alice", ClientID: "backup-unlimited", AdditionalDays: 15, Reason: "Synthetic partial month two"},
	}
	for _, input := range f.extends {
		r, err := s.ExtendSellerClientV1(ctx, rep.Token, input)
		require(t, err)
		f.orders = append(f.orders, r)
	}
	if f.orders[1].UnlimitedDebitUnits != 6 || f.orders[3].UnlimitedDebitUnits != 2 || f.orders[4].UnlimitedDebitUnits != 1 {
		t.Fatal("legacy cumulative unlimited charging formula changed")
	}
	c, err := s.Client(ctx, "backup-volume")
	require(t, err)
	h := sha256.Sum256([]byte("offline-backup-native-usage-fixture"))
	id := hex.EncodeToString(h[:])
	email, err := uc.CounterEmail(c.ID)
	require(t, err)
	epoch := uc.Epoch{ID: id[:32], NodeID: "backup-test-node", InstallationID: id[:32], ConfigSHA: id,
		KernelBootID: "00000000-0000-0000-0000-000000000001", PID: 123, ProcessStartTicks: 456,
		Bindings: []uc.Binding{{ClientID: c.ID, Email: email, Billable: true}}}
	require(t, s.UsageLedger().RegisterEpoch(ctx, epoch))
	box, err := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "synthetic-meter"), Key: bytes.Repeat([]byte{7}, 32), Epoch: epoch, Create: true})
	require(t, err)
	f.report, err = box.Observe([]uc.Usage{{ClientID: c.ID, Uplink: 13, Downlink: 27}}, true)
	require(t, err)
	require(t, box.Close())
	f.principal = usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}
	f.usage, err = s.UsageLedger().Accept(ctx, f.principal, f.report)
	require(t, err)
	_, err = s.SetClientState(ctx, business.SetClientState{EventID: "backup-manual-archive-0001", ClientID: c.ID, Action: "archive", Reason: "Synthetic unused-volume return"})
	require(t, err)
	f.refund = business.VolumeExpiryRefundRequest{ClientID: c.ID, ExpectedExpiresUnix: c.ExpiresUnix, ExpectedChargedVolumeBytes: c.ChargeVolumeBytes}
	returned, err := s.RefundExpiredVolume(ctx, nil, f.refund)
	require(t, err)
	if !returned.Applied || returned.Replayed || returned.Receipt == nil || returned.Receipt.RefundedBytes != 110 || returned.Receipt.RepresentativeID != "backup-rep-a" {
		t.Fatal("fixture refund did not return exactly the unused volume to the funding representative")
	}
	f.refunded = *returned.Receipt
	f.state = captureEconomicState(t, s)
	if f.state.Representatives[0].VolumeCreditBytes != 110 || f.state.Representatives[0].UnlimitedCreditUnits != 0 ||
		f.state.Sellers[0].ReservedBytes != 850 || f.state.Sellers[0].ReservedUnits != 91 ||
		f.state.Sellers[1].ReservedBytes != 0 || f.state.Sellers[1].ReservedUnits != 0 ||
		f.state.Representatives[1].VolumeCreditBytes != 9007199254740993 ||
		f.state.Usage[0].Total != 40 || !f.state.Usage[0].Known || f.state.Usage[2].Known || f.state.Usage[2].Eligible {
		t.Fatal("fixture did not establish exact independent balances and unknown historical usage")
	}
	require(t, s.Close())
	f.inventory, err = hubstate.BuildInventory(hubstate.Options{Root: f.root, Registry: filepath.Join(f.root, "registry.json"),
		Replacements: filepath.Join(f.root, "replacements.json"), BusinessDB: f.path, Listen: "127.0.0.1:2087", SourceRevision: strings.Repeat("a", 40),
		Environment: []string{"DARK_PRO_LAB_MODE=1", "DARK_PRO_ENABLE_BUSINESS_CORE=1", hubstate.EnableEnv + "=1", "DARK_PRO_LAB_TOKEN=" + strings.Repeat("b", 64),
			"DARK_PRO_ENABLE_USAGE_LEDGER=1", "DARK_PRO_ENABLE_SELLER_MANAGEMENT_V2=1", "DARK_PRO_ENABLE_VOLUME_REFUNDS_V1=1"}})
	require(t, err)
	require(t, hubstate.WriteInventory(f.lock, f.inventory))
	f.inventory, err = hubstate.LoadInventory(f.lock)
	require(t, err)
	return f
}

// Hash every value in every native table, not only selected API projections.
// Values stay typed (int64, blob, string, nil); there is no float64 conversion of
// credit quantities. Only hashes, table names and counts can reach test output.
func databaseRows(t *testing.T, path string) map[string]string {
	t.Helper()
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1&_pragma=query_only(1)&_pragma=trusted_schema(0)"}
	db, err := sql.Open("sqlite", u.String())
	require(t, err)
	defer db.Close()
	tables, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT GLOB 'sqlite_*' ORDER BY name`)
	require(t, err)
	var names []string
	for tables.Next() {
		var name string
		require(t, tables.Scan(&name))
		names = append(names, name)
	}
	require(t, tables.Err())
	require(t, tables.Close())
	out := map[string]string{}
	for _, name := range names {
		rows, err := db.Query(`SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`)
		require(t, err)
		columns, err := rows.Columns()
		require(t, err)
		var records []string
		for rows.Next() {
			values, pointers := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			require(t, rows.Scan(pointers...))
			raw, err := json.Marshal(values)
			require(t, err)
			records = append(records, string(raw))
		}
		require(t, rows.Err())
		require(t, rows.Close())
		sort.Strings(records)
		raw, err := json.Marshal(struct {
			Columns []string
			Records []string
		}{columns, records})
		require(t, err)
		h := sha256.Sum256(raw)
		out[name] = hex.EncodeToString(h[:])
	}
	return out
}

func assertRows(t *testing.T, want, got map[string]string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatal("native table inventory changed")
	}
	for name, hash := range want {
		if got[name] != hash {
			t.Fatal("native table values changed:", name)
		}
	}
}

func TestNativeSnapshotEncryptedRestorePreservesFullEconomicStateAndReplay(t *testing.T) {
	ctx := context.Background()
	f := newEconomicFixture(t)
	before, err := os.ReadFile(f.path)
	require(t, err)
	wantRows := databaseRows(t, f.path)
	snapshotDir := t.TempDir()
	require(t, os.Chmod(snapshotDir, 0700))
	sources, err := hubstate.Snapshot(ctx, f.lock, f.inventory, snapshotDir)
	require(t, err)
	after, err := os.ReadFile(f.path)
	require(t, err)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("consistent snapshot altered the offline source")
	}
	var dbSource string
	var snapshotInventory hubstate.Inventory
	files := make([]probackup.FileSource, 0, len(sources))
	for _, source := range sources {
		path := source.Path
		if source.Role == "business" {
			dbSource = path
		}
		if source.Role == "inventory" {
			raw, err := os.ReadFile(path)
			require(t, err)
			require(t, json.Unmarshal(raw, &snapshotInventory))
		}
		files = append(files, probackup.FileSource{Name: source.Name, Role: probackup.Role(source.Role), Size: source.Size,
			Open: func(context.Context) (io.ReadCloser, error) { return os.Open(path) }})
	}
	if dbSource == "" {
		t.Fatal("snapshot omitted business state")
	}
	if snapshotInventory.Root != f.inventory.Root || !reflect.DeepEqual(snapshotInventory.Environment, f.inventory.Environment) || len(snapshotInventory.SnapshotFiles) != len(sources)-1 {
		t.Fatal("snapshot did not bind the actual inventory and all files")
	}
	assertRows(t, wantRows, databaseRows(t, dbSource))
	validation, err := business.ValidateOfflineBackup(ctx, dbSource)
	require(t, err)
	identity, err := age.GenerateX25519Identity()
	require(t, err)
	var encrypted bytes.Buffer
	metadata := probackup.Metadata{BackupID: "native-economic-acceptance-v1", CreatedUnix: time.Now().Unix(), SourceRevision: f.inventory.SourceRevision,
		BusinessSchemaVersion: int64(validation.SchemaVersion), FeatureVersions: map[string]int{"seller_quota": 2, "seller_orders": 1, "volume_refunds": 1, "usage_ledger": 1}}
	manifest, err := probackup.Write(ctx, &encrypted, identity.Recipient().String(), metadata, files, probackup.DefaultLimits())
	require(t, err)
	verified, err := probackup.Verify(ctx, bytes.NewReader(encrypted.Bytes()), identity.String(), probackup.DefaultLimits())
	require(t, err)
	if !reflect.DeepEqual(manifest, verified) || bytes.Contains(encrypted.Bytes(), []byte(f.state.Clients[0].UUID)) || bytes.Contains(encrypted.Bytes(), []byte(f.token)) {
		t.Fatal("container metadata differed or private fixture state was exposed")
	}
	destination := filepath.Join(t.TempDir(), "restored-quarantine")
	validate := func(stage string, m probackup.Manifest) error {
		v, err := business.ValidateOfflineBackup(ctx, filepath.Join(stage, "data/business.sqlite"))
		if err != nil || !reflect.DeepEqual(v, validation) || m.Metadata.BusinessSchemaVersion != int64(v.SchemaVersion) {
			return errors.New("native snapshot compatibility mismatch")
		}
		if err := business.ValidateOfflineBackupAssets(ctx, filepath.Join(stage, "data/business.sqlite"), nil); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(stage, "inventory.json"))
		if err != nil {
			return err
		}
		var inv hubstate.Inventory
		if json.Unmarshal(raw, &inv) != nil || !reflect.DeepEqual(inv, snapshotInventory) {
			return errors.New("native inventory changed")
		}
		return hubstate.ValidateSnapshot(ctx, stage, inv)
	}
	restored, err := probackup.Restore(ctx, bytes.NewReader(encrypted.Bytes()), identity.String(), destination, probackup.DefaultLimits(), validate)
	require(t, err)
	if !reflect.DeepEqual(manifest, restored) {
		t.Fatal("restore changed authenticated manifest")
	}
	if _, err := hubstate.Acquire(destination); !errors.Is(err, hubstate.ErrQuarantined) {
		t.Fatal("restored quarantine admitted a HUB state owner", err)
	}
	if _, err := probackup.Restore(ctx, bytes.NewReader(encrypted.Bytes()), identity.String(), destination, probackup.DefaultLimits(), validate); !errors.Is(err, probackup.ErrDestination) {
		t.Fatal("restore overwrote a published quarantine", err)
	}
	restoredDB := filepath.Join(destination, "data/business.sqlite")
	assertRows(t, wantRows, databaseRows(t, restoredDB))
	wantBytes, err := os.ReadFile(dbSource)
	require(t, err)
	gotBytes, err := os.ReadFile(restoredDB)
	require(t, err)
	if !bytes.Equal(wantBytes, gotBytes) {
		t.Fatal("restored database is not the exact authenticated snapshot")
	}
	// Isolated library reopens exercise financial replay only. The quarantine
	// marker remains present and the real HUB startup fence was checked above.
	for restart := 0; restart < 2; restart++ {
		s, err := business.Open(restoredDB)
		require(t, err)
		if !reflect.DeepEqual(f.state, captureEconomicState(t, s)) {
			t.Fatal("UUID, expiry, limits, usage or balances changed on restored reopen")
		}
		var replayed []business.SellerOrderReceiptV1
		for _, input := range f.creates {
			r, err := s.CreateSellerClientV1(ctx, f.token, input)
			require(t, err)
			replayed = append(replayed, r)
		}
		for _, input := range f.extends {
			r, err := s.ExtendSellerClientV1(ctx, f.token, input)
			require(t, err)
			replayed = append(replayed, r)
		}
		for i, r := range replayed {
			if !r.Replayed {
				t.Fatal("purchase or renewal replay was applied twice")
			}
			r.Replayed = false
			if !reflect.DeepEqual(r, f.orders[i]) {
				t.Fatal("immutable purchase/renewal receipt changed")
			}
		}
		paid, err := s.RefundExpiredVolume(ctx, nil, f.refund)
		require(t, err)
		if paid.Applied || !paid.Replayed || paid.Receipt == nil || !reflect.DeepEqual(*paid.Receipt, f.refunded) {
			t.Fatal("refund replay credited again or lost its original immutable receipt")
		}
		usage, err := s.UsageLedger().Accept(ctx, f.principal, f.report)
		require(t, err)
		if usage != f.usage {
			t.Fatal("trusted finalized usage replay changed its receipt")
		}
		conflict := f.creates[0]
		conflict.VolumeBytes++
		if _, err := s.CreateSellerClientV1(ctx, f.token, conflict); !errors.Is(err, business.ErrConflict) {
			t.Fatal("restored provenance accepted a conflicting financial replay", err)
		}
		if !reflect.DeepEqual(f.state, captureEconomicState(t, s)) {
			t.Fatal("replay changed financial state, current client identity or measured usage")
		}
		require(t, s.Close())
		assertRows(t, wantRows, databaseRows(t, restoredDB))
	}
	// Historical policy still rejects direct mutation after restored reopen.
	u := url.URL{Scheme: "file", Path: restoredDB}
	db, err := sql.Open("sqlite", u.String())
	require(t, err)
	for _, table := range []string{"seller_order_receipts_v1", "volume_refund_receipts", "events", "client_operations", "seller_quota_events_v2", "pro_usage_receipts"} {
		if _, err := db.Exec(`DELETE FROM "` + table + `"`); err == nil {
			t.Fatal("restored immutable audit accepted deletion:", table)
		}
	}
	require(t, db.Close())
	assertRows(t, wantRows, databaseRows(t, restoredDB))
	if _, err := os.Stat(filepath.Join(destination, probackup.QuarantineName)); err != nil {
		t.Fatal("isolated replay removed the operational quarantine", err)
	}
}
