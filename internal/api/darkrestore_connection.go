package api

import (
	"errors"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// Startup-only configuration. The browser cannot choose a verifier destination.
func (s *Server) EnableDarkRestoreCustomerConnectionChecks(e mx.ProtocolHealthEndpoint) error {
	if s.business == nil || len(s.inboundKey) != 32 || s.customerConnectionEndpoint != nil || !e.Valid() {
		return errors.New("invalid imported customer connection verifier")
	}
	e.CABundle = append([]byte(nil), e.CABundle...)
	s.customerConnectionEndpoint = &e
	return nil
}
