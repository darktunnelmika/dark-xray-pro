package telegrambot

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const syntheticQRURI = "vless://11111111-2222-4333-8444-555555555555@edge.example.invalid:443?encryption=none&security=tls&type=tcp#DARK-QR-%D8%AA%D8%B3%D8%AA"

func qrProof(content string) DeliveryProof {
	sum := sha256.Sum256([]byte(content))
	return DeliveryProof{ClientID: "synthetic-client", SHA256: hex.EncodeToString(sum[:]), TermsSHA256: strings.Repeat("a", 64), ExpiresUnixNano: time.Now().Add(time.Minute).UnixNano()}
}

func TestQRNativeConfigurationProducesBoundedLosslessPNG(t *testing.T) {
	nearLimit := strings.TrimSuffix(syntheticQRURI, "#DARK-QR-%D8%AA%D8%B3%D8%AA") + "&padding="
	nearLimit += strings.Repeat("abc9d27fe1", (maxQRURIBytes-len(nearLimit))/10)
	nearLimit += strings.Repeat("x", maxQRURIBytes-len(nearLimit))
	for name, uri := range map[string]string{"native": syntheticQRURI, "near-limit": nearLimit} {
		t.Run(name, func(t *testing.T) {
			content := uri + "\n"
			proof := qrProof(content)
			doc, err := prepareQRDelivery(content, proof, "Scan this private service configuration in your client.")
			if err != nil {
				t.Fatal(err)
			}
			if doc.Name != "dark-service-qr.png" || doc.ContentType != "image/png" || doc.SourceSHA256 != proof.SHA256 || string(doc.SourceContent) != content || len(doc.Content) > maxDocumentBytes || !validDocument(*doc) {
				t.Fatal("invalid PNG delivery")
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(doc.Content))
			if err != nil || cfg.Width != cfg.Height || cfg.Width > maxQRPNGSide || cfg.Width%5 != 0 {
				t.Fatal("image scaling/dimensions", cfg, err)
			}
			raw, err := json.Marshal(wireDocument{ChatID: 2001, Name: doc.Name, Content: doc.Content, Caption: doc.Caption, ContentType: doc.ContentType, SourceContent: doc.SourceContent, SourceSHA256: doc.SourceSHA256, ContentSHA256: doc.ContentSHA256})
			if err != nil || len(raw) > 64<<10 {
				t.Fatal("private queue payload exceeds bound", err)
			}
			var restored wireDocument
			if decodeJSON(raw, &restored) != nil || !bytes.Equal(restored.Content, doc.Content) || !bytes.Equal(restored.SourceContent, doc.SourceContent) || !validDocument(restored.document()) {
				t.Fatal("JSON corrupted binary PNG")
			}
			if bytes.Contains(raw, []byte(uri)) || strings.Contains(doc.Caption, "vless://") {
				t.Fatal("plaintext configuration exposed in receipt")
			}
			// Optional local-only independent scanner QA; all inputs are synthetic.
			if dir := os.Getenv("DARK_PRO_QR_TEST_OUTPUT"); dir != "" {
				if !filepath.IsAbs(dir) {
					t.Fatal("fixture output must be absolute")
				}
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name+".png"), doc.Content, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(uri), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestQRUnavailablePreservesNativeFileFallbackBoundary(t *testing.T) {
	for name, content := range map[string]string{"multiple routes": syntheticQRURI + "\n" + syntheticQRURI + "\n", "blank line": syntheticQRURI + "\n\n", "CRLF": syntheticQRURI + "\r\n", "oversized": syntheticQRURI + strings.Repeat("x", maxQRURIBytes), "leading space": " " + syntheticQRURI, "arbitrary web URL": "https://example.invalid/subscription", "JSON": "{\"outbounds\":[]}", "base64 subscription": base64.StdEncoding.EncodeToString([]byte(syntheticQRURI)), "missing credentials": "vless://edge.example.invalid:443", "invalid port": "trojan://secret@edge.example.invalid:0", "broken vmess": "vmess://not-a-base64-json"} {
		t.Run(name, func(t *testing.T) {
			doc, err := prepareQRDelivery(content, qrProof(content), "Private configuration")
			if !errors.Is(err, ErrQRUnavailable) || doc != nil {
				t.Fatal("unsupported QR did not request exact original-file fallback", err)
			}
		})
	}
	vmess := "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","add":"edge.example.invalid","port":"443","id":"11111111-2222-4333-8444-555555555555"}`))
	for _, uri := range []string{vmess, "trojan://synthetic-password@edge.example.invalid:443?security=tls", "ss://YWVzLTEyOC1nY206c3ludGhldGlj@edge.example.invalid:443", "hysteria2://synthetic-password@edge.example.invalid:443"} {
		if _, err := prepareQRDelivery(uri, qrProof(uri), "Private configuration"); err != nil {
			t.Fatal("native URI rejected", err)
		}
	}
}

func TestQRInvalidProofCannotBecomeFileFallback(t *testing.T) {
	for name, change := range map[string]func(*DeliveryProof){"wrong content hash": func(p *DeliveryProof) { p.SHA256 = strings.Repeat("b", 64) }, "missing terms": func(p *DeliveryProof) { p.TermsSHA256 = "" }, "expired": func(p *DeliveryProof) { p.ExpiresUnixNano = time.Now().Add(-time.Second).UnixNano() }, "missing client": func(p *DeliveryProof) { p.ClientID = "" }} {
		t.Run(name, func(t *testing.T) {
			p := qrProof(syntheticQRURI)
			change(&p)
			doc, err := prepareQRDelivery(syntheticQRURI, p, "Private QR")
			if !errors.Is(err, ErrProtocol) || errors.Is(err, ErrQRUnavailable) || doc != nil {
				t.Fatal("unverified proof permitted fallback", err)
			}
		})
	}
	p := qrProof(syntheticQRURI)
	if _, err := prepareQRDelivery(syntheticQRURI+"\n", p, "Private QR"); !errors.Is(err, ErrProtocol) {
		t.Fatal("source hash checked only after normalization")
	}
}

func TestPNGDocumentValidationRejectsCorruptionAndUnboundedImages(t *testing.T) {
	doc, err := prepareQRDelivery(syntheticQRURI, qrProof(syntheticQRURI), "Private QR")
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Document){
		"filename mismatch": func(d *Document) { d.Name = "service.txt" }, "missing type": func(d *Document) { d.ContentType = "" }, "wrong type": func(d *Document) { d.ContentType = "image/jpeg" }, "missing source binding": func(d *Document) { d.SourceSHA256 = "" }, "wrong image hash": func(d *Document) { d.ContentSHA256 = strings.Repeat("0", 64) },
		"missing original source": func(d *Document) { d.SourceContent = nil }, "changed original source": func(d *Document) { d.SourceContent = []byte(syntheticQRURI + "changed") }, "oversized original source": func(d *Document) { d.SourceContent = make([]byte, maxQRURIBytes+2) },
		"oversized content": func(d *Document) { d.Content = make([]byte, maxDocumentBytes+1) }, "trailing data": func(d *Document) { d.Content = append(d.Content, []byte("hidden metadata")...) }, "CRC corruption": func(d *Document) { d.Content[len(d.Content)-1] ^= 1 }, "dimension bomb": func(d *Document) { binary.BigEndian.PutUint32(d.Content[16:20], 1<<28) }, "not square": func(d *Document) { binary.BigEndian.PutUint32(d.Content[20:24], 64) },
	} {
		t.Run(name, func(t *testing.T) {
			copyDoc := *doc
			copyDoc.Content = append([]byte(nil), doc.Content...)
			mutate(&copyDoc)
			if name != "wrong image hash" {
				sum := sha256.Sum256(copyDoc.Content)
				copyDoc.ContentSHA256 = hex.EncodeToString(sum[:])
			}
			if validDocument(copyDoc) {
				t.Fatal("malformed PNG accepted")
			}
		})
	}
	legacy := Document{Name: "dark-service.txt", Content: []byte(syntheticQRURI + "\n"), Caption: "Private configuration"}
	if !validDocument(legacy) {
		t.Fatal("legacy text document rejected")
	}
}

func TestQRPNGValidationBindsPixelsIndependentlyOfCompression(t *testing.T) {
	doc, err := prepareQRDelivery(syntheticQRURI, qrProof(syntheticQRURI), "Private QR")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(doc.Content))
	if err != nil {
		t.Fatal(err)
	}
	var alternate bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&alternate, decoded); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(alternate.Bytes(), doc.Content) {
		t.Fatal("fixture did not change PNG compression")
	}
	doc.Content = alternate.Bytes()
	sum := sha256.Sum256(doc.Content)
	doc.ContentSHA256 = hex.EncodeToString(sum[:])
	if !validDocument(*doc) {
		t.Fatal("unchanged QR pixels rejected because PNG compression changed")
	}
}

func TestPinnedQREncoderProvenanceMatchesUnmodifiedUpstream(t *testing.T) {
	root := filepath.Join("..", "..", "third_party", "go-qrcode")
	raw, err := os.ReadFile(filepath.Join(root, "UPSTREAM.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Commit, License, Modifications string
		Files                          []struct {
			Path  string
			SHA   string `json:"git_blob_sha1"`
			Bytes int
		}
	}
	if json.Unmarshal(raw, &meta) != nil || meta.Commit != "da1b6568686e89143e94f980a98bc2dbd5537f13" || meta.License != "MIT" || meta.Modifications != "none" || len(meta.Files) != 21 {
		t.Fatal("unpinned encoder source")
	}
	for _, entry := range meta.Files {
		data, err := os.ReadFile(filepath.Join(root, entry.Path))
		if err != nil {
			t.Fatal(err)
		}
		h := sha1.New()
		fmt.Fprintf(h, "blob %d%c", len(data), 0)
		h.Write(data)
		if hex.EncodeToString(h.Sum(nil)) != entry.SHA || len(data) != entry.Bytes {
			t.Fatalf("upstream source changed: %s", entry.Path)
		}
	}
}
