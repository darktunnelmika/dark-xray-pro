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
	flag.Parse()
	if flag.NArg() != 0 || *at <= 0 {
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
	if *output == "" {
		result, err = darkrestore.InspectLocalSettlement(ctx, inputs, seal)
	} else {
		result, err = darkrestore.ExportLocalImportCopy(ctx, inputs, seal, *output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "local source accounting unverified")
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(1)
	}
}
