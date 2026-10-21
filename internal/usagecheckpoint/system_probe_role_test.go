package usagecheckpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSystemProbeRoleBindsAuthorityAndPreservesLegacyEpochHash(t *testing.T) {
	// This is the exact pre-system-role wire shape. Empty new roles must not
	// rewrite hashes or invalidate old durable financial outboxes/receipts.
	old := `{"node_id":"node","installation_id":"` + strings.Repeat("a", 32) + `","epoch_id":"` + strings.Repeat("b", 32) + `","config_sha":"` + strings.Repeat("c", 64) + `","kernel_boot_id":"11111111-1111-4111-8111-111111111111","pid":123,"process_start_ticks":"77","bindings":[{"client_id":"customer","email":"customer@pro.invalid","billable":true}]}`
	var e Epoch
	if err := json.Unmarshal([]byte(old), &e); err != nil {
		t.Fatal(err)
	}
	_, binding, err := CanonicalEpoch(e)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(old))
	if binding != hex.EncodeToString(h[:]) {
		t.Fatal("legacy epoch hash changed")
	}
	e.Bindings[0].Billable = false
	_, transit, err := CanonicalEpoch(e)
	if err != nil {
		t.Fatal("legacy nonbillable transit rejected", err)
	}
	e.Bindings[0].Role = SystemProbeRole
	_, system, err := CanonicalEpoch(e)
	if err != nil || system == transit {
		t.Fatal("explicit system role not bound to epoch", err)
	}
	e.Bindings[0].Billable = true
	if _, _, err = CanonicalEpoch(e); !errors.Is(err, ErrInvalid) {
		t.Fatal("system role marked billable", err)
	}
	e.Bindings[0].Billable = false
	e.Bindings[0].Role = "unknown-role"
	if _, _, err = CanonicalEpoch(e); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown accounting role accepted", err)
	}
}
