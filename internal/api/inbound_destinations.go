package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

const localHubNodeID = "hub-local"

type inboundDestination struct {
	noderegistry.NodeInfo
	Kind string `json:"kind"`
}

// The HUB identity is derived from this installation's persisted encryption
// key. It cannot accidentally alias a paired remote Node or change on restart.
func (s *Server) localHubInstallation() string {
	if len(s.inboundKey) != 32 {
		return ""
	}
	hash := hmac.New(sha256.New, s.inboundKey)
	_, _ = hash.Write([]byte("DARK-PRO-EXPLICIT-INBOUND-HUB-v1"))
	return hex.EncodeToString(hash.Sum(nil)[:16])
}

func (s *Server) inboundDestinations() []inboundDestination {
	rows := make([]inboundDestination, 0)
	if installation := s.localHubInstallation(); installation != "" {
		rows = append(rows, inboundDestination{NodeInfo: noderegistry.NodeInfo{NodeID: localHubNodeID, InstallationID: installation, Name: "HUB — this server"}, Kind: "hub"})
	}
	for _, node := range s.registry.List() {
		if node.NodeID != localHubNodeID {
			rows = append(rows, inboundDestination{NodeInfo: node, Kind: "node"})
		}
	}
	return rows
}
