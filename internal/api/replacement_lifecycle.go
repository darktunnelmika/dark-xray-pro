package api

import "github.com/darktunnelmika/dark-xray-pro/internal/replacement"

// Called while replacementLifecycle is held. Historical stages and LAB
// revisions remain readable but never authorize a fresh Agent delivery,
// receipt or revision selection after the source operation is cancelled.
func (s *Server) currentReplacement(operation, nodeID, installation, manifest string) bool {
	if s.replacements == nil {
		return false
	}
	op, err := s.replacements.Get(operation)
	if err != nil || op.Phase != replacement.TCPChecked || op.CanActivate ||
		op.NewNodeID != nodeID || op.InstallationID != installation ||
		op.Preview.ManifestSHA256 != manifest {
		return false
	}
	current, paired := pairedNodes(s.registry, op.OldNodeID, op.NewNodeID)
	return paired && current == installation
}
