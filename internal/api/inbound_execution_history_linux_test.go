//go:build linux

package api

import (
	"fmt"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// The real-traffic fixture has one owner path and no concurrent reporting
// between these reads. Its complete history must equal that path's committed
// counters, including any zero-admission bucket across a minute boundary.
func validateManagedPathHistory(history business.InboundTrafficHistory, traffic business.InboundTraffic) error {
	if history.InboundID != traffic.InboundID || history.Window != "1h" || history.BucketSeconds != 60 || history.Coverage != traffic.Coverage || history.Until-history.From != 3600 || len(traffic.Paths) != 1 || len(history.Buckets) == 0 {
		return fmt.Errorf("invalid history metadata or path count")
	}
	path := traffic.Paths[0]
	principal := "aggregate-" + ix.BytesSHA([]byte(path.NodeID + "\x00" + path.Tag))[:32]
	var upload, download uint64
	seen := make(map[int64]bool)
	for _, bucket := range history.Buckets {
		if bucket.NodeID != path.NodeID || bucket.PrincipalID != principal || bucket.Role != "owner_aggregate" {
			return fmt.Errorf("unexpected history identity: %+v", bucket)
		}
		if bucket.At%60 != 0 || bucket.At < history.From/60*60 || bucket.At > history.Until || seen[bucket.At] {
			return fmt.Errorf("invalid or duplicate history bucket: %d", bucket.At)
		}
		seen[bucket.At] = true
		if bucket.Upload > ^uint64(0)-upload || bucket.Download > ^uint64(0)-download {
			return fmt.Errorf("history counter overflow")
		}
		upload += bucket.Upload
		download += bucket.Download
	}
	if upload == 0 || download == 0 || upload != path.Upload || download != path.Download {
		return fmt.Errorf("history totals %d/%d differ from path %d/%d", upload, download, path.Upload, path.Download)
	}
	return nil
}

func TestManagedPathHistoryMinuteRollover(t *testing.T) {
	path := business.InboundPathTraffic{NodeID: "de", Tag: "fixture-direct", Upload: 120, Download: 250}
	principal := "aggregate-" + ix.BytesSHA([]byte(path.NodeID + "\x00" + path.Tag))[:32]
	bucket := func(at int64, up, down uint64) business.InboundTrafficBucket {
		return business.InboundTrafficBucket{NodeID: "de", PrincipalID: principal, Role: "owner_aggregate", At: at, Upload: up, Download: down}
	}
	for _, tc := range []struct {
		name    string
		buckets []business.InboundTrafficBucket
		invalid bool
	}{
		{"same_minute", []business.InboundTrafficBucket{bucket(7140, 120, 250)}, false},
		{"zero_receipt_previous_minute", []business.InboundTrafficBucket{bucket(7080, 0, 0), bucket(7140, 120, 250)}, false},
		{"payload_across_minutes", []business.InboundTrafficBucket{bucket(7080, 40, 50), bucket(7140, 80, 200)}, false},
		{"missing_payload", []business.InboundTrafficBucket{bucket(7080, 0, 0)}, true},
		{"lost_bytes", []business.InboundTrafficBucket{bucket(7080, 40, 50), bucket(7140, 79, 200)}, true},
		{"extra_bytes", []business.InboundTrafficBucket{bucket(7140, 121, 250)}, true},
		{"duplicate_bucket", []business.InboundTrafficBucket{bucket(7140, 0, 0), bucket(7140, 120, 250)}, true},
		{"unaligned_bucket", []business.InboundTrafficBucket{bucket(7141, 120, 250)}, true},
		{"outside_window", []business.InboundTrafficBucket{bucket(3540, 120, 250)}, true},
		{"future_bucket", []business.InboundTrafficBucket{bucket(7260, 120, 250)}, true},
		{"overflow", []business.InboundTrafficBucket{bucket(7080, ^uint64(0), 50), bucket(7140, 121, 200)}, true},
		{"empty_history", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := business.InboundTrafficHistory{InboundID: 501, Window: "1h", From: 3600, Until: 7200, BucketSeconds: 60, Coverage: "since_path_metering", Buckets: tc.buckets}
			traffic := business.InboundTraffic{InboundID: 501, Coverage: "since_path_metering", Paths: []business.InboundPathTraffic{path}}
			if err := validateManagedPathHistory(history, traffic); (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v: %v", tc.invalid, err)
			}
		})
	}
	for _, field := range []string{"node", "principal", "role", "inbound", "coverage", "window", "bucket_seconds"} {
		t.Run("reject_wrong_"+field, func(t *testing.T) {
			history := business.InboundTrafficHistory{InboundID: 501, Window: "1h", From: 3600, Until: 7200, BucketSeconds: 60, Coverage: "since_path_metering", Buckets: []business.InboundTrafficBucket{bucket(7140, 120, 250)}}
			traffic := business.InboundTraffic{InboundID: 501, Coverage: "since_path_metering", Paths: []business.InboundPathTraffic{path}}
			switch field {
			case "node":
				history.Buckets[0].NodeID = "other"
			case "principal":
				history.Buckets[0].PrincipalID = "other"
			case "role":
				history.Buckets[0].Role = "customer"
			case "inbound":
				history.InboundID++
			case "coverage":
				history.Coverage = "other"
			case "window":
				history.Window = "24h"
			case "bucket_seconds":
				history.BucketSeconds = 120
			}
			if err := validateManagedPathHistory(history, traffic); err == nil {
				t.Fatal("accepted mismatched history")
			}
		})
	}
}
