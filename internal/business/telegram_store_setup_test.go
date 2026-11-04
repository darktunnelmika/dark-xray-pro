package business

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func telegramSetupEmptyFixture(t *testing.T) (*Store, LoginSession, TelegramStore) {
	t.Helper()
	ctx := context.Background()
	s, _ := opened(t)
	if _, err := s.BootstrapOwner(ctx, "setupowner", "synthetic-setup-owner-password"); err != nil {
		t.Fatal(err)
	}
	owner, err := s.Login(ctx, "setupowner", "synthetic-setup-owner-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableTelegramCommerce(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := s.ConfigureTelegramStore(ctx, owner.Token, TelegramStoreRegistration{
		EventID: "setup-owner-register-0001", ID: "setup-owner", BotID: 500, RepresentativeID: "owner",
		Name: "Setup owner", ManagerUserID: 5001, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, owner, store
}

func telegramSetupMap(t *testing.T, s *Store, token string) map[string]TelegramStoreSetupSnapshot {
	t.Helper()
	items, err := s.ListTelegramStoreSetupAs(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]TelegramStoreSetupSnapshot, len(items))
	previous := ""
	for _, item := range items {
		if item.ID <= previous {
			t.Fatal("store snapshot lost its unique ordering", item.ID)
		}
		previous = item.ID
		out[item.ID] = item
	}
	return out
}

func telegramSetupRepLogin(t *testing.T, s *Store, id string) LoginSession {
	t.Helper()
	ctx := context.Background()
	if _, err := s.ProvisionRepresentativeLogin(ctx, id, id, "synthetic-setup-representative-password"); err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, id, "synthetic-setup-representative-password")
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestTelegramStoreSetupScopedCatalogAndManagerFacts(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	newRep(t, f.s, "tg-rep-b")
	register := func(id string, bot int64, rep string) TelegramStore {
		t.Helper()
		store, err := f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{
			EventID: "setup-register-" + id, ID: id, BotID: bot, RepresentativeID: rep,
			Name: id, ManagerUserID: f.manager.UserID, Enabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	shared := register("shop-a-second", 101, f.store.RepresentativeID)
	other := register("shop-b", 102, "tg-rep-b")
	ownerShop := register("shop-owner", 103, "owner")

	unpriced := f.product.CatalogSpec
	unpriced.ID = "setup-unpriced"
	tgCommercePublish(t, f.s, unpriced)
	draft := unpriced
	draft.ID = "setup-draft"
	if _, err := f.s.CreateCatalogProduct(ctx, CatalogCreate{CatalogSpec: draft, EventID: "setup-draft-create-0001", Reason: "Synthetic draft"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetTelegramProductPrice(ctx, f.manager, TelegramProductPrice{EventID: "setup-draft-price-0001", ProductID: draft.ID, PriceStars: 8}); err != nil {
		t.Fatal(err)
	}
	archived := unpriced
	archived.ID = "setup-archived"
	archivedProduct := tgCommercePublish(t, f.s, archived)
	if _, err := f.s.SetTelegramProductPrice(ctx, f.manager, TelegramProductPrice{EventID: "setup-archived-price-0001", ProductID: archived.ID, PriceStars: 9}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpdateCatalogProduct(ctx, CatalogUpdate{CatalogSpec: archived, EventID: "setup-archive-0001", ExpectedRevision: archivedProduct.Revision, Status: "archived", Reason: "Synthetic archived offer"}); err != nil {
		t.Fatal(err)
	}
	foreign := unpriced
	foreign.ID, foreign.RepresentativeID = "setup-foreign", other.RepresentativeID
	tgCommercePublish(t, f.s, foreign)
	ownerProduct := unpriced
	ownerProduct.ID, ownerProduct.RepresentativeID = "setup-owner-product", "owner"
	tgCommercePublish(t, f.s, ownerProduct)
	// A structurally valid SQLite price from another native account must not
	// enter a storefront's setup counts, even if such legacy data is present.
	if _, err := f.s.db.Exec(`INSERT INTO telegram_commerce_prices(store_id,product_id,amount_stars,revision) VALUES(?,?,77,1)`, shared.ID, foreign.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetTelegramTrialPolicy(ctx, f.manager, TelegramTrialPolicyChange{EventID: "setup-trial-policy-0001", Enabled: true, VolumeBytes: 200}); err != nil {
		t.Fatal(err)
	}

	all := telegramSetupMap(t, f.s, f.owner.Token)
	if len(all) != 4 {
		t.Fatal("owner did not receive all store snapshots", len(all))
	}
	a, second, b, own := all[f.store.ID].Setup, all[shared.ID].Setup, all[other.ID].Setup, all[ownerShop.ID].Setup
	if a.PublishedProducts != 2 || a.PricedProducts != 1 || !a.ManagerRegistered || !a.TrialEnabled {
		t.Fatal("first brand lost its own catalog or saved settings", a)
	}
	if second.PublishedProducts != 2 || second.PricedProducts != 0 || second.ManagerRegistered || second.TrialEnabled {
		t.Fatal("shared native account merged brand-specific price, manager or trial state", second)
	}
	if b.PublishedProducts != 1 || b.PricedProducts != 0 || b.ManagerRegistered || b.TrialEnabled {
		t.Fatal("other representative acquired foreign setup state", b)
	}
	if own.PublishedProducts != 1 || own.PricedProducts != 0 || !own.AccountEnabled || !own.CanCreateClients || !own.CanRenewClients {
		t.Fatal("owner catalog or owner permission semantics changed", own)
	}
	legacy, err := f.s.ListTelegramStoresAs(ctx, f.owner.Token)
	if err != nil || len(legacy) != len(all) {
		t.Fatal("legacy connection-guard listing changed", legacy, err)
	}
	for _, store := range legacy {
		if all[store.ID].TelegramStore != store {
			t.Fatal("metadata and setup are not the same store snapshot", store.ID)
		}
	}
	loginA := telegramSetupRepLogin(t, f.s, f.store.RepresentativeID)
	loginB := telegramSetupRepLogin(t, f.s, other.RepresentativeID)
	visibleA, visibleB := telegramSetupMap(t, f.s, loginA.Token), telegramSetupMap(t, f.s, loginB.Token)
	if len(visibleA) != 2 || visibleA[f.store.ID].Setup != a || visibleA[shared.ID].Setup != second || len(visibleB) != 1 || visibleB[other.ID].Setup != b {
		t.Fatal("representative setup scope escaped its native account", visibleA, visibleB)
	}

	sharedActor := TelegramActor{BotID: shared.BotID, UserID: shared.ManagerUserID, ChatID: shared.ManagerUserID}
	if _, err = f.s.TelegramStart(ctx, sharedActor); err != nil {
		t.Fatal(err)
	}
	if !telegramSetupMap(t, f.s, f.owner.Token)[shared.ID].Setup.ManagerRegistered {
		t.Fatal("authenticated manager registration not observed")
	}
	if _, err = f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "setup-replace-manager-0001", ID: shared.ID, BotID: shared.BotID, RepresentativeID: shared.RepresentativeID, Name: shared.Name, ManagerUserID: f.buyer.UserID, Enabled: true, ExpectedRevision: shared.Revision}); err != nil {
		t.Fatal(err)
	}
	if telegramSetupMap(t, f.s, f.owner.Token)[shared.ID].Setup.ManagerRegistered {
		t.Fatal("prior manager or registration in another bot qualified the current manager")
	}
}

func TestTelegramStoreSetupNativeAccountAndCurrentGrants(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	login := telegramSetupRepLogin(t, f.s, f.store.RepresentativeID)
	initial := telegramSetupMap(t, f.s, login.Token)[f.store.ID].Setup
	if !initial.AccountEnabled || !initial.CanCreateClients || !initial.CanRenewClients {
		t.Fatal("missing control row lost native default grants", initial)
	}
	permissions := DefaultRepresentativePermissions()
	permissions.CreateClients, permissions.ViewFinance = false, false
	control, err := f.s.UpdateRepresentativeControl(ctx, RepresentativeControlChange{
		EventID: "setup-restrict-representative-0001", RepresentativeID: f.store.RepresentativeID,
		DisplayName: "Restricted seller", Permissions: permissions, Reason: "Synthetic current grants",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ListTelegramStoreSetupAs(ctx, login.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("revoked session retained setup access", err)
	}
	// Explicit controls are authoritative; their legacy auth_users.disabled
	// compatibility flag must not block a new, otherwise valid V9 session.
	login, err = f.s.Login(ctx, f.store.RepresentativeID, "synthetic-setup-representative-password")
	if err != nil {
		t.Fatal(err)
	}
	current := telegramSetupMap(t, f.s, login.Token)[f.store.ID].Setup
	if !current.AccountEnabled || current.CanCreateClients || !current.CanRenewClients {
		t.Fatal("setup did not reflect the current native grants", current)
	}
	control, err = f.s.UpdateRepresentativeControl(ctx, RepresentativeControlChange{
		EventID: "setup-suspend-representative-0001", RepresentativeID: f.store.RepresentativeID,
		DisplayName: "Restricted seller", Permissions: permissions, LoginSuspended: true,
		ExpectedRevision: control.Control.Revision, Reason: "Synthetic suspended account",
	})
	if err != nil {
		t.Fatal(err)
	}
	blocked := telegramSetupMap(t, f.s, f.owner.Token)[f.store.ID]
	if !blocked.Enabled || blocked.Setup.AccountEnabled || blocked.Setup.CanCreateClients || blocked.Setup.CanRenewClients {
		t.Fatal("store flag concealed native suspension", blocked)
	}
	if _, err = f.s.ListTelegramStoreSetupAs(ctx, login.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("suspended representative read setup", err)
	}
	if _, err = f.s.UpdateRepresentativeControl(ctx, RepresentativeControlChange{
		EventID: "setup-unsuspend-representative-0001", RepresentativeID: f.store.RepresentativeID,
		DisplayName: "Restricted seller", Permissions: permissions,
		ExpectedRevision: control.Control.Revision, Reason: "Synthetic account re-enable",
	}); err != nil {
		t.Fatal(err)
	}
	login, err = f.s.Login(ctx, f.store.RepresentativeID, "synthetic-setup-representative-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`UPDATE representatives SET enabled=0 WHERE id=?`, f.store.RepresentativeID); err != nil {
		t.Fatal(err)
	}
	blocked = telegramSetupMap(t, f.s, f.owner.Token)[f.store.ID]
	if blocked.Setup.AccountEnabled || blocked.Setup.CanCreateClients || blocked.Setup.CanRenewClients {
		t.Fatal("disabled native account retained effective sales permissions", blocked.Setup)
	}
	if _, err = f.s.ListTelegramStoreSetupAs(ctx, login.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("disabled native account retained its saved browser session", err)
	}
}

func TestTelegramStoreSetupReadOnlyDefaults(t *testing.T) {
	ctx := context.Background()
	s, owner, store := telegramSetupEmptyFixture(t)
	var changesBefore, changesAfter int64
	if err := s.db.QueryRow(`SELECT total_changes()`).Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	defer s.db.Exec(`PRAGMA query_only=OFF`)
	items := telegramSetupMap(t, s, owner.Token)
	want := TelegramStoreSetup{AccountEnabled: true, CanCreateClients: true, CanRenewClients: true,
		CatalogLimit: 200, TrialDurationHours: 24, TrialStartsAt: "issuance"}
	if len(items) != 1 || items[store.ID].TelegramStore != store || items[store.ID].Setup != want {
		t.Fatal("new store acquired invented setup state", items)
	}
	if err := s.db.QueryRow(`SELECT total_changes()`).Scan(&changesAfter); err != nil || changesAfter != changesBefore {
		t.Fatal("setup read changed the native database", changesBefore, changesAfter, err)
	}
	for _, table := range []string{"telegram_commerce_customers", "telegram_commerce_prices", "telegram_commerce_trial_policy", "telegram_commerce_trials", "telegram_commerce_orders", "telegram_commerce_payments", "telegram_commerce_wallet_ledger"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("setup read initialized customer, policy or financial state", table, count, err)
		}
	}
	if _, err := s.ListTelegramStoreSetupAs(ctx, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("anonymous setup read", err)
	}
	if _, err := s.ListTelegramStoreSetupAs(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("unknown owner session accepted", err)
	}
}

func TestTelegramStoreSetupNoImplicitSchemaOrUnavailableFallback(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	if _, err := s.ListTelegramStoreSetupAs(ctx, ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing commerce schema treated as empty setup", err)
	}
	var tables int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'telegram_commerce_*'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatal("read migrated commerce schema", tables, err)
	}
	s, owner, _ := telegramSetupEmptyFixture(t)
	if _, err := s.db.Exec(`DROP TABLE telegram_commerce_trial_policy`); err != nil {
		t.Fatal(err)
	}
	if items, err := s.ListTelegramStoreSetupAs(ctx, owner.Token); err == nil || items != nil {
		t.Fatal("missing trial evidence became a fabricated disabled policy", items, err)
	}
}

func TestTelegramStoreSetupCatalogAndStoreBounds(t *testing.T) {
	ctx := context.Background()
	s, owner, store := telegramSetupEmptyFixture(t)
	newRep(t, s, "setup-small-rep")
	other, err := s.ConfigureTelegramStore(ctx, owner.Token, TelegramStoreRegistration{EventID: "setup-small-store-0001", ID: "setup-small", BotID: 501, RepresentativeID: "setup-small-rep", Name: "Small scope", ManagerUserID: 5002, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	login := telegramSetupRepLogin(t, s, other.RepresentativeID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 201; i++ {
		id := fmt.Sprintf("setup-catalog-%03d", i)
		if _, err = tx.Exec(`INSERT INTO catalog_products(id,representative_id,display_name,plan_kind,duration_days,ip_count,volume_bytes,price_toman,status,revision,created_unix,updated_unix) VALUES(?,'owner',?,'volume',30,1,100,1000,'published',1,1,1)`, id, id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO telegram_commerce_prices(store_id,product_id,amount_stars,revision) VALUES(?,?,1,1)`, store.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 998; i++ {
		id := fmt.Sprintf("setup-extra-%04d", i)
		if _, err = tx.Exec(`INSERT INTO telegram_commerce_stores(store_id,bot_id,representative_id,name,manager_user_id,enabled,revision,created_unix) VALUES(?,?,'owner',?,5001,1,1,1)`, id, 10000+i, id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	items := telegramSetupMap(t, s, owner.Token)
	if len(items) != 1000 || items[store.ID].Setup.PublishedProducts != 201 || items[store.ID].Setup.PricedProducts != 201 || items[store.ID].Setup.CatalogLimit != 200 {
		t.Fatal("catalog boundary was silently truncated or changed", len(items), items[store.ID].Setup)
	}
	if _, err = s.db.Exec(`INSERT INTO telegram_commerce_stores(store_id,bot_id,representative_id,name,manager_user_id,enabled,revision,created_unix) VALUES('setup-overflow',20000,'owner','Overflow',5001,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if items, err := s.ListTelegramStoreSetupAs(ctx, owner.Token); !errors.Is(err, ErrUnavailable) || items != nil {
		t.Fatal("owner store capacity returned a partial snapshot", len(items), err)
	}
	if _, err = s.ListTelegramStoresAs(ctx, owner.Token); !errors.Is(err, ErrUnavailable) {
		t.Fatal("setup and existing listing capacity disagree", err)
	}
	scoped := telegramSetupMap(t, s, login.Token)
	if len(scoped) != 1 || scoped[other.ID].Setup.PublishedProducts != 0 || scoped[other.ID].Setup.PricedProducts != 0 {
		t.Fatal("global store/catalog capacity escaped representative scope", scoped)
	}
}
