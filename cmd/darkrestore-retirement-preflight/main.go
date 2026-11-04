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
	var report any
	var err error
	if approvalInputs == len(approvalValues) {
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
