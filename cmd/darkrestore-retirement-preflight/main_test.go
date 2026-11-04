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
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func preflightDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestRetirementPreflightCLIReadsCompletePinnedManifest(t *testing.T) {
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
	epoch := strings.Repeat("a", 32)
	sourceSHA := strings.Repeat("b", 64)
	receiptSHA := strings.Repeat("c", 64)
	authoritySHA := strings.Repeat("d", 64)
	journalSHA := strings.Repeat("e", 64)
	handoffSHA := strings.Repeat("f", 64)
	sourcePublicSHA := preflightDigest(sourcePublic)
	inventory := darkrestore.LocalHandoffInventoryV1{
		Version: 1, SourceSHA: sourceSHA, SourceID: "synthetic-declared-source",
		Sources: []darkrestore.LocalHandoffSourcePinV1{{
			EpochID: epoch, Sequence: 7, ReceiptSHA: receiptSHA,
			AuthoritySHA: authoritySHA, JournalSHA: journalSHA,
			HandoffSHA: handoffSHA, SignerPublicSHA: sourcePublicSHA,
		}},
	}
	inventorySHA, err := darkrestore.DigestLocalHandoffInventoryV1(inventory)
	if err != nil {
		t.Fatal(err)
	}
	destinationSHA := strings.Repeat("1", 64)
	fenceSHA := preflightDigest([]byte("opaque synthetic fence\n"))
	statement := darkrestore.SourceRetirementStatementV1{
		Version: 1, Scope: darkrestore.SourceRetirementScopeV1,
		SourceSHA: sourceSHA, SourceID: inventory.SourceID,
		InventorySHA: inventorySHA, DestinationSHA: destinationSHA,
		EpochID: epoch, Sequence: 7, TerminalReceiptSHA: receiptSHA,
		AuthoritySHA: authoritySHA, JournalSHA: journalSHA, HandoffSHA: handoffSHA,
		SourceSignerPublicSHA: sourcePublicSHA, RetiredUnixNano: 10_000,
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
	retirementSHA := preflightDigest(retirementRaw)
	witness := darkrestore.SourceRetirementWitnessV1{
		Version: 1, Scope: darkrestore.SourceRetirementWitnessScopeV1,
		RetirementDocumentSHA: retirementSHA, SourcePublicKeySHA: sourcePublicSHA,
		InventorySHA: inventorySHA, DestinationSHA: destinationSHA,
		WitnessedUnixNano: 12_000,
	}
	message, err = darkrestore.SourceRetirementWitnessSigningMessageV1(witness)
	if err != nil {
		t.Fatal(err)
	}
	witnessRaw, err := json.Marshal(darkrestore.SignedSourceRetirementWitnessV1{
		Version: 1, Witness: witness, Signature: ed25519.Sign(witnessPrivate, message),
	})
	if err != nil {
		t.Fatal(err)
	}
	witnessRaw = append(witnessRaw, '\n')
	write := func(name string, raw []byte) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	manifest := darkrestore.SourceRetirementManifestV1{
		Version: 1, Inventory: inventory, InventorySHA: inventorySHA,
		DestinationSHA: destinationSHA,
		Sources: []darkrestore.SourceRetirementManifestSourceV1{{
			RetirementDocumentPath: write("retirement.json", retirementRaw),
			SourcePublicKeyPath:    write("source.pub", sourcePublic),
			WitnessDocumentPath:    write("witness.json", witnessRaw),
			WitnessPublicKeyPath:   write("witness.pub", witnessPublic),
			Pins: darkrestore.SourceRetirementPinsV1{
				RetirementDocumentSHA: retirementSHA, SourcePublicKeySHA: sourcePublicSHA,
				WitnessDocumentSHA: preflightDigest(witnessRaw), WitnessPublicKeySHA: preflightDigest(witnessPublic),
				FenceDocumentSHA: fenceSHA, InventorySHA: inventorySHA, DestinationSHA: destinationSHA,
			},
		}},
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw = append(manifestRaw, '\n')
	manifestPath := write("manifest.json", manifestRaw)
	manifestSHA := preflightDigest(manifestRaw)
	args := []string{"-manifest", manifestPath, "-manifest-sha256", manifestSHA}
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatal("preflight CLI failed", code, stderr.String())
	}
	var report darkrestore.SourceRetirementManifestReportV1
	if json.Unmarshal(stdout.Bytes(), &report) != nil || report.ManifestSHA != manifestSHA ||
		report.Coverage.DeclaredSources != 1 || report.Coverage.AuthorizesActivation || report.AuthorizesActivation {
		t.Fatal("preflight report changed authority", stdout.String())
	}
	if code := run([]string{"-manifest", manifestPath, "-manifest-sha256", strings.Repeat("2", 64)},
		&bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatal("foreign manifest pin accepted", code)
	}
	if code := run(nil, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatal("missing inputs accepted", code)
	}

	manifestReport, err := darkrestore.VerifySourceRetirementManifestFileV1(manifestPath, manifestSHA)
	if err != nil {
		t.Fatal(err)
	}
	approvalPublic, approvalPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	approval := darkrestore.HandoffCoverageApprovalV1{
		Version: 1, InventorySHA: inventorySHA, SourceSHA: sourceSHA, SourceID: inventory.SourceID,
		Scope: darkrestore.HandoffCoverageScopeV1, Statement: darkrestore.HandoffCoverageStatementV1,
		DeclaredSources: 1, EpochIDs: []string{epoch},
		InventoryCompleteAsserted: true, DeclaredSourcesRetired: true,
		RetirementBundleSHA: manifestReport.Coverage.BundleSHA, ApprovedUnixNano: 12_001,
		DestinationSHA: destinationSHA, DestinationStateSHA: strings.Repeat("2", 64),
		NativeBaselineSHA: strings.Repeat("3", 64), ImportReceiptSHA: strings.Repeat("4", 64),
		BillingAttributionSHA: strings.Repeat("5", 64), BillingAttribution: "disjoint_source_epoch_deltas",
	}
	approvalValue, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	approvalMessage := append([]byte("DARK-PRO-DARKRESTORE-HANDOFF-COVERAGE-v1\x00"), approvalValue...)
	approvalRaw, err := json.Marshal(darkrestore.SignedHandoffCoverageApprovalV1{
		Version: 1, Approval: approval, Signature: ed25519.Sign(approvalPrivate, approvalMessage),
	})
	if err != nil {
		t.Fatal(err)
	}
	approvalRaw = append(approvalRaw, '\n')
	approvalPath := write("coverage-approval.json", approvalRaw)
	approvalPublicPath := write("coverage-approval.pub", approvalPublic)
	approvalArgs := append(append([]string(nil), args...),
		"-coverage-approval", approvalPath,
		"-coverage-approval-sha256", preflightDigest(approvalRaw),
		"-coverage-approval-public-key", approvalPublicPath,
		"-coverage-approval-public-key-sha256", preflightDigest(approvalPublic),
	)
	stdout.Reset()
	stderr.Reset()
	if code := run(approvalArgs, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatal("approval preflight CLI failed", code, stderr.String())
	}
	var approvalReport darkrestore.SourceRetirementApprovalReportV1
	if json.Unmarshal(stdout.Bytes(), &approvalReport) != nil || approvalReport.AuthorizesActivation ||
		approvalReport.CoverageApprovalSHA != preflightDigest(approvalRaw) ||
		approvalReport.Manifest.Coverage.BundleSHA != manifestReport.Coverage.BundleSHA {
		t.Fatal("approval preflight report changed authority", stdout.String())
	}
	partialArgs := append(append([]string(nil), args...), "-coverage-approval", approvalPath)
	if code := run(partialArgs, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatal("partial approval inputs accepted", code)
	}
}
