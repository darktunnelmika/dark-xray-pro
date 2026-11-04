//go:build linux

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func cliDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func invokeRetirementWitness(args []string, stdout, stderr *bytes.Buffer) int {
	binary := os.Getenv("DARK_PRO_TEST_RETIREMENT_WITNESS_BINARY")
	if binary == "" {
		return run(args, stdout, stderr)
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return -1
	}
	return 0
}

func TestWitnessCLIEmitsVerifierCompatibleReceipt(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	sourcePublic, sourcePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	witnessPublic, witnessPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fenceRaw := []byte("opaque synthetic fence\n")
	fenceSHA := cliDigest(fenceRaw)
	inventorySHA := strings.Repeat("1", 64)
	destinationSHA := strings.Repeat("2", 64)
	statement := darkrestore.SourceRetirementStatementV1{
		Version: 1, Scope: darkrestore.SourceRetirementScopeV1,
		SourceSHA: strings.Repeat("3", 64), SourceID: "synthetic-source",
		InventorySHA: inventorySHA, DestinationSHA: destinationSHA,
		EpochID: strings.Repeat("4", 32), Sequence: 7,
		TerminalReceiptSHA: strings.Repeat("5", 64), AuthoritySHA: strings.Repeat("6", 64),
		JournalSHA: strings.Repeat("7", 64), HandoffSHA: strings.Repeat("8", 64),
		SourceSignerPublicSHA: cliDigest(sourcePublic), RetiredUnixNano: 10_000,
		FenceDocumentSHA: fenceSHA, FenceObservedUnixNano: 11_000,
	}
	message, err := darkrestore.SourceRetirementSigningMessageV1(statement)
	if err != nil {
		t.Fatal(err)
	}
	retirementRaw, err := json.Marshal(darkrestore.SignedSourceRetirementV1{
		Version: 1, Statement: statement, Signature: ed25519.Sign(sourcePrivate, message),
	})
	if err != nil {
		t.Fatal(err)
	}
	retirementRaw = append(retirementRaw, '\n')
	retirementPath := filepath.Join(dir, "retirement.json")
	sourcePublicPath := filepath.Join(dir, "source.pub")
	fencePath := filepath.Join(dir, "fence.bin")
	witnessKeyPath := filepath.Join(dir, "witness.key")
	witnessPath := filepath.Join(dir, "witness.json")
	for path, raw := range map[string][]byte{
		retirementPath: retirementRaw, sourcePublicPath: sourcePublic,
		fencePath: fenceRaw, witnessKeyPath: witnessPrivate,
	} {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{
		"-retirement-document", retirementPath,
		"-retirement-document-sha256", cliDigest(retirementRaw),
		"-source-public-key", sourcePublicPath,
		"-source-public-key-sha256", cliDigest(sourcePublic),
		"-active-fence-document", fencePath,
		"-active-fence-document-sha256", fenceSHA,
		"-inventory-sha256", inventorySHA,
		"-destination-sha256", destinationSHA,
		"-witness-key-file", witnessKeyPath,
		"-witness-output", witnessPath,
		"-witnessed-unix-nano", strconv.FormatInt(12_000, 10),
	}
	var stdout, stderr bytes.Buffer
	if code := invokeRetirementWitness(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatal("witness CLI failed", code, stderr.String())
	}
	var result darkrestore.SourceRetirementWitnessFileV1
	if json.Unmarshal(stdout.Bytes(), &result) != nil || result.AuthorizesActivation {
		t.Fatal("invalid CLI result", stdout.String())
	}
	witnessRaw, err := os.ReadFile(witnessPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = darkrestore.VerifySignedSourceRetirementV1(retirementRaw, sourcePublic, witnessRaw, witnessPublic,
		darkrestore.SourceRetirementPinsV1{
			RetirementDocumentSHA: cliDigest(retirementRaw), SourcePublicKeySHA: cliDigest(sourcePublic),
			WitnessDocumentSHA: result.SHA, WitnessPublicKeySHA: result.PublicKeySHA,
			FenceDocumentSHA: fenceSHA, InventorySHA: inventorySHA, DestinationSHA: destinationSHA,
		}); err != nil {
		t.Fatal("CLI receipt is not verifier-compatible", err)
	}
	var replay bytes.Buffer
	if code := invokeRetirementWitness(args, &replay, &stderr); code != 0 || replay.String() != stdout.String() {
		t.Fatal("CLI exact replay failed", code, stderr.String())
	}

	unsafeArgs := append([]string(nil), args...)
	for i := range unsafeArgs {
		if unsafeArgs[i] == witnessKeyPath {
			unsafeArgs[i] = filepath.Join(dir, "source.key")
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "source.key"), sourcePrivate, 0600); err != nil {
		t.Fatal(err)
	}
	if code := invokeRetirementWitness(unsafeArgs, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatal("same-key witness accepted", code)
	}
	if code := invokeRetirementWitness(nil, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatal("missing CLI inputs accepted", code)
	}
}
