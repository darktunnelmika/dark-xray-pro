package inboundexec

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestManagedBundleManifestProtectsComponentsWithoutMutatingPrivateBytes(t *testing.T) {
	key := bytes.Repeat([]byte{0x91}, 32)
	raw := []byte(`{"private":"worker-only"}`)
	manifest := []byte(`{"engine":"tuic"}`)
	b := Bundle{Version: Version, NodeID: "selected-node", InstallationID: strings.Repeat("a", 32), Generation: 7, OperationID: "reviewed-operation", NativeConfig: []byte(`{"inbounds":[]}`), Components: []Component{{Engine: "tuic", Tag: "component-1", Config: raw, Manifest: manifest, SHA256: BytesSHA(raw)}}, Revisions: []RevisionRef{}, Listeners: []Listener{}, Principals: []Principal{}}
	sealed, err := SealBundle(key, b)
	if err != nil || VerifyBundle(key, sealed) != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Components[0].Config, raw) || !bytes.Equal(sealed.Components[0].Config, raw) || !bytes.Equal(sealed.Components[0].Manifest, manifest) {
		t.Fatal("manifest hashing destroyed private component")
	}
	tampered := sealed
	tampered.Components = append([]Component{}, sealed.Components...)
	tampered.Components[0].Manifest = []byte(`{"engine":"changed"}`)
	if VerifyBundle(key, tampered) == nil {
		t.Fatal("unsigned adapter manifest accepted")
	}
	tampered = sealed
	tampered.Generation++
	if VerifyBundle(key, tampered) == nil {
		t.Fatal("generation replay accepted")
	}
}

func TestManagedCompilerUsesPhysicalResourcesAndExplicitNodeSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec inboundspec.Spec
		want string
	}{
		{"xhttp-h3", inboundspec.Spec{Network: "xhttp", TLS: map[string]any{"alpn": []any{"h3"}}}, "udp"},
		{"masque-both", inboundspec.Spec{Protocol: "masque", Network: "masque", TLS: map[string]any{"alpn": []any{"h2", "h3"}}}, "tcp,udp"},
		{"mixed-tcp", inboundspec.Spec{Protocol: "mixed", Network: "tcp", Settings: map[string]any{"udp": false}}, "tcp"},
		{"ss-udp", inboundspec.Spec{Protocol: "shadowsocks", Settings: map[string]any{"network": "udp"}}, "udp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := socketNetwork(&tc.spec); got != tc.want {
				t.Fatal(got)
			}
		})
	}
	spec := inboundspec.Spec{Version: 1, Protocol: "vmess", Network: "tcp", Security: "none"}
	p := xrayconfig.Profile{InboundID: 7, Spec: &spec, Users: []xrayconfig.User{{UUID: "11111111-1111-4111-8111-111111111111", Email: "a@pro.invalid"}}}
	top, err := xrayconfig.RenderCreationTopology(p, []xrayconfig.CreationNode{{NodeID: "remote-only", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.8", DirectPort: 21993}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	item := CompileItem{Ref: RevisionRef{InboundID: 7, Revision: 1}, Profile: p, Topology: top, AccountingMode: AccountingPerClient, Principals: []Principal{{ID: "retail-binding", Engine: "xray", ClientID: "client", Role: "customer", Counter: "user>>>a@pro.invalid>>>traffic", Billable: true}}}
	b, err := Compile(context.Background(), CompileRequest{NodeID: "remote-only", InstallationID: strings.Repeat("a", 32), Generation: 1, OperationID: "selected-route-operation", Items: []CompileItem{item}})
	if err != nil || len(b.Listeners) != 1 || b.Listeners[0].Protocol != "vmess" {
		t.Fatal(b, err)
	}
	hub, err := Compile(context.Background(), CompileRequest{NodeID: "hub-local", InstallationID: strings.Repeat("b", 32), Generation: 1, OperationID: "selected-route-operation", Items: []CompileItem{item}})
	if err != nil || len(hub.Listeners) != 0 || len(hub.Principals) != 0 {
		t.Fatal("implicit HUB listener or billing identity", hub, err)
	}
}
