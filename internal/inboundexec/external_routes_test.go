package inboundexec

import (
	"encoding/json"
	"testing"
)

func TestDirectExternalWorkersNeverBypassSelectedNativeRouting(t *testing.T) {
	for _, engine := range []string{"tuic", "amneziawg"} {
		for _, tc := range []struct {
			name             string
			outbounds, rules []json.RawMessage
			manifest         string
			allowed          bool
		}{
			{"plain_default", []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)}, nil, `{}`, true},
			{"proxy_default", []json.RawMessage{json.RawMessage(`{"tag":"proxy","protocol":"socks"}`)}, nil, `{}`, false},
			{"explicit_proxy", []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`), json.RawMessage(`{"tag":"proxy","protocol":"socks"}`)}, nil, `{"outbound_tag":"proxy"}`, false},
			{"matching_route", []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`), json.RawMessage(`{"tag":"proxy","protocol":"socks"}`)}, []json.RawMessage{json.RawMessage(`{"type":"field","inboundTag":["external-one"],"outboundTag":"proxy"}`)}, `{}`, false},
			{"global_route", []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`), json.RawMessage(`{"tag":"proxy","protocol":"socks"}`)}, []json.RawMessage{json.RawMessage(`{"type":"field","domain":["example.org"],"outboundTag":"proxy"}`)}, `{}`, false},
			{"other_inbound_route", []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`), json.RawMessage(`{"tag":"proxy","protocol":"socks"}`)}, []json.RawMessage{json.RawMessage(`{"type":"field","inboundTag":["native-one"],"outboundTag":"proxy"}`)}, `{}`, true},
			{"custom_freedom", []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom","settings":{"redirect":"127.0.0.1:2222"}}`)}, nil, `{}`, false},
			{"unsupported_bridge", []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)}, nil, `{"requires_socks_bridge":true}`, false},
		} {
			t.Run(engine+"/"+tc.name, func(t *testing.T) {
				err := validateDirectComponentRoutes([]Component{{Engine: engine, Tag: "external-one", Manifest: []byte(tc.manifest)}}, tc.outbounds, tc.rules)
				if (err == nil) != tc.allowed {
					t.Fatal("external route policy was silently changed", err)
				}
			})
		}
	}
}
