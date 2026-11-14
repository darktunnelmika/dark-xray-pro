package hubcore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

func TestWireGuardInbound(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "de-1", "Germany"); err != nil {
		t.Fatal(err)
	}
	if p1, pub1 := WGKeys("abc"); p1 == "" || pub1 == "" {
		t.Fatal("keys")
	} else if p2, _ := WGKeys("abc"); p2 != p1 {
		t.Fatal("keys must be stable per secret")
	}
	req := PresetRequest{Tag: "de-wg", NodeID: "de-1", Preset: "wireguard", Port: 51820, Path: "10.77.0.0/24",
		Endpoints: []sub.Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 51820}}}
	if err := s.CreatePreset(ctx, req); err != nil {
		t.Fatal(err)
	}
	for _, e := range []string{"ann", "ben"} {
		if err := s.PutClient(ctx, Client{Email: e, Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-wg"}}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := s.RenderNode(ctx, "de-1")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Inbounds []struct {
			Protocol string `json:"protocol"`
			Settings struct {
				SecretKey string `json:"secretKey"`
				Peers     []struct {
					PublicKey  string   `json:"publicKey"`
					AllowedIPs []string `json:"allowedIPs"`
					Email      string   `json:"email"`
				} `json:"peers"`
				Clients []any `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil || len(parsed.Inbounds) != 2 {
		t.Fatal(err, string(cfg))
	}
	wg := parsed.Inbounds[1]
	if wg.Protocol != "wireguard" || wg.Settings.SecretKey == "" || len(wg.Settings.Peers) != 2 || wg.Settings.Clients != nil || strings.Contains(string(cfg), wgKey) {
		t.Fatalf("%+v", wg)
	}
	ips := map[string]string{}
	for _, p := range wg.Settings.Peers {
		ips[p.Email] = p.AllowedIPs[0]
	}
	if ips["ann"] == ips["ben"] || !strings.HasPrefix(ips["ann"], "10.77.0.") {
		t.Fatal(ips)
	}
	links, _, err := s.Subscription(ctx, "ann")
	if err != nil || len(links) != 1 || !strings.HasPrefix(links[0], "wireguard://") || !strings.Contains(links[0], "address="+strings.ReplaceAll(ips["ann"], "/", "%2F")) {
		t.Fatal(links, err)
	}
	priv, _ := WGKeys(clientSecret(t, s, "ann"))
	if !strings.Contains(links[0], strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D").Replace(priv)) && !strings.Contains(links[0], priv) {
		t.Fatal("link must carry the client's own key")
	}
	// The address stays the same on the next render.
	cfg2, _ := s.RenderNode(ctx, "de-1")
	if string(cfg2) != string(cfg) {
		t.Fatal("render must be deterministic")
	}
	body, _, _, err := s.SubscriptionBody(ctx, "ann", "singbox")
	if err != nil || !strings.Contains(string(body), `"endpoints"`) || !strings.Contains(string(body), `"type": "wireguard"`) {
		t.Fatalf("%v %s", err, body)
	}
}

func clientSecret(t *testing.T, s *Store, email string) string {
	var sec string
	if err := s.ro.QueryRow(`SELECT secret FROM clients WHERE email=?`, email).Scan(&sec); err != nil {
		t.Fatal(err)
	}
	return sec
}
