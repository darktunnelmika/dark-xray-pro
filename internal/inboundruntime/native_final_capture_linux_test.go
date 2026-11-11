//go:build linux

package inboundruntime

import (
	"bytes"
	"context"
	"encoding/json"
	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"golang.org/x/sys/unix"
	"os"
	"strings"
	"testing"
)

func sealedRecordNative(t *testing.T, mutate func(*barrier.FinalRecord), changeBytes func([]byte) []byte, sealed bool) *Native {
	t.Helper()
	f, binding, e := newNativeFinalCapture()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close() })
	p := &Native{finalCapture: f, finalBinding: binding, autonomousLease: true, configSHA: strings.Repeat("b", 64), seen: map[string]bool{}, last: map[string]ix.Counter{}, principals: []ix.Principal{{ID: "customer-0001", Engine: "xray", Counter: "user>>>capture-fixture>>>traffic"}}}
	p.identity.PID = os.Getpid()
	r := barrier.FinalRecord{Version: barrier.FinalRecordVersion, Final: true, PID: p.identity.PID, Binding: binding, ConfigSHA: p.configSHA, Autonomous: true, Counters: map[string]int64{"user>>>capture-fixture>>>traffic>>>uplink": 3, "user>>>capture-fixture>>>traffic>>>downlink": 5}}
	if mutate != nil {
		mutate(&r)
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if changeBytes != nil {
		b = changeBytes(b)
	}
	if _, e = f.WriteAt(b, 0); e != nil {
		t.Fatal(e)
	}
	if sealed {
		if _, e = unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); e != nil {
			t.Fatal(e)
		}
	}
	return p
}
func TestExitedNativeFinalCaptureIsAuthenticAndNonceBound(t *testing.T) {
	p := sealedRecordNative(t, nil, nil, true)
	nonce := strings.Repeat("c", 64)
	for i := 0; i < 2; i++ {
		values, e := p.Final(context.Background(), nonce)
		if e != nil || len(values) != 1 || values[0].Upload != 3 || values[0].Download != 5 {
			t.Fatal("authentic final was not recovered", e, values)
		}
	}
	if _, e := p.Final(context.Background(), strings.Repeat("d", 64)); e == nil {
		t.Fatal("captured final accepted a second nonce")
	}
	if _, e := p.finalCapture.WriteAt([]byte("tamper"), 0); e == nil {
		t.Fatal("kernel capture was mutable")
	}
}
func TestExitedNativeFinalCaptureRejectsMissingOrCorruptedProof(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*barrier.FinalRecord)
		bytes  func([]byte) []byte
		sealed bool
	}{
		{name: "unsealed", sealed: false},
		{name: "wrong_birth", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Binding = strings.Repeat("f", 64) }},
		{name: "wrong_pid", sealed: true, mutate: func(r *barrier.FinalRecord) { r.PID++ }},
		{name: "wrong_config", sealed: true, mutate: func(r *barrier.FinalRecord) { r.ConfigSHA = strings.Repeat("f", 64) }},
		{name: "not_final", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Final = false }},
		{name: "wrong_version", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Version++ }},
		{name: "different_bound_nonce", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Nonce = strings.Repeat("f", 64) }},
		{name: "missing_manual_nonce", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Autonomous = false }},
		{name: "missing_counter_proof", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Counters = nil }},
		{name: "negative_counter", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Counters["user>>>capture-fixture>>>traffic>>>uplink"] = -1 }},
		{name: "unrecognized_customer", sealed: true, mutate: func(r *barrier.FinalRecord) { r.Counters["user>>>other>>>traffic>>>uplink"] = 1 }},
		{name: "duplicate_field", sealed: true, bytes: func(b []byte) []byte {
			return bytes.Replace(b, []byte("\"version\":1"), []byte("\"version\":1,\"version\":1"), 1)
		}},
		{name: "oversize", sealed: true, bytes: func(b []byte) []byte { return append(b, bytes.Repeat([]byte(" "), barrier.FinalRecordLimit)...) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := sealedRecordNative(t, c.mutate, c.bytes, c.sealed)
			if v, e := p.Final(context.Background(), strings.Repeat("c", 64)); e == nil || v != nil {
				t.Fatal("invalid capture became a final receipt")
			}
		})
	}
}
func TestExitedNativeFinalCaptureKeepsCounterContinuityAndCapabilityFence(t *testing.T) {
	t.Run("legacy_capability", func(t *testing.T) {
		p := sealedRecordNative(t, nil, nil, true)
		p.autonomousLease = false
		if _, e := p.Final(context.Background(), strings.Repeat("c", 64)); e == nil {
			t.Fatal("legacy process acquired escrow proof")
		}
	})
	t.Run("counter_decrease", func(t *testing.T) {
		p := sealedRecordNative(t, nil, nil, true)
		p.seen["customer-0001"] = true
		p.last["customer-0001"] = ix.Counter{PrincipalID: "customer-0001", Upload: 9, Download: 11}
		if _, e := p.Final(context.Background(), strings.Repeat("c", 64)); e == nil {
			t.Fatal("final counters decreased")
		}
	})
	t.Run("observed_identity_missing", func(t *testing.T) {
		p := sealedRecordNative(t, func(r *barrier.FinalRecord) { r.Counters = map[string]int64{} }, nil, true)
		p.seen["customer-0001"] = true
		if _, e := p.Final(context.Background(), strings.Repeat("c", 64)); e == nil {
			t.Fatal("observed identity was inferred as zero")
		}
	})
}
