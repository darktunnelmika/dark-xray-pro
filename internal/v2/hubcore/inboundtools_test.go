package hubcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

func TestGenerateKeys(t *testing.T) {
	r, err := GenerateKeys(KeyRequest{Kind: "reality"})
	if err != nil || r.Private == "" || r.Public == "" {
		t.Fatal(r, err)
	}
	d, err := GenerateKeys(KeyRequest{Kind: "reality", Private: r.Private})
	if err != nil || d.Public != r.Public {
		t.Fatal("deriving the public key of a pasted private key", d, err)
	}
	if _, err := GenerateKeys(KeyRequest{Kind: "reality", Private: "nope"}); err == nil {
		t.Fatal("a bad private key must be refused")
	}
	enc := base64.RawURLEncoding
	x, _ := GenerateKeys(KeyRequest{Kind: "vlessenc-x25519"})
	if !strings.HasPrefix(x.Decryption, "mlkem768x25519plus.native.600s.") || !strings.HasPrefix(x.Encryption, "mlkem768x25519plus.native.0rtt.") {
		t.Fatal(x)
	}
	if b, _ := enc.DecodeString(x.Encryption[strings.LastIndex(x.Encryption, ".")+1:]); len(b) != 32 {
		t.Fatal("x25519 client key", len(b))
	}
	m, _ := GenerateKeys(KeyRequest{Kind: "vlessenc-mlkem"})
	if b, _ := enc.DecodeString(m.Decryption[strings.LastIndex(m.Decryption, ".")+1:]); len(b) != 64 {
		t.Fatal("ML-KEM seed must be 64 bytes", len(b))
	}
	if b, _ := enc.DecodeString(m.Encryption[strings.LastIndex(m.Encryption, ".")+1:]); len(b) != 1184 {
		t.Fatal("ML-KEM-768 encapsulation key must be 1184 bytes", len(b))
	}
	if s, _ := GenerateKeys(KeyRequest{Kind: "shortid"}); len(s.ShortID) != 16 {
		t.Fatal(s)
	}
}

func TestCloneRotateAndLinks(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	priv, pub, _ := RealityKeys()
	tpl := `{"protocol":"vless","port":443,"settings":{"flow":"xtls-rprx-vision","decryption":"mlkem768x25519plus.native.600s.AAAA"},
"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"dest":"a.com:443","serverNames":["a.com"],"privateKey":"` + priv + `","shortIds":["abcd"]}}}`
	meta := `{"endpoints":[{"name":"Direct","address":"1.2.3.4","port":443}],"public_key":"` + pub + `","short_id":"abcd","vless_encryption":"mlkem768x25519plus.native.0rtt.BBBB"}`
	if err := s.PutInboundChecked(ctx, "r1", "de-1", []byte(tpl), []byte(meta), true); err != nil {
		t.Fatal(err)
	}
	c, _ := s.ClientByEmail(ctx, "alice")
	c.Inbounds = append(c.Inbounds, "r1")
	if err := s.PutClient(ctx, c.Client); err != nil {
		t.Fatal(err)
	}
	links, err := s.ClientLinks(ctx, "alice")
	if err != nil || len(links) == 0 {
		t.Fatal(links, err)
	}
	found := false
	for _, l := range links {
		if strings.Contains(l.Link, "encryption=mlkem768x25519plus.native.0rtt.BBBB") && strings.Contains(l.Link, "flow=xtls-rprx-vision") {
			found = true
		}
	}
	if !found {
		t.Fatal("VLESS encryption must reach the share link", links)
	}
	// Clone: new port, fresh REALITY keys, same clients.
	if err := s.CloneInbound(ctx, "r1", CloneRequest{Tag: "r2", Port: 8443, Clients: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := s.InboundRaw(ctx, "r2")
	if err != nil {
		t.Fatal(err)
	}
	var ct map[string]any
	var cm sub.Meta
	_ = json.Unmarshal(raw.Template, &ct)
	_ = json.Unmarshal(raw.Meta, &cm)
	rs := ct["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
	if intOf(ct["port"]) != 8443 || rs["privateKey"] == priv || cm.PublicKey == pub || cm.Endpoints[0].Port != 8443 {
		t.Fatalf("clone %v %+v", ct, cm)
	}
	d, _ := GenerateKeys(KeyRequest{Kind: "reality", Private: rs["privateKey"].(string)})
	if d.Public != cm.PublicKey {
		t.Fatal("the clone's public key must match its private key")
	}
	c, _ = s.ClientByEmail(ctx, "alice")
	has := false
	for _, x := range c.Inbounds {
		has = has || x == "r2"
	}
	if !has {
		t.Fatal("clients must follow the clone", c.Inbounds)
	}
	if err := s.CloneInbound(ctx, "r1", CloneRequest{Tag: "r2", Port: 9443}); err == nil {
		t.Fatal("an existing tag must be refused")
	}
	// Rotate: new UUID and secret keep the link; a new link keeps the UUID.
	before, _ := s.ClientByEmail(ctx, "alice")
	secret := func() string {
		var x string
		_ = s.db.QueryRow(`SELECT secret FROM clients WHERE email='alice'`).Scan(&x)
		return x
	}
	oldSecret := secret()
	if err := s.RotateClient(ctx, "alice", true, false); err != nil {
		t.Fatal(err)
	}
	mid, _ := s.ClientByEmail(ctx, "alice")
	if mid.UUID == before.UUID || secret() == oldSecret || mid.SubToken != before.SubToken {
		t.Fatal("credentials rotation", before.Client, mid.Client)
	}
	if err := s.RotateClient(ctx, "alice", false, true); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ClientByEmail(ctx, "alice")
	if after.UUID != mid.UUID || after.SubToken == mid.SubToken {
		t.Fatal("link rotation", mid.Client, after.Client)
	}
	if err := s.RotateClient(ctx, "nobody", true, true); err == nil {
		t.Fatal("unknown client")
	}
}

func TestSubMultiModeAndSingboxSkip(t *testing.T) {
	tpl := []byte(`{"protocol":"vless","port":443,"settings":{},"streamSettings":{"network":"grpc","security":"none","grpcSettings":{"serviceName":"x","multiMode":true}}}`)
	in := sub.Inbound{Tag: "g", Template: tpl, Meta: sub.Meta{Endpoints: []sub.Endpoint{{Address: "1.1.1.1", Port: 443}}, VLESSEncryption: "mlkem768x25519plus.native.0rtt.Q"}}
	links, err := sub.Links(sub.Account{Email: "a", UUID: "u"}, []sub.Inbound{in})
	if err != nil || len(links) != 1 || !strings.Contains(links[0], "mode=multi") {
		t.Fatal(links, err)
	}
	sb, err := sub.SingBox(sub.Account{Email: "a", UUID: "u"}, []sub.Inbound{in})
	if err != nil || strings.Contains(string(sb), `"type": "vless"`) {
		t.Fatal("sing-box must skip VLESS encryption inbounds", string(sb), err)
	}
}
