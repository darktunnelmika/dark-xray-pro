//go:build linux

package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

type retirementWriterFixture struct {
	proof                        LocalSourceSettlement
	handoff, handoffSHA, key     string
	fence, fenceSHA              string
	inventorySHA, destinationSHA string
	out                          string
	retiredNS, fenceObservedNS   int64
	public                       ed25519.PublicKey
}

func writerDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func newRetirementWriterFixture(t *testing.T) retirementWriterFixture {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "source.key")
	if err = os.WriteFile(key, private, 0600); err != nil {
		t.Fatal(err)
	}
	proof := LocalSourceSettlement{
		Scope: "single_local_source_epoch", SourceID: "declared-source-a", SourceSHA: strings.Repeat("a", 64),
		AuthoritySHA: strings.Repeat("b", 64), JournalSHA: strings.Repeat("c", 64),
		Terminal:   uc.Receipt{EpochID: strings.Repeat("d", 32), Sequence: 7, SHA: strings.Repeat("e", 64)},
		PriorBytes: 10, Up: 20, Down: 30, Historical: 60, Remaining: 40,
	}
	message, err := localHandoffMessage(proof)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(SignedLocalHandoffV1{Version: 1, Settlement: proof, Signature: ed25519.Sign(private, message)})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	handoff := filepath.Join(dir, "handoff.json")
	if err = os.WriteFile(handoff, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fenceRaw := []byte("opaque synthetic active-fence assertion\n")
	fence := filepath.Join(dir, "fence.bin")
	if err = os.WriteFile(fence, fenceRaw, 0600); err != nil {
		t.Fatal(err)
	}
	return retirementWriterFixture{
		proof: proof, handoff: handoff, handoffSHA: writerDigest(raw), key: key,
		fence: fence, fenceSHA: writerDigest(fenceRaw), inventorySHA: strings.Repeat("1", 64),
		destinationSHA: strings.Repeat("2", 64), out: filepath.Join(dir, "retirement.json"),
		retiredNS: 10_000, fenceObservedNS: 11_000, public: public,
	}
}

func (f retirementWriterFixture) write() (SourceRetirementFileV1, error) {
	return WriteSourceRetirementV1(f.proof, f.handoff, f.handoffSHA, f.key, f.fence, f.fenceSHA,
		f.inventorySHA, f.destinationSHA, f.out, f.retiredNS, f.fenceObservedNS)
}

func TestWriteSourceRetirementV1BindsVerifiedSourceEvidence(t *testing.T) {
	f := newRetirementWriterFixture(t)
	result, err := f.write()
	if err != nil || result.AuthorizesActivation || result.SHA == "" {
		t.Fatal("source retirement write", result, err)
	}
	publicSHA := writerDigest(f.public)
	if result.PublicKeySHA != publicSHA || result.Statement.SourceSignerPublicSHA != publicSHA ||
		result.Statement.HandoffSHA != f.handoffSHA || result.Statement.FenceDocumentSHA != f.fenceSHA ||
		result.Statement.InventorySHA != f.inventorySHA || result.Statement.DestinationSHA != f.destinationSHA ||
		result.Statement.EpochID != f.proof.Terminal.EpochID || result.Statement.Sequence != f.proof.Terminal.Sequence ||
		result.Statement.TerminalReceiptSHA != f.proof.Terminal.SHA || result.Statement.AuthoritySHA != f.proof.AuthoritySHA ||
		result.Statement.JournalSHA != f.proof.JournalSHA {
		t.Fatal("derived evidence changed", result)
	}
	raw, err := os.ReadFile(f.out)
	if err != nil || writerDigest(raw) != result.SHA || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("retirement output", err)
	}
	info, err := os.Stat(f.out)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("retirement output permissions", info, err)
	}
	var signed SignedSourceRetirementV1
	if json.Unmarshal(raw, &signed) != nil || signed.Statement != result.Statement {
		t.Fatal("noncanonical result")
	}
	message, err := SourceRetirementSigningMessageV1(signed.Statement)
	if err != nil || !ed25519.Verify(f.public, message, signed.Signature) {
		t.Fatal("source signature invalid", err)
	}
	witnessPublic, witnessPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	witness := SourceRetirementWitnessV1{
		Version: 1, Scope: SourceRetirementWitnessScopeV1,
		RetirementDocumentSHA: result.SHA, SourcePublicKeySHA: result.PublicKeySHA,
		InventorySHA: f.inventorySHA, DestinationSHA: f.destinationSHA,
		WitnessedUnixNano: f.fenceObservedNS + 1,
	}
	witnessMessage, err := SourceRetirementWitnessSigningMessageV1(witness)
	if err != nil {
		t.Fatal(err)
	}
	witnessRaw, err := json.Marshal(SignedSourceRetirementWitnessV1{
		Version: 1, Witness: witness, Signature: ed25519.Sign(witnessPrivate, witnessMessage),
	})
	if err != nil {
		t.Fatal(err)
	}
	witnessRaw = append(witnessRaw, '\n')
	pins := SourceRetirementPinsV1{
		RetirementDocumentSHA: result.SHA, SourcePublicKeySHA: result.PublicKeySHA,
		WitnessDocumentSHA: writerDigest(witnessRaw), WitnessPublicKeySHA: writerDigest(witnessPublic),
		FenceDocumentSHA: f.fenceSHA, InventorySHA: f.inventorySHA, DestinationSHA: f.destinationSHA,
	}
	verified, err := VerifySignedSourceRetirementV1(raw, f.public, witnessRaw, witnessPublic, pins)
	if err != nil {
		t.Fatal("emitted source document is not verifier-compatible", err)
	}
	if _, _, _, mutation, err := verified.Evidence(); err != nil || mutation {
		t.Fatal("verified source retirement granted mutation", mutation, err)
	}
	again, err := f.write()
	if err != nil || again != result {
		t.Fatal("identical retry did not replay", again, err)
	}
}

func TestWriteSourceRetirementV1FailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, *retirementWriterFixture)
	}{
		{"current settlement mismatch", func(_ *testing.T, f *retirementWriterFixture) { f.proof.SourceID = "other-source" }},
		{"handoff pin mismatch", func(_ *testing.T, f *retirementWriterFixture) { f.handoffSHA = strings.Repeat("3", 64) }},
		{"fence pin mismatch", func(_ *testing.T, f *retirementWriterFixture) { f.fenceSHA = strings.Repeat("4", 64) }},
		{"invalid inventory pin", func(_ *testing.T, f *retirementWriterFixture) { f.inventorySHA = "unknown" }},
		{"foreign destination", func(_ *testing.T, f *retirementWriterFixture) { f.destinationSHA = "unknown" }},
		{"stale fence", func(_ *testing.T, f *retirementWriterFixture) {
			f.fenceObservedNS = f.retiredNS + maxRetirementWitnessDelayNano + 1
		}},
		{"foreign signing key", func(t *testing.T, f *retirementWriterFixture) {
			_, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(filepath.Dir(f.key), "foreign.key")
			if err = os.WriteFile(path, private, 0600); err != nil {
				t.Fatal(err)
			}
			f.key = path
		}},
		{"public fence file", func(t *testing.T, f *retirementWriterFixture) {
			if err := os.Chmod(f.fence, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink fence", func(t *testing.T, f *retirementWriterFixture) {
			link := filepath.Join(filepath.Dir(f.fence), "fence-link")
			if err := os.Symlink(f.fence, link); err != nil {
				t.Fatal(err)
			}
			f.fence = link
		}},
		{"foreign existing output", func(t *testing.T, f *retirementWriterFixture) {
			if err := os.WriteFile(f.out, []byte("foreign\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newRetirementWriterFixture(t)
			test.change(t, &f)
			if _, err := f.write(); err == nil {
				t.Fatal("unsafe source retirement emitted")
			}
		})
	}
}
