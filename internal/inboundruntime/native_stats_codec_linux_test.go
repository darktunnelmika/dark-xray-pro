//go:build linux

package inboundruntime

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"

	xraystats "github.com/darktunnelmika/dark-xray-pro/internal/xraystats"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// Match the pinned CLI's reflection output, rather than protojson: int64 is a
// JSON number, zero-valued fields are omitted, indent is four spaces, HTML is
// not escaped, and fmt.Println adds a second newline after Encoder.Encode.
func nativeStatsLegacyJSON(t *testing.T, response *xraystats.QueryStatsResponse) []byte {
	t.Helper()
	type stat struct {
		Name  string `json:"name,omitempty"`
		Value int64  `json:"value,omitempty"`
	}
	type result struct {
		Stat []stat `json:"stat,omitempty"`
	}
	var value result
	for _, entry := range response.Stat {
		value.Stat = append(value.Stat, stat{Name: entry.Name, Value: entry.Value})
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "    ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func nativeStatsWire(t *testing.T, response *xraystats.QueryStatsResponse) []byte {
	t.Helper()
	raw, err := proto.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestNativeStatsCodecMatchesLegacyJSON(t *testing.T) {
	controls := make([]byte, 32)
	for i := range controls {
		controls[i] = byte(i)
	}
	cases := []*xraystats.QueryStatsResponse{
		{},
		{Stat: []*xraystats.Stat{{Name: "zero"}}},
		{Stat: []*xraystats.Stat{{Name: "one", Value: 1}, {Name: "maximum", Value: math.MaxInt64}}},
		{Stat: []*xraystats.Stat{{Name: "quote\"slash\\<>/&" + string(controls) + "\u2028\u2029ایران😀", Value: 17}}},
		{Stat: []*xraystats.Stat{{Name: strings.Repeat("long-name-", 300), Value: 123456789}}},
	}
	for i, response := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			raw := nativeStatsWire(t, response)
			size, err := nativeStatsJSONSize(raw)
			if err != nil || size != len(nativeStatsLegacyJSON(t, response)) {
				t.Fatalf("legacy size mismatch: size=%d err=%v", size, err)
			}
			var decoded xraystats.QueryStatsResponse
			if err := (nativeStatsCodec{}).Unmarshal(raw, &decoded); err != nil || !proto.Equal(response, &decoded) {
				t.Fatalf("decode changed valid counters: %v", err)
			}
		})
	}
}

func TestNativeStatsCodecPreservesWireAndJSONEnvelopes(t *testing.T) {
	t.Run("wire", func(t *testing.T) {
		// At this length both nested protobuf lengths use four-byte varints.
		response := &xraystats.QueryStatsResponse{Stat: []*xraystats.Stat{{Name: strings.Repeat("a", nativeStatsWireLimit-10)}}}
		raw := nativeStatsWire(t, response)
		if len(raw) != nativeStatsWireLimit {
			t.Fatalf("wrong boundary: %d", len(raw))
		}
		if _, err := nativeStatsJSONSize(raw); err != nil {
			t.Fatal("old wire limit refused", err)
		}
		response.Stat[0].Name += "a"
		if _, err := nativeStatsJSONSize(nativeStatsWire(t, response)); err == nil {
			t.Fatal("wire limit exceeded")
		}
	})
	t.Run("JSON", func(t *testing.T) {
		base := len(nativeStatsJSONPrefix) + len(nativeStatsJSONSuffix) + len(nativeStatsJSONEntryPrefix) + 2 + len(nativeStatsJSONEntrySuffix)
		padding := nativeStatsJSONLimit - base
		name := strings.Repeat("\x00", padding/6) + strings.Repeat("a", padding%6)
		response := &xraystats.QueryStatsResponse{Stat: []*xraystats.Stat{{Name: name}}}
		if got := len(nativeStatsLegacyJSON(t, response)); got != nativeStatsJSONLimit {
			t.Fatalf("wrong JSON boundary: %d", got)
		}
		size, err := nativeStatsJSONSize(nativeStatsWire(t, response))
		if err != nil || size != nativeStatsJSONLimit {
			t.Fatalf("old JSON limit refused: %d %v", size, err)
		}
		response.Stat[0].Name += "a"
		if _, err := nativeStatsJSONSize(nativeStatsWire(t, response)); err == nil {
			t.Fatal("JSON limit exceeded")
		}
	})
	t.Run("no-new-principal-cap", func(t *testing.T) {
		response := &xraystats.QueryStatsResponse{}
		for i := 0; i < 65537; i++ {
			response.Stat = append(response.Stat, &xraystats.Stat{Name: "n" + strconv.Itoa(i)})
		}
		raw := nativeStatsWire(t, response)
		var decoded xraystats.QueryStatsResponse
		if err := (nativeStatsCodec{}).Unmarshal(raw, &decoded); err != nil || len(decoded.Stat) != len(response.Stat) {
			t.Fatalf("valid old envelope acquired a record-count cap: %v", err)
		}
	})
}

func TestNativeStatsCodecRejectsMalformedBeforeDecode(t *testing.T) {
	wrap := func(entry []byte) []byte {
		return protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), entry)
	}
	name := protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), []byte("n"))
	value := protowire.AppendVarint(protowire.AppendTag(nil, 2, protowire.VarintType), 1)
	cases := map[string][]byte{
		"unknown-response-field":   protowire.AppendBytes(protowire.AppendTag(nil, 2, protowire.BytesType), name),
		"wrong-response-kind":      protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 0),
		"empty-stat":               wrap(nil),
		"missing-name":             wrap(value),
		"empty-name":               wrap([]byte{0x0a, 0}),
		"invalid-UTF8":             wrap([]byte{0x0a, 1, 0xff}),
		"duplicate-name-field":     wrap(append(append([]byte{}, name...), name...)),
		"duplicate-value-field":    wrap(append(append(append([]byte{}, name...), value...), value...)),
		"unknown-stat-field":       wrap(append(append([]byte{}, name...), 0x18, 1)),
		"negative-value":           wrap(protowire.AppendVarint(append(append([]byte{}, name...), 0x10), math.MaxUint64)),
		"overflow-varint":          wrap(append(append([]byte{}, name...), 0x10, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1)),
		"truncated-outer":          {0x0a, 0xff},
		"truncated-name":           wrap([]byte{0x0a, 3, 'n'}),
		"too-many-minimal-entries": bytes.Repeat(wrap(name), nativeStatsMaximumEntries+1),
	}
	for label, raw := range cases {
		t.Run(label, func(t *testing.T) {
			response := xraystats.QueryStatsResponse{Stat: []*xraystats.Stat{{Name: "unchanged"}}}
			if err := (nativeStatsCodec{}).Unmarshal(raw, &response); err == nil {
				t.Fatal("malformed response accepted")
			}
			if len(response.Stat) != 1 || response.Stat[0].Name != "unchanged" {
				t.Fatal("malformed response reached protobuf allocation")
			}
		})
	}
	// An explicitly encoded zero is equivalent to the CLI's omitted zero.
	zero := wrap(append(append([]byte{}, name...), 0x10, 0))
	if _, err := nativeStatsJSONSize(zero); err != nil {
		t.Fatal("valid explicit zero refused", err)
	}
}

func TestNativeStatsCodecAllowsOnlyResetFreeKnownQueries(t *testing.T) {
	codec := nativeStatsCodec{}
	for _, pattern := range []string{"", nativeProtocol + "capabilities", nativeProtocol + "health", nativeProtocol + "activate/" + strings.Repeat("a", 64), nativeProtocol + "final/" + strings.Repeat("b", 64)} {
		raw, err := codec.Marshal(&xraystats.QueryStatsRequest{Pattern: pattern})
		var request xraystats.QueryStatsRequest
		if err != nil || proto.Unmarshal(raw, &request) != nil || request.Pattern != pattern || request.Reset_ {
			t.Fatalf("valid query changed: %v", err)
		}
	}
	for _, request := range []any{&xraystats.QueryStatsRequest{Reset_: true}, &xraystats.QueryStatsRequest{Pattern: "sysstats"}, &xraystats.QueryStatsRequest{Pattern: nativeProtocol + "activate/short"}, &xraystats.QueryStatsRequest{Pattern: nativeProtocol + "final/" + strings.Repeat("A", 64)}, (*xraystats.QueryStatsRequest)(nil), &xraystats.GetStatsRequest{}} {
		if _, err := codec.Marshal(request); err == nil {
			t.Fatal("unsupported request accepted")
		}
	}
}
