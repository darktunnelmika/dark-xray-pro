package inboundexec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEpochIdentityRequiresEverySignedWorkerAndPinnedBinary(t *testing.T) {
	b := Bundle{NodeID: "one", InstallationID: strings.Repeat("a", 32), Generation: 1, ConfigSHA: strings.Repeat("b", 64), ManifestSHA: strings.Repeat("c", 64), NativeConfig: []byte(`{}`), Components: []Component{{Engine: "mtproto", Tag: "first"}, {Engine: "mtproto", Tag: "second"}}}
	e := EpochInput{NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: 1, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, BinarySHAs: map[string]string{"xray": strings.Repeat("d", 64), "mtproto": strings.Repeat("e", 64)}}
	group := map[string]string{}
	for name, engine := range map[string]string{"xray": "xray", "mtproto/first": "mtproto", "mtproto/second": "mtproto"} {
		raw, _ := json.Marshal(ProcessMember{BootID: "00000000-0000-0000-0000-000000000001", PID: 1, StartTicks: 2, BinarySHA: e.BinarySHAs[engine], ConfigSHA: strings.Repeat("f", 64)})
		group[name] = string(raw)
	}
	identity, _ := json.Marshal(group)
	e.ProcessIdentity = string(identity)
	if err := ValidateEpochIdentity(b, e); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*EpochInput, map[string]string)
	}{
		{"missing_member", func(_ *EpochInput, g map[string]string) { delete(g, "mtproto/second") }},
		{"foreign_component", func(_ *EpochInput, g map[string]string) {
			g["mtproto/foreign"] = g["mtproto/first"]
			delete(g, "mtproto/first")
		}},
		{"wrong_release", func(e *EpochInput, _ map[string]string) { e.BinarySHAs["xray"] = strings.Repeat("0", 64) }},
		{"missing_engine", func(e *EpochInput, _ map[string]string) { delete(e.BinarySHAs, "xray") }},
		{"duplicate_inner_key", func(_ *EpochInput, g map[string]string) { g["xray"] = strings.TrimSuffix(g["xray"], "}") + `,"pid":1}` }},
		{"foreign_generation", func(e *EpochInput, _ map[string]string) { e.Generation++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := e
			bad.BinarySHAs = map[string]string{}
			for k, v := range e.BinarySHAs {
				bad.BinarySHAs[k] = v
			}
			g := map[string]string{}
			for k, v := range group {
				g[k] = v
			}
			tc.change(&bad, g)
			raw, _ := json.Marshal(g)
			bad.ProcessIdentity = string(raw)
			if ValidateEpochIdentity(b, bad) == nil {
				t.Fatal("incomplete or substituted process identity accepted")
			}
		})
	}
}
