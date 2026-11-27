package hubcore

import (
	"context"
	"testing"
)

func TestSubCountries(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for _, n := range [][2]string{{"de-1", "Germany"}, {"nl-1", "Netherlands"}} {
		if _, err := s.CreateNode(ctx, n[0], n[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutInboundMeta(ctx, "de-vless", "de-1", []byte(vlessTemplate), []byte(`{"public_key":"PUB","endpoints":[{"name":"DE","address":"203.0.113.10","port":18580}]}`), true); err != nil {
		t.Fatal(err)
	}
	nlTpl := []byte(`{"protocol":"vless","port":18581,"listen":"0.0.0.0","settings":{"flow":"xtls-rprx-vision"},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"dest":"example.com:443","serverNames":["example.com"],"privateKey":"test","shortIds":[""]}}}`)
	if err := s.PutInboundMeta(ctx, "nl-vless", "nl-1", nlTpl, []byte(`{"public_key":"PUB","endpoints":[{"name":"NL","address":"203.0.113.20","port":18581}]}`), true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutClient(ctx, Client{Email: "alice", UUID: "u-1", Enabled: true, VolumeBytes: 1 << 30, Inbounds: []string{"de-vless", "nl-vless"}}); err != nil {
		t.Fatal(err)
	}
	nodes, err := s.SubNodes(ctx, "alice")
	if err != nil || len(nodes) != 2 {
		t.Fatalf("nodes %+v %v", nodes, err)
	}
	all, _, _ := s.Subscription(ctx, "alice")
	if len(all) != 2 {
		t.Fatalf("links %v", all)
	}
	if err := s.PutSubHidden(ctx, "alice", []string{"nl-1"}); err != nil {
		t.Fatal(err)
	}
	some, _, _ := s.Subscription(ctx, "alice")
	if len(some) != 1 {
		t.Fatalf("hidden: %v", some)
	}
	if nodes, _ := s.SubNodes(ctx, "alice"); !(nodes[0].Hidden || nodes[1].Hidden) {
		t.Fatal("hidden flag")
	}
	if err := s.PutSubHidden(ctx, "alice", []string{"nl-1", "de-1"}); err == nil {
		t.Fatal("all hidden")
	}
	if err := s.PutSubHidden(ctx, "alice", nil); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := s.Subscription(ctx, "alice"); len(again) != 2 {
		t.Fatalf("shown again: %v", again)
	}
	for _, c := range [][3]string{{"de1", "المان 1", "🇩🇪"}, {"ger5", "هلند فلگ", "🇳🇱"}, {"turkey2", "ترکیه", "🇹🇷"}, {"us-1", "USA", "🇺🇸"}, {"uk-1", "UK", "🇬🇧"}, {"x9", "Server", "🌐"}, {"fi-1", "Helsinki", "🇫🇮"}} {
		if f := nodeFlag(c[0], c[1]); f != c[2] {
			t.Errorf("flag %s %s = %s, want %s", c[0], c[1], f, c[2])
		}
	}
}
