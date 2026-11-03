//go:build linux

package darkrestore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func inventorySignedFixture(t *testing.T, epoch string) (VerifiedLocalHandoff, LocalHandoffSourcePinV1) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proof := LocalSourceSettlement{Scope: "single_local_source_epoch", SourceSHA: strings.Repeat("a", 64), SourceID: "synthetic-customer",
		AuthoritySHA: strings.Repeat("b", 64), JournalSHA: strings.Repeat("c", 64), Terminal: uc.Receipt{EpochID: strings.Repeat(epoch, 32), Sequence: 1, SHA: strings.Repeat("d", 64)}, PriorBytes: 10, Up: 20, Down: 30, Historical: 60, Remaining: 40}
	message, err := localHandoffMessage(proof)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(SignedLocalHandoffV1{Version: 1, Settlement: proof, Signature: ed25519.Sign(key, message)})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, 10)
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "signed.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	verified, err := VerifySignedLocalHandoff(path, digest, pub)
	if err != nil {
		t.Fatal(err)
	}
	publicSum := sha256.Sum256(pub)
	return verified, LocalHandoffSourcePinV1{EpochID: proof.Terminal.EpochID, Sequence: proof.Terminal.Sequence, ReceiptSHA: proof.Terminal.SHA, AuthoritySHA: proof.AuthoritySHA, JournalSHA: proof.JournalSHA, HandoffSHA: digest, SignerPublicSHA: hex.EncodeToString(publicSum[:])}
}
func TestPinnedLocalHandoffInventoryCoverage(t *testing.T) {
	first, a := inventorySignedFixture(t, "1")
	second, b := inventorySignedFixture(t, "2")
	inventory := LocalHandoffInventoryV1{Version: 1, SourceSHA: strings.Repeat("a", 64), SourceID: "synthetic-customer", Sources: []LocalHandoffSourcePinV1{b, a}}
	before := append([]LocalHandoffSourcePinV1(nil), inventory.Sources...)
	pin, err := DigestLocalHandoffInventoryV1(inventory)
	if err != nil {
		t.Fatal(err)
	}
	out, err := CheckLocalHandoffCoverageV1(inventory, pin, []VerifiedLocalHandoff{second, first})
	if err != nil || out.MatchedSources != 2 || out.Scope != "pinned_inventory_only" || out.AuthorizesActivation || !reflect.DeepEqual(out.EpochIDs, []string{a.EpochID, b.EpochID}) {
		t.Fatal(out, err)
	}
	if !reflect.DeepEqual(before, inventory.Sources) {
		t.Fatal("caller inventory mutated")
	}
	inventory.Sources = []LocalHandoffSourcePinV1{a, b}
	again, err := DigestLocalHandoffInventoryV1(inventory)
	if err != nil || again != pin {
		t.Fatal("unstable inventory digest")
	}
	for _, test := range []struct {
		name     string
		handoffs []VerifiedLocalHandoff
	}{
		{"missing", []VerifiedLocalHandoff{first}},
		{"extra", []VerifiedLocalHandoff{first, second, first}},
		{"duplicate", []VerifiedLocalHandoff{first, first}},
		{"unverified", []VerifiedLocalHandoff{first, {}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CheckLocalHandoffCoverageV1(inventory, pin, test.handoffs); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("unsafe coverage accepted", err)
			}
		})
	}
	for _, field := range []string{"source", "identity", "epoch", "sequence", "receipt", "authority", "journal", "file", "signer", "duplicate_epoch", "empty"} {
		t.Run(field, func(t *testing.T) {
			changed := inventory
			changed.Sources = append([]LocalHandoffSourcePinV1(nil), inventory.Sources...)
			s := &changed.Sources[0]
			switch field {
			case "source":
				changed.SourceSHA = strings.Repeat("e", 64)
			case "identity":
				changed.SourceID = "another-customer"
			case "epoch":
				s.EpochID = strings.Repeat("3", 32)
			case "sequence":
				s.Sequence++
			case "receipt":
				s.ReceiptSHA = strings.Repeat("e", 64)
			case "authority":
				s.AuthoritySHA = strings.Repeat("e", 64)
			case "journal":
				s.JournalSHA = strings.Repeat("e", 64)
			case "file":
				s.HandoffSHA = strings.Repeat("e", 64)
			case "signer":
				s.SignerPublicSHA = strings.Repeat("e", 64)
			case "duplicate_epoch":
				changed.Sources[1].EpochID = s.EpochID
			case "empty":
				changed.Sources = nil
			}
			alteredPin, err := DigestLocalHandoffInventoryV1(changed)
			if err == nil {
				if _, err = CheckLocalHandoffCoverageV1(changed, alteredPin, []VerifiedLocalHandoff{first, second}); !errors.Is(err, ErrHandoffInventory) {
					t.Fatal("mismatched evidence accepted", field)
				}
			} else if field != "duplicate_epoch" && field != "empty" {
				t.Fatal(err)
			}
			if _, err = CheckLocalHandoffCoverageV1(changed, pin, []VerifiedLocalHandoff{first, second}); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("changed independent pin accepted")
			}
		})
	}
}
