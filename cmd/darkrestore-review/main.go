// darkrestore-review produces a private, non-executable review of an already
// checkpointed and independently approved old HUB SQLite copy.
// It does NOT import clients or change PRO/legacy services.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func main() {
	var source, out, sha string
	var asOf int64
	var verified bool
	flag.StringVar(&source, "source", "", "absolute path to OFFLINE legacy SQLite copy")
	flag.StringVar(&out, "private-output", "", "new private JSON review path")
	flag.StringVar(&sha, "expected-sha256", "", "approved SHA-256 of offline SQLite copy")
	flag.Int64Var(&asOf, "as-of-unix", 0, "snapshot review date as Unix seconds")
	flag.BoolVar(&verified, "offline-copy-verified", false, "operator attests source is independent and checkpointed")
	flag.Parse()
	failed := func(code string) {
		b, _ := json.Marshal(map[string]any{"status": "blocked", "import_enabled": false, "error": code})
		fmt.Println(string(b))
		os.Exit(1)
	}
	if !verified || !filepath.IsAbs(source) || filepath.Clean(source) != source ||
		!filepath.IsAbs(out) || filepath.Clean(out) != out ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(sha) || asOf <= 0 || flag.NArg() != 0 {
		failed("explicit_offline_input_required")
	}
	parent := filepath.Dir(out)
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		failed("review_path_must_not_exist")
	}
	canonical, e := filepath.EvalSymlinks(parent)
	if e != nil || canonical != parent {
		failed("private_directory_required")
	}
	dir, e := os.Lstat(parent)
	if e != nil || !dir.IsDir() || dir.Mode().Perm()&0077 != 0 {
		failed("private_directory_required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	review, e := darkrestore.BuildReview(ctx, source, sha, time.Unix(asOf, 0))
	if e != nil {
		failed("offline_source_validation_failed")
	}
	if review.Status != "ready_for_review" {
		failed("offline_review_blocked")
	}
	f, e := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		failed("review_create_failed")
	}
	keep := false
	defer func() {
		f.Close()
		if !keep {
			os.Remove(out)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(true)
	if e = enc.Encode(review); e != nil {
		f.Close()
		os.Remove(out)
		failed("review_write_failed")
	}
	if e = f.Sync(); e != nil {
		f.Close()
		os.Remove(out)
		failed("review_sync_failed")
	}
	if e = f.Close(); e != nil {
		os.Remove(out)
		failed("review_close_failed")
	}
	stored, e := os.Open(out)
	if e != nil {
		os.Remove(out)
		failed("review_readback_failed")
	}
	digest := sha256.New()
	_, e = io.Copy(digest, stored)
	stored.Close()
	if e != nil {
		os.Remove(out)
		failed("review_readback_failed")
	}
	keep = true
	summary := map[string]any{"status": "ready_for_review", "import_enabled": false,
		"subscriptions": len(review.Clients), "groups": len(review.Groups),
		"review_sha256": hex.EncodeToString(digest.Sum(nil)), "source_sha256": sha,
		"review_file": "private; path intentionally not printed"}
	output, _ := json.Marshal(summary)
	fmt.Println(string(output))
}
