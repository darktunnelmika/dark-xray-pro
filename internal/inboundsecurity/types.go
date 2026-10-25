// Package inboundsecurity provides owner-requested inbound key, certificate and
// public TLS target tools. It does not write configurations or schedule scans.
package inboundsecurity

import (
	"context"
	"crypto/x509"
	"net"
	"time"
)

const (
	MaxTargets       = 32
	MaxCIDRAddresses = 256
	MaxCertificates  = 16
	MaxPEMBytes      = 128 << 10
)

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// Config is startup-only authority. None of its policy or transport options can
// be overridden by an HTTP request. LocalNodeID is set only inside a Node agent;
// a Hub uses NodeProbe to obtain results from the selected Node.
type Config struct {
	CorePath       string
	CoreSHA256     string
	DefaultTargets []Target
	ProbeTimeout   time.Duration
	FindTimeout    time.Duration
	MaxConcurrency int
	LocalNodeID    string
	NodeProbe      func(context.Context, string, ProbeRequest) (ProbeResult, error)
	Resolver       Resolver
	DialContext    func(context.Context, string, string) (net.Conn, error)
	RootCAs        *x509.CertPool
	Now            func() time.Time
}

type Target struct {
	Target     string `json:"target"`
	ServerName string `json:"server_name"`
}

type ProbeRequest struct {
	Scope      string `json:"scope"`
	NodeID     string `json:"node_id"`
	Target     string `json:"target"`
	ServerName string `json:"server_name"`
}

type FindRequest struct {
	Scope      string   `json:"scope"`
	NodeID     string   `json:"node_id"`
	Targets    []Target `json:"targets,omitempty"`
	CIDR       string   `json:"cidr,omitempty"`
	Port       int      `json:"port,omitempty"`
	ServerName string   `json:"server_name,omitempty"`
}

type CertificateMetadata struct {
	Subject          string   `json:"subject"`
	Issuer           string   `json:"issuer"`
	SerialNumber     string   `json:"serial_number"`
	NotBefore        string   `json:"not_before"`
	NotAfter         string   `json:"not_after"`
	DNSNames         []string `json:"dns_names"`
	IPAddresses      []string `json:"ip_addresses"`
	KeyAlgorithm     string   `json:"key_algorithm"`
	Signature        string   `json:"signature_algorithm"`
	IsCA             bool     `json:"is_ca"`
	DERBytes         int      `json:"der_bytes"`
	SHA256Hex        string   `json:"sha256_hex"`
	SHA256Base64     string   `json:"sha256_base64"`
	SPKISHA256Hex    string   `json:"spki_sha256_hex"`
	SPKISHA256Base64 string   `json:"spki_sha256_base64"`
}

type CertificateInspection struct {
	Certificates  []CertificateMetadata `json:"certificates"`
	ServerName    string                `json:"server_name"`
	ChainValid    bool                  `json:"chain_valid"`
	HostnameValid bool                  `json:"hostname_valid"`
	TimeValid     bool                  `json:"time_valid"`
	Valid         bool                  `json:"valid"`
	Errors        []string              `json:"errors"`
}

type GenerateCertificateRequest struct {
	ServerNames []string `json:"server_names"`
	ValidDays   int      `json:"valid_days"`
}

type GeneratedCertificate struct {
	CertificatePEM string                `json:"certificate_pem"`
	PrivateKeyPEM  string                `json:"private_key_pem"`
	Certificate    CertificateInspection `json:"certificate"`
}

type ProbeResult struct {
	Scope             string                `json:"scope"`
	NodeID            string                `json:"node_id"`
	Target            string                `json:"target"`
	Host              string                `json:"host"`
	IP                string                `json:"ip"`
	Port              int                   `json:"port"`
	ServerName        string                `json:"server_name"`
	ServerNameSource  string                `json:"server_name_source"`
	OK                bool                  `json:"ok"`
	Suitable          bool                  `json:"suitable"`
	Error             string                `json:"error"`
	Reason            string                `json:"reason"`
	TLS13             bool                  `json:"tls13"`
	TLSVersion        string                `json:"tls_version"`
	H2                bool                  `json:"h2"`
	ALPN              string                `json:"alpn"`
	X25519            bool                  `json:"x25519"`
	CurveID           string                `json:"curve_id"`
	HybridKeyExchange string                `json:"hybrid_key_exchange"`
	LatencyMS         int64                 `json:"latency_ms"`
	CertChainBytes    int                   `json:"cert_chain_bytes"`
	Certificate       CertificateInspection `json:"certificate"`
}

type FindResult struct {
	Scope     string        `json:"scope"`
	NodeID    string        `json:"node_id"`
	Source    string        `json:"source"`
	Results   []ProbeResult `json:"results"`
	Total     int           `json:"total"`
	Completed int           `json:"completed"`
	Suitable  int           `json:"suitable"`
	Cancelled bool          `json:"cancelled"`
}

type KeyPair struct {
	Kind       string `json:"kind"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

type MLDSA65Keys struct {
	Kind   string `json:"kind"`
	Seed   string `json:"seed"`
	Verify string `json:"verify"`
}

type ECHKeys struct {
	Kind       string `json:"kind"`
	ServerName string `json:"server_name"`
	Config     string `json:"config"`
	PrivateKey string `json:"private_key"`
}

type Capabilities struct {
	Enabled            bool              `json:"enabled"`
	Keys               map[string]bool   `json:"keys"`
	Certificates       map[string]bool   `json:"certificates"`
	Reality            RealityCapability `json:"reality"`
	NodeProbeAvailable bool              `json:"node_probe_available"`
	DefaultTargets     []Target          `json:"default_targets"`
	UnavailableReasons map[string]string `json:"unavailable_reasons"`
}

type RealityCapability struct {
	Scan              bool `json:"scan"`
	Find              bool `json:"find"`
	PublicTargetsOnly bool `json:"public_targets_only"`
	MaxTargets        int  `json:"max_targets"`
	MaxCIDRAddresses  int  `json:"max_cidr_addresses"`
	MaxConcurrency    int  `json:"max_concurrency"`
	HybridKeyExchange bool `json:"hybrid_key_exchange"`
}

// Error contains a fixed, public-safe code. In particular it never contains
// private keys, executable output, resolver errors or remote certificate data.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

func failure(code string) error { return &Error{Code: code} }
