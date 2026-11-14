package nodeagent

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun/netstack"
)

func wgKey(t *testing.T) (priv, pub string) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes())
}

func hexKey(b64 string) string {
	raw, _ := base64.StdEncoding.DecodeString(b64)
	return hex.EncodeToString(raw)
}

func TestSplitSide(t *testing.T) {
	raw := []byte(`{"inbounds":[{"tag":"api","protocol":"dokodemo-door"},{"tag":"a","protocol":"amneziawg","port":1},{"tag":"m","protocol":"mtproto","port":2},{"tag":"v","protocol":"vless"}],"outbounds":[]}`)
	x, side, err := SplitSide(raw)
	if err != nil || len(side) != 2 || strings.Contains(string(x), "amneziawg") || strings.Contains(string(x), "mtproto") || !strings.Contains(string(x), `"vless"`) {
		t.Fatal(string(x), len(side), err)
	}
	same, none, _ := SplitSide([]byte(`{"inbounds":[{"tag":"v","protocol":"vless"}]}`))
	if none != nil || !strings.Contains(string(same), "vless") {
		t.Fatal("configs without side inbounds pass unchanged")
	}
}

// A real AmneziaWG client (user-space) reaches a web server through the
// agent's AmneziaWG server; the bytes are counted for its email and a
// removed peer stops working.
func TestAWGEndToEnd(t *testing.T) {
	allowLocalDest = true
	defer func() { allowLocalDest = false }()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 50000))) }))
	defer web.Close()
	sPriv, sPub := wgKey(t)
	cPriv, cPub := wgKey(t)
	l, _ := net.ListenPacket("udp", "127.0.0.1:0")
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()
	obf := map[string]any{"jc": 4, "jmin": 40, "jmax": 70, "s1": 33, "s2": 77, "h1": 123456, "h2": 234567, "h3": 345678, "h4": 456789}
	set := map[string]any{"secretKey": sPriv, "mtu": 1280, "peers": []any{map[string]any{"publicKey": cPub, "allowedIPs": []string{"10.67.0.2/32"}, "email": "ali"}}}
	for k, v := range obf {
		set[k] = v
	}
	in, _ := json.Marshal(map[string]any{"tag": "awg1", "protocol": "amneziawg", "listen": "127.0.0.1", "port": port, "settings": set})
	side := NewSideServers(t.Logf)
	defer side.Close()
	side.Apply([]json.RawMessage{in})
	if side.awg["awg1"] == nil {
		t.Fatal("server not started")
	}
	// Client.
	ctun, cnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.67.0.2")}, nil, 1280)
	if err != nil {
		t.Fatal(err)
	}
	cdev := device.NewDevice(ctun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "client: "))
	defer cdev.Close()
	conf := fmt.Sprintf("private_key=%s\njc=4\njmin=40\njmax=70\ns1=33\ns2=77\nh1=123456\nh2=234567\nh3=345678\nh4=456789\npublic_key=%s\nendpoint=127.0.0.1:%d\nallowed_ip=0.0.0.0/0\npersistent_keepalive_interval=5\n",
		hexKey(cPriv), hexKey(sPub), port)
	if err := cdev.IpcSet(conf); err != nil {
		t.Fatal(err)
	}
	if err := cdev.Up(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, n, a string) (net.Conn, error) { return cnet.DialContext(ctx, n, a) }}}
	resp, err := client.Get(web.URL)
	if err != nil {
		t.Fatal("through the tunnel:", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 50000 {
		t.Fatal("body", len(body))
	}
	users, inbounds := side.Take()
	if users["ali"][1] < 50000 || inbounds["awg1"][1] < 50000 {
		t.Fatalf("traffic not counted: %v %v", users, inbounds)
	}
	if on := side.Online(time.Now()); len(on["ali"]) != 1 {
		t.Fatal("online", on)
	}
	// Peer removed (client disabled): the next request fails.
	set["peers"] = []any{}
	in2, _ := json.Marshal(map[string]any{"tag": "awg1", "protocol": "amneziawg", "listen": "127.0.0.1", "port": port, "settings": set})
	side.Apply([]json.RawMessage{in2})
	client.Timeout = 3 * time.Second
	if _, err := client.Get(web.URL + "/again"); err == nil {
		t.Fatal("a removed peer must not get through")
	}
}
