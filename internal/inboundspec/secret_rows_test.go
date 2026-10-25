package inboundspec

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPreviewRowIDsDoNotRevealWhetherSecretGuessesMatch(t *testing.T) {
	first := fixture(t, "trojan")
	second := first
	second.TLS = testTLS(t) // Same public certificate options, different key material.
	for _, pair := range [][2]Spec{{first, second}, {
		{Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Stream: map[string]any{"finalmask": map[string]any{"tcp": []any{map[string]any{"type": "sudoku", "settings": map[string]any{"password": "guess-one"}}}}}},
		{Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Stream: map[string]any{"finalmask": map[string]any{"tcp": []any{map[string]any{"type": "sudoku", "settings": map[string]any{"password": "guess-two"}}}}}},
	}} {
		a, e := Normalize(pair[0])
		if e != nil {
			t.Fatal(e)
		}
		b, e := Normalize(pair[1])
		if e != nil {
			t.Fatal(e)
		}
		publicA, _ := json.Marshal(Public(a))
		publicB, _ := json.Marshal(Public(b))
		if !bytes.Equal(publicA, publicB) {
			t.Fatal("changing only a write-only value changed its public preview or row ID")
		}
	}
	// Create-time identities remain random rather than deriving from a secret.
	a, e := EnsureSecrets(first)
	if e != nil {
		t.Fatal(e)
	}
	b, e := EnsureSecrets(first)
	if e != nil {
		t.Fatal(e)
	}
	if str(list(a.TLS["certificates"])[0].(map[string]any), "id") == str(list(b.TLS["certificates"])[0].(map[string]any), "id") {
		t.Fatal("independent creations reused a deterministic row ID")
	}
}

func TestCredentialRowsFollowIdentityRatherThanArrayIndex(t *testing.T) {
	s := fixture(t, "trojan")
	second := testTLS(t)
	s.TLS["certificates"] = append(list(s.TLS["certificates"]), list(second["certificates"])...)
	n, e := Normalize(s)
	if e != nil {
		t.Fatal(e)
	}
	old := list(n.TLS["certificates"])
	for _, reorder := range [][]int{{1}, {1, 0}} {
		p := Public(n).Spec
		rows := list(p.TLS["certificates"])
		next := []any{}
		for _, i := range reorder {
			next = append(next, rows[i])
		}
		p.TLS["certificates"] = next
		merged, e := MergeSecrets(n, p)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = Normalize(merged); e != nil {
			t.Fatal(e)
		}
		for i, original := range reorder {
			got, _ := json.Marshal(list(merged.TLS["certificates"])[i])
			want, _ := json.Marshal(old[original])
			if string(got) != string(want) {
				t.Fatal("a moved certificate acquired another row's private material")
			}
		}
	}
	p := Public(n).Spec
	row := list(p.TLS["certificates"])[0].(map[string]any)
	delete(row, "id")
	if _, e = MergeSecrets(n, p); e == nil {
		t.Fatal("anonymous retained credential accepted")
	}
}

func TestMaskRetypeDoesNotInheritAnotherMaskPassword(t *testing.T) {
	s := fixture(t, "vless")
	s.Stream = map[string]any{"finalmask": map[string]any{"tcp": []any{map[string]any{"type": "sudoku", "settings": map[string]any{"password": "synthetic-sudoku-secret"}}}}}
	n, e := Normalize(s)
	if e != nil {
		t.Fatal(e)
	}
	p := Public(n).Spec
	row := list(obj(p.Stream, "finalmask")["tcp"])[0].(map[string]any)
	row["type"] = "xmc"
	row["settings"] = map[string]any{}
	merged, e := MergeSecrets(n, p)
	if e != nil {
		t.Fatal(e)
	}
	row = list(obj(merged.Stream, "finalmask")["tcp"])[0].(map[string]any)
	if str(obj(row, "settings"), "password") != "" {
		t.Fatal("mask retype resurrected unrelated password")
	}
	if _, e = Normalize(merged); e == nil {
		t.Fatal("incomplete new mask accepted")
	}
}
