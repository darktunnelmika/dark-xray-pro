package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestClientGroupAndImportOriginFiltersRetainBrowserRepresentativeScope(t *testing.T) {
	f := stagedRestoreAPI(t)
	ctx := context.Background()
	review := f.s.darkRestoreStage.review
	intent := business.DarkRestoreImportIntentV1{Review: review, Mappings: []business.DarkRestoreGroupMappingV1{{SourceGroupID: "synthetic-group", RepresentativeID: "transfer-target"}}}
	q, err := f.s.business.QuoteDarkRestoreImportV1(ctx, f.owner.Value, intent)
	if err != nil {
		t.Fatal(err)
	}
	req := business.DarkRestoreImportCommitV1{DarkRestoreImportIntentV1: intent, EventID: "origin-api-import-0001", QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix}
	receipt, err := f.s.business.CommitDarkRestoreImportV1(ctx, f.owner.Value, f.s.darkRestoreStage.path, f.s.inboundKey, req)
	if err != nil || receipt.Count != 1 {
		t.Fatal("private snapshot import", err)
	}
	group := receipt.Groups[0].TargetGroupID
	read := func(cookie *http.Cookie, path string, want int, total int) {
		t.Helper()
		w := browserRequest(t, f.h, "GET", path, nil, cookie, "", "")
		if w.Code != want {
			t.Fatalf("status %d for %s, want %d: %s", w.Code, path, want, w.Body.String())
		}
		if want != 200 {
			return
		}
		var out map[string]json.RawMessage
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("malformed list response")
		}
		var clients []business.Client
		var found int
		var origin string
		if json.Unmarshal(out["clients"], &clients) != nil || json.Unmarshal(out["total"], &found) != nil || found != total || len(clients) != total {
			t.Fatal("filter count/page", w.Body.String())
		}
		if json.Unmarshal(out["origin"], &origin) != nil || origin == "" {
			t.Fatal("missing filter echo")
		}
		if cookie == f.source {
			for _, c := range clients {
				if c.RepresentativeID != "transfer-source" {
					t.Fatal("group filter exposed another representative")
				}
			}
		}
	}
	base := "/api/v1/workspace/clients?"
	read(f.owner, base+"origin=native", 200, 1)
	read(f.owner, base+"origin=dark_restore", 200, 1)
	read(f.target, base+"origin=dark_restore&group_id="+group, 200, 1)
	read(f.target, base+"origin=native&group_id="+group, 200, 0)
	read(f.source, base+"origin=dark_restore&group_id="+group, 200, 0)
	read(f.source, base+"group_id="+group, 200, 0)
	read(f.source, base+"origin=dark_restore&representative_id=transfer-target&group_id="+group, 403, 0)
	read(nil, base+"origin=dark_restore&group_id="+group, 401, 0)
	for _, query := range []string{"origin=unknown", "origin=native&origin=dark_restore", "group_id=bad%20group", "group_id=first&group_id=second"} {
		read(f.owner, base+query, 422, 0)
	}
}
