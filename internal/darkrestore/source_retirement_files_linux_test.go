//go:build linux

package darkrestore

import (
	"os"
	"path/filepath"
	"testing"
)

func retirementFilesFixture(t *testing.T, f retirementFixture, i int) SourceRetirementFilesV1 {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, raw []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	return SourceRetirementFilesV1{
		RetirementDocumentPath: write("retirement.json", f.raw[i]),
		SourcePublicKeyPath:    write("source.pub", f.sourceKeys[i]),
		WitnessDocumentPath:    write("witness.json", f.witnessRaw[i]),
		WitnessPublicKeyPath:   write("witness.pub", f.witnessKeys[i]),
		Pins:                   f.pins[i],
	}
}

func TestVerifySourceRetirementFilesV1UsesPrivatePinnedFiles(t *testing.T) {
	f := newRetirementFixture(t)
	files := retirementFilesFixture(t, f, 0)
	verified, err := VerifySourceRetirementFilesV1(files)
	if err != nil {
		t.Fatal(err)
	}
	statement, _, pins, mutation, err := verified.Evidence()
	if err != nil || mutation || statement.EpochID != f.inventory.Sources[0].EpochID || pins != f.pins[0] {
		t.Fatal("trusted file projection changed evidence", mutation, err)
	}
	if err = os.Chmod(files.WitnessDocumentPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifySourceRetirementFilesV1(files); err == nil {
		t.Fatal("world-readable witness accepted")
	}
}

func TestVerifySourceRetirementFilesV1RefusesSymlink(t *testing.T) {
	f := newRetirementFixture(t)
	files := retirementFilesFixture(t, f, 0)
	link := filepath.Join(filepath.Dir(files.RetirementDocumentPath), "retirement-link.json")
	if err := os.Symlink(files.RetirementDocumentPath, link); err != nil {
		t.Fatal(err)
	}
	files.RetirementDocumentPath = link
	if _, err := VerifySourceRetirementFilesV1(files); err == nil {
		t.Fatal("symlink-selected retirement accepted")
	}
}

func TestVerifySourceRetirementBundleFilesV1RequiresEveryDeclaredEpoch(t *testing.T) {
	f := newRetirementFixture(t)
	files := []SourceRetirementFilesV1{
		retirementFilesFixture(t, f, 1),
		retirementFilesFixture(t, f, 0),
	}
	in := SourceRetirementBundleFilesV1{
		Inventory: f.inventory, InventorySHA: f.inventorySH,
		DestinationSHA: retirementTestDestination, Sources: files,
	}
	verified, coverage, err := VerifySourceRetirementBundleFilesV1(in)
	if err != nil || len(verified) != len(f.inventory.Sources) || coverage.BundleSHA == "" ||
		coverage.DeclaredSources != len(f.inventory.Sources) || coverage.AuthorizesActivation {
		t.Fatal("complete private retirement bundle rejected", coverage, err)
	}

	missing := in
	missing.Sources = files[:1]
	if _, _, err = VerifySourceRetirementBundleFilesV1(missing); err == nil {
		t.Fatal("partial private retirement bundle accepted")
	}
	duplicate := in
	duplicate.Sources = []SourceRetirementFilesV1{files[0], files[0]}
	if _, _, err = VerifySourceRetirementBundleFilesV1(duplicate); err == nil {
		t.Fatal("duplicate private retirement bundle accepted")
	}
	foreign := in
	foreign.DestinationSHA = retirementSHA([]byte("foreign destination"))
	if _, _, err = VerifySourceRetirementBundleFilesV1(foreign); err == nil {
		t.Fatal("foreign-destination private retirement bundle accepted")
	}
	staleInventory := in
	staleInventory.InventorySHA = retirementSHA([]byte("stale inventory"))
	if _, _, err = VerifySourceRetirementBundleFilesV1(staleInventory); err == nil {
		t.Fatal("stale-inventory private retirement bundle accepted")
	}
}
