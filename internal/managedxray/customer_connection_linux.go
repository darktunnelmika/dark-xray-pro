//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// This is an explicitly consented customer-credential test. Its actual bytes
// remain billable in the existing customer epoch. It is never a system probe,
// publication permission, general serving-health or a new traffic grant.
type CustomerConnectionState struct {
	Expected     RuntimeCondition
	Usage        MeteredCondition
	LeaseExpires int64
}
type CustomerConnectionChallenge struct {
	Version                int
	ID                     string
	PlanSHA                string
	ClientID               string
	EpochID                string
	ConfigSHA              string
	CoreSHA                string
	CredentialSHA          string
	ListenerSHA            string
	EndpointSHA            string
	NodeID, InstallationID string
	Process                uc.ProcessToken
	InboundID              uint64
	Port                   uint16
	State                  CustomerConnectionState
	Signer                 []byte
	Issued, Expires        int64
	Consent                bool
	Signature              string
}
type CustomerConnectionReceipt struct {
	Challenge   CustomerConnectionChallenge
	Completed   int64
	ResponseSHA string
	Signature   []byte
}
type customerConnectionAttempt struct {
	Challenge CustomerConnectionChallenge
	Receipt   *CustomerConnectionReceipt
}

func connectionMAC(key []byte, q CustomerConnectionChallenge) string {
	q.Signature = ""
	raw, _ := json.Marshal(q)
	m := hmac.New(sha256.New, key)
	m.Write([]byte("DARK-PRO-CUSTOMER-CONNECTION-REQUEST-v1\x00"))
	m.Write(raw)
	return hex.EncodeToString(m.Sum(nil))
}
func validConnectionChallenge(q CustomerConnectionChallenge) bool {
	return q.Version == 1 && transactionID.MatchString(q.ID) && transactionNode.MatchString(q.NodeID) && transactionID.MatchString(q.InstallationID) &&
		transactionHash.MatchString(q.PlanSHA) && transactionHash.MatchString(q.ConfigSHA) && transactionHash.MatchString(q.CoreSHA) &&
		transactionHash.MatchString(q.CredentialSHA) && transactionHash.MatchString(q.ListenerSHA) && transactionHash.MatchString(q.EndpointSHA) &&
		transactionID.MatchString(q.EpochID) && transactionNode.MatchString(q.ClientID) && q.Consent && q.Port >= 1024 && q.InboundID > 0 &&
		len(q.Signer) == ed25519.PublicKeySize && q.Process.PID > 0 && q.Process.StartTicks > 0 && q.Process.BootID != "" &&
		q.State.Expected.ProcessReady && q.State.Expected.ActiveSHA == q.ConfigSHA && q.State.Expected.ActiveSequence > 0 && q.State.Expected.SavedSHA == q.ConfigSHA && q.State.Expected.SavedSequence == q.State.Expected.ActiveSequence && q.State.Expected.Highwater == q.State.Expected.ActiveSequence &&
		q.State.Usage.EpochID == q.EpochID && validMeteredCondition(q.State.Usage) && q.State.Usage.Settlement == (uc.Receipt{}) &&
		q.Issued > 0 && q.Expires > q.Issued && q.Expires-q.Issued <= int64(8*time.Second) && q.Expires <= q.State.LeaseExpires
}
func (q CustomerConnectionChallenge) Valid() bool {
	return validConnectionChallenge(q) && transactionHash.MatchString(q.Signature)
}

func SignCustomerConnectionChallenge(key []byte, q CustomerConnectionChallenge) (CustomerConnectionChallenge, error) {
	if len(key) < 32 || !validConnectionChallenge(q) {
		return CustomerConnectionChallenge{}, ErrProtocolHealth
	}
	q.Signer = append([]byte(nil), q.Signer...)
	q.Signature = connectionMAC(key, q)
	return q, nil
}
func VerifyCustomerConnectionChallenge(key []byte, q CustomerConnectionChallenge, now int64) bool {
	return len(key) >= 32 && validConnectionChallenge(q) && q.Issued <= now && now < q.Expires &&
		hmac.Equal([]byte(q.Signature), []byte(connectionMAC(key, q)))
}
func (r CustomerConnectionReceipt) signingBytes() []byte {
	r.Signature = nil
	raw, _ := json.Marshal(r)
	return append([]byte("DARK-PRO-CUSTOMER-CONNECTION-RECEIPT-v1\x00"), raw...)
}
func (r CustomerConnectionReceipt) Verify(q CustomerConnectionChallenge) bool {
	return q.Valid() && hashJSON(r.Challenge) == hashJSON(q) && r.Completed >= q.Issued && r.Completed < q.Expires &&
		r.ResponseSHA == protocolHealthResponseSHA(q.ID) && ed25519.Verify(ed25519.PublicKey(q.Signer), r.signingBytes(), r.Signature)
}
func (r *Runtime) customerConnectionStateLocked() CustomerConnectionState {
	s := r.snapshot()
	u := MeteredCondition{}
	if r.state.Usage != nil {
		u.EpochID = r.state.Usage.Epoch.ID
		u.EpochSHA = hashJSON(r.state.Usage.Epoch)
	}
	if r.state.Admission != nil {
		u.AdmissionSHA = hashJSON(r.state.Admission)
		if r.state.Admission.Settlement != nil {
			u.Settlement = *r.state.Admission.Settlement
		}
	}
	return CustomerConnectionState{Expected: Condition(s), Usage: u, LeaseExpires: s.LeaseExpires}
}
func (r *Runtime) CustomerConnectionState() CustomerConnectionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.customerConnectionStateLocked()
}
func (r *Runtime) customerConnectionCurrent(q CustomerConnectionChallenge) bool {
	if r.closed || r.fenced || r.active == nil || r.observer == nil || !r.observer.healthy() || r.state.Usage == nil ||
		r.state.Phase != "ready_observed_admission" || r.options.BinarySHA != q.CoreSHA || r.options.NodeID != q.NodeID || r.options.InstallationID != q.InstallationID || r.options.ListenIP != "127.0.0.1" {
		return false
	}
	state := r.customerConnectionStateLocked()
	epoch := r.state.Usage.Epoch
	if Condition(r.snapshot()) != q.State.Expected || state.Usage != q.State.Usage || q.Expires > state.LeaseExpires ||
		epoch.KernelBootID != q.Process.BootID || epoch.PID != q.Process.PID || epoch.ProcessStartTicks != q.Process.StartTicks {
		return false
	}
	email, e := uc.CounterEmail(q.ClientID)
	if e != nil {
		return false
	}
	found := false
	for _, b := range epoch.Bindings {
		if b.ClientID == q.ClientID && b.Email == email && b.Billable {
			found = true
		}
	}
	return found && uc.VerifyOwnedStatsEndpoint(epoch, MeteredStatsEndpoint) == nil
}
func validCustomerConnectionJournal(j journal) bool {
	a := j.CustomerConnection
	if a == nil {
		return true
	}
	if j.Usage == nil || j.Good == nil || !validConnectionChallenge(a.Challenge) || a.Challenge.EpochID != j.Usage.Epoch.ID || a.Challenge.ConfigSHA != j.Good.SHA {
		return false
	}
	return a.Receipt == nil || a.Receipt.Verify(a.Challenge)
}

// An attempted nonce is saved before I/O. A crash leaves it unknown; reopening
// or a lost HUB acknowledgment can recover a saved receipt but never reruns it.
func (r *Runtime) CheckCustomerConnection(ctx context.Context, q CustomerConnectionChallenge, e ProtocolHealthEndpoint, signer ed25519.PrivateKey) (CustomerConnectionReceipt, error) {
	var zero CustomerConnectionReceipt
	q.Signer = append([]byte(nil), q.Signer...)
	if ctx == nil || ctx.Err() != nil || len(signer) != ed25519.PrivateKeySize || !bytes.Equal(signer.Public().(ed25519.PublicKey), q.Signer) {
		return zero, ErrProtocolHealth
	}
	r.mu.Lock()
	if a := r.state.CustomerConnection; a != nil {
		if hashJSON(a.Challenge) != hashJSON(q) || a.Receipt == nil {
			r.mu.Unlock()
			return zero, ErrProtocolHealth
		}
		out := cloneCustomerConnectionReceipt(*a.Receipt)
		r.mu.Unlock()
		return out, nil
	}
	if !VerifyCustomerConnectionChallenge(r.options.LeaseKey, q, time.Now().UnixNano()) || q.EndpointSHA != e.SHA() || !r.customerConnectionCurrent(q) {
		r.mu.Unlock()
		return zero, ErrProtocolHealth
	}
	tlsConfig, err := e.tlsConfig()
	if err != nil {
		r.mu.Unlock()
		return zero, err
	}
	c := r.active.candidate()
	next := r.state
	next.CustomerConnection = &customerConnectionAttempt{Challenge: q}
	if err = r.persist(next); err != nil {
		r.mu.Unlock()
		return zero, err
	}
	r.mu.Unlock()
	probeCtx, cancel := context.WithDeadline(ctx, time.Unix(0, q.Expires))
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return zero, err
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	// Reuse the audited TCP/REALITY client compiler with the exact customer's
	// credential and counter email. No nonbillable identity is invented.
	wire := ProtocolHealthRequest{Version: InitialApplyProtocolHealthVersion, InboundID: q.InboundID, Port: q.Port, SystemProbeID: q.ClientID, CredentialSHA: q.CredentialSHA, EndpointSHA: q.EndpointSHA}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return zero, err
	}
	password := hex.EncodeToString(secret)
	clientCandidate, err := customerConnectionClient(c, wire, port, e, password)
	if err != nil {
		return zero, err
	}
	if _, err = xrayconfig.ValidateOffline(probeCtx, clientCandidate, r.options.Binary, r.options.BinarySHA); err != nil {
		return zero, err
	}
	child, err := startChild(probeCtx, clientCandidate, r.options.Binary, time.Unix(0, q.Expires))
	if err != nil {
		return zero, err
	}
	defer child.stop()
	stop := context.AfterFunc(probeCtx, child.stop)
	defer stop()
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	transport := &http.Transport{TLSClientConfig: tlsConfig, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 4096,
		DialContext: func(c context.Context, _, address string) (net.Conn, error) {
			if address != e.Address || !child.bound(clientCandidate.Listeners) {
				return nil, ErrProtocolHealth
			}
			return dialProtocolHealthSOCKS(c, socks, e.Address, "customer-connection", password)
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrProtocolHealth }}
	req, err := http.NewRequestWithContext(probeCtx, "GET", "https://"+e.Address+"/dark-pro/protocol-health/v1/"+q.ID, nil)
	if err != nil {
		return zero, err
	}
	req.Host = e.ServerName
	response, err := client.Do(req)
	if err != nil {
		return zero, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 257))
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Encoding") != "" || !bytes.Equal(body, protocolHealthBody(q.ID)) || probeCtx.Err() != nil {
		return zero, ErrProtocolHealth
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.customerConnectionCurrent(q) || child.dead() || !VerifyCustomerConnectionChallenge(r.options.LeaseKey, q, time.Now().UnixNano()) {
		return zero, ErrProtocolHealth
	}
	receipt := CustomerConnectionReceipt{Challenge: q, Completed: time.Now().UnixNano(), ResponseSHA: protocolHealthResponseSHA(q.ID)}
	receipt.Signature = ed25519.Sign(signer, receipt.signingBytes())
	if !receipt.Verify(q) {
		return zero, ErrProtocolHealth
	}
	next = r.state
	next.CustomerConnection = &customerConnectionAttempt{Challenge: q, Receipt: &receipt}
	if err = r.persist(next); err != nil {
		return zero, err
	}
	return cloneCustomerConnectionReceipt(receipt), nil
}

// SavedCustomerConnectionReceipt recovers only a completed, persisted receipt.
// It never reruns a pending attempt, including after an Agent restart.
func (r *Runtime) SavedCustomerConnectionReceipt() *CustomerConnectionReceipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.CustomerConnection == nil || r.state.CustomerConnection.Receipt == nil {
		return nil
	}
	out := cloneCustomerConnectionReceipt(*r.state.CustomerConnection.Receipt)
	return &out
}
func cloneCustomerConnectionReceipt(r CustomerConnectionReceipt) CustomerConnectionReceipt {
	r.Signature = append([]byte(nil), r.Signature...)
	r.Challenge.Signer = append([]byte(nil), r.Challenge.Signer...)
	return r
}

// The paid customer's temporary loopback proxy is private to this attempt.
// Generic nonbillable health keeps its existing compiler and wire behavior.
func customerConnectionClient(c xrayconfig.Candidate, q ProtocolHealthRequest, port uint16, e ProtocolHealthEndpoint, password string) (xrayconfig.Candidate, error) {
	if !transactionHash.MatchString(password) {
		return xrayconfig.Candidate{}, ErrProtocolHealth
	}
	out, err := protocolHealthClient(c, q, port, e)
	if err != nil {
		return xrayconfig.Candidate{}, err
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(out.Config, &doc) != nil {
		return xrayconfig.Candidate{}, ErrProtocolHealth
	}
	var inbounds []map[string]json.RawMessage
	if json.Unmarshal(doc["inbounds"], &inbounds) != nil || len(inbounds) != 1 {
		return xrayconfig.Candidate{}, ErrProtocolHealth
	}
	settings := map[string]any{"auth": "password", "udp": false, "accounts": []map[string]string{{"user": "customer-connection", "pass": password}}}
	inbounds[0]["settings"], err = json.Marshal(settings)
	if err != nil {
		return xrayconfig.Candidate{}, err
	}
	doc["inbounds"], err = json.Marshal(inbounds)
	if err != nil {
		return xrayconfig.Candidate{}, err
	}
	out.Config, err = json.Marshal(doc)
	if err != nil {
		return xrayconfig.Candidate{}, err
	}
	digest := sha256.Sum256(out.Config)
	out.SHA256 = hex.EncodeToString(digest[:])
	return out, nil
}
