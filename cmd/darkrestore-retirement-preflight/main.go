//go:build linux

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("dark-pro-darkrestore-retirement-preflight", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifest := flags.String("manifest", "", "owner-private canonical retirement manifest")
	manifestSHA := flags.String("manifest-sha256", "", "independently approved manifest SHA-256")
	approval := flags.String("coverage-approval", "", "owner-private canonical signed coverage approval")
	approvalSHA := flags.String("coverage-approval-sha256", "", "independently pinned coverage approval SHA-256")
	approvalPublic := flags.String("coverage-approval-public-key", "", "owner-private copy of the approval Ed25519 public key")
	approvalPublicSHA := flags.String("coverage-approval-public-key-sha256", "", "independently pinned approval public-key SHA-256")
	destinationState := flags.String("destination-state", "", "owner-private opaque destination-state document")
	destinationStateSHA := flags.String("destination-state-sha256", "", "independently pinned destination-state SHA-256")
	nativeBaseline := flags.String("native-baseline", "", "owner-private opaque native-baseline document")
	nativeBaselineSHA := flags.String("native-baseline-sha256", "", "independently pinned native-baseline SHA-256")
	importReceipt := flags.String("import-receipt", "", "owner-private opaque import-receipt document")
	importReceiptSHA := flags.String("import-receipt-sha256", "", "independently pinned import-receipt SHA-256")
	billingAttribution := flags.String("billing-attribution", "", "owner-private opaque billing-attribution document")
	billingAttributionSHA := flags.String("billing-attribution-sha256", "", "independently pinned billing-attribution SHA-256")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *manifest == "" || *manifestSHA == "" {
		fmt.Fprintln(stderr, "retirement preflight inputs invalid")
		return 2
	}
	approvalValues := []string{*approval, *approvalSHA, *approvalPublic, *approvalPublicSHA}
	approvalInputs := 0
	for _, value := range approvalValues {
		if value != "" {
			approvalInputs++
		}
	}
	if approvalInputs != 0 && approvalInputs != len(approvalValues) {
		fmt.Fprintln(stderr, "retirement preflight inputs invalid")
		return 2
	}
	evidenceValues := []string{*destinationState, *destinationStateSHA, *nativeBaseline, *nativeBaselineSHA,
		*importReceipt, *importReceiptSHA, *billingAttribution, *billingAttributionSHA}
	evidenceInputs := 0
	for _, value := range evidenceValues {
		if value != "" {
			evidenceInputs++
		}
	}
	if evidenceInputs != 0 && (evidenceInputs != len(evidenceValues) || approvalInputs != len(approvalValues)) {
		fmt.Fprintln(stderr, "retirement preflight inputs invalid")
		return 2
	}
	var report any
	var err error
	if evidenceInputs == len(evidenceValues) {
		report, err = darkrestore.VerifySourceRetirementApprovalEvidenceFilesV1(
			*manifest, *manifestSHA, *approval, *approvalSHA, *approvalPublic, *approvalPublicSHA,
			*destinationState, *destinationStateSHA, *nativeBaseline, *nativeBaselineSHA,
			*importReceipt, *importReceiptSHA, *billingAttribution, *billingAttributionSHA,
		)
	} else if approvalInputs == len(approvalValues) {
		report, err = darkrestore.VerifySourceRetirementApprovalFilesV1(
			*manifest, *manifestSHA, *approval, *approvalSHA, *approvalPublic, *approvalPublicSHA,
		)
	} else {
		report, err = darkrestore.VerifySourceRetirementManifestFileV1(*manifest, *manifestSHA)
	}
	if err != nil {
		fmt.Fprintln(stderr, "retirement bundle unverified")
		return 1
	}
	if err = json.NewEncoder(stdout).Encode(report); err != nil {
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
