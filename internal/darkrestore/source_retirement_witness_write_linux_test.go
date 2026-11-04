//go:build linux

package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type retirementWitnessWriterFixture struct {
	source        retirementWriterFixture
	retirement    SourceRetirementFileV1
	publicPath    string
	witnessKey    string
	witnessPublic ed25519.PublicKey
	out           string
	pins          SourceRetirementStatementPinsV1
	witnessedNS   int64
}

func newRetirementWitnessWriterFixture(t *testing.T) retirementWitnessWriterFixture {
	t.Helper()
	source := newRetirementWriterFixture(t)
	retirement, err := source.write()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(source.out)
	publicPath := filepath.Join(dir, "source.pub")
	if err = os.WriteFile(publicPath, source.public, 0600); err != nil {
		t.Fatal(err)
	}
	witnessPublic, witnessPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	witnessKey := filepath.Join(dir, "witness.key")
	if err = os.WriteFile(witnessKey, witnessPrivate, 0600); err != nil {
		t.Fatal(err)
	}
	return retirementWitnessWriterFixture{
		source: source, retirement: retirement, publicPath: publicPath,
		witnessKey: witnessKey, witnessPublic: witnessPublic,
		out: filepath.Join(dir, "witness.json"),
		pins: SourceRetirementStatementPinsV1{
			RetirementDocumentSHA: retirement.SHA, SourcePublicKeySHA: retirement.PublicKeySHA,
			FenceDocumentSHA: source.fenceSHA, InventorySHA: source.inventorySHA,
			DestinationSHA: source.destinationSHA,
		},
		witnessedNS: source.fenceObservedNS + 1,
	}
}

func (f retirementWitnessWriterFixture) write() (SourceRetirementWitnessFileV1, error) {
	return WriteSourceRetirementWitnessV1(f.source.out, f.publicPath, f.source.fence,
		f.witnessKey, f.out, f.pins, f.witnessedNS)
}

func TestWriteSourceRetirementWitnessV1EmitsIndependentReceipt(t *testing.T) {
	f := newRetirementWitnessWriterFixture(t)
	result, err := f.write()
	if err != nil || result.AuthorizesActivation || result.SHA == "" || result.PublicKeySHA != writerDigest(f.witnessPublic) {
		t.Fatal("witness write", result, err)
	}
	if result.Witness.RetirementDocumentSHA != f.retirement.SHA ||
		result.Witness.SourcePublicKeySHA != f.retirement.PublicKeySHA ||
		result.Witness.InventorySHA != f.source.inventorySHA ||
		result.Witness.DestinationSHA != f.source.destinationSHA ||
		result.Witness.WitnessedUnixNano != f.witnessedNS {
		t.Fatal("witness binding changed", result)
	}
	raw, err := os.ReadFile(f.out)
	if err != nil || writerDigest(raw) != result.SHA || !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("witness output", err)
	}
	info, err := os.Stat(f.out)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("witness output permissions", info, err)
	}
	var signed SignedSourceRetirementWitnessV1
	if json.Unmarshal(raw, &signed) != nil || signed.Witness != result.Witness {
		t.Fatal("noncanonical witness result")
	}
	retirementRaw, err := os.ReadFile(f.source.out)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifySignedSourceRetirementV1(retirementRaw, f.source.public, raw, f.witnessPublic,
		SourceRetirementPinsV1{
			RetirementDocumentSHA: f.retirement.SHA, SourcePublicKeySHA: f.retirement.PublicKeySHA,
			WitnessDocumentSHA: result.SHA, WitnessPublicKeySHA: result.PublicKeySHA,
			FenceDocumentSHA: f.source.fenceSHA, InventorySHA: f.source.inventorySHA,
			DestinationSHA: f.source.destinationSHA,
		})
	if err != nil {
		t.Fatal("emitted witness is not verifier-compatible", err)
	}
	if _, _, _, mutation, err := verified.Evidence(); err != nil || mutation {
		t.Fatal("verified witness granted mutation", mutation, err)
	}
	again, err := f.write()
	if err != nil || again != result {
		t.Fatal("identical retry did not replay", again, err)
	}
}

func TestWriteSourceRetirementWitnessV1FailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, *retirementWitnessWriterFixture)
	}{
		{"same source and witness key", func(_ *testing.T, f *retirementWitnessWriterFixture) { f.witnessKey = f.source.key }},
		{"retirement pin mismatch", func(_ *testing.T, f *retirementWitnessWriterFixture) {
			f.pins.RetirementDocumentSHA = strings.Repeat("3", 64)
		}},
		{"source key pin mismatch", func(_ *testing.T, f *retirementWitnessWriterFixture) {
			f.pins.SourcePublicKeySHA = strings.Repeat("4", 64)
		}},
		{"fence pin mismatch", func(_ *testing.T, f *retirementWitnessWriterFixture) {
			f.pins.FenceDocumentSHA = strings.Repeat("5", 64)
		}},
		{"inventory pin mismatch", func(_ *testing.T, f *retirementWitnessWriterFixture) { f.pins.InventorySHA = strings.Repeat("6", 64) }},
		{"destination pin mismatch", func(_ *testing.T, f *retirementWitnessWriterFixture) { f.pins.DestinationSHA = strings.Repeat("7", 64) }},
		{"witness before fence", func(_ *testing.T, f *retirementWitnessWriterFixture) { f.witnessedNS = f.source.fenceObservedNS - 1 }},
		{"stale witness", func(_ *testing.T, f *retirementWitnessWriterFixture) {
			f.witnessedNS = f.source.fenceObservedNS + maxRetirementWitnessDelayNano + 1
		}},
		{"public source key", func(t *testing.T, f *retirementWitnessWriterFixture) {
			if err := os.Chmod(f.publicPath, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink source key", func(t *testing.T, f *retirementWitnessWriterFixture) {
			link := filepath.Join(filepath.Dir(f.publicPath), "source-link.pub")
			if err := os.Symlink(f.publicPath, link); err != nil {
				t.Fatal(err)
			}
			f.publicPath = link
		}},
		{"public witness key", func(t *testing.T, f *retirementWitnessWriterFixture) {
			if err := os.Chmod(f.witnessKey, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed witness key", func(t *testing.T, f *retirementWitnessWriterFixture) {
			raw, err := os.ReadFile(f.witnessKey)
			if err != nil {
				t.Fatal(err)
			}
			raw[len(raw)-1] ^= 0xff
			if err = os.WriteFile(f.witnessKey, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"public fence", func(t *testing.T, f *retirementWitnessWriterFixture) {
			if err := os.Chmod(f.source.fence, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink fence", func(t *testing.T, f *retirementWitnessWriterFixture) {
			link := filepath.Join(filepath.Dir(f.source.fence), "witness-fence-link")
			if err := os.Symlink(f.source.fence, link); err != nil {
				t.Fatal(err)
			}
			f.source.fence = link
		}},
		{"foreign existing output", func(t *testing.T, f *retirementWitnessWriterFixture) {
			if err := os.WriteFile(f.out, []byte("foreign\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"relative output", func(_ *testing.T, f *retirementWitnessWriterFixture) { f.out = "witness.json" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newRetirementWitnessWriterFixture(t)
			test.change(t, &f)
			if _, err := f.write(); err == nil {
				t.Fatal("unsafe witness emitted")
			}
		})
	}
}
