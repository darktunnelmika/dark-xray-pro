package xrayconfig

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
)

// LabSubscriptionRoutes extracts a single consumer's connection fields from
// the exact native candidate supplied by the authenticated applied-stage path.
// It supports only the current LAB proof: distinct Direct TCP Reality Vision
// listeners bound to 127.0.0.1. It cannot infer any public endpoint.
//
// This function checks the config digest and complete listener/config agreement.
// The caller must first bind both hashes to the applied stage and current proof:
// the manifest includes installation and predecessor metadata absent from a
// Candidate, so its digest cannot be independently recomputed here. The caller
// also owns current client eligibility, ownership and billable/non-probe checks.
// Opaque Xray accounting emails do not encode those roles; a syntactically valid
// UUID alone is never evidence that a credential belongs to a paying consumer.
//
// The result contains the intended UUID plus the corresponding client-side
// Reality credentials, never a server private key, counter email or camouflage
// destination. No socket, runtime, subscription or database state is changed.
func LabSubscriptionRoutes(c Candidate, clientUUID string, labels map[uint64]string) ([]subscriptioncodec.Route, error) {
	if c.NoListeners || c.CanActivate || !c.RequiresXrayTest || !c.RequiresDataPlaneProof ||
		!c.RequiresSubscriptionCommit || len(c.Config) == 0 || len(c.Config) > MaxCandidateBytes ||
		len(c.Listeners) == 0 || len(c.Listeners) > subscriptioncodec.MaxRoutes ||
		!uuidRE.MatchString(clientUUID) || !labSubscriptionHash(c.ManifestSHA256) {
		return nil, ErrInvalid
	}
	sum := sha256.Sum256(c.Config)
	if hex.EncodeToString(sum[:]) != c.SHA256 || !labSubscriptionJSON(c.Config) {
		return nil, ErrInvalid
	}
	var doc struct {
		Log       json.RawMessage `json:"log"`
		Stats     json.RawMessage `json:"stats"`
		Policy    json.RawMessage `json:"policy"`
		API       json.RawMessage `json:"api,omitempty"`
		Inbounds  []inbound       `json:"inbounds"`
		Outbounds []outbound      `json:"outbounds"`
	}
	decoder := json.NewDecoder(bytes.NewReader(c.Config))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || len(doc.Inbounds) != len(c.Listeners) {
		return nil, ErrInvalid
	}
	byTag := make(map[string]Listener, len(c.Listeners))
	ids := make(map[uint64]bool, len(c.Listeners))
	ports := make(map[uint16]bool, len(c.Listeners))
	for _, listener := range c.Listeners {
		if listener.InboundID == 0 || listener.InboundID > 1<<53-1 || listener.NodePort < 1024 ||
			listener.Kind != "direct" || listener.Attribution != "distinct" ||
			listener.Tag != fmt.Sprintf("dxp-in-%d-direct", listener.InboundID) ||
			ids[listener.InboundID] || ports[listener.NodePort] {
			return nil, ErrInvalid
		}
		ids[listener.InboundID], ports[listener.NodePort] = true, true
		byTag[listener.Tag] = listener
	}
	var routes []subscriptioncodec.Route
	for _, in := range doc.Inbounds {
		listener, exists := byTag[in.Tag]
		if !exists || in.Listen != "127.0.0.1" || in.Port != listener.NodePort ||
			in.Protocol != "vless" || in.Settings.Decryption != "none" ||
			in.Stream.Network != "tcp" || in.Stream.Security != "reality" || in.Stream.GRPC != nil {
			return nil, ErrInvalid
		}
		delete(byTag, in.Tag)
		profile := Profile{
			InboundID: listener.InboundID, Transport: "tcp", RealityTarget: in.Stream.Reality.Dest,
			ServerNames: in.Stream.Reality.ServerNames, ShortIDs: in.Stream.Reality.ShortIDs,
			RealityPrivateKey: in.Stream.Reality.PrivateKey, Options: optionsFromStream(in.Stream),
		}
		matchedUUID := ""
		for _, user := range in.Settings.Clients {
			if user.Level != 0 || user.Flow != "xtls-rprx-vision" {
				return nil, ErrInvalid
			}
			profile.Users = append(profile.Users, User{UUID: user.ID, Email: user.Email})
			if strings.EqualFold(user.ID, clientUUID) {
				if matchedUUID != "" {
					return nil, ErrInvalid
				}
				matchedUUID = user.ID
			}
		}
		if validProfile(profile) != nil {
			return nil, ErrInvalid
		}
		key, err := base64.RawURLEncoding.Strict().DecodeString(profile.RealityPrivateKey)
		if err != nil || base64.RawURLEncoding.EncodeToString(key) != profile.RealityPrivateKey {
			return nil, ErrInvalid
		}
		private, err := ecdh.X25519().NewPrivateKey(key)
		if err != nil {
			return nil, ErrInvalid
		}
		if matchedUUID == "" {
			continue
		}
		routes = append(routes, subscriptioncodec.Route{
			InboundID: listener.InboundID, Kind: "direct", Host: "127.0.0.1", Port: in.Port,
			Label: labels[listener.InboundID], UUID: matchedUUID, Transport: "tcp",
			ServerName: profile.ServerNames[0], ShortID: profile.ShortIDs[0],
			PublicKey: base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
		})
	}
	if len(byTag) != 0 {
		return nil, ErrInvalid
	}
	if _, err := subscriptioncodec.Render(routes, subscriptioncodec.FormatRaw); err != nil {
		return nil, ErrInvalid
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].InboundID < routes[j].InboundID })
	return routes, nil
}

func labSubscriptionHash(value string) bool {
	if len(value) != 64 || value == strings.Repeat("0", 64) || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

// Reject duplicate JSON member names (including case aliases), trailing values
// and excessive nesting before decoding the private config into typed fields.
func labSubscriptionJSON(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delim, composite := token.(json.Delim)
		if !composite {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				member, err := decoder.Token()
				name, ok := member.(string)
				name = strings.ToLower(name)
				if err != nil || !ok || seen[name] || !value(depth+1) {
					return false
				}
				seen[name] = true
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
