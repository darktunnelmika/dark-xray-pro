package inboundspec

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Row IDs are authoring metadata. A retained credential follows its row through
// reorder/delete operations instead of following a vulnerable array index.
func ensureStableRowIDs(s *Spec) error { return assignRowIDs(s, false) }
func assignRowIDs(s *Spec, random bool) error {
	assign := func(rows []any, scope string) error {
		seen := map[string]bool{}
		for i, v := range rows {
			m, ok := v.(map[string]any)
			if !ok {
				return invalid(scope, "row_object_required")
			}
			id := str(m, "id")
			if id == "" {
				if random {
					b := make([]byte, 16)
					if _, e := rand.Read(b); e != nil {
						return e
					}
					id = hex.EncodeToString(b)
				} else {
					copy := cloneMap(m)
					delete(copy, "id")
					// Preview IDs must not be public verifiers for a guessed
					// password or other low-entropy write-only value. Derive
					// them only from information the public view already has.
					paths := []string{}
					walkSecrets(copy, fmt.Sprintf("%s.%d", scope, i), true, &paths)
					raw, e := json.Marshal(copy)
					if e != nil {
						return ErrInvalid
					}
					h := sha256.Sum256(append([]byte(fmt.Sprintf("inbound-row-public-v1/%s/%d/", scope, i)), raw...))
					id = hex.EncodeToString(h[:16])
				}
				m["id"] = id
			}
			decoded, e := hex.DecodeString(id)
			if e != nil || len(decoded) != 16 || seen[id] {
				return invalid(scope+".id", "row_id_invalid_or_duplicate")
			}
			seen[id] = true
		}
		return nil
	}
	if e := assign(list(s.TLS["certificates"]), "tls.certificates"); e != nil {
		return e
	}
	if fm := obj(s.Stream, "finalmask"); fm != nil {
		for _, key := range []string{"tcp", "udp"} {
			rows := list(fm[key])
			if e := assign(rows, "stream.finalmask."+key); e != nil {
				return e
			}
			for i, v := range rows {
				m := v.(map[string]any)
				if tls := obj(obj(m, "settings"), "tlsConfig"); tls != nil {
					if e := assign(list(tls["certificates"]), fmt.Sprintf("stream.finalmask.%s.%d.settings.tlsConfig.certificates", key, i)); e != nil {
						return e
					}
				}
			}
		}
	}
	return nil
}
func stripCertificateIDs(tls map[string]any) {
	for _, v := range list(tls["certificates"]) {
		if m, ok := v.(map[string]any); ok {
			delete(m, "id")
		}
	}
}
func stripMaskIDs(fm map[string]any) {
	for _, key := range []string{"tcp", "udp"} {
		for _, v := range list(fm[key]) {
			if m, ok := v.(map[string]any); ok {
				delete(m, "id")
				stripCertificateIDs(obj(obj(m, "settings"), "tlsConfig"))
			}
		}
	}
}
