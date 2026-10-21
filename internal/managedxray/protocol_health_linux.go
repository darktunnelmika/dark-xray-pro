//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

var ErrProtocolHealth = errors.New("protocol health proof unavailable or stale")

const ProtocolHealthVersion = 2

// ProtocolHealthEndpoint is operator-approved verifier configuration, never an
// arbitrary browser URL. Only literal public IPv4 destinations are supported.
// CABundle is optional; ordinary certificate identity and pin checks both apply.
type ProtocolHealthEndpoint struct {
	Address        string `json:"address"`
	ServerName     string `json:"server_name"`
	CertificateSHA string `json:"certificate_sha"`
	CABundle       []byte `json:"ca_bundle,omitempty"`
}

func (e ProtocolHealthEndpoint) SHA() string { return hashJSON(e) }

func (e ProtocolHealthEndpoint) tlsConfig() (*tls.Config, error) {
	host, port, err := net.SplitHostPort(e.Address)
	ip := net.ParseIP(host)
	p, pe := strconv.Atoi(port)
	if err != nil || pe != nil || p < 1 || p > 65535 || ip == nil || ip.To4() == nil || host != ip.String() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || !transactionHash.MatchString(e.CertificateSHA) || len(e.ServerName) > 253 || !strings.Contains(e.ServerName, ".") || strings.ContainsAny(e.ServerName, ":/\\@% \t\r\n") || net.ParseIP(e.ServerName) != nil || len(e.CABundle) > 64<<10 {
		return nil, ErrProtocolHealth
	}
	var roots *x509.CertPool
	if len(e.CABundle) != 0 {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(e.CABundle) {
			return nil, ErrProtocolHealth
		}
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, ServerName: e.ServerName, RootCAs: roots, VerifyConnection: func(c tls.ConnectionState) error {
		if len(c.PeerCertificates) == 0 {
			return ErrProtocolHealth
		}
		h := sha256.Sum256(c.PeerCertificates[0].Raw)
		if hex.EncodeToString(h[:]) != e.CertificateSHA {
			return ErrProtocolHealth
		}
		return nil
	}}, nil
}

// The credential is identified by a one-way hash, never included in a receipt.
// This is not publication authority: a future HUB consumer must issue and
// consume a unique challenge and recheck current observations transactionally.
type ProtocolHealthRequest struct {
	Version         int              `json:"version"`
	ID              string           `json:"id"`
	HandoverID      string           `json:"handover_id"`
	NodeID          string           `json:"node_id"`
	InstallationID  string           `json:"installation_id"`
	RuntimeInstance string           `json:"runtime_instance"`
	StageID         string           `json:"stage_id"`
	ApplyCommandID  string           `json:"apply_command_id"`
	InboundID       uint64           `json:"inbound_id"`
	Port            uint16           `json:"port"`
	CredentialSHA   string           `json:"credential_sha"`
	SystemProbeID   string           `json:"system_probe_id"`
	EndpointSHA     string           `json:"endpoint_sha"`
	Expected        RuntimeCondition `json:"expected"`
	Usage           MeteredCondition `json:"usage"`
	Issued          int64            `json:"issued_unix_nano"`
	Expires         int64            `json:"expires_unix_nano"`
	Signature       string           `json:"signature"`
}

func validProtocolHealthRequest(q ProtocolHealthRequest) bool {
	return q.Version == ProtocolHealthVersion && transactionNode.MatchString(q.SystemProbeID) && transactionID.MatchString(q.ID) && transactionID.MatchString(q.HandoverID) && transactionNode.MatchString(q.NodeID) && transactionID.MatchString(q.InstallationID) && transactionID.MatchString(q.RuntimeInstance) && transactionID.MatchString(q.StageID) && transactionID.MatchString(q.ApplyCommandID) && q.InboundID > 0 && q.Port >= 1024 && transactionHash.MatchString(q.CredentialSHA) && transactionHash.MatchString(q.EndpointSHA) && q.Expected.ProcessReady && q.Expected.ActiveSequence > 0 && transactionHash.MatchString(q.Expected.ActiveSHA) && validMeteredCondition(q.Usage) && q.Usage.EpochID != "" && q.Usage.Settlement == (uc.Receipt{}) && q.Issued > 0 && q.Expires > q.Issued && q.Expires-q.Issued <= int64(8*time.Second)
}

func SignProtocolHealthRequest(key []byte, q ProtocolHealthRequest) (ProtocolHealthRequest, error) {
	if len(key) < 32 || !validProtocolHealthRequest(q) {
		return ProtocolHealthRequest{}, ErrProtocolHealth
	}
	q.Signature = ""
	q.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-REQUEST-v2", q)
	return q, nil
}

func verifyProtocolHealthRequest(key []byte, q ProtocolHealthRequest, now int64) bool {
	want, err := SignProtocolHealthRequest(key, q)
	return err == nil && q.Issued <= now && now < q.Expires && transactionHash.MatchString(q.Signature) && hmac.Equal([]byte(want.Signature), []byte(q.Signature))
}

type ProtocolHealthReceipt struct {
	Version     int    `json:"version"`
	RequestSHA  string `json:"request_sha"`
	ResponseSHA string `json:"response_sha"`
	Completed   int64  `json:"completed_unix_nano"`
	Signature   string `json:"signature"`
}

func protocolHealthBody(id string) []byte { return []byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + id + "\n") }
func protocolHealthResponseSHA(id string) string {
	h := sha256.Sum256(protocolHealthBody(id))
	return hex.EncodeToString(h[:])
}

// Historical cryptographic evidence only. Callers must additionally check
// live epoch/stage/lease/observation and a persisted, unconsumed HUB challenge.
func VerifyProtocolHealthReceipt(key []byte, q ProtocolHealthRequest, p ProtocolHealthReceipt, now time.Time) bool {
	if !verifyProtocolHealthRequest(key, q, now.UnixNano()) || p.Version != ProtocolHealthVersion || p.RequestSHA != hashJSON(q) || p.ResponseSHA != protocolHealthResponseSHA(q.ID) || p.Completed < q.Issued || p.Completed > now.UnixNano() || p.Completed >= q.Expires || !transactionHash.MatchString(p.Signature) {
		return false
	}
	want := p
	want.Signature = ""
	return hmac.Equal([]byte(p.Signature), []byte(transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2", want)))
}

// Caller holds t.mu. Neither execution ACKs nor an open foreign socket suffice.
func (t *TransactionRuntime) protocolHealthCurrent(q ProtocolHealthRequest) (xrayconfig.Candidate, bool) {
	if t.closed || t.metered == nil || t.state.Pending != nil || t.instance != q.RuntimeInstance || t.options.NodeID != q.NodeID || t.options.InstallationID != q.InstallationID || t.state.Current == nil || t.state.Current.ID != q.StageID || t.state.LastCommand == nil || t.state.LastCommand.ID != q.ApplyCommandID || t.state.LastCommand.Action != "apply" || t.state.LastReceipt == nil || t.state.LastReceipt.Outcome != "applied" {
		return xrayconfig.Candidate{}, false
	}
	r := t.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.snapshot()
	if r.closed || r.fenced || r.active == nil || r.observer == nil || !r.observer.healthy() || Condition(s) != q.Expected || !s.ProcessReady || q.Expires > s.LeaseExpires || s.Usage == nil || !s.Usage.Running || s.Usage.Settled || s.Usage.Failure != "" {
		return xrayconfig.Candidate{}, false
	}
	u := MeteredCondition{}
	if r.state.Admission != nil {
		u.AdmissionSHA = hashJSON(r.state.Admission)
		if r.state.Admission.Settlement != nil {
			u.Settlement = *r.state.Admission.Settlement
		}
	}
	if r.state.Usage != nil {
		u.EpochID, u.EpochSHA = r.state.Usage.Epoch.ID, hashJSON(r.state.Usage.Epoch)
	}
	if u != q.Usage {
		return xrayconfig.Candidate{}, false
	}
	systemEmail, err := uc.CounterEmail(q.SystemProbeID)
	if err != nil || r.state.Usage == nil {
		return xrayconfig.Candidate{}, false
	}
	found := false
	for _, b := range r.state.Usage.Epoch.Bindings {
		if b.ClientID == q.SystemProbeID && b.Email == systemEmail && !b.Billable {
			found = true
		}
	}
	if !found {
		return xrayconfig.Candidate{}, false
	}
	return r.active.candidate(), true
}

// ProbeMeteredProtocol runs a short-lived, pinned Vision client through this
// exact active Direct listener. It never changes the server lease or journal,
// enables a listener, renews authority or sets DataPlaneVerified/CanPublish.
// No HTTP/Agent endpoint calls this primitive yet.
func (t *TransactionRuntime) ProbeMeteredProtocol(ctx context.Context, q ProtocolHealthRequest, endpoint ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
	empty := ProtocolHealthReceipt{}
	if ctx == nil || ctx.Err() != nil || !t.protocolProbe.TryLock() {
		return empty, ErrProtocolHealth
	}
	defer t.protocolProbe.Unlock()
	if !verifyProtocolHealthRequest(t.options.LeaseKey, q, time.Now().UnixNano()) || q.EndpointSHA != endpoint.SHA() {
		return empty, ErrProtocolHealth
	}
	tlsConfig, err := endpoint.tlsConfig()
	if err != nil {
		return empty, ErrProtocolHealth
	}
	t.mu.Lock()
	candidate, current := t.protocolHealthCurrent(q)
	t.mu.Unlock()
	if !current {
		return empty, ErrProtocolHealth
	}
	probeCtx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Duration(q.Expires-time.Now().UnixNano())))
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return empty, ErrProtocolHealth
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	clientCandidate, err := protocolHealthClient(candidate, q, port, endpoint)
	if err != nil {
		return empty, ErrProtocolHealth
	}
	if _, err = xrayconfig.ValidateOffline(probeCtx, clientCandidate, t.options.Binary, t.options.BinarySHA); err != nil {
		return empty, ErrProtocolHealth
	}
	p, err := startChild(probeCtx, clientCandidate, t.options.Binary, time.Now().Add(time.Until(time.Unix(0, q.Expires))))
	if err != nil {
		return empty, ErrProtocolHealth
	}
	defer p.stop()
	stopCancel := context.AfterFunc(probeCtx, p.stop)
	defer stopCancel()
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	transport := &http.Transport{TLSClientConfig: tlsConfig, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 4096, DialContext: func(c context.Context, _, address string) (net.Conn, error) {
		if address != endpoint.Address || !p.bound(clientCandidate.Listeners) {
			return nil, ErrProtocolHealth
		}
		return protocolHealthSOCKS(c, socks, endpoint.Address)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrProtocolHealth }}
	req, err := http.NewRequestWithContext(probeCtx, "GET", "https://"+endpoint.Address+"/dark-pro/protocol-health/v1/"+q.ID, nil)
	if err != nil {
		return empty, ErrProtocolHealth
	}
	req.Host = endpoint.ServerName
	response, err := client.Do(req)
	if err != nil {
		return empty, ErrProtocolHealth
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 257))
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Encoding") != "" || !bytes.Equal(body, protocolHealthBody(q.ID)) || probeCtx.Err() != nil || !p.bound(clientCandidate.Listeners) {
		return empty, ErrProtocolHealth
	}
	// The probe must not block an emergency drain. Recheck under the ordinary
	// controller locks only after network I/O, then sign while still current.
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, current = t.protocolHealthCurrent(q); !current || probeCtx.Err() != nil || p.dead() || !verifyProtocolHealthRequest(t.options.LeaseKey, q, time.Now().UnixNano()) {
		return empty, ErrProtocolHealth
	}
	proof := ProtocolHealthReceipt{Version: ProtocolHealthVersion, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: time.Now().UnixNano()}
	if proof.Completed >= q.Expires {
		return empty, ErrProtocolHealth
	}
	proof.Signature = transactionMAC(t.options.LeaseKey, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2", proof)
	return proof, nil
}

func protocolHealthClient(c xrayconfig.Candidate, q ProtocolHealthRequest, socksPort uint16, endpoint ProtocolHealthEndpoint) (xrayconfig.Candidate, error) {
	var doc struct {
		Inbounds []struct {
			Listen   string `json:"listen"`
			Port     uint16 `json:"port"`
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					ID    string `json:"id"`
					Email string `json:"email"`
					Flow  string `json:"flow"`
				} `json:"clients"`
			} `json:"settings"`
			Stream struct {
				Network  string `json:"network"`
				Security string `json:"security"`
				Reality  struct {
					PrivateKey  string   `json:"privateKey"`
					ServerNames []string `json:"serverNames"`
					ShortIDs    []string `json:"shortIds"`
				} `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if socksPort < 1024 || socksPort == q.Port || json.Unmarshal(c.Config, &doc) != nil {
		return xrayconfig.Candidate{}, ErrProtocolHealth
	}
	tag := ""
	for _, l := range c.Listeners {
		if l.InboundID == q.InboundID && l.NodePort == q.Port && l.Kind == "direct" {
			if tag != "" {
				return xrayconfig.Candidate{}, ErrProtocolHealth
			}
			tag = l.Tag
		}
	}
	for _, in := range doc.Inbounds {
		if tag == "" || in.Tag != tag || in.Port != q.Port {
			continue
		}
		if in.Listen != "127.0.0.1" || in.Protocol != "vless" || in.Stream.Network != "tcp" || in.Stream.Security != "reality" || len(in.Stream.Reality.ServerNames) == 0 || len(in.Stream.Reality.ShortIDs) == 0 {
			return xrayconfig.Candidate{}, ErrProtocolHealth
		}
		uuid := ""
		systemEmail, err := uc.CounterEmail(q.SystemProbeID)
		if err != nil {
			return xrayconfig.Candidate{}, ErrProtocolHealth
		}
		for _, user := range in.Settings.Clients {
			h := sha256.Sum256([]byte(user.ID))
			if hex.EncodeToString(h[:]) == q.CredentialSHA && user.Email == systemEmail && user.Flow == "xtls-rprx-vision" {
				if uuid != "" {
					return xrayconfig.Candidate{}, ErrProtocolHealth
				}
				uuid = user.ID
			}
		}
		key, err := base64.RawURLEncoding.DecodeString(in.Stream.Reality.PrivateKey)
		if err != nil || uuid == "" {
			return xrayconfig.Candidate{}, ErrProtocolHealth
		}
		private, err := ecdh.X25519().NewPrivateKey(key)
		if err != nil {
			return xrayconfig.Candidate{}, ErrProtocolHealth
		}
		host, destinationPort, err := net.SplitHostPort(endpoint.Address)
		if err != nil || q.EndpointSHA != endpoint.SHA() {
			return xrayconfig.Candidate{}, ErrProtocolHealth
		}
		config := map[string]any{
			"log": map[string]string{"loglevel": "none"},
			"inbounds": []any{map[string]any{
				"tag": "protocol-health", "listen": "127.0.0.1", "port": socksPort, "protocol": "socks",
				"settings": map[string]any{"auth": "noauth", "udp": false},
			}},
			"outbounds": []any{
				// All unmatched traffic fails closed, including a local process
				// discovering the temporary SOCKS listener during its short lease.
				map[string]any{"tag": "deny", "protocol": "blackhole"},
				map[string]any{
					"tag": "verifier", "protocol": "vless",
					"settings": map[string]any{"vnext": []any{map[string]any{
						"address": "127.0.0.1", "port": q.Port,
						"users": []any{map[string]any{"id": uuid, "encryption": "none", "flow": "xtls-rprx-vision"}},
					}}},
					"streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{
						"serverName": in.Stream.Reality.ServerNames[0], "fingerprint": "chrome",
						"publicKey": base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), "shortId": in.Stream.Reality.ShortIDs[0],
					}},
				},
			},
			"routing": map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"protocol-health"}, "ip": []string{host + "/32"},
				"port": destinationPort, "network": "tcp", "outboundTag": "verifier",
			}}},
		}
		raw, err := json.Marshal(config)
		if err != nil {
			return xrayconfig.Candidate{}, ErrProtocolHealth
		}
		h := sha256.Sum256(raw)
		return xrayconfig.Candidate{Config: raw, SHA256: hex.EncodeToString(h[:]), ManifestSHA256: q.EndpointSHA, Listeners: []xrayconfig.Listener{{NodePort: socksPort, Tag: "protocol-health"}}, RequiresXrayTest: true, RequiresDataPlaneProof: true, RequiresSubscriptionCommit: true}, nil
	}
	return xrayconfig.Candidate{}, ErrProtocolHealth
}

func protocolHealthSOCKS(ctx context.Context, socks, address string) (net.Conn, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp4", socks)
	if err != nil {
		return nil, ErrProtocolHealth
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	if deadline, yes := ctx.Deadline(); yes {
		_ = c.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		return nil, ErrProtocolHealth
	}
	answer := make([]byte, 2)
	if _, err = io.ReadFull(c, answer); err != nil || !bytes.Equal(answer, []byte{5, 0}) {
		return nil, ErrProtocolHealth
	}
	host, ps, err := net.SplitHostPort(address)
	port, pe := strconv.Atoi(ps)
	ip := net.ParseIP(host).To4()
	if err != nil || pe != nil || ip == nil || port < 1 || port > 65535 {
		return nil, ErrProtocolHealth
	}
	request := append([]byte{5, 1, 0, 1}, ip...)
	request = append(request, byte(port>>8), byte(port))
	if _, err = c.Write(request); err != nil {
		return nil, ErrProtocolHealth
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(c, header); err != nil || header[0] != 5 || header[1] != 0 || header[2] != 0 {
		return nil, ErrProtocolHealth
	}
	size := 0
	switch header[3] {
	case 1:
		size = 6
	case 4:
		size = 18
	case 3:
		b := make([]byte, 1)
		if _, err = io.ReadFull(c, b); err != nil {
			return nil, ErrProtocolHealth
		}
		size = int(b[0]) + 2
	default:
		return nil, ErrProtocolHealth
	}
	if _, err = io.CopyN(io.Discard, c, int64(size)); err != nil {
		return nil, ErrProtocolHealth
	}
	ok = true
	return c, nil
}
