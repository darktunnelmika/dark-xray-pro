package xrayconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
)

func labSubscriptionCandidate(t *testing.T) Candidate {
	t.Helper()
	first, second, unrelated := testProfile(11), testProfile(22), testProfile(33)
	// RFC 7748 section 6.1 Alice keypair, independently specified test vector:
	// https://www.rfc-editor.org/rfc/rfc7748.txt
	private, err := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	if err != nil {
		t.Fatal(err)
	}
	first.RealityPrivateKey = base64.RawURLEncoding.EncodeToString(private)
	first.ServerNames = []string{"FIRST.example.org", "second.example.org"}
	first.ShortIDs = []string{"", "aa11"}
	unrelated.Users = []User{{UUID: uuidB, Email: "unrelated@pro.invalid"}}
	c, err := RenderAssigned("new-lab-node", strings.Repeat("1", 32), "127.0.0.1", []Assignment{
		{Profile: second, DirectPort: 12443}, {Profile: unrelated, DirectPort: 13443}, {Profile: first, DirectPort: 11443},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err = WithUsageAPI(c, "127.0.0.1:15443")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLabSubscriptionExtractsOnlyExactConsumerRoutesAndPublicKey(t *testing.T) {
	c := labSubscriptionCandidate(t)
	beforeConfig, beforeListeners := bytes.Clone(c.Config), append([]Listener(nil), c.Listeners...)
	labels := map[uint64]string{11: "مسیر یک", 22: "مسیر دو"}
	routes, err := LabSubscriptionRoutes(c, strings.ToUpper(uuidA), labels)
	if err != nil || len(routes) != 2 {
		t.Fatalf("consumer's exact routes were not extracted: %v", err)
	}
	if !bytes.Equal(c.Config, beforeConfig) || !reflect.DeepEqual(c.Listeners, beforeListeners) || len(labels) != 2 {
		t.Fatal("extraction mutated its candidate or labels")
	}
	public, _ := hex.DecodeString("8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")
	if routes[0].InboundID != 11 || routes[1].InboundID != 22 || routes[0].PublicKey != base64.RawURLEncoding.EncodeToString(public) ||
		routes[0].ServerName != "FIRST.example.org" || routes[0].ShortID != "" {
		t.Fatal("candidate's first SNI/short ID or RFC 7748 public key was not preserved")
	}
	for _, format := range []subscriptioncodec.Format{subscriptioncodec.FormatRaw, subscriptioncodec.FormatBase64} {
		doc, err := subscriptioncodec.Render(routes, format)
		if err != nil {
			t.Fatal(err)
		}
		if format == subscriptioncodec.FormatBase64 {
			doc, err = base64.StdEncoding.DecodeString(string(doc))
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, forbidden := range []string{uuidB, "@pro.invalid", "privateKey", "www.example.org:443", "dxp-in-", testProfile(22).RealityPrivateKey} {
			if bytes.Contains(doc, []byte(forbidden)) {
				t.Fatal("private or unrelated metadata appeared in the consumer document")
			}
		}
		for _, line := range strings.Split(strings.TrimSuffix(string(doc), "\n"), "\n") {
			u, err := url.Parse(line)
			if err != nil || u.User.Username() != uuidA || u.Hostname() != "127.0.0.1" || (u.Port() != "11443" && u.Port() != "12443") {
				t.Fatal("consumer document gained an unrelated or unproven route")
			}
		}
	}
	// Ordering of the external descriptor cannot alter the deterministic result.
	c.Listeners[0], c.Listeners[2] = c.Listeners[2], c.Listeners[0]
	again, err := LabSubscriptionRoutes(c, uuidA, labels)
	if err != nil || !reflect.DeepEqual(routes, again) {
		t.Fatal("listener iteration order changed extracted routes")
	}
}

func editLabSubscriptionConfig(t *testing.T, c *Candidate, edit func(map[string]any)) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(c.Config, &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	c.Config = raw
	sum := sha256.Sum256(raw)
	c.SHA256 = hex.EncodeToString(sum[:])
}

func TestLabSubscriptionRejectsAlteredIncompleteAndUnprovenCandidates(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Candidate)
	}{
		{"altered config SHA", func(c *Candidate) { c.Config = append(c.Config, ' ') }},
		{"invalid manifest", func(c *Candidate) { c.ManifestSHA256 = "invalid" }},
		{"zero manifest", func(c *Candidate) { c.ManifestSHA256 = strings.Repeat("0", 64) }},
		{"stopped", func(c *Candidate) { c.NoListeners = true }},
		{"preactivated", func(c *Candidate) { c.CanActivate = true }},
		{"lost syntax gate", func(c *Candidate) { c.RequiresXrayTest = false }},
		{"lost proof gate", func(c *Candidate) { c.RequiresDataPlaneProof = false }},
		{"lost subscription gate", func(c *Candidate) { c.RequiresSubscriptionCommit = false }},
		{"missing listener", func(c *Candidate) { c.Listeners = c.Listeners[:2] }},
		{"wrong listener port", func(c *Candidate) { c.Listeners[0].NodePort++ }},
		{"wrong listener tag", func(c *Candidate) { c.Listeners[0].Tag = "probe" }},
		{"duplicate listener", func(c *Candidate) { c.Listeners[1] = c.Listeners[0] }},
		{"swap route", func(c *Candidate) { c.Listeners[0].Kind = "swap" }},
		{"ambiguous accounting", func(c *Candidate) { c.Listeners[0].Attribution = "ambiguous_shared_port" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := labSubscriptionCandidate(t)
			tc.change(&c)
			got, err := LabSubscriptionRoutes(c, uuidA, map[uint64]string{11: "one", 22: "two"})
			if !errors.Is(err, ErrInvalid) || got != nil {
				t.Fatal("altered candidate yielded consumer routes")
			}
		})
	}
	for _, field := range []string{"listen", "protocol", "decryption", "network", "security", "flow", "privateKey", "serverNames", "shortIds", "duplicate user", "duplicate inbound", "unknown field", "nonconsumer inbound invalid"} {
		t.Run(field, func(t *testing.T) {
			c := labSubscriptionCandidate(t)
			editLabSubscriptionConfig(t, &c, func(doc map[string]any) {
				ins := doc["inbounds"].([]any)
				in := ins[0].(map[string]any)
				settings := in["settings"].(map[string]any)
				stream := in["streamSettings"].(map[string]any)
				reality := stream["realitySettings"].(map[string]any)
				switch field {
				case "listen":
					in["listen"] = "0.0.0.0"
				case "protocol":
					in["protocol"] = "socks"
				case "decryption":
					settings["decryption"] = "other"
				case "network":
					stream["network"] = "grpc"
				case "security":
					stream["security"] = "tls"
				case "flow":
					settings["clients"].([]any)[0].(map[string]any)["flow"] = ""
				case "privateKey":
					reality["privateKey"] = "invalid"
				case "serverNames":
					reality["serverNames"] = []any{}
				case "shortIds":
					reality["shortIds"] = []any{"a"}
				case "duplicate user":
					users := settings["clients"].([]any)
					settings["clients"] = append(users, users[0])
				case "duplicate inbound":
					ins[1] = ins[0]
				case "unknown field":
					stream["sockopt"] = map[string]any{"dialerProxy": "unexpected"}
				case "nonconsumer inbound invalid":
					ins[2].(map[string]any)["listen"] = "192.0.2.1"
				}
			})
			got, err := LabSubscriptionRoutes(c, uuidA, map[uint64]string{11: "one", 22: "two"})
			if !errors.Is(err, ErrInvalid) || got != nil {
				t.Fatal("unsupported or inconsistent config yielded routes")
			}
		})
	}
}

func TestLabSubscriptionRequiresExplicitConsumerMembershipAndSafeLabels(t *testing.T) {
	c := labSubscriptionCandidate(t)
	for _, request := range []struct {
		uuid   string
		labels map[uint64]string
	}{
		{"", map[uint64]string{11: "one", 22: "two"}},
		{"11223344-5566-4788-99aa-bbccddeeff99", map[uint64]string{11: "one", 22: "two"}},
		{uuidA, nil},
		{uuidA, map[uint64]string{11: "one"}},
		{uuidA, map[uint64]string{11: "one\ninjected", 22: "two"}},
	} {
		got, err := LabSubscriptionRoutes(c, request.uuid, request.labels)
		if !errors.Is(err, ErrInvalid) || got != nil {
			t.Fatal("missing membership or invalid display label yielded routes")
		}
	}
}

func TestLabSubscriptionRejectsDuplicateJSONKeysEvenWithUpdatedDigest(t *testing.T) {
	for _, replacement := range []string{`"listen":"127.0.0.1","listen":"127.0.0.1"`, `"listen":"127.0.0.1","Listen":"127.0.0.1"`} {
		c := labSubscriptionCandidate(t)
		c.Config = bytes.Replace(c.Config, []byte(`"listen":"127.0.0.1"`), []byte(replacement), 1)
		sum := sha256.Sum256(c.Config)
		c.SHA256 = hex.EncodeToString(sum[:])
		got, err := LabSubscriptionRoutes(c, uuidA, map[uint64]string{11: "one", 22: "two"})
		if !errors.Is(err, ErrInvalid) || got != nil {
			t.Fatal("ambiguous private JSON yielded consumer routes")
		}
	}
}
