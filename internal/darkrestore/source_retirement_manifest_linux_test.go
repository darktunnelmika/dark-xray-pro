//go:build linux

package darkrestore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type retirementManifestFixture struct {
	manifest SourceRetirementManifestV1
	path     string
	sha      string
}

func newRetirementManifestFixture(t *testing.T) retirementManifestFixture {
	t.Helper()
	f := newRetirementFixture(t)
	files := []SourceRetirementFilesV1{
		retirementFilesFixture(t, f, 1),
		retirementFilesFixture(t, f, 0),
	}
	sources := make([]SourceRetirementManifestSourceV1, 0, len(files))
	for _, source := range files {
		sources = append(sources, SourceRetirementManifestSourceV1{
			RetirementDocumentPath: source.RetirementDocumentPath,
			SourcePublicKeyPath:    source.SourcePublicKeyPath,
			WitnessDocumentPath:    source.WitnessDocumentPath,
			WitnessPublicKeyPath:   source.WitnessPublicKeyPath,
			Pins:                   source.Pins,
		})
	}
	manifest := SourceRetirementManifestV1{
		Version: 1, Inventory: f.inventory, InventorySHA: f.inventorySH,
		DestinationSHA: retirementTestDestination, Sources: sources,
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "retirement-manifest.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return retirementManifestFixture{manifest: manifest, path: path, sha: retirementSHA(raw)}
}

func (f *retirementManifestFixture) rewrite(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err = os.WriteFile(f.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.sha = retirementSHA(raw)
}

func TestVerifySourceRetirementManifestFileV1ChecksCompleteSelection(t *testing.T) {
	f := newRetirementManifestFixture(t)
	report, err := VerifySourceRetirementManifestFileV1(f.path, f.sha)
	if err != nil || report.ManifestSHA != f.sha || report.AuthorizesActivation ||
		report.Coverage.AuthorizesActivation || report.Coverage.DeclaredSources != 2 || report.Coverage.BundleSHA == "" {
		t.Fatal("complete pinned manifest rejected", report, err)
	}
}

func TestVerifySourceRetirementManifestFileV1FailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, *retirementManifestFixture)
	}{
		{"manifest pin mismatch", func(_ *testing.T, f *retirementManifestFixture) { f.sha = retirementSHA([]byte("foreign")) }},
		{"missing source", func(t *testing.T, f *retirementManifestFixture) {
			f.manifest.Sources = f.manifest.Sources[:1]
			f.rewrite(t)
		}},
		{"duplicate source", func(t *testing.T, f *retirementManifestFixture) {
			f.manifest.Sources[1] = f.manifest.Sources[0]
			f.rewrite(t)
		}},
		{"stale inventory", func(t *testing.T, f *retirementManifestFixture) {
			f.manifest.InventorySHA = retirementSHA([]byte("stale"))
			f.rewrite(t)
		}},
		{"foreign destination", func(t *testing.T, f *retirementManifestFixture) {
			f.manifest.DestinationSHA = retirementSHA([]byte("foreign"))
			f.rewrite(t)
		}},
		{"missing file path", func(t *testing.T, f *retirementManifestFixture) {
			f.manifest.Sources[0].WitnessDocumentPath = ""
			f.rewrite(t)
		}},
		{"world-readable manifest", func(t *testing.T, f *retirementManifestFixture) {
			if err := os.Chmod(f.path, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink manifest", func(t *testing.T, f *retirementManifestFixture) {
			link := filepath.Join(filepath.Dir(f.path), "manifest-link.json")
			if err := os.Symlink(f.path, link); err != nil {
				t.Fatal(err)
			}
			f.path = link
		}},
		{"noncanonical manifest", func(t *testing.T, f *retirementManifestFixture) {
			raw, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			raw = append([]byte(" "), raw...)
			if err = os.WriteFile(f.path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			f.sha = retirementSHA(raw)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newRetirementManifestFixture(t)
			test.change(t, &f)
			if _, err := VerifySourceRetirementManifestFileV1(f.path, f.sha); err == nil {
				t.Fatal("unsafe retirement manifest accepted")
			}
		})
	}
}
