//go:build linux

package inboundruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"golang.org/x/sys/unix"
)

var (
	ErrPublicAliasUnavailable = errors.New("inbound_public_alias_unavailable")
	ErrPublicAliasIdentity    = errors.New("inbound_public_alias_identity_mismatch")
	ErrPublicAliasConflict    = errors.New("inbound_public_alias_address_conflict")
	ErrPublicAliasCleanup     = errors.New("inbound_public_alias_cleanup_pending")
)

type publicAliasReference struct {
	ComponentTag     string `json:"component_tag"`
	PrincipalID      string `json:"principal_id"`
	Email            string `json:"email"`
	PeerPublicKeySHA string `json:"peer_public_key_sha"`
}

type publicAliasEntry struct {
	Host         publicAliasHost        `json:"host"`
	Address      string                 `json:"address"`
	Owner        string                 `json:"owner"`
	References   []publicAliasReference `json:"references"`
	Phase        string                 `json:"phase"`
	CreatedTicks *uint32                `json:"created_ticks,omitempty"`
}

type publicAliasJournal struct {
	Version                 int                `json:"version"`
	IntentID                string             `json:"intent_id"`
	NodeID                  string             `json:"node_id"`
	InstallationID          string             `json:"installation_id"`
	Generation              uint64             `json:"generation"`
	ConfigSHA               string             `json:"config_sha"`
	ManifestSHA             string             `json:"manifest_sha"`
	InitialLeaseExpiresNano int64              `json:"initial_lease_expires_unix_nano"`
	Entries                 []publicAliasEntry `json:"entries"`
}

func validAliasInterfaceName(name string) bool {
	if len(name) == 0 || len(name) > 15 || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}

// Desired addresses come only from the immutable rendered peer assignment.
// A sibling may share an alias only for the same business client (or the exact
// same aggregate peer key); matching an IP alone is not ownership authority.
func desiredPublicAliases(bundle ix.Bundle) ([]publicAliasEntry, error) {
	entries := map[string]*publicAliasEntry{}
	for _, component := range bundle.Components {
		if component.Engine != "amneziawg" {
			continue
		}
		var config struct {
			Server struct {
				IPv6Enabled           bool   `json:"ipv6Enabled"`
				IPv6Subnet            string `json:"ipv6Subnet"`
				IPv6ExternalInterface string `json:"ipv6ExternalInterface"`
				ExternalInterface     string `json:"externalInterface"`
			} `json:"server"`
			Peers []struct {
				PublicKey  string   `json:"publicKey"`
				AllowedIPs []string `json:"allowedIPs"`
				Email      string   `json:"email"`
			} `json:"peers"`
		}
		if json.Unmarshal(component.Config, &config) != nil || component.SHA256 != ix.BytesSHA(component.Config) {
			return nil, ErrProof
		}
		name := config.Server.IPv6ExternalInterface
		if name == "" {
			name = config.Server.ExternalInterface
		}
		if name == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(config.Server.IPv6Subnet)
		if !config.Server.IPv6Enabled || !validAliasInterfaceName(name) || err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Bits() > 126 || len(config.Peers) == 0 || len(config.Peers) > 4096 {
			return nil, ErrPublicAliasUnavailable
		}
		prefix = prefix.Masked()
		principals := componentPrincipals(bundle.Principals, component.Engine, component.Tag)
		users := map[string]ix.Principal{}
		var aggregate *ix.Principal
		for _, p := range principals {
			if p.ComponentTag != component.Tag || p.ID == "" {
				return nil, ErrProof
			}
			parts := strings.Split(p.Counter, ">>>")
			if len(parts) != 3 || parts[2] != "traffic" {
				return nil, ErrProof
			}
			if len(principals) == 1 && p.Role == "owner_aggregate" && parts[0] == "inbound" && parts[1] == component.Tag && p.ClientID == "" && !p.Billable {
				copy := p
				aggregate = &copy
				continue
			}
			if parts[0] != "user" || p.ClientID == "" || p.Role != "customer" || !p.Billable || users[parts[1]].ID != "" {
				return nil, ErrProof
			}
			users[parts[1]] = p
		}
		if aggregate == nil && len(users) != len(config.Peers) {
			return nil, ErrProof
		}
		seenEmails, seenAddresses := map[string]bool{}, map[string]bool{}
		for _, peer := range config.Peers {
			key, err := base64.StdEncoding.DecodeString(peer.PublicKey)
			if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != peer.PublicKey || peer.Email == "" || seenEmails[peer.Email] {
				return nil, ErrProof
			}
			seenEmails[peer.Email] = true
			keySHA := ix.BytesSHA([]byte(peer.PublicKey))
			p, ok := users[peer.Email]
			owner := "client:" + p.ClientID
			if aggregate != nil {
				p, ok, owner = *aggregate, true, "peer:"+keySHA
			}
			if !ok || aggregate == nil && p.CredentialSHA != keySHA {
				return nil, ErrProof
			}
			var address netip.Addr
			for _, raw := range peer.AllowedIPs {
				assigned, err := netip.ParsePrefix(raw)
				if err != nil {
					return nil, ErrProof
				}
				if assigned.Addr().Is6() {
					if address.IsValid() || assigned.Bits() != 128 || assigned.Addr().Is4In6() || !assigned.Addr().IsGlobalUnicast() || !prefix.Contains(assigned.Addr()) || assigned.Addr() == prefix.Addr() || assigned.Addr() == prefix.Addr().Next() {
						return nil, ErrProof
					}
					address = assigned.Addr()
				}
			}
			if !address.IsValid() || seenAddresses[address.String()] {
				return nil, ErrProof
			}
			seenAddresses[address.String()] = true
			id := name + "\x00" + address.String()
			entry := entries[id]
			if entry == nil {
				entry = &publicAliasEntry{Host: publicAliasHost{Interface: ix.PublicAliasInterface{Name: name}}, Address: address.String(), Owner: owner, Phase: "planned", References: []publicAliasReference{}}
				entries[id] = entry
			} else if entry.Owner != owner {
				return nil, ErrPublicAliasConflict
			}
			entry.References = append(entry.References, publicAliasReference{ComponentTag: component.Tag, PrincipalID: p.ID, Email: peer.Email, PeerPublicKeySHA: keySHA})
		}
	}
	if len(entries) > 4096 {
		return nil, ErrPublicAliasUnavailable
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]publicAliasEntry, 0, len(keys))
	for _, key := range keys {
		entry := entries[key]
		sort.Slice(entry.References, func(i, j int) bool {
			if entry.References[i].ComponentTag != entry.References[j].ComponentTag {
				return entry.References[i].ComponentTag < entry.References[j].ComponentTag
			}
			return entry.References[i].Email < entry.References[j].Email
		})
		out = append(out, *entry)
	}
	return out, nil
}

func validatePublicAliasJournal(s journal) error {
	a := s.PublicAliases
	if a == nil {
		return nil
	}
	if s.Bundle == nil || a.Version != 1 || len(a.IntentID) != 32 || a.NodeID != s.NodeID || a.InstallationID != s.InstallationID || a.Generation != s.Generation || a.ConfigSHA != s.Bundle.ConfigSHA || a.ManifestSHA != s.Bundle.ManifestSHA || a.InitialLeaseExpiresNano <= 0 || len(a.Entries) == 0 {
		return ErrPublicAliasIdentity
	}
	want, err := desiredPublicAliases(*s.Bundle)
	if err != nil || len(want) != len(a.Entries) {
		return ErrPublicAliasIdentity
	}
	for i, entry := range a.Entries {
		if entry.Address != want[i].Address || entry.Owner != want[i].Owner || entry.Host.Interface.Name != want[i].Host.Interface.Name || ix.Digest(entry.References) != ix.Digest(want[i].References) || len(entry.Host.BootID) != 36 || entry.Host.Namespace == 0 || entry.Host.Interface.Index == 0 || entry.Host.Interface.MAC == "" {
			return ErrPublicAliasIdentity
		}
		switch entry.Phase {
		case "planned", "intent":
			if entry.CreatedTicks != nil {
				return ErrPublicAliasIdentity
			}
		case "owned":
			if entry.CreatedTicks == nil {
				return ErrPublicAliasIdentity
			}
		case "released":
		default:
			return ErrPublicAliasIdentity
		}
	}
	return nil
}

func (w *Worker) aliasKernel() publicAliasKernel {
	if w.publicAliasKernel == nil {
		w.publicAliasKernel = linuxPublicAliasKernel{}
	}
	return w.publicAliasKernel
}

func publicAliasLifetime(deadline time.Time) (uint32, error) {
	seconds := int64(time.Until(deadline) / time.Second)
	if seconds <= 0 {
		return 0, ErrLease
	}
	if seconds > 30 {
		seconds = 30
	}
	return uint32(seconds), nil
}

func validPublicAliasObservation(entry publicAliasEntry, observed publicAliasAddress, ready bool) bool {
	if observed.Address.String() != entry.Address || observed.PrefixLength != 128 || !observed.HasProtocol || observed.Protocol != ix.PublicAliasProtocol || !observed.HasCache || observed.Valid == 0 || observed.Valid > 31 || observed.Preferred == 0 || observed.Preferred > observed.Valid || observed.Flags&aliasBadFlags != 0 {
		return false
	}
	if entry.CreatedTicks != nil && observed.CreatedTicks != *entry.CreatedTicks {
		return false
	}
	return !ready || observed.Flags&unix.IFA_F_TENTATIVE == 0
}

func (w *Worker) preparePublicAliases(ctx context.Context, bundle ix.Bundle, deadline time.Time) (map[string]ix.PublicAliasAuthority, error) {
	if w.state.PublicAliases != nil {
		return nil, ErrPublicAliasCleanup
	}
	entries, err := desiredPublicAliases(bundle)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	kernel := w.aliasKernel()
	hosts := map[string]publicAliasHost{}
	for i := range entries {
		entry := &entries[i]
		name := entry.Host.Interface.Name
		host, ok := hosts[name]
		if !ok {
			host, err = kernel.Host(ctx, name)
			if err != nil {
				return nil, err
			}
			hosts[name] = host
		}
		entry.Host = host
		_, exists, err := kernel.Address(ctx, host.Interface, netip.MustParseAddr(entry.Address))
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrPublicAliasConflict
		}
	}
	intent, err := randomID(16)
	if err != nil {
		return nil, err
	}
	w.state.PublicAliases = &publicAliasJournal{Version: 1, IntentID: intent, NodeID: bundle.NodeID, InstallationID: bundle.InstallationID, Generation: bundle.Generation, ConfigSHA: bundle.ConfigSHA, ManifestSHA: bundle.ManifestSHA, InitialLeaseExpiresNano: deadline.UnixNano(), Entries: entries}
	if err = w.save(); err != nil {
		return nil, err
	}
	for i := range w.state.PublicAliases.Entries {
		entry := &w.state.PublicAliases.Entries[i]
		lifetime, err := publicAliasLifetime(deadline)
		if err != nil || lifetime < 12 {
			return nil, ErrLease
		}
		current, err := kernel.Host(ctx, entry.Host.Interface.Name)
		if err != nil || current != entry.Host {
			return nil, ErrPublicAliasIdentity
		}
		entry.Phase = "intent"
		if err = w.save(); err != nil {
			return nil, err
		}
		ack, err := kernel.Add(ctx, entry.Host.Interface, netip.MustParseAddr(entry.Address), lifetime)
		if err != nil {
			if ack {
				// A negative ACK is proof this exclusive create did not occur.
				entry.Phase = "released"
				if e := w.save(); e != nil {
					return nil, e
				}
			}
			return nil, err
		}
		if !ack {
			return nil, ErrPublicAliasIdentity
		}
		observed, exists, err := kernel.Address(ctx, entry.Host.Interface, netip.MustParseAddr(entry.Address))
		if err != nil || !exists || !validPublicAliasObservation(*entry, observed, false) {
			return nil, ErrPublicAliasIdentity
		}
		stamp := observed.CreatedTicks
		entry.CreatedTicks, entry.Phase = &stamp, "owned"
		if err = w.save(); err != nil {
			return nil, err
		}
	}
	// Creates are issued first so ordinary DAD can proceed in parallel for all
	// peers. No NODAD/optimistic shortcut may open public ingress early.
	wait, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		ready := true
		for _, entry := range w.state.PublicAliases.Entries {
			observed, exists, err := kernel.Address(wait, entry.Host.Interface, netip.MustParseAddr(entry.Address))
			if err != nil || !exists || !validPublicAliasObservation(entry, observed, false) {
				return nil, ErrPublicAliasIdentity
			}
			ready = ready && observed.Flags&unix.IFA_F_TENTATIVE == 0
		}
		if ready {
			return w.publicAliasAuthorities(bundle)
		}
		select {
		case <-wait.Done():
			return nil, ErrPublicAliasUnavailable
		case <-tick.C:
		}
	}
}

func (w *Worker) publicAliasAuthorities(bundle ix.Bundle) (map[string]ix.PublicAliasAuthority, error) {
	out := map[string]ix.PublicAliasAuthority{}
	a := w.state.PublicAliases
	if a == nil {
		return out, nil
	}
	if err := validatePublicAliasJournal(w.state); err != nil {
		return nil, err
	}
	components := map[string]ix.Component{}
	for _, c := range bundle.Components {
		components[c.Tag] = c
	}
	for _, entry := range a.Entries {
		if entry.Phase != "owned" || entry.CreatedTicks == nil {
			return nil, ErrPublicAliasIdentity
		}
		for _, ref := range entry.References {
			authority, ok := out[ref.ComponentTag]
			if !ok {
				authority = ix.PublicAliasAuthority{Version: 1, IntentID: a.IntentID, NodeID: a.NodeID, InstallationID: a.InstallationID, Generation: a.Generation, ConfigSHA: a.ConfigSHA, ManifestSHA: a.ManifestSHA, ComponentTag: ref.ComponentTag, ComponentConfigSHA: ix.BytesSHA(components[ref.ComponentTag].Config), BootID: entry.Host.BootID, NetworkNamespace: entry.Host.Namespace, Interface: entry.Host.Interface, InitialLeaseExpiresNano: a.InitialLeaseExpiresNano, Aliases: []ix.PublicAliasBinding{}}
			}
			if authority.Interface != entry.Host.Interface || authority.BootID != entry.Host.BootID || authority.NetworkNamespace != entry.Host.Namespace {
				return nil, ErrPublicAliasIdentity
			}
			authority.Aliases = append(authority.Aliases, ix.PublicAliasBinding{PrincipalID: ref.PrincipalID, Email: ref.Email, PeerPublicKeySHA: ref.PeerPublicKeySHA, Address: entry.Address, PrefixLength: 128, Protocol: ix.PublicAliasProtocol, CreatedTicks: *entry.CreatedTicks})
			out[ref.ComponentTag] = authority
		}
	}
	for tag, authority := range out {
		sort.Slice(authority.Aliases, func(i, j int) bool { return authority.Aliases[i].Email < authority.Aliases[j].Email })
		out[tag] = authority
	}
	return out, nil
}

func (w *Worker) renewPublicAliases(ctx context.Context, deadline time.Time) error {
	if w.state.PublicAliases == nil {
		return nil
	}
	lifetime, err := publicAliasLifetime(deadline)
	if err != nil {
		return err
	}
	kernel := w.aliasKernel()
	for _, entry := range w.state.PublicAliases.Entries {
		current, err := kernel.Host(ctx, entry.Host.Interface.Name)
		if err != nil || current != entry.Host || entry.Phase != "owned" || entry.CreatedTicks == nil {
			return ErrPublicAliasIdentity
		}
		address := netip.MustParseAddr(entry.Address)
		observed, exists, err := kernel.Address(ctx, entry.Host.Interface, address)
		if err != nil || !exists || !validPublicAliasObservation(entry, observed, true) {
			return ErrPublicAliasIdentity
		}
		ack, err := kernel.Renew(ctx, entry.Host.Interface, address, lifetime)
		if err != nil || !ack {
			return ErrPublicAliasIdentity
		}
		observed, exists, err = kernel.Address(ctx, entry.Host.Interface, address)
		if err != nil || !exists || !validPublicAliasObservation(entry, observed, true) {
			return ErrPublicAliasIdentity
		}
	}
	return nil
}

func (w *Worker) fencePublicAliases(ctx context.Context, cause error) error {
	// Losing source authority closes and joins public relays before any cleanup
	// decision. A genuine final remains durable/replayable even while an alias
	// is waiting for its finite kernel lifetime to expire.
	if err := w.captureFinal(ctx); err != nil {
		_ = w.markUncertain()
		return cause
	}
	if w.process != nil {
		w.process.Stop()
		w.process = nil
	}
	return cause
}

// Cleanup is deliberately a resumable read-only step. Positive expiry evidence
// is required before replacing the resource journal or admitting a successor;
// neither an uncertain add nor an identity mismatch grants deletion authority.
func (w *Worker) cleanupPublicAliases(ctx context.Context) error {
	a := w.state.PublicAliases
	if a == nil {
		return nil
	}
	if w.process != nil {
		return ErrPublicAliasCleanup
	}
	kernel := w.aliasKernel()
	pending := false
	changed := false
	for i := range a.Entries {
		entry := &a.Entries[i]
		if entry.Phase == "released" {
			continue
		}
		if entry.Phase == "planned" {
			entry.Phase, changed = "released", true
			continue
		}
		host, interfaceExists, err := kernel.HostIndex(ctx, entry.Host.Interface.Index)
		if err != nil {
			return ErrPublicAliasCleanup
		}
		if host.BootID != entry.Host.BootID {
			// A reboot destroys the old kernel resource; no current address is
			// touched or adopted. Namespace changes within one boot are weaker.
			entry.Phase, changed = "released", true
			continue
		}
		if host.Namespace != entry.Host.Namespace {
			return ErrPublicAliasIdentity
		}
		if !interfaceExists {
			entry.Phase, changed = "released", true
			continue
		}
		_, exists, err := kernel.Address(ctx, host.Interface, netip.MustParseAddr(entry.Address))
		if err != nil {
			return ErrPublicAliasCleanup
		}
		if exists {
			pending = true
		} else {
			entry.Phase, changed = "released", true
		}
	}
	if !pending {
		w.state.PublicAliases = nil
		changed = true
	}
	if changed {
		if err := w.save(); err != nil {
			return err
		}
	}
	if pending {
		return ErrPublicAliasCleanup
	}
	return nil
}

// ProbePublicAliasKernel checks read-only rtnetlink availability. It does not
// advertise mutation, DAD, or marker support: those are proven by the exclusive
// add/readback before the relevant generation can register or activate.
func ProbePublicAliasKernel(ctx context.Context) bool {
	data := make([]byte, unix.SizeofIfAddrmsg)
	data[0] = unix.AF_INET6
	_, _, err := publicAliasNetlink(ctx, unix.RTM_GETADDR, unix.NLM_F_DUMP, data)
	return err == nil
}
