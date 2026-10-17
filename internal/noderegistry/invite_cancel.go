package noderegistry

// CancelPending revokes only an unclaimed, pending invitation by logical Node ID.
// A paired Node, its installation credential, traffic and reports cannot be
// changed or removed by this operation.
func (r *Registry) CancelPending(nodeID string) error {
	if !idPattern.MatchString(nodeID) {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := clone(r.state)
	found := false
	for digest, invite := range next.Invites {
		if invite.NodeID == nodeID {
			delete(next.Invites, digest)
			found = true
		}
	}
	if !found {
		return ErrInvite
	}
	return r.saveLocked(next)
}
