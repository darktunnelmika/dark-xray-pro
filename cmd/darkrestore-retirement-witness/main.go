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

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("dark-pro-darkrestore-retirement-witness", flag.ContinueOnError)
	flags.SetOutput(stderr)
	retirement := flags.String("retirement-document", "", "private canonical source retirement document")
	retirementSHA := flags.String("retirement-document-sha256", "", "independently pinned source retirement SHA-256")
	sourcePublic := flags.String("source-public-key", "", "private copy of the source Ed25519 public key")
	sourcePublicSHA := flags.String("source-public-key-sha256", "", "independently pinned source public-key SHA-256")
	fence := flags.String("active-fence-document", "", "private opaque active-fence document")
	fenceSHA := flags.String("active-fence-document-sha256", "", "independently pinned active-fence SHA-256")
	inventorySHA := flags.String("inventory-sha256", "", "independently pinned declared inventory SHA-256")
	destinationSHA := flags.String("destination-sha256", "", "independently pinned destination SHA-256")
	witnessKey := flags.String("witness-key-file", "", "private raw 64-byte independent Ed25519 key")
	output := flags.String("witness-output", "", "new private canonical witness document")
	witnessedNS := flags.Int64("witnessed-unix-nano", 0, "witness assertion timestamp in Unix nanoseconds")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	for _, value := range []string{*retirement, *retirementSHA, *sourcePublic, *sourcePublicSHA, *fence,
		*fenceSHA, *inventorySHA, *destinationSHA, *witnessKey, *output} {
		if value == "" {
			fmt.Fprintln(stderr, "invalid inputs")
			return 2
		}
	}
	if flags.NArg() != 0 || *witnessedNS <= 0 {
		fmt.Fprintln(stderr, "invalid inputs")
		return 2
	}
	result, err := darkrestore.WriteSourceRetirementWitnessV1(*retirement, *sourcePublic, *fence, *witnessKey, *output,
		darkrestore.SourceRetirementStatementPinsV1{
			RetirementDocumentSHA: *retirementSHA, SourcePublicKeySHA: *sourcePublicSHA,
			FenceDocumentSHA: *fenceSHA, InventorySHA: *inventorySHA, DestinationSHA: *destinationSHA,
		}, *witnessedNS)
	if err != nil {
		fmt.Fprintln(stderr, "source retirement witness unverified")
		return 1
	}
	if err = json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return 0
}
