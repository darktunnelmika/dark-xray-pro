package inboundsecurity

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (s *Service) Probe(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	if err := s.scopeAvailable(request.Scope, request.NodeID); err != nil {
		return ProbeResult{}, err
	}
	if _, _, err := parseTarget(request.Target, request.ServerName); err != nil {
		return ProbeResult{}, err
	}
	if request.Scope == "node" && request.NodeID != s.config.LocalNodeID {
		ctx, cancel := context.WithTimeout(ctx, s.config.ProbeTimeout)
		defer cancel()
		if err := s.acquire(ctx); err != nil {
			return ProbeResult{}, err
		}
		defer func() { <-s.probes }()
		result, err := s.config.NodeProbe(ctx, request.NodeID, request)
		if err != nil {
			return ProbeResult{}, failure("selected_node_probe_failed")
		}
		if result.Scope != "node" || result.NodeID != request.NodeID || result.Target != request.Target {
			return ProbeResult{}, failure("selected_node_probe_scope_mismatch")
		}
		return result, nil
	}
	return s.ProbeLocal(ctx, request)
}

// ProbeLocal is used by an authenticated Node transport as well as by the Hub.
// LocalNodeID must match before a local result can claim a Node's network scope.
func (s *Service) ProbeLocal(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	if err := validateScope(request.Scope, request.NodeID); err != nil {
		return ProbeResult{}, err
	}
	if (request.Scope == "node" && (s.config.LocalNodeID == "" || request.NodeID != s.config.LocalNodeID)) ||
		(request.Scope == "hub" && s.config.LocalNodeID != "") {
		return ProbeResult{}, failure("local_probe_scope_mismatch")
	}
	host, port, err := parseTarget(request.Target, request.ServerName)
	if err != nil {
		return ProbeResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.ProbeTimeout)
	defer cancel()
	result := emptyResult(request)
	result.Host, result.Port = host, port
	started := time.Now()
	if err := s.acquire(ctx); err != nil {
		return failedProbe(result, "probe_cancelled", started), nil
	}
	defer func() { <-s.probes }()
	address, err := s.resolvePinned(ctx, host)
	if err != nil {
		return failedProbe(result, errorCode(err), started), nil
	}
	result.IP = address.String()
	serverName := request.ServerName
	if serverName != "" {
		result.ServerNameSource = "request"
	} else if validDNSName(host) {
		serverName, result.ServerNameSource = host, "target"
	} else {
		serverName, result.ServerNameSource = host, "target_ip"
	}
	result.ServerName = serverName
	state, err := s.handshake(ctx, address, port, serverName)
	if err != nil {
		return failedProbe(result, errorCode(err), started), nil
	}
	// An IP candidate may suggest a concrete SAN from its own certificate. A
	// second handshake stays on the same pinned IP and explicitly records SNI.
	if request.ServerName == "" && net.ParseIP(host) != nil && len(state.PeerCertificates) != 0 {
		for _, candidate := range state.PeerCertificates[0].DNSNames {
			if validDNSName(candidate) {
				candidate = strings.ToLower(candidate)
				second, secondErr := s.handshake(ctx, address, port, candidate)
				if secondErr == nil {
					state, serverName = second, candidate
					result.ServerName, result.ServerNameSource = candidate, "certificate_san"
				}
				break
			}
		}
	}
	result.OK = true
	result.TLS13 = state.Version == tls.VersionTLS13
	result.TLSVersion = tls.VersionName(state.Version)
	result.ALPN, result.H2 = state.NegotiatedProtocol, state.NegotiatedProtocol == "h2"
	// The only offered curve is X25519 and resumption is disabled. A TLS 1.3
	// handshake therefore proves X25519 without relying on newer Go CurveID APIs.
	result.X25519 = result.TLS13
	if result.X25519 {
		result.CurveID = "X25519 (forced)"
	}
	result.Certificate = s.inspectChain(state.PeerCertificates, serverName)
	for _, certificate := range state.PeerCertificates {
		result.CertChainBytes += len(certificate.Raw)
	}
	result.Suitable = result.TLS13 && result.H2 && result.X25519 && result.Certificate.Valid && validDNSName(serverName)
	switch {
	case !result.TLS13:
		result.Reason = "tls13_required"
	case !result.H2:
		result.Reason = "h2_not_negotiated"
	case !result.Certificate.ChainValid:
		result.Reason = "certificate_chain_untrusted"
	case !result.Certificate.TimeValid:
		result.Reason = "certificate_time_invalid"
	case !result.Certificate.HostnameValid:
		result.Reason = "certificate_name_mismatch"
	case !validDNSName(serverName):
		result.Reason = "dns_server_name_required"
	default:
		result.Reason = "tls_target_suitable"
	}
	result.LatencyMS = time.Since(started).Milliseconds()
	return result, nil
}

func (s *Service) handshake(ctx context.Context, address netip.Addr, port int, serverName string) (tls.ConnectionState, error) {
	connection, err := s.config.DialContext(ctx, "tcp", net.JoinHostPort(address.String(), strconv.Itoa(port)))
	if err != nil {
		if ctx.Err() != nil {
			return tls.ConnectionState{}, failure("probe_cancelled")
		}
		return tls.ConnectionState{}, failure("tcp_connect_failed")
	}
	defer connection.Close()
	if !remoteMatches(connection, address, port) {
		return tls.ConnectionState{}, failure("pinned_address_mismatch")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if connection.SetDeadline(deadline) != nil {
			return tls.ConnectionState{}, failure("connection_deadline_failed")
		}
	}
	client := tls.Client(connection, &tls.Config{
		// Collect the peer chain first, then verify its trust, time and hostname
		// independently in inspectChain. A handshake alone never means suitable.
		InsecureSkipVerify: true,
		ServerName:         serverName, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"}, CurvePreferences: []tls.CurveID{tls.X25519},
	})
	if err := client.HandshakeContext(ctx); err != nil {
		if ctx.Err() != nil {
			return tls.ConnectionState{}, failure("probe_cancelled")
		}
		return tls.ConnectionState{}, failure("tls_handshake_failed")
	}
	state := client.ConnectionState()
	if len(state.PeerCertificates) == 0 || len(state.PeerCertificates) > MaxCertificates {
		return tls.ConnectionState{}, failure("certificate_chain_size_invalid")
	}
	chainBytes := 0
	for _, certificate := range state.PeerCertificates {
		chainBytes += len(certificate.Raw)
	}
	if chainBytes > MaxPEMBytes {
		return tls.ConnectionState{}, failure("certificate_chain_size_invalid")
	}
	return state, nil
}

func emptyResult(request ProbeRequest) ProbeResult {
	return ProbeResult{Scope: request.Scope, NodeID: request.NodeID, Target: request.Target, ServerName: request.ServerName,
		HybridKeyExchange: "not_measured", Certificate: CertificateInspection{Certificates: []CertificateMetadata{}, Errors: []string{}}}
}

func errorCode(err error) string {
	var public *Error
	if errors.As(err, &public) {
		return public.Code
	}
	return "probe_failed"
}

func failedProbe(result ProbeResult, code string, started time.Time) ProbeResult {
	result.Error, result.Reason, result.LatencyMS = code, code, time.Since(started).Milliseconds()
	return result
}

func (s *Service) Find(ctx context.Context, request FindRequest) (FindResult, error) {
	if err := s.scopeAvailable(request.Scope, request.NodeID); err != nil {
		return FindResult{}, err
	}
	targets, source, err := s.findTargets(request)
	if err != nil {
		return FindResult{}, err
	}
	select {
	case s.finds <- struct{}{}:
		defer func() { <-s.finds }()
	default:
		return FindResult{}, failure("target_finder_busy")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.FindTimeout)
	defer cancel()
	result := FindResult{Scope: request.Scope, NodeID: request.NodeID, Source: source, Total: len(targets), Results: make([]ProbeResult, len(targets))}
	for index, target := range targets {
		result.Results[index] = emptyResult(ProbeRequest{Scope: request.Scope, NodeID: request.NodeID, Target: target.Target, ServerName: target.ServerName})
		result.Results[index].Error, result.Results[index].Reason = "not_started", "not_started"
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(s.config.MaxConcurrency, len(targets))
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				target := targets[index]
				request := ProbeRequest{Scope: request.Scope, NodeID: request.NodeID, Target: target.Target, ServerName: target.ServerName}
				value, err := s.Probe(ctx, request)
				if err != nil {
					value = emptyResult(request)
					value.Error, value.Reason = errorCode(err), errorCode(err)
				}
				result.Results[index] = value
			}
		}()
	}
queue:
	for index := range targets {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break queue
		}
	}
	close(jobs)
	workers.Wait()
	result.Cancelled = ctx.Err() != nil
	for index := range result.Results {
		value := &result.Results[index]
		if value.Error == "not_started" {
			if result.Cancelled {
				value.Error, value.Reason = "probe_cancelled", "probe_cancelled"
			}
		} else {
			result.Completed++
		}
		if value.Suitable {
			result.Suitable++
		}
	}
	return result, nil
}
