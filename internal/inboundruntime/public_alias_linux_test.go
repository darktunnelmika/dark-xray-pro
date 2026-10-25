//go:build linux

package inboundruntime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"golang.org/x/sys/unix"
)

type aliasFixtureKernel struct {
	host          publicAliasHost
	addresses     map[netip.Addr]publicAliasAddress
	adds, renews  int
	beforeAdd     func(int)
	addResult     func(int, netip.Addr) (bool, error)
	afterAdd      func(int)
	hostReadError error
}

func newAliasFixtureKernel() *aliasFixtureKernel {
	return &aliasFixtureKernel{host: publicAliasHost{BootID: "00000000-0000-0000-0000-000000000001", Namespace: 44, Interface: ix.PublicAliasInterface{Name: "eth-test", Index: 7, MAC: "02:00:00:00:00:01"}}, addresses: map[netip.Addr]publicAliasAddress{}}
}
func (k *aliasFixtureKernel) Host(ctx context.Context, name string) (publicAliasHost, error) {
	if err := ctx.Err(); err != nil {
		return publicAliasHost{}, err
	}
	if k.hostReadError != nil {
		return publicAliasHost{}, k.hostReadError
	}
	if name != k.host.Interface.Name {
		return publicAliasHost{}, ErrPublicAliasUnavailable
	}
	return k.host, nil
}
func (k *aliasFixtureKernel) HostIndex(ctx context.Context, index uint32) (publicAliasHost, bool, error) {
	if err := ctx.Err(); err != nil {
		return publicAliasHost{}, false, err
	}
	if k.hostReadError != nil {
		return publicAliasHost{}, false, k.hostReadError
	}
	return k.host, index == k.host.Interface.Index, nil
}
func (k *aliasFixtureKernel) Address(ctx context.Context, iface ix.PublicAliasInterface, address netip.Addr) (publicAliasAddress, bool, error) {
	if err := ctx.Err(); err != nil {
		return publicAliasAddress{}, false, err
	}
	if iface.Index != k.host.Interface.Index {
		return publicAliasAddress{}, false, ErrPublicAliasIdentity
	}
	v, ok := k.addresses[address]
	return v, ok, nil
}
func (k *aliasFixtureKernel) Add(ctx context.Context, _ ix.PublicAliasInterface, address netip.Addr, lifetime uint32) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	k.adds++
	if k.beforeAdd != nil {
		k.beforeAdd(k.adds)
	}
	if k.addResult != nil {
		return k.addResult(k.adds, address)
	}
	if _, ok := k.addresses[address]; ok {
		return true, unix.EEXIST
	}
	k.addresses[address] = publicAliasAddress{Address: address, PrefixLength: 128, Protocol: ix.PublicAliasProtocol, HasProtocol: true, HasCache: true, CreatedTicks: uint32(100 + k.adds), Preferred: lifetime, Valid: lifetime}
	if k.afterAdd != nil {
		k.afterAdd(k.adds)
	}
	return true, nil
}
func (k *aliasFixtureKernel) Renew(ctx context.Context, _ ix.PublicAliasInterface, address netip.Addr, lifetime uint32) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	k.renews++
	v, ok := k.addresses[address]
	if !ok {
		return true, unix.ENOENT
	}
	v.Preferred, v.Valid = lifetime, lifetime
	k.addresses[address] = v
	return true, nil
}

func aliasFixtureBundle(t *testing.T, w *Worker) ix.Bundle {
	t.Helper()
	peers := []map[string]any{}
	principals := []ix.Principal{}
	for i, suffix := range []string{"2", "3"} {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(rune('A'+i)), 32)))
		email := "alias-peer-" + suffix
		peers = append(peers, map[string]any{"email": email, "publicKey": key, "allowedIPs": []string{"10.88.0." + suffix + "/32", "fd42:55::" + suffix + "/128"}})
		principals = append(principals, ix.Principal{ID: "principal-" + suffix, Engine: "amneziawg", ComponentTag: "alias-component", ClientID: "client-" + suffix, Role: "customer", Counter: "user>>>" + email + ">>>traffic", CredentialSHA: ix.BytesSHA([]byte(key)), InboundID: 501, Billable: true})
	}
	raw, err := json.Marshal(map[string]any{"server": map[string]any{"ipv6Enabled": true, "ipv6Subnet": "fd42:55::/64", "ipv6ExternalInterface": "eth-test"}, "peers": peers})
	if err != nil {
		t.Fatal(err)
	}
	b := ix.Bundle{Version: 1, NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Generation: 1, OperationID: "alias-prepare-0001", Revisions: []ix.RevisionRef{{InboundID: 501, Revision: 1, PlanRevision: 1, InboundRevision: 1}}, Components: []ix.Component{{Engine: "amneziawg", Tag: "alias-component", Config: raw, SHA256: ix.BytesSHA(raw), Manifest: []byte(`{"adapter":"amneziawg-v3.1-v1"}`)}}, Principals: principals, Listeners: []ix.Listener{{InboundID: 501, Tag: "alias-component", Engine: "amneziawg", Protocol: "amneziawg", ResourceKind: "socket", Listen: "127.0.0.1", Port: 20001, Network: "udp", HostRequirements: []string{ix.HostIPv6Alias}}}}
	return fixtureDelivery(t, w.cfg.Key, b).Bundle
}

func aliasFixtureWorker(t *testing.T) (*Worker, *aliasFixtureKernel) {
	t.Helper()
	w, _, _ := fixtureWorker(t, "prepared")
	w.process = nil
	b := aliasFixtureBundle(t, w)
	w.state = journal{Version: 1, NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Generation: b.Generation, Bundle: &b, Phase: "preparing"}
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	k := newAliasFixtureKernel()
	w.publicAliasKernel = k
	return w, k
}

func durableAliasFixtureState(t *testing.T, w *Worker) journal {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(w.journal.dir, "state.enc"))
	if err != nil {
		t.Fatal(err)
	}
	n := w.journal.aead.NonceSize()
	clear, err := w.journal.aead.Open(nil, raw[:n], raw[n:], w.journal.aad)
	if err != nil {
		t.Fatal(err)
	}
	var state journal
	if err := json.Unmarshal(clear, &state); err != nil {
		t.Fatal(err)
	}
	if err := validateJournal(w.cfg.Key, state); err != nil {
		t.Fatal("durable alias state cannot reopen", err)
	}
	return state
}

func TestPublicAliasPreparationRequiresDurableIntentAndExactKernelBirth(t *testing.T) {
	w, kernel := aliasFixtureWorker(t)
	kernel.beforeAdd = func(count int) {
		state := durableAliasFixtureState(t, w)
		if state.PublicAliases == nil || state.PublicAliases.Entries[count-1].Phase != "intent" || state.PublicAliases.Entries[count-1].CreatedTicks != nil {
			t.Fatal("exclusive add preceded its durable exact intent")
		}
	}
	authorities, err := w.preparePublicAliases(context.Background(), *w.state.Bundle, time.Now().Add(30*time.Second))
	if err != nil || len(authorities) != 1 || kernel.adds != 2 {
		t.Fatal(authorities, err)
	}
	a := authorities["alias-component"]
	if len(a.Aliases) != 2 || a.NetworkNamespace != kernel.host.Namespace || a.Interface != kernel.host.Interface || a.ConfigSHA != w.state.Bundle.ConfigSHA || a.ComponentConfigSHA != w.state.Bundle.Components[0].SHA256 {
		t.Fatal("private authority lost reviewed config or actual kernel identity")
	}
	for _, binding := range a.Aliases {
		observed := kernel.addresses[netip.MustParseAddr(binding.Address)]
		if binding.CreatedTicks != observed.CreatedTicks || binding.Protocol != ix.PublicAliasProtocol || binding.PrefixLength != 128 {
			t.Fatal("kernel birth not bound")
		}
	}
	durableAliasFixtureState(t, w)
	if err := w.renewPublicAliases(context.Background(), time.Now().Add(25*time.Second)); err != nil || kernel.renews != 2 {
		t.Fatal("exact aliases could not renew", err)
	}
}

func TestPublicAliasExistingAddressAndNegativeACKAreNeverAdopted(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "raced-exclusive-add"}[race], func(t *testing.T) {
			w, kernel := aliasFixtureWorker(t)
			address := netip.MustParseAddr("fd42:55::2")
			foreign := publicAliasAddress{Address: address, PrefixLength: 128, CreatedTicks: 99, HasCache: true, Valid: ^uint32(0), Preferred: ^uint32(0)}
			if race {
				kernel.addResult = func(_ int, a netip.Addr) (bool, error) { kernel.addresses[a] = foreign; return true, unix.EEXIST }
			} else {
				kernel.addresses[address] = foreign
			}
			if _, err := w.preparePublicAliases(context.Background(), *w.state.Bundle, time.Now().Add(30*time.Second)); err == nil {
				t.Fatal("foreign address adopted")
			}
			if err := w.cleanupPublicAliases(context.Background()); err != nil {
				t.Fatal("proved negative create did not clear its own journal", err)
			}
			if kernel.addresses[address] != foreign || kernel.renews != 0 || w.state.PublicAliases != nil {
				t.Fatal("foreign address changed during refusal")
			}
		})
	}
}

func TestPublicAliasLostAddACKSurvivesRestartWithoutAdoption(t *testing.T) {
	w, kernel := aliasFixtureWorker(t)
	kernel.addResult = func(_ int, address netip.Addr) (bool, error) {
		kernel.addresses[address] = publicAliasAddress{Address: address, PrefixLength: 128, Protocol: ix.PublicAliasProtocol, HasProtocol: true, HasCache: true, CreatedTicks: 312, Preferred: 29, Valid: 29}
		return false, context.DeadlineExceeded
	}
	if _, err := w.preparePublicAliases(context.Background(), *w.state.Bundle, time.Now().Add(30*time.Second)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	state := durableAliasFixtureState(t, w)
	if state.PublicAliases.Entries[0].Phase != "intent" || state.PublicAliases.Entries[0].CreatedTicks != nil {
		t.Fatal("ambiguous add invented ownership")
	}
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopened.publicAliasKernel = kernel
	if err := reopened.cleanupPublicAliases(context.Background()); !errors.Is(err, ErrPublicAliasCleanup) {
		t.Fatal("ambiguous alias was cleared before expiry", err)
	}
	if kernel.renews != 0 || kernel.adds != 1 {
		t.Fatal("ambiguous add was adopted or retried")
	}
	delete(kernel.addresses, netip.MustParseAddr("fd42:55::2"))
	if err := reopened.cleanupPublicAliases(context.Background()); err != nil || reopened.state.PublicAliases != nil {
		t.Fatal("verified expiry did not finish cleanup", err)
	}
}

func TestPublicAliasPostAddJournalFailureRetainsOnlyDurableIntent(t *testing.T) {
	w, kernel := aliasFixtureWorker(t)
	oldDir := w.journal.dir
	badPath := filepath.Join(t.TempDir(), "ordinary-file")
	if err := os.WriteFile(badPath, []byte("no directory"), 0600); err != nil {
		t.Fatal(err)
	}
	kernel.afterAdd = func(_ int) { w.journal.dir = badPath }
	if _, err := w.preparePublicAliases(context.Background(), *w.state.Bundle, time.Now().Add(30*time.Second)); !errors.Is(err, ErrPersistence) {
		t.Fatal(err)
	}
	w.journal.dir = oldDir
	state := durableAliasFixtureState(t, w)
	if state.PublicAliases.Entries[0].Phase != "intent" || !w.persistFailed || kernel.adds != 1 {
		t.Fatal("undurable kernel ownership escaped persistence failure")
	}
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopened.publicAliasKernel = kernel
	if err := reopened.cleanupPublicAliases(context.Background()); !errors.Is(err, ErrPublicAliasCleanup) {
		t.Fatal("persisted intent silently adopted after restart", err)
	}
}

func TestPublicAliasRenewRejectsMissingChangedTentativeAndUnmarkedAddresses(t *testing.T) {
	for _, change := range []string{"removed", "stamp", "protocol", "tentative", "DAD-failed", "infinite-lifetime", "interface"} {
		t.Run(change, func(t *testing.T) {
			w, kernel := aliasFixtureWorker(t)
			if _, err := w.preparePublicAliases(context.Background(), *w.state.Bundle, time.Now().Add(30*time.Second)); err != nil {
				t.Fatal(err)
			}
			a := netip.MustParseAddr("fd42:55::2")
			v := kernel.addresses[a]
			switch change {
			case "removed":
				delete(kernel.addresses, a)
			case "stamp":
				v.CreatedTicks++
				kernel.addresses[a] = v
			case "protocol":
				v.HasProtocol = false
				kernel.addresses[a] = v
			case "tentative":
				v.Flags = unix.IFA_F_TENTATIVE
				kernel.addresses[a] = v
			case "DAD-failed":
				v.Flags = unix.IFA_F_DADFAILED
				kernel.addresses[a] = v
			case "infinite-lifetime":
				v.Valid = ^uint32(0)
				kernel.addresses[a] = v
			case "interface":
				kernel.host.Interface.MAC = "02:00:00:00:00:ff"
			}
			if err := w.renewPublicAliases(context.Background(), time.Now().Add(30*time.Second)); !errors.Is(err, ErrPublicAliasIdentity) {
				t.Fatal("lost source authority was renewed", err)
			}
			if kernel.renews != 0 || kernel.adds != 2 {
				t.Fatal("mismatched address was recreated or modified")
			}
		})
	}
}

func TestPublicAliasCleanupNeedsPositiveAbsenceAndDoesNotWaitForLifetime(t *testing.T) {
	w, kernel := aliasFixtureWorker(t)
	if _, err := w.preparePublicAliases(context.Background(), *w.state.Bundle, time.Now().Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	if err := w.cleanupPublicAliases(context.Background()); !errors.Is(err, ErrPublicAliasCleanup) || time.Since(before) > time.Second {
		t.Fatal("cleanup blocked on a finite lifetime", err)
	}
	kernel.hostReadError = errors.New("netlink unavailable")
	if err := w.cleanupPublicAliases(context.Background()); !errors.Is(err, ErrPublicAliasCleanup) || w.state.PublicAliases == nil {
		t.Fatal("failed host read was mistaken for absence", err)
	}
	kernel.hostReadError = nil
	kernel.host.Namespace++
	if err := w.cleanupPublicAliases(context.Background()); !errors.Is(err, ErrPublicAliasIdentity) || w.state.PublicAliases == nil {
		t.Fatal("foreign namespace cleared unresolved resources", err)
	}
	kernel.host.Namespace--
	for a := range kernel.addresses {
		delete(kernel.addresses, a)
	}
	if err := w.cleanupPublicAliases(context.Background()); err != nil || w.state.PublicAliases != nil {
		t.Fatal("positive absence did not resolve journal", err)
	}
}
