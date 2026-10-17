package api

import (
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/nodebootstrap"
)

// The artifact must be the current release-matched PRO Agent. Authorization
// happens on every download, not when this option is enabled at startup.
func (s *Server) EnableNodeInstaller(bundle *nodebootstrap.Bundle) error {
	if bundle == nil {
		return errors.New("verified PRO Agent bundle required")
	}
	s.nodeBundle = bundle
	return nil
}

// Scope reports the actual listener bind only. A public socket is not
// proof of TLS trust, reachable route, or correctly restricted firewall.
func (s *Server) SetNodeTransportScope(scope string) {
	switch scope {
	case "disabled", "loopback_only", "external_tls_unverified":
		s.nodeTransportScope = scope
	default:
		s.nodeTransportScope = "disabled"
	}
}
