// Package clientservices defines the narrow representative membership boundary.
// Callers must load Snapshot from authoritative storage in the same transaction
// that records the execution command. Client-supplied snapshots are not trusted.
package clientservices

import (
	"errors"
	"sort"
)

var ErrDenied = errors.New("client service access denied")
var ErrConflict = errors.New("client service revision changed")
var ErrInvalid = errors.New("invalid client service membership")

type Grant struct {
	RepresentativeID string
	InboundID        uint64
	Revision         uint64
	Enabled          bool
}

type Snapshot struct {
	Grant                  Grant
	RepresentativeEnabled  bool
	LoginSuspended         bool
	CanManageClients       bool
	ClientID               string
	ClientRepresentativeID string
	ClientArchived         bool
	InboundID              uint64
	ExecutionRevision      uint64
	Ready                  bool
	Members                []string
}

// Change deliberately accepts no replacement membership list, owner identity,
// execution plan, infrastructure settings, or representative ID.
type Change struct {
	ClientID                  string
	InboundID                 uint64
	ExpectedGrantRevision     uint64
	ExpectedExecutionRevision uint64
	Add                       bool
}

// Membership derives a complete execution membership list from server state,
// changing only the caller's own client. It does not grant execution authority.
// The caller must enforce grant revocation again when admitting the command;
// a preview result alone is never authority for a later write.
func Membership(actorRepresentativeID string, s Snapshot, c Change) ([]string, error) {
	if actorRepresentativeID == "" || !s.RepresentativeEnabled || s.LoginSuspended || !s.CanManageClients ||
		s.ClientArchived || s.ClientRepresentativeID != actorRepresentativeID ||
		!s.Grant.Enabled || s.Grant.RepresentativeID != actorRepresentativeID ||
		s.Grant.InboundID != s.InboundID {
		return nil, ErrDenied
	}
	if c.ClientID == "" || c.InboundID == 0 || c.ClientID != s.ClientID || c.InboundID != s.InboundID ||
		c.ExpectedGrantRevision == 0 || c.ExpectedExecutionRevision == 0 {
		return nil, ErrInvalid
	}
	if c.ExpectedGrantRevision != s.Grant.Revision || c.ExpectedExecutionRevision != s.ExecutionRevision || !s.Ready {
		return nil, ErrConflict
	}
	seen := make(map[string]bool, len(s.Members))
	out := make([]string, 0, len(s.Members)+1)
	for _, id := range s.Members {
		if id == "" || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		if id != c.ClientID || c.Add {
			out = append(out, id)
		}
	}
	if c.Add && !seen[c.ClientID] {
		out = append(out, c.ClientID)
	}
	sort.Strings(out)
	return out, nil
}
