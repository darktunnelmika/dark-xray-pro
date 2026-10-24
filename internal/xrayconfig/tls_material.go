package xrayconfig

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
)

const (
	MaxTLSCertificatePEMBytes = 64 << 10
	MaxTLSPrivateKeyPEMBytes  = 16 << 10
	MaxTLSCertificateChain    = 8
	MaxTLSCertificateDNSNames = 1024
)

// TLSMaterial is PRIVATE. Only encrypted profiles and private candidate bytes
// may contain it; public plans, previews and receipts use the metadata below.
type TLSMaterial struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

type TLSCertificateMetadata struct {
	SHA256          string   `json:"sha256"`
	PublicKeySHA256 string   `json:"public_key_sha256"`
	DNSNames        []string `json:"dns_names"`
	NotBeforeUnix   int64    `json:"not_before_unix"`
	NotAfterUnix    int64    `json:"not_after_unix"`
	KeyType         string   `json:"key_type"`
}

func NormalizeProfileSecurity(security string) (string, error) {
	value, err := subscriptioncodec.NormalizeSecurity(security)
	if err != nil {
		return "", &ProfileValidationError{Field: "security", Code: "security_invalid"}
	}
	return value, nil
}

func tlsCertificateError(code string) error {
	return &ProfileValidationError{Field: "tls_credentials.certificate_pem", Code: code}
}

func tlsKeyError(code string) error {
	return &ProfileValidationError{Field: "tls_credentials.private_key_pem", Code: code}
}

// NormalizeTLSMaterial checks a bounded, ordered PEM chain and its one private
// key. It does not consult a clock or assert public CA trust. This lets expired
// saved profiles remain readable for replacement. Preview/save call the time
// helper separately; clients still perform their normal trust verification.
func NormalizeTLSMaterial(in *TLSMaterial) (*TLSMaterial, *TLSCertificateMetadata, error) {
	out, chain, pair, err := parseTLSMaterial(in)
	if err != nil {
		return nil, nil, err
	}
	leaf := chain[0]
	leafSHA := sha256.Sum256(leaf.Raw)
	keySHA := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	keyType := ""
	switch pair.PrivateKey.(type) {
	case *rsa.PrivateKey:
		keyType = "RSA"
	case *ecdsa.PrivateKey:
		keyType = "ECDSA"
	case ed25519.PrivateKey:
		keyType = "Ed25519"
	}
	meta := &TLSCertificateMetadata{
		SHA256: hex.EncodeToString(leafSHA[:]), PublicKeySHA256: hex.EncodeToString(keySHA[:]),
		DNSNames: append([]string(nil), leaf.DNSNames...), NotBeforeUnix: leaf.NotBefore.Unix(),
		NotAfterUnix: leaf.NotAfter.Unix(), KeyType: keyType,
	}
	return out, meta, nil
}

// ValidateTLSCertificateTime is separate from storage decoding and
// ValidateProfile. A replay of an immutable receipt is not a fresh save.
func ValidateTLSCertificateTime(in *TLSMaterial, now time.Time) error {
	_, chain, _, err := parseTLSMaterial(in)
	if err != nil {
		return err
	}
	for _, cert := range chain {
		if now.Before(cert.NotBefore) {
			return tlsCertificateError("tls_certificate_not_yet_valid")
		}
		if now.After(cert.NotAfter) {
			return tlsCertificateError("tls_certificate_expired")
		}
	}
	return nil
}

func parseTLSMaterial(in *TLSMaterial) (*TLSMaterial, []*x509.Certificate, tls.Certificate, error) {
	var empty tls.Certificate
	if in == nil || len(in.CertificatePEM) == 0 || len(in.CertificatePEM) > MaxTLSCertificatePEMBytes {
		return nil, nil, empty, tlsCertificateError("tls_certificate_invalid")
	}
	if len(in.PrivateKeyPEM) == 0 || len(in.PrivateKeyPEM) > MaxTLSPrivateKeyPEMBytes {
		return nil, nil, empty, tlsKeyError("tls_private_key_invalid")
	}
	remaining := bytes.TrimSpace([]byte(in.CertificatePEM))
	var chain []*x509.Certificate
	var canonical bytes.Buffer
	seen := map[[32]byte]bool{}
	for len(remaining) != 0 {
		if len(chain) >= MaxTLSCertificateChain || !bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, nil, empty, tlsCertificateError("tls_certificate_invalid")
		}
		block, rest := decodeTLSPEMFrame(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, nil, empty, tlsCertificateError("tls_certificate_invalid")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.NotAfter.After(cert.NotBefore) || len(cert.UnhandledCriticalExtensions) != 0 {
			return nil, nil, empty, tlsCertificateError("tls_certificate_invalid")
		}
		digest := sha256.Sum256(cert.Raw)
		if seen[digest] {
			return nil, nil, empty, tlsCertificateError("tls_certificate_chain_invalid")
		}
		seen[digest] = true
		if len(chain) > 0 && (!bytes.Equal(chain[len(chain)-1].RawIssuer, cert.RawSubject) || chain[len(chain)-1].CheckSignatureFrom(cert) != nil) {
			return nil, nil, empty, tlsCertificateError("tls_certificate_chain_invalid")
		}
		if len(cert.ExtKeyUsage) != 0 || len(cert.UnknownExtKeyUsage) != 0 {
			serverAuth := false
			for _, usage := range cert.ExtKeyUsage {
				serverAuth = serverAuth || usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny
			}
			if !serverAuth {
				return nil, nil, empty, tlsCertificateError("tls_certificate_usage_invalid")
			}
		}
		chain = append(chain, cert)
		_ = pem.Encode(&canonical, block)
		remaining = bytes.TrimSpace(rest)
	}
	if len(chain) == 0 || chain[0].IsCA || len(chain[0].DNSNames) == 0 || len(chain[0].DNSNames) > MaxTLSCertificateDNSNames ||
		chain[0].KeyUsage != 0 && chain[0].KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return nil, nil, empty, tlsCertificateError("tls_certificate_usage_invalid")
	}
	for _, name := range chain[0].DNSNames {
		if len(name) > 253 || !validDNS(strings.TrimPrefix(name, "*.")) {
			return nil, nil, empty, tlsCertificateError("tls_certificate_names_invalid")
		}
	}
	keyPEM := bytes.TrimSpace([]byte(in.PrivateKeyPEM))
	if !bytes.HasPrefix(keyPEM, []byte("-----BEGIN ")) {
		return nil, nil, empty, tlsKeyError("tls_private_key_invalid")
	}
	block, rest := decodeTLSPEMFrame(keyPEM)
	if block == nil || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 ||
		(block.Type != "PRIVATE KEY" && block.Type != "RSA PRIVATE KEY" && block.Type != "EC PRIVATE KEY") {
		return nil, nil, empty, tlsKeyError("tls_private_key_invalid")
	}
	keyPEM = pem.EncodeToMemory(block)
	pair, err := tls.X509KeyPair(canonical.Bytes(), keyPEM)
	if err != nil {
		return nil, nil, empty, tlsKeyError("tls_private_key_mismatch")
	}
	switch key := pair.PrivateKey.(type) {
	case *rsa.PrivateKey:
		if key.N.BitLen() < 2048 || key.N.BitLen() > 8192 || key.Validate() != nil {
			return nil, nil, empty, tlsKeyError("tls_private_key_invalid")
		}
	case *ecdsa.PrivateKey:
		bits := key.Curve.Params().BitSize
		if bits != 256 && bits != 384 && bits != 521 {
			return nil, nil, empty, tlsKeyError("tls_private_key_invalid")
		}
	case ed25519.PrivateKey:
		if len(key) != ed25519.PrivateKeySize {
			return nil, nil, empty, tlsKeyError("tls_private_key_invalid")
		}
	default:
		return nil, nil, empty, tlsKeyError("tls_private_key_invalid")
	}
	return &TLSMaterial{CertificatePEM: canonical.String(), PrivateKeyPEM: string(keyPEM)}, chain, pair, nil
}

// pem.Decode searches forward past malformed blocks. A credential parser must
// instead consume the first exact frame or fail, so the certificate displayed
// to the owner is the same first certificate that reaches the core.
func decodeTLSPEMFrame(raw []byte) (*pem.Block, []byte) {
	lineEnd := bytes.IndexByte(raw, '\n')
	if lineEnd < 0 {
		return nil, nil
	}
	header := strings.TrimSuffix(string(raw[:lineEnd]), "\r")
	if !strings.HasPrefix(header, "-----BEGIN ") || !strings.HasSuffix(header, "-----") {
		return nil, nil
	}
	kind := strings.TrimSuffix(strings.TrimPrefix(header, "-----BEGIN "), "-----")
	end := []byte("-----END " + kind + "-----")
	index := bytes.Index(raw[lineEnd+1:], end)
	if index < 0 {
		return nil, nil
	}
	frameEnd := lineEnd + 1 + index + len(end)
	frame := raw[:frameEnd]
	if bytes.Contains(frame[lineEnd+1:], []byte("-----BEGIN ")) {
		return nil, nil
	}
	block, rest := pem.Decode(frame)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, nil
	}
	return block, raw[frameEnd:]
}

func validateProfileTLS(p Profile) error {
	if p.RealityTarget != "" || p.RealityPrivateKey != "" || len(p.ShortIDs) != 0 {
		return &ProfileValidationError{Field: "security", Code: "tls_reality_fields_invalid"}
	}
	if o := p.Options; o != nil {
		if o.RealityXver != 0 || o.RealityMaxTimeDiff != 0 || o.RealityMinClientVer != "" || o.RealityMaxClientVer != "" || o.RealityShow {
			return &ProfileValidationError{Field: "security", Code: "tls_reality_options_invalid"}
		}
		if o.Client != nil && o.Client.SpiderX != "" {
			return &ProfileValidationError{Field: "options.client.spider_x", Code: "tls_spider_x_invalid"}
		}
		if len(o.Fallbacks) != 0 {
			return &ProfileValidationError{Field: "options.fallbacks", Code: "tls_fallbacks_unsupported"}
		}
	}
	_, chain, _, err := parseTLSMaterial(p.TLS)
	if err != nil {
		return err
	}
	for _, name := range p.ServerNames {
		if chain[0].VerifyHostname(name) != nil {
			return &ProfileValidationError{Field: "server_names", Code: "tls_server_name_uncovered"}
		}
	}
	return nil
}

type tlsSettings struct {
	Certificates     []tlsInlineCertificate `json:"certificates"`
	ALPN             []string               `json:"alpn"`
	MinVersion       string                 `json:"minVersion"`
	RejectUnknownSNI bool                   `json:"rejectUnknownSni"`
}

type tlsInlineCertificate struct {
	Certificate    []string `json:"certificate"`
	Key            []string `json:"key"`
	OneTimeLoading bool     `json:"oneTimeLoading"`
}

func profileTLSSettings(p Profile) *tlsSettings {
	// Validation has established a matching key and bounded PEM.
	material, _, _ := NormalizeTLSMaterial(p.TLS)
	alpn := []string{"h2", "http/1.1"}
	switch p.Transport {
	case "grpc", "xhttp":
		alpn = []string{"h2"}
	case "ws", "httpupgrade":
		alpn = []string{"http/1.1"}
	}
	return &tlsSettings{MinVersion: "1.3", ALPN: alpn, RejectUnknownSNI: true,
		Certificates: []tlsInlineCertificate{{
			Certificate: strings.Split(strings.TrimSuffix(material.CertificatePEM, "\n"), "\n"),
			Key:         strings.Split(strings.TrimSuffix(material.PrivateKeyPEM, "\n"), "\n"), OneTimeLoading: true,
		}}}
}
