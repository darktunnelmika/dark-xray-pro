//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestAdmissionBootstrapHasNoCustomerIdentityOrListener(t *testing.T) {
	source := testCandidate(t, 19841, "one")
	original := append([]byte(nil), source.Config...)
	desired, err := xrayconfig.WithUsageAPI(source, "127.0.0.1:19842")
	if err != nil {
		t.Fatal(err)
	}
	boot, payload, endpoint, err := xrayconfig.UsageBootstrap(desired)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "127.0.0.1:19842" || bytes.Contains(boot.Config, []byte("af469f46")) || bytes.Contains(boot.Config, []byte("privateKey")) || bytes.Contains(boot.Config, []byte("19841")) {
		t.Fatal("customer authority escaped into bootstrap")
	}
	if !bytes.Contains(payload, []byte("af469f46")) || !bytes.Equal(source.Config, original) || desired.SHA256 == source.SHA256 || desired.ManifestSHA256 == source.ManifestSHA256 {
		t.Fatal("desired revision or exact activation payload lost")
	}
	if _, _, _, err = xrayconfig.UsageBootstrap(source); err == nil {
		t.Fatal("ordinary candidate unexpectedly allowed admission")
	}
}
func TestAdmissionBootstrapRejectsUnreviewedControlAndTrafficPaths(t *testing.T) {
	source := testCandidate(t, 19841, "one")
	for _, endpoint := range []string{"0.0.0.0:19842", "localhost:19842", "127.0.0.1:019842", "127.0.0.1:19841", "127.0.0.1:2087", "127.0.0.1:8443"} {
		if _, err := xrayconfig.WithUsageAPI(source, endpoint); err == nil {
			t.Fatal("unsafe or colliding API endpoint", endpoint)
		}
	}
	candidate, err := xrayconfig.WithUsageAPI(source, "127.0.0.1:19842")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]any){
		"extra-active-topology": func(d map[string]any) { d["reverse"] = map[string]any{} },
		"missing-downlink": func(d map[string]any) {
			d["policy"].(map[string]any)["levels"].(map[string]any)["0"].(map[string]any)["statsUserDownlink"] = false
		},
		"reverse-outbound": func(d map[string]any) { d["outbounds"].([]any)[0].(map[string]any)["protocol"] = "vless" },
		"writable-log":     func(d map[string]any) { d["log"].(map[string]any)["access"] = "/tmp/not-permitted" },
		"extra-api-service": func(d map[string]any) {
			d["api"].(map[string]any)["services"] = []string{"HandlerService", "StatsService", "RoutingService"}
		},
		"fallback": func(d map[string]any) {
			d["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["fallbacks"] = []any{map[string]any{"dest": 80}}
		},
		"wrong-user-level": func(d map[string]any) {
			d["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["clients"].([]any)[0].(map[string]any)["level"] = 1
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var doc map[string]any
			if json.Unmarshal(candidate.Config, &doc) != nil {
				t.Fatal("fixture")
			}
			change(doc)
			raw, _ := json.Marshal(doc)
			bad := candidate
			bad.Config = raw
			sum := sha256.Sum256(raw)
			bad.SHA256 = hex.EncodeToString(sum[:])
			if _, _, _, err := xrayconfig.UsageBootstrap(bad); err == nil {
				t.Fatal("unreviewed bootstrap topology accepted")
			}
		})
	}
}

func TestAdmissionMarkedCandidateCannotUseOrdinaryApply(t *testing.T) {
	o := testOptions(t)
	r := mustOpen(t, o)
	c, e := xrayconfig.WithUsageAPI(testCandidate(t, 19841, "one"), "127.0.0.1:19842")
	if e != nil {
		t.Fatal(e)
	}
	l := grant(t, o, 1, c, 30*time.Second)
	if _, e = r.Apply(context.Background(), Revision{1, c}, l); !errors.Is(e, ErrUsageSettlement) {
		t.Fatal("ordinary Apply bypassed baseline receipt", e)
	}
	if r.Snapshot().Highwater != 0 || r.process != nil {
		t.Fatal("refused bypass modified runtime")
	}
	l.Signature = "bad"
	if _, e = r.PrepareUsageAdmission(context.Background(), Revision{1, c}, l); !errors.Is(e, ErrLease) {
		t.Fatal("unsigned prepare accepted", e)
	}
	if r.UsageStatus().Required || r.Snapshot().Highwater != 0 {
		t.Fatal("invalid lease wrote admission intent")
	}
}
func TestAdmissionJournalRejectsDowngradeAndDescriptorTampering(t *testing.T) {
	for _, mode := range []string{"version-one", "version-two", "missing-descriptor", "wrong-bootstrap", "wrong-endpoint"} {
		t.Run(mode, func(t *testing.T) {
			o := testOptions(t)
			r := mustOpen(t, o)
			c, e := xrayconfig.WithUsageAPI(testCandidate(t, 19841, "one"), "127.0.0.1:19842")
			if e != nil {
				t.Fatal(e)
			}
			boot, _, endpoint, e := xrayconfig.UsageBootstrap(c)
			if e != nil {
				t.Fatal(e)
			}
			j := journal{Version: 3, Highwater: 1, Good: freeze(Revision{1, c}), Admission: &usageAdmission{StatsEndpoint: endpoint, BootstrapSHA: boot.SHA256}}
			switch mode {
			case "version-one":
				j.Version = 1
			case "version-two":
				j.Version = 2
			case "missing-descriptor":
				j.Admission = nil
			case "wrong-bootstrap":
				j.Admission.BootstrapSHA = c.SHA256
			case "wrong-endpoint":
				j.Admission.StatsEndpoint = "127.0.0.1:19843"
			}
			if e = r.disk.save(j); e != nil {
				t.Fatal(e)
			}
			r.Close()
			if reopened, e := Open(o); !errors.Is(e, ErrCorrupt) {
				if reopened != nil {
					reopened.Close()
				}
				t.Fatal("damaged admission journal adopted", e)
			}
		})
	}
}
