package business

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRepresentativeDirectoryGlobalOwnerReadOnlyAndBounded(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	for i := 0; i < 70; i++ {
		id := fmt.Sprintf("rep-%03d", i)
		name := fmt.Sprintf("Representative %03d", i)
		if i == 69 {
			name = "نماینده ٪ خاص%_"
		}
		if _, err := s.CreateRepresentative(ctx, id, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE representatives SET enabled=0 WHERE id=?", "rep-068"); err != nil {
		t.Fatal(err)
	}
	grant(t, s, "rep-069", "rep-search-grant-00000001", 1000, 3)
	for i := 0; i < 2; i++ {
		if _, err := s.CreateClient(ctx, NewClient{
			EventID:          fmt.Sprintf("rep-directory-client-000%d", i),
			ClientID:         fmt.Sprintf("rep-directory-client-%d", i),
			RepresentativeID: "rep-069", Name: "Real client", VolumeBytes: 50,
			DurationDays: 30,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetClientState(ctx, SetClientState{
		EventID: "rep-directory-archive-00001", ClientID: "rep-directory-client-1",
		Action: "archive", Reason: "preserve historical client",
	}); err != nil {
		t.Fatal(err)
	}
	page, err := s.SearchRepresentatives(ctx, RepresentativeSearch{Limit: 25})
	if err != nil || page.Total != 71 || len(page.Representatives) != 25 || !page.HasMore {
		t.Fatalf("first page %+v, %v", page, err)
	}
	last, err := s.SearchRepresentatives(ctx, RepresentativeSearch{Limit: 25, Offset: 50})
	if err != nil || last.Total != 71 || len(last.Representatives) != 21 || last.HasMore {
		t.Fatalf("last page %+v, %v", last, err)
	}
	scoped, err := s.SearchRepresentatives(ctx, RepresentativeSearch{Query: "%_", Limit: 50})
	if err != nil || scoped.Total != 1 || len(scoped.Representatives) != 1 ||
		scoped.Representatives[0].ID != "rep-069" ||
		scoped.Representatives[0].ClientCount != 2 || scoped.Representatives[0].ArchivedCount != 1 {
		t.Fatalf("literal search/counts %+v, %v", scoped, err)
	}
	persian, err := s.SearchRepresentatives(ctx, RepresentativeSearch{Query: "نماینده", Limit: 25})
	if err != nil || persian.Total != 1 {
		t.Fatalf("Persian lookup %+v %v", persian, err)
	}
	disabled, err := s.SearchRepresentatives(ctx, RepresentativeSearch{State: "disabled", Limit: 25})
	if err != nil || disabled.Total != 1 || disabled.Representatives[0].ID != "rep-068" {
		t.Fatalf("disabled account %+v %v", disabled, err)
	}
	sorted, err := s.SearchRepresentatives(ctx, RepresentativeSearch{Sort: "volume", Limit: 25})
	if err != nil || sorted.Representatives[0].ID != "rep-069" {
		t.Fatalf("credit sorting %+v %v", sorted, err)
	}
	for _, input := range []RepresentativeSearch{
		{Limit: 0}, {Limit: 201}, {Limit: 50, Offset: -1},
		{Limit: 50, Offset: 1_000_001}, {Limit: 50, Query: strings.Repeat("x", 129)},
		{Limit: 50, Query: "bad\nquery"}, {Limit: 50, Sort: "x;DROP TABLE events"},
		{Limit: 50, State: "alive"},
	} {
		if _, err := s.SearchRepresentatives(ctx, input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted unsafe search: %+v, %v", input, err)
		}
	}
	client, err := s.Client(ctx, "rep-directory-client-1")
	if err != nil || !client.Archived {
		t.Fatalf("directory read must preserve archived UUID and record: %+v %v", client, err)
	}
	rep, err := s.Representative(ctx, "rep-069")
	if err != nil || rep.VolumeCreditBytes != 900 || rep.UnlimitedCreditUnits != 3 {
		t.Fatalf("directory read changed financial ledger %+v %v", rep, err)
	}
}
