package telegrambot

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	qrcode "github.com/skip2/go-qrcode"
)

const maxQRURIBytes = 2048
const qrModulePixels = 5

// prepareQRDelivery transforms only an already verified native projection.
// Ownership/current terms are established by BusinessBackend's delivery bridge
// and checked again by the existing send/ack protocol. No URI is fetched and no
// credential is sent to an image service. Encoding is pure Go, in memory.
//
// ErrQRUnavailable permits an honest original-file fallback. An invalid or
// expired proof returns ErrProtocol, which must never become a fallback send.
func prepareQRDelivery(content string, proof DeliveryProof, caption string) (*Document, error) {
	if !validOpaque(proof.ClientID, 128) || !hexDigest(proof.SHA256) || !hexDigest(proof.TermsSHA256) || proof.ExpiresUnixNano <= time.Now().UnixNano() || len(content) < 1 || len(content) > 32768 || !utf8.ValidString(content) || (caption != "" && !validText(caption, 1024)) {
		return nil, ErrProtocol
	}
	sum := sha256.Sum256([]byte(content))
	if hex.EncodeToString(sum[:]) != proof.SHA256 {
		return nil, ErrProtocol
	}
	uri, ok := singleQRURI(content)
	if !ok {
		return nil, ErrQRUnavailable
	}
	code, err := qrcode.New(uri, qrcode.Medium)
	if err != nil {
		return nil, ErrQRUnavailable
	}
	// Keep the standard four-module quiet zone and integer scaling. Five
	// pixels/module bounds a version-40 symbol to 925x925 without resampling.
	pngBytes, err := code.PNG(-qrModulePixels)
	if err != nil {
		return nil, ErrQRUnavailable
	}
	if proof.ExpiresUnixNano <= time.Now().UnixNano() {
		return nil, ErrProtocol
	}
	imageSum := sha256.Sum256(pngBytes)
	d := &Document{Name: "dark-service-qr.png", Content: pngBytes, Caption: caption, ContentType: "image/png", SourceContent: []byte(content), SourceSHA256: proof.SHA256, ContentSHA256: hex.EncodeToString(imageSum[:])}
	if !validDocument(*d) {
		return nil, ErrQRUnavailable
	}
	return d, nil
}

// Check every pixel against the pinned encoder's deterministic modules. This
// binds the image to the hash-verified original URI, rather than trusting two
// unrelated declared hashes. PNG compression may change without changing the
// symbol. The private queue retains the bounded source only until completion.
func qrPixelsMatchSource(actual *image.Paletted, uri string) bool {
	code, err := qrcode.New(uri, qrcode.Medium)
	if err != nil {
		return false
	}
	modules := code.Bitmap()
	side := len(modules) * qrModulePixels
	if side > maxQRPNGSide || actual.Bounds() != image.Rect(0, 0, side, side) {
		return false
	}
	for y := 0; y < side; y++ {
		expected := modules[y/qrModulePixels]
		if len(expected) != len(modules) {
			return false
		}
		row := actual.Pix[y*actual.Stride : y*actual.Stride+side]
		for x, index := range row {
			if index > 1 || (index == 1) != expected[x/qrModulePixels] {
				return false
			}
		}
	}
	return true
}

func singleQRURI(content string) (string, bool) {
	// Native URI documents may contain exactly one final LF. Do not trim
	// arbitrary whitespace, choose a first route, rewrite credentials, or
	// encode multi-route/base64/JSON subscriptions as if they were one URI.
	uri := strings.TrimSuffix(content, "\n")
	if len(uri) == 0 || len(uri) > maxQRURIBytes {
		return "", false
	}
	for _, r := range uri {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", false
		}
	}
	if strings.HasPrefix(uri, "vmess://") {
		payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "vmess://"))
		if err != nil || len(payload) == 0 || !json.Valid(payload) {
			return "", false
		}
		var fields map[string]json.RawMessage
		if decodeJSON(payload, &fields) != nil || len(fields) == 0 {
			return "", false
		}
		return uri, true
	}
	u, err := url.Parse(uri)
	if err != nil || u.Opaque != "" || u.Hostname() == "" || u.User == nil || u.User.Username() == "" {
		return "", false
	}
	switch u.Scheme {
	case "vless", "trojan", "ss", "hysteria2":
	default:
		return "", false
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 {
		return "", false
	}
	return uri, true
}
