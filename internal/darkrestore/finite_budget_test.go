package darkrestore

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestFiniteBudgetPreservesHistoryAcrossRenewalAndUnknownBaseline(t *testing.T) {
	history := int64(70)
	native := NativeCutoverCountersV1{Uplink: 7, Downlink: 13}
	before, err := ProjectFiniteCutoverBudgetV1(100, &history, &native)
	if err != nil || *before.RemainingBytes != 10 || *before.TotalBytes != 90 || before.AuthorizesActivation {
		t.Fatal(before, err)
	}
	renewed, err := ProjectFiniteCutoverBudgetV1(150, &history, &native)
	if err != nil || *renewed.RemainingBytes != 60 || *renewed.HistoricalBytes != 70 || *renewed.PostMigrationBytes != 20 {
		t.Fatal("renewal or history counted twice", renewed, err)
	}
	exhausted, err := ProjectFiniteCutoverBudgetV1(80, &history, &native)
	if err != nil || *exhausted.RemainingBytes != 0 || *exhausted.TotalBytes != 90 {
		t.Fatal("actual consumption clipped", exhausted, err)
	}
	for _, test := range []struct {
		historical *int64
		native     *NativeCutoverCountersV1
		missing    int
	}{
		{nil, &native, 1}, {&history, nil, 1}, {nil, nil, 2},
	} {
		p, e := ProjectFiniteCutoverBudgetV1(100, test.historical, test.native)
		if e != nil || p.RemainingBytes != nil || p.TotalBytes != nil || len(p.MissingEvidence) != test.missing || p.AuthorizesActivation {
			t.Fatal("unknown became fabricated zero", p, e)
		}
	}
	// A measured zero and absent measurement have different meanings.
	zero := int64(0)
	measured, err := ProjectFiniteCutoverBudgetV1(100, &zero, &NativeCutoverCountersV1{})
	if err != nil || *measured.RemainingBytes != 100 || measured.Scope != "projection_only" || measured.AuthorizesActivation {
		t.Fatal(measured, err)
	}
	// Exact string serialization is required beyond JavaScript safe integers.
	large := int64(9007199254740993)
	p, err := ProjectFiniteCutoverBudgetV1(math.MaxInt64, &large, &NativeCutoverCountersV1{})
	raw, e := json.Marshal(p)
	if err != nil || e != nil || !strings.Contains(string(raw), "\"historical_bytes\":\"9007199254740993\"") {
		t.Fatal("inexact byte encoding", string(raw), err, e)
	}
}
func TestFiniteBudgetRejectsInvalidOrOverflowingEvidence(t *testing.T) {
	negative := int64(-1)
	maximum := int64(math.MaxInt64)
	for _, test := range []struct {
		quota   int64
		history *int64
		native  *NativeCutoverCountersV1
	}{
		{0, nil, nil}, {-1, nil, nil}, {10, &negative, nil},
		{10, nil, &NativeCutoverCountersV1{Uplink: -1}},
		{10, nil, &NativeCutoverCountersV1{Uplink: math.MaxInt64, Downlink: 1}},
		{10, &maximum, &NativeCutoverCountersV1{Downlink: 1}},
	} {
		if _, err := ProjectFiniteCutoverBudgetV1(test.quota, test.history, test.native); !errors.Is(err, ErrFiniteBudget) {
			t.Fatal("invalid evidence accepted", err)
		}
	}
}
