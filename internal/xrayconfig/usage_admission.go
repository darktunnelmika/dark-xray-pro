package xrayconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"strconv"
)

const UsageAPITag = "dxp-usage-api"

// WithUsageAPI derives a NEW candidate before the HUB signs it. It cannot
// modify an already signed lease. Only the private loopback control endpoint
// is added; the desired customer UUIDs, transports and assigned ports survive.
func WithUsageAPI(c Candidate, endpoint string) (Candidate, error) {
	if _, err := usagePort(endpoint); err != nil {
		return Candidate{}, err
	}
	sum := sha256.Sum256(c.Config)
	if c.NoListeners || hex.EncodeToString(sum[:]) != c.SHA256 || len(c.Config) > MaxCandidateBytes {
		return Candidate{}, ErrInvalid
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(c.Config, &doc) != nil {
		return Candidate{}, ErrInvalid
	}
	if _, exists := doc["api"]; exists {
		return Candidate{}, ErrConflict
	}
	api, _ := json.Marshal(map[string]any{"tag": UsageAPITag, "listen": endpoint, "services": []string{"HandlerService", "StatsService"}})
	doc["api"] = api
	raw, err := json.Marshal(doc)
	if err != nil {
		return Candidate{}, ErrInvalid
	}
	oldManifest := c.ManifestSHA256
	sum = sha256.Sum256(raw)
	c.Config = raw
	c.SHA256 = hex.EncodeToString(sum[:])
	sum = sha256.Sum256([]byte("DARK-PRO-USAGE-API-v1\x00" + oldManifest + "\x00" + c.SHA256))
	c.ManifestSHA256 = hex.EncodeToString(sum[:])
	c.Listeners = append([]Listener(nil), c.Listeners...)
	if _, _, _, err = UsageBootstrap(c); err != nil {
		return Candidate{}, err
	}
	return c, nil
}

func usagePort(endpoint string) (uint16, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host != "127.0.0.1" {
		return 0, ErrInvalid
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number < 1024 || strconv.FormatUint(number, 10) != port || number == 2087 || number == 8443 || number == 24443 {
		return 0, ErrInvalid
	}
	return uint16(number), nil
}

// UsageBootstrap accepts the already signed final candidate and produces an
// API-only launch document plus an exact private AddInbound payload. It never
// opens sockets and never treats an API listener as customer readiness.
func UsageBootstrap(c Candidate) (Candidate, []byte, string, error) {
	fail := func() (Candidate, []byte, string, error) { return Candidate{}, nil, "", ErrInvalid }
	digest := sha256.Sum256(c.Config)
	if c.NoListeners || len(c.Config) > MaxCandidateBytes || hex.EncodeToString(digest[:]) != c.SHA256 || len(c.Listeners) == 0 {
		return fail()
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(c.Config, &doc) != nil {
		return fail()
	}
	allowed := map[string]bool{"log": true, "stats": true, "policy": true, "api": true, "inbounds": true, "outbounds": true}
	if len(doc) != len(allowed) {
		return fail()
	}
	for name := range doc {
		if !allowed[name] {
			return fail()
		}
	}
	var api struct {
		Tag      string   `json:"tag"`
		Listen   string   `json:"listen"`
		Services []string `json:"services"`
	}
	decode := func(raw []byte, to any) bool {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		return d.Decode(to) == nil
	}
	if !decode(doc["api"], &api) || api.Tag != UsageAPITag || len(api.Services) != 2 {
		return fail()
	}
	services := map[string]bool{}
	for _, s := range api.Services {
		services[s] = true
	}
	if !services["HandlerService"] || !services["StatsService"] {
		return fail()
	}
	port, err := usagePort(api.Listen)
	if err != nil {
		return fail()
	}
	var log map[string]string
	if !decode(doc["log"], &log) || len(log) != 1 || log["loglevel"] != "none" {
		return fail()
	}
	var stats map[string]any
	if json.Unmarshal(doc["stats"], &stats) != nil || stats == nil || len(stats) != 0 {
		return fail()
	}
	var policy struct {
		Levels map[string]struct {
			Up   bool `json:"statsUserUplink"`
			Down bool `json:"statsUserDownlink"`
		} `json:"levels"`
	}
	if json.Unmarshal(doc["policy"], &policy) != nil || !policy.Levels["0"].Up || !policy.Levels["0"].Down {
		return fail()
	}
	var outbounds []struct {
		Protocol string `json:"protocol"`
		Tag      string `json:"tag"`
	}
	if json.Unmarshal(doc["outbounds"], &outbounds) != nil || len(outbounds) != 1 || outbounds[0].Protocol != "freedom" || outbounds[0].Tag != "direct" {
		return fail()
	}
	var ins []inbound
	if !decode(doc["inbounds"], &ins) || len(ins) != len(c.Listeners) {
		return fail()
	}
	tags := map[string]uint16{}
	ports := map[uint16]bool{}
	users := 0
	for _, in := range ins {
		if in.Protocol != "vless" || in.Tag == "" || in.Tag == UsageAPITag || tags[in.Tag] != 0 || ports[in.Port] || in.Port < 1024 || in.Port == port || in.Settings.Decryption != "none" || in.Stream.Security != "reality" || (in.Stream.Network != "tcp" && in.Stream.Network != "grpc") {
			return fail()
		}
		tags[in.Tag] = in.Port
		ports[in.Port] = true
		for _, u := range in.Settings.Clients {
			if !uuidRE.MatchString(u.ID) || !emailRE.MatchString(u.Email) || u.Level != 0 {
				return fail()
			}
			users++
		}
	}
	if users == 0 {
		return fail()
	}
	seen := map[string]bool{}
	for _, l := range c.Listeners {
		if tags[l.Tag] != l.NodePort || seen[l.Tag] {
			return fail()
		}
		seen[l.Tag] = true
	}
	payload, _ := json.Marshal(map[string]json.RawMessage{"inbounds": doc["inbounds"]})
	doc["inbounds"] = json.RawMessage("[]")
	raw, err := json.Marshal(doc)
	if err != nil {
		return fail()
	}
	digest = sha256.Sum256(raw)
	sha := hex.EncodeToString(digest[:])
	manifest := sha256.Sum256([]byte("DARK-PRO-API-ONLY-v1\x00" + c.SHA256 + "\x00" + sha))
	// This internal readiness descriptor intentionally names ONLY the API socket.
	// It is never issued by BuildNodeConfig or exposed as a customer candidate.
	bootstrap := Candidate{Config: raw, SHA256: sha, ManifestSHA256: hex.EncodeToString(manifest[:]), Listeners: []Listener{{Tag: UsageAPITag, NodePort: port}}, RequiresXrayTest: true, RequiresDataPlaneProof: true, RequiresSubscriptionCommit: true}
	return bootstrap, payload, api.Listen, nil
}

// RequiresUsageAdmission is also checked by ordinary Apply/Recover, preventing
// an old delivery path from opening the desired config without its receipt gate.
func RequiresUsageAdmission(c Candidate) bool {
	var doc struct {
		API struct {
			Tag string `json:"tag"`
		} `json:"api"`
	}
	return json.Unmarshal(c.Config, &doc) == nil && doc.API.Tag == UsageAPITag
}
