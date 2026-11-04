package telegrambot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/png"
	"strings"
	"unicode/utf8"
)

const maxDocumentBytes = 32768
const maxQRPNGSide = 1024

func validDocument(d Document) bool {
	if len(d.Name) < 5 || len(d.Name) > 64 || len(d.Content) < 1 || len(d.Content) > maxDocumentBytes || (d.Caption != "" && !validText(d.Caption, 1024)) {
		return false
	}
	for _, c := range d.Name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	if strings.HasSuffix(d.Name, ".txt") {
		return utf8.Valid(d.Content) && (d.ContentType == "" || d.ContentType == "text/plain" || d.ContentType == "text/plain; charset=utf-8") && len(d.SourceContent) == 0 && d.SourceSHA256 == "" && d.ContentSHA256 == ""
	}
	if !strings.HasSuffix(d.Name, ".png") || d.ContentType != "image/png" || !hexDigest(d.SourceSHA256) || !hexDigest(d.ContentSHA256) || len(d.SourceContent) < 1 || len(d.SourceContent) > maxQRURIBytes+1 || !utf8.Valid(d.SourceContent) {
		return false
	}
	sourceSum := sha256.Sum256(d.SourceContent)
	if hex.EncodeToString(sourceSum[:]) != d.SourceSHA256 {
		return false
	}
	uri, ok := singleQRURI(string(d.SourceContent))
	if !ok {
		return false
	}
	sum := sha256.Sum256(d.Content)
	return hex.EncodeToString(sum[:]) == d.ContentSHA256 && validQRPNG(d.Content, uri)
}

// Accept only the small, opaque black/white PNG form our pinned encoder emits.
// Check dimensions before decoding so compressed input cannot allocate an
// unbounded image. Reject appended bytes and metadata-bearing ancillary chunks.
func validQRPNG(data []byte, uri string) bool {
	if len(data) < 45 || len(data) > maxDocumentBytes || !bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) || binary.BigEndian.Uint32(data[8:12]) != 13 || string(data[12:16]) != "IHDR" || data[24] != 1 || data[25] != 3 || data[26] != 0 || data[27] != 0 || data[28] != 0 {
		return false
	}
	w, h := binary.BigEndian.Uint32(data[16:20]), binary.BigEndian.Uint32(data[20:24])
	if w < 64 || w > maxQRPNGSide || w != h {
		return false
	}
	seenPalette, seenData, seenEnd := false, false, false
	for pos := 8; pos < len(data); {
		if len(data)-pos < 12 {
			return false
		}
		length := int64(binary.BigEndian.Uint32(data[pos : pos+4]))
		if length > int64(len(data)-pos-12) {
			return false
		}
		end := pos + 12 + int(length)
		switch string(data[pos+4 : pos+8]) {
		case "IHDR":
			if pos != 8 {
				return false
			}
		case "PLTE":
			if seenPalette || seenData || !bytes.Equal(data[pos+8:pos+8+int(length)], []byte{255, 255, 255, 0, 0, 0}) {
				return false
			}
			seenPalette = true
		case "IDAT":
			if !seenPalette || seenEnd {
				return false
			}
			seenData = true
		case "IEND":
			if length != 0 || !seenData || end != len(data) {
				return false
			}
			seenEnd = true
		default:
			return false
		}
		pos = end
	}
	if !seenEnd {
		return false
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != int(w) || config.Height != int(h) {
		return false
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds().Dx() != int(w) || decoded.Bounds().Dy() != int(h) {
		return false
	}
	indexed, ok := decoded.(*image.Paletted)
	return ok && qrPixelsMatchSource(indexed, uri)
}
