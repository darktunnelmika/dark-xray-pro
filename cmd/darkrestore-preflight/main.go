// darkrestore-preflight inspects a stand-alone, checkpointed OLD HUB SQLite
// export. It cannot import, start Xray or mutate any native PRO account.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func execute(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("darkrestore-preflight", flag.ContinueOnError)
	// Do not echo user-provided paths or database errors (potential secrets).
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "absolute path to an OFFLINE checkpointed SQLite file")
	verified := flags.Bool("offline-copy-verified", false, "acknowledge source is not the live OLD HUB database")
	atUnix := flags.Int64("as-of-unix", 0, "optional explicit audit timestamp, Unix seconds")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 0 {
		fmt.Fprintln(errOut, "ERROR: invalid offline preflight arguments")
		return 2
	}
	if !*verified || *source == "" {
		fmt.Fprintln(errOut, "ERROR: only a separately checkpointed offline copy with -offline-copy-verified is accepted")
		return 2
	}
	at := time.Now().UTC()
	if *atUnix != 0 {
		if *atUnix < 1 || *atUnix > 253402300799 {
			fmt.Fprintln(errOut, "ERROR: invalid as-of time")
			return 2
		}
		at = time.Unix(*atUnix, 0).UTC()
	}
	report, err := darkrestore.Analyze(context.Background(), *source, at)
	if err != nil {
		fmt.Fprintln(errOut, "ERROR: source copy unsafe or inspection failed; no import or data changes")
		return 3
	}
	data, err := report.JSON()
	if err != nil {
		fmt.Fprintln(errOut, "ERROR: reporting failed")
		return 3
	}
	if _, err = out.Write(append(data, '\n')); err != nil {
		fmt.Fprintln(errOut, "ERROR: report write failed")
		return 3
	}
	if report.Status != "ready_for_manual_review" {
		return 4
	}
	return 0
}
func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }
