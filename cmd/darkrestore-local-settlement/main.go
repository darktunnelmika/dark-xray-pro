//go:build linux

// Inspect a privately staged, single-source LAB migration rehearsal.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func main() {
	source := flag.String("source", "", "private pre-admission legacy SQLite copy")
	sha := flag.String("source-sha256", "", "approved pre-admission SHA-256")
	authority := flag.String("authority", "", "private offline copy of the source accounting ledger")
	pin := flag.String("authority-sha256", "", "independently approved authority SHA-256")
	id := flag.String("source-id", "", "legacy Restore ID")
	node := flag.String("node", "", "expected independent source node ID")
	install := flag.String("installation", "", "expected source installation ID")
	at := flag.Int64("as-of-unix", 0, "legacy snapshot timestamp")
	journal := flag.String("journal", "", "private source runtime.enc copy")
	journalSHA := flag.String("journal-sha256", "", "approved final journal SHA-256")
	key := flag.String("state-key-file", "", "source journal key file")
	listen := flag.String("source-listen-ip", "", "expected source supervisor listen IP")
	output := flag.String("export-copy", "", "optional new private M2 SQLite input for one identity")
	handoff := flag.String("signed-handoff-output", "", "optional private signed result for a previously imported identity")
	signingKey := flag.String("handoff-signing-key-file", "", "private raw 64-byte Ed25519 key on the source host")
	retirement := flag.String("source-retirement-output", "", "optional private source-signed retirement statement")
	signedHandoffInput := flag.String("signed-handoff-input", "", "existing private signed handoff bound by the retirement")
	handoffSHA := flag.String("signed-handoff-sha256", "", "independently pinned signed handoff SHA-256")
	inventorySHA := flag.String("inventory-sha256", "", "independently pinned declared inventory SHA-256")
	destinationSHA := flag.String("destination-sha256", "", "independently pinned destination SHA-256")
	fence := flag.String("active-fence-document", "", "private opaque active-fence document")
	fenceSHA := flag.String("active-fence-document-sha256", "", "independently pinned active-fence SHA-256")
	retiredNS := flag.Int64("retired-unix-nano", 0, "source retirement assertion timestamp in Unix nanoseconds")
	fenceObservedNS := flag.Int64("active-fence-observed-unix-nano", 0, "active-fence observation timestamp in Unix nanoseconds")
	flag.Parse()
	retirementFields := []string{*signedHandoffInput, *handoffSHA, *inventorySHA, *destinationSHA, *fence, *fenceSHA}
	allRetirementFields := true
	anyRetirementFields := *retiredNS != 0 || *fenceObservedNS != 0
	for _, value := range retirementFields {
		allRetirementFields = allRetirementFields && value != ""
		anyRetirementFields = anyRetirementFields || value != ""
	}
	modes := 0
	for _, selected := range []bool{*output != "", *handoff != "", *retirement != ""} {
		if selected {
			modes++
		}
	}
	invalidRetirement := (*retirement != "" && (!allRetirementFields || *retiredNS <= 0 || *fenceObservedNS <= 0 || *signingKey == "")) ||
		(*retirement == "" && anyRetirementFields)
	invalidKey := ((*handoff != "" || *retirement != "") != (*signingKey != ""))
	if flag.NArg() != 0 || *at <= 0 || modes > 1 || invalidRetirement || invalidKey {
		fmt.Fprintln(os.Stderr, "invalid inputs")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	seal, err := managedxray.ReadLocalTCPSeal(*journal, *key, *journalSHA, *node, *install, *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "source process exit or final receipt unverified")
		os.Exit(1)
	}
	inputs := darkrestore.LocalInputs{Source: *source, SourceSHA: *sha, Authority: *authority, AuthoritySHA: *pin, ID: *id, Node: *node, Installation: *install, AsOf: time.Unix(*at, 0)}
	var result any
	if *retirement != "" {
		proof, inspectErr := darkrestore.InspectLocalSettlement(ctx, inputs, seal)
		if inspectErr != nil {
			err = inspectErr
		} else {
			result, err = darkrestore.WriteSourceRetirementV1(proof, *signedHandoffInput, *handoffSHA, *signingKey,
				*fence, *fenceSHA, *inventorySHA, *destinationSHA, *retirement, *retiredNS, *fenceObservedNS)
		}
	} else if *handoff != "" {
		result, err = darkrestore.WriteSignedLocalHandoff(ctx, inputs, seal, *signingKey, *handoff)
	} else if *output != "" {
		result, err = darkrestore.ExportLocalImportCopy(ctx, inputs, seal, *output)
	} else {
		result, err = darkrestore.InspectLocalSettlement(ctx, inputs, seal)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "local source accounting unverified")
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(1)
	}
}
