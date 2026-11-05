package business

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestClientContactCreateSearchReplayAndScope(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "contact-a")
	newRep(t, s, "contact-b")
	grant(t, s, "contact-a", "contact-credit-grant-0001", 1000, 10)
	n := NewClient{EventID: "contact-client-create-0001", ClientID: "contact-client-a", RepresentativeID: "contact-a", Name: "Alice", DurationDays: 30, VolumeBytes: 100, Contact: &ClientContactInput{TelegramUsername: " @Alice_TEST ", TelegramID: "123456789", Note: " customer note "}}
	c, e := s.CreateClientScoped(ctx, n, "contact-a")
	if e != nil || c.Contact == nil || c.Contact.TelegramUsername != "alice_test" || c.Contact.Note != "customer note" {
		t.Fatal(c, e)
	}
	if _, e = s.CreateClientScoped(ctx, n, "contact-a"); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{"@ALICE_test", "alice_TEST", "123456789", "customer note"} {
		page, e := s.SearchClients(ctx, ClientSearch{RepresentativeID: "contact-a", Query: q, Limit: 25})
		if e != nil || page.Total != 1 || page.Clients[0].Contact == nil {
			t.Fatal(q, page, e)
		}
		page, e = s.SearchClients(ctx, ClientSearch{RepresentativeID: "contact-b", Query: q, Limit: 25})
		if e != nil || page.Total != 0 {
			t.Fatal("scope", page, e)
		}
	}
	edit := ClientContactChange{ClientContactInput: ClientContactInput{TelegramUsername: "new_name", Note: "changed"}, EventID: "contact-client-update-0001", ExpectedRevision: 1}
	if _, e = s.SetClientContact(ctx, c.ID, "contact-b", edit); !errors.Is(e, ErrOwnershipDenied) {
		t.Fatal("scope edit", e)
	}
	result, e := s.SetClientContact(ctx, c.ID, "contact-a", edit)
	if e != nil || result.Contact.Revision != 2 {
		t.Fatal(result, e)
	}
	result, e = s.SetClientContact(ctx, c.ID, "contact-a", edit)
	if e != nil || !result.Replayed {
		t.Fatal("replay", result, e)
	}
	conflict := edit
	conflict.EventID = "contact-client-update-0002"
	if _, e = s.SetClientContact(ctx, c.ID, "contact-a", conflict); !errors.Is(e, ErrConflict) {
		t.Fatal("stale revision", e)
	}
	conflict = edit
	conflict.Note = "different"
	if _, e = s.SetClientContact(ctx, c.ID, "contact-a", conflict); !errors.Is(e, ErrConflict) {
		t.Fatal("event collision", e)
	}
	clear := ClientContactChange{EventID: "contact-client-clear-0001", ExpectedRevision: 2}
	if _, e = s.SetClientContact(ctx, c.ID, "contact-a", clear); e != nil {
		t.Fatal(e)
	}
	c, e = s.Client(ctx, c.ID)
	if e != nil || c.Contact == nil || c.Contact.Revision != 3 || c.Contact.TelegramUsername != "" {
		t.Fatal("clear", c, e)
	}
	rep, _ := s.Representative(ctx, "contact-a")
	if rep.VolumeCreditBytes != 900 {
		t.Fatal("contact changed credit", rep)
	}
	for _, v := range []ClientContactInput{{TelegramID: "-123"}, {TelegramID: "4503599627370496"}, {TelegramUsername: "https://t.me/alice"}, {TelegramUsername: "<script>"}, {Note: "bad\x00note"}} {
		n.EventID = "contact-invalid-create-001"
		n.ClientID = "invalid-contact"
		n.Contact = &v
		if _, e = s.CreateClientScoped(ctx, n, "contact-a"); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid metadata", v, e)
		}
	}
}
func TestClientContactConcurrentRevisionAndOwnershipRecheck(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	c, e := s.CreateClient(ctx, NewClient{EventID: "contact-concurrent-create-001", ClientID: "contact-race", RepresentativeID: "owner", Name: "Race", DurationDays: 30, VolumeBytes: 100})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"contact-concurrent-edit-0001", "contact-concurrent-edit-0002"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, e := s.SetClientContact(ctx, c.ID, "", ClientContactChange{ClientContactInput: ClientContactInput{TelegramUsername: "race_test"}, EventID: id})
			results <- e
		}(id)
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal(ok, conflicts)
	}
}

func TestClientContactOfflineBackupAndReopen(t *testing.T) {
	s, path := opened(t)
	ctx := context.Background()
	c, e := s.CreateClient(ctx, NewClient{EventID: "contact-backup-create-0001", ClientID: "contact-backup", RepresentativeID: "owner", Name: "Backup", DurationDays: 30, VolumeBytes: 100, Contact: &ClientContactInput{TelegramUsername: "backup_user", TelegramID: "123456789", Note: "private note"}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.SetClientContact(ctx, c.ID, "", ClientContactChange{ClientContactInput: ClientContactInput{TelegramUsername: "updated_user", TelegramID: "987654321", Note: "retained note"}, ExpectedRevision: 1, EventID: "contact-backup-update-0001"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	v, e := ValidateOfflineBackup(ctx, path)
	if e != nil || v.TableCounts["client_contacts_v1"] != 1 || v.TableCounts["client_contact_events_v1"] != 1 {
		t.Fatal(v, e)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("backup validation mutated database")
	}
	copyDir := filepath.Join(t.TempDir(), "private")
	if e = os.Mkdir(copyDir, 0700); e != nil {
		t.Fatal(e)
	}
	copyPath := filepath.Join(copyDir, "restored.sqlite")
	if e = os.WriteFile(copyPath, before, 0600); e != nil {
		t.Fatal(e)
	}
	restored, e := Open(copyPath)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	got, e := restored.Client(ctx, c.ID)
	if e != nil || got.Contact == nil || got.Contact.TelegramUsername != "updated_user" || got.Contact.Note != "retained note" || got.Contact.Revision != 2 {
		t.Fatal(got, e)
	}
	mutateOfflineFixture(t, path, "DROP TRIGGER client_contact_events_no_update_v1")
	if _, e = ValidateOfflineBackup(ctx, path); e == nil {
		t.Fatal("corrupted contact schema accepted")
	}
}
func TestClientContactBackupReadsPriorSchemaWithoutMigration(t *testing.T) {
	path := offlineValidationFixture(t, false)
	mutateOfflineFixture(t, path, "DROP TABLE client_contact_events_v1", "DROP TABLE client_contacts_v1")
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ValidateOfflineBackup(context.Background(), path); e != nil {
		t.Fatal("old snapshot rejected", e)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("old snapshot was migrated")
	}
}
