package hubcore

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// warpAPI is replaceable in tests.
var warpAPI = "https://api.cloudflareclient.com/v0a2483/reg"

// RegisterWARP creates a free Cloudflare WARP account and returns a ready
// Xray WireGuard outbound for it.
func RegisterWARP(ctx context.Context) (json.RawMessage, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pub := base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	priv := base64.StdEncoding.EncodeToString(key.Bytes())
	body, _ := json.Marshal(map[string]any{"key": pub, "install_id": "", "fcm_token": "", "tos": time.Now().UTC().Format(time.RFC3339),
		"type": "Android", "model": "PC", "locale": "en_US", "warp_enabled": true})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, warpAPI, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okhttp/3.12.1")
	req.Header.Set("CF-Client-Version", "a-6.30-3596")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("warp: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("warp: HTTP %d", resp.StatusCode)
	}
	var r struct {
		Config struct {
			ClientID string `json:"client_id"`
			Peers    []struct {
				PublicKey string `json:"public_key"`
				Endpoint  struct {
					Host string `json:"host"`
				} `json:"endpoint"`
			} `json:"peers"`
			Interface struct {
				Addresses struct {
					V4 string `json:"v4"`
					V6 string `json:"v6"`
				} `json:"addresses"`
			} `json:"interface"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Config.Peers) == 0 {
		return nil, fmt.Errorf("warp: unexpected answer")
	}
	p := r.Config.Peers[0]
	endpoint := p.Endpoint.Host
	if endpoint == "" || strings.HasSuffix(endpoint, ":0") {
		endpoint = "engage.cloudflareclient.com:2408"
	}
	addr := []string{}
	if a := r.Config.Interface.Addresses.V4; a != "" {
		addr = append(addr, a+"/32")
	}
	if a := r.Config.Interface.Addresses.V6; a != "" {
		addr = append(addr, a+"/128")
	}
	settings := map[string]any{"secretKey": priv, "address": addr, "mtu": 1280,
		"peers": []any{map[string]any{"publicKey": p.PublicKey, "endpoint": endpoint}}}
	if id, err := base64.StdEncoding.DecodeString(r.Config.ClientID); err == nil && len(id) == 3 {
		settings["reserved"] = []int{int(id[0]), int(id[1]), int(id[2])}
	}
	return json.Marshal(map[string]any{"protocol": "wireguard", "settings": settings})
}
