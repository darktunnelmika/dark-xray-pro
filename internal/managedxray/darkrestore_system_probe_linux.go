//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type DarkRestoreSystemProbeState struct {
	Expected     RuntimeCondition `json:"expected"`
	Usage        MeteredCondition `json:"usage"`
	LeaseExpires int64            `json:"lease_expires_unix_nano"`
}

type darkRestoreSystemProbeAttempt struct {
	Request ProtocolHealthRequest  `json:"request"`
	Receipt *ProtocolHealthReceipt `json:"receipt,omitempty"`
}

func (r *Runtime) DarkRestoreSystemProbeState() DarkRestoreSystemProbeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.customerConnectionStateLocked()
	return DarkRestoreSystemProbeState{Expected: state.Expected, Usage: state.Usage, LeaseExpires: state.LeaseExpires}
}

func (r *Runtime) darkRestoreSystemProbeCurrent(q ProtocolHealthRequest) bool {
	if r.closed || r.fenced || r.active == nil || r.observer == nil || !r.observer.healthy() ||
		r.state.Usage == nil || r.state.Phase != "ready_observed_admission" ||
		r.options.NodeID != q.NodeID || r.options.InstallationID != q.InstallationID ||
		r.options.ListenIP != "127.0.0.1" {
		return false
	}
	state := r.customerConnectionStateLocked()
	if Condition(r.snapshot()) != q.Expected || state.Usage != q.Usage || q.Expires > state.LeaseExpires {
		return false
	}
	email, err := uc.CounterEmail(q.SystemProbeID)
	if err != nil {
		return false
	}
	found := false
	for _, binding := range r.state.Usage.Epoch.Bindings {
		if binding.ClientID == q.SystemProbeID && binding.Email == email && !binding.Billable && binding.Role == uc.SystemProbeRole {
			if found {
				return false
			}
			found = true
		}
	}
	return found && uc.VerifyOwnedStatsEndpoint(r.state.Usage.Epoch, MeteredStatsEndpoint) == nil
}

func validDarkRestoreSystemProbeJournal(j journal) bool {
	attempt := j.SystemProbe
	if attempt == nil {
		return true
	}
	if j.Usage == nil || j.Good == nil || !validProtocolHealthRequest(attempt.Request) ||
		attempt.Request.Version != InitialApplyProtocolHealthVersion ||
		attempt.Request.Expected.ActiveSHA != j.Good.SHA ||
		attempt.Request.Usage.EpochID != j.Usage.Epoch.ID {
		return false
	}
	email, err := uc.CounterEmail(attempt.Request.SystemProbeID)
	if err != nil {
		return false
	}
	found := false
	for _, binding := range j.Usage.Epoch.Bindings {
		if binding.ClientID == attempt.Request.SystemProbeID && binding.Email == email && !binding.Billable && binding.Role == uc.SystemProbeRole {
			if found {
				return false
			}
			found = true
		}
	}
	if !found {
		return false
	}
	return attempt.Receipt == nil ||
		attempt.Receipt.Version == attempt.Request.Version &&
			attempt.Receipt.RequestSHA == hashJSON(attempt.Request) &&
			attempt.Receipt.ResponseSHA == protocolHealthResponseSHA(attempt.Request.ID) &&
			attempt.Receipt.Completed >= attempt.Request.Issued &&
			attempt.Receipt.Completed < attempt.Request.Expires
}

// CheckDarkRestoreSystemProbe persists one exact challenge before any network
// I/O. A pending attempt is never rerun after restart. A completed receipt is
// persisted before it can be acknowledged by the HUB.
func (r *Runtime) CheckDarkRestoreSystemProbe(ctx context.Context, q ProtocolHealthRequest, endpoint ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
	var zero ProtocolHealthReceipt
	if ctx == nil || ctx.Err() != nil {
		return zero, ErrProtocolHealth
	}
	r.mu.Lock()
	if attempt := r.state.SystemProbe; attempt != nil {
		if hashJSON(attempt.Request) != hashJSON(q) || attempt.Receipt == nil {
			r.mu.Unlock()
			return zero, ErrProtocolHealth
		}
		out := *attempt.Receipt
		r.mu.Unlock()
		return out, nil
	}
	if !verifyProtocolHealthRequest(r.options.LeaseKey, q, time.Now().UnixNano()) ||
		q.EndpointSHA != endpoint.SHA() || !r.darkRestoreSystemProbeCurrent(q) {
		r.mu.Unlock()
		return zero, ErrProtocolHealth
	}
	tlsConfig, err := endpoint.tlsConfig()
	if err != nil {
		r.mu.Unlock()
		return zero, err
	}
	candidate := r.active.candidate()
	next := r.state
	next.SystemProbe = &darkRestoreSystemProbeAttempt{Request: q}
	if err = r.persist(next); err != nil {
		r.mu.Unlock()
		return zero, err
	}
	r.mu.Unlock()

	probeCtx, cancel := context.WithDeadline(ctx, time.Unix(0, q.Expires))
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return zero, ErrProtocolHealth
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	clientCandidate, err := protocolHealthClient(candidate, q, port, endpoint)
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
			if address != endpoint.Address || !child.bound(clientCandidate.Listeners) {
				return nil, ErrProtocolHealth
			}
			return protocolHealthSOCKS(c, socks, endpoint.Address)
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrProtocolHealth }}
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "https://"+endpoint.Address+"/dark-pro/protocol-health/v1/"+q.ID, nil)
	if err != nil {
		return zero, err
	}
	request.Host = endpoint.ServerName
	response, err := client.Do(request)
	if err != nil {
		return zero, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 257))
	if err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" ||
		!bytes.Equal(body, protocolHealthBody(q.ID)) || probeCtx.Err() != nil {
		return zero, ErrProtocolHealth
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.darkRestoreSystemProbeCurrent(q) || child.dead() ||
		!verifyProtocolHealthRequest(r.options.LeaseKey, q, time.Now().UnixNano()) {
		return zero, ErrProtocolHealth
	}
	receipt := ProtocolHealthReceipt{Version: q.Version, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: time.Now().UnixNano()}
	if receipt.Completed >= q.Expires {
		return zero, ErrProtocolHealth
	}
	receipt.Signature = transactionMAC(r.options.LeaseKey, protocolHealthMACDomain("RECEIPT", q.Version), receipt)
	if !VerifyProtocolHealthReceipt(r.options.LeaseKey, q, receipt, time.Now()) {
		return zero, ErrProtocolHealth
	}
	next = r.state
	next.SystemProbe = &darkRestoreSystemProbeAttempt{Request: q, Receipt: &receipt}
	if err = r.persist(next); err != nil {
		return zero, err
	}
	return receipt, nil
}

func (r *Runtime) SavedDarkRestoreSystemProbeReceipt() *ProtocolHealthReceipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.SystemProbe == nil || r.state.SystemProbe.Receipt == nil {
		return nil
	}
	out := *r.state.SystemProbe.Receipt
	return &out
}
