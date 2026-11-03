package api

import (
	"errors"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// Startup-only configuration. The browser cannot choose a verifier destination.
func (s *Server) EnableDarkRestoreSystemProbeChecks(e mx.ProtocolHealthEndpoint) error {
	if s.business == nil || len(s.inboundKey) != 32 || s.darkRestoreProbeEndpoint != nil || !e.Valid() {
		return errors.New("invalid imported system-probe verifier")
	}
	e.CABundle = append([]byte(nil), e.CABundle...)
	s.darkRestoreProbeEndpoint = &e
	return nil
}

func (s *Server) EnableDarkRestoreCustomerConnectionChecks(e mx.ProtocolHealthEndpoint) error {
	if s.business == nil || len(s.inboundKey) != 32 || s.customerConnectionEndpoint != nil || !e.Valid() {
		return errors.New("invalid imported customer connection verifier")
	}
	e.CABundle = append([]byte(nil), e.CABundle...)
	s.customerConnectionEndpoint = &e
	return nil
}
