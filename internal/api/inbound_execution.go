package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

type InboundExecutionConfig struct {
	Directory string
	Native    inboundruntime.BinaryConfig
	External  map[string]inboundruntime.BinaryConfig
}
type inboundExecutionService struct {
	config       InboundExecutionConfig
	local        *inboundruntime.Worker
	localKey     []byte
	mu           sync.Mutex
	capabilities map[string]executionCapabilityReport
	started      bool
	done         chan struct{}
}

// EnableInboundExecution is a single permanent PRO capability. It does not
// activate an authoring plan, choose the HUB implicitly, or claim old channels.
func (s *Server) EnableInboundExecution(cfg InboundExecutionConfig) error {
	if s.inboundExecution != nil || s.business == nil || s.business.UsageLedger() == nil || len(s.inboundKey) != 32 || !filepath.IsAbs(cfg.Directory) {
		return errors.New("managed_inbound_prerequisites_missing")
	}
	h := hmac.New(sha256.New, s.inboundKey)
	_, _ = h.Write([]byte("DARK-PRO-HUB-LOCAL-EXECUTION-v1"))
	key := []byte(hex.EncodeToString(h.Sum(nil)))
	worker, e := inboundruntime.Open(inboundruntime.Config{Directory: cfg.Directory, NodeID: localHubNodeID, InstallationID: s.localHubInstallation(), Key: key, Native: cfg.Native, External: cfg.External})
	if e != nil {
		return e
	}
	if e = s.business.EnableInboundExecution(context.Background()); e != nil {
		worker.Close()
		return e
	}
	s.inboundExecution = &inboundExecutionService{config: cfg, local: worker, localKey: key, capabilities: map[string]executionCapabilityReport{}, done: make(chan struct{})}
	return nil
}

func (s *Server) RunInboundExecution(ctx context.Context) {
	x := s.inboundExecution
	if x == nil {
		return
	}
	x.mu.Lock()
	if x.started {
		x.mu.Unlock()
		return
	}
	x.started = true
	x.mu.Unlock()
	defer close(x.done)
	monitor, stopMonitor := context.WithCancel(ctx)
	monitored := make(chan struct{})
	go func() {
		defer close(monitored)
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			attempt, cancel := context.WithTimeout(monitor, 4*time.Second)
			_, _ = s.business.SweepInboundExecutionLiveness(attempt, s.inboundKey)
			cancel()
			select {
			case <-monitor.Done():
				return
			case <-tick.C:
			}
		}
	}()
	defer func() { stopMonitor(); <-monitored }()
	x.local.Run(ctx, localInboundExchange{s: s})
}

func (s *Server) WaitInboundExecution(ctx context.Context) {
	if s.inboundExecution == nil {
		return
	}
	select {
	case <-s.inboundExecution.done:
	case <-ctx.Done():
	}
}

func decodeExecution(w http.ResponseWriter, r *http.Request, max int64, dst any) error {
	var raw json.RawMessage
	if decode(w, r, max, &raw) != nil {
		return business.ErrInvalid
	}
	if usagewire.Decode(raw, dst) != nil {
		return business.ErrInvalid
	}
	return nil
}

func (s *Server) attachInboundExecution(mux *http.ServeMux) {
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	base := "/api/v1/workspace/native-inbounds/{id}/execution"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		if s.inboundExecution == nil {
			respond(w, 200, inboundexec.State{Version: 1, Enabled: false, InboundID: id, DesiredState: "disabled", ObservedState: "disabled", AccountingMode: inboundexec.AccountingPerClient, AllowedAccountingModes: []string{}, ServerTimeMillis: time.Now().UnixMilli(), PublicationState: "withheld", ClientIDs: []string{}, EffectiveClientIDs: []string{}, Targets: []inboundexec.TargetState{}, Reconciliations: []inboundexec.ReconciliationState{}, AllowedActions: []string{}, Blockers: []inboundexec.Blocker{{Code: "execution_not_enabled"}}})
			return
		}
		state, e := s.business.InboundExecution(r.Context(), s.inboundKey, id)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, s.executionHostState(state))
	})
	for _, op := range []string{"preview", "commands"} {
		op := op
		mux.HandleFunc("POST "+base+"/"+op, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, true, true)
			if !ok {
				return
			}
			id, ok := nativePlanPromotionID(w, r)
			if !ok {
				return
			}
			if s.inboundExecution == nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			var input inboundexec.CommandInput
			if decodeExecution(w, r, 2<<20, &input) != nil {
				businessError(w, business.ErrInvalid)
				return
			}
			s.replacementLifecycle.Lock()
			defer s.replacementLifecycle.Unlock()
			if input.Action != "stop" && input.Action != "archive" {
				plan, e := s.business.NativeInboundPlan(r.Context(), s.inboundKey, id)
				if e != nil {
					businessError(w, e)
					return
				}
				if e = s.nativePlanReferences(plan.Nodes); e != nil {
					businessError(w, e)
					return
				}
			}
			if op == "preview" {
				p, e := s.business.PreviewInboundExecution(r.Context(), s.inboundKey, session.User.ID, id, input)
				if e != nil {
					businessError(w, e)
					return
				}
				respond(w, 200, s.executionHostPreview(p))
				return
			}
			if input.Action != "stop" && input.Action != "archive" {
				// An exact previously accepted command remains replayable even
				// if host capabilities changed after acceptance.
				_, previous := s.business.InboundExecutionOperation(r.Context(), s.inboundKey, id, input.EventID)
				if errors.Is(previous, business.ErrNotFound) {
					preview, err := s.business.PreviewInboundExecution(r.Context(), s.inboundKey, session.User.ID, id, input)
					if err != nil {
						businessError(w, err)
						return
					}
					if !s.executionHostPreview(preview).Valid {
						businessError(w, business.ErrConflict)
						return
					}
				} else if previous != nil {
					businessError(w, previous)
					return
				}
			}
			result, e := s.business.QueueInboundExecution(r.Context(), s.inboundKey, session.User.ID, id, input)
			if e != nil {
				businessError(w, e)
				return
			}
			respond(w, http.StatusAccepted, result)
		})
	}
	mux.HandleFunc("POST "+base+"/clients", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		if s.inboundExecution == nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		var input inboundexec.BindingsInput
		if decodeExecution(w, r, 2<<20, &input) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		state, e := s.business.BindInboundExecutionClients(r.Context(), s.inboundKey, session.User.ID, id, input)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, state)
	})
	mux.HandleFunc("GET "+base+"/operations/{operation}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		if s.inboundExecution == nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		result, e := s.business.InboundExecutionOperation(r.Context(), s.inboundKey, id, r.PathValue("operation"))
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, result)
	})
	s.attachInboundExecutionReconciliations(mux, base)
}

func (s *Server) attachInboundExecutionReconciliations(mux *http.ServeMux, base string) {
	for _, preview := range []bool{true, false} {
		preview := preview
		path := base + "/reconciliations"
		if preview {
			path += "/preview"
		}
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, true, true)
			if !ok {
				return
			}
			id, ok := nativePlanPromotionID(w, r)
			if !ok {
				return
			}
			if s.inboundExecution == nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			var input inboundexec.ReconciliationInput
			if decodeExecution(w, r, 32<<10, &input) != nil {
				businessError(w, business.ErrInvalid)
				return
			}
			if preview {
				result, err := s.business.PreviewInboundExecutionReconciliation(r.Context(), s.inboundKey, session.User.ID, id, input)
				if err != nil {
					businessError(w, err)
					return
				}
				respond(w, http.StatusOK, result)
				return
			}
			result, err := s.business.ReconcileInboundExecution(r.Context(), s.inboundKey, session.User.ID, id, input)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, result)
		})
	}
	mux.HandleFunc("GET "+base+"/reconciliations/{event}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, true)
		if !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		if s.inboundExecution == nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		result, err := s.business.InboundExecutionReconciliationReceipt(r.Context(), s.inboundKey, session.User.ID, id, r.PathValue("event"))
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, result)
	})
}

func (s *Server) executionNode(node, install string) bool {
	if node == localHubNodeID {
		return install == s.localHubInstallation()
	}
	actual, ok := s.nativeNode(node)
	return ok && actual == install
}

func (s *Server) executionCapabilities(c inboundruntime.Capabilities) bool {
	x := s.inboundExecution
	if x == nil || c.Version != 1 || len(c.Binaries) == 0 || len(c.Binaries) > 4 {
		return false
	}
	if c.Binaries["xray"] != x.config.Native.SHA256 {
		return false
	}
	for engine, pin := range c.Binaries {
		if engine == "xray" {
			continue
		}
		expected, ok := x.config.External[engine]
		if !ok || pin != expected.SHA256 {
			return false
		}
	}
	return true
}

func (s *Server) executionDelivery(ctx context.Context, input inboundruntime.PollInput, key []byte) (*inboundruntime.Delivery, error) {
	if !s.executionNode(input.NodeID, input.InstallationID) {
		return nil, business.ErrConflict
	}
	if input.UncertainEpochID != "" {
		clearance, found, err := s.business.InboundExecutionClearance(ctx, s.inboundKey, input.NodeID, input.InstallationID, input.UncertainEpochID)
		if err != nil {
			return nil, err
		}
		if found {
			clearance, err = inboundexec.SealClearance(key, clearance)
			if err != nil {
				return nil, err
			}
			// Settlement of an old uncertain journal does not carry admission
			// authority. A subsequent poll must earn a fresh bundle and lease.
			return &inboundruntime.Delivery{Clearance: &clearance}, nil
		}
	}
	if !s.executionCapabilities(input.Capabilities) {
		return nil, business.ErrConflict
	}
	s.recordExecutionCapabilities(input.NodeID, input.InstallationID, input.Capabilities)
	if _, e := s.business.RefreshInboundExecutionEligibility(ctx, s.inboundKey, input.NodeID, input.InstallationID); e != nil {
		return nil, e
	}
	bundle, found, e := s.business.InboundExecutionBundle(ctx, s.inboundKey, input.NodeID, input.InstallationID)
	if e != nil || !found {
		return nil, e
	}
	if e = s.business.InboundExecutionAuthority(ctx, s.inboundKey, input.NodeID, input.InstallationID, bundle.Generation); e != nil {
		return nil, e
	}
	requirements, e := inboundexec.BundleHostRequirements(bundle)
	if e != nil || len(inboundexec.HostRequirementBlockers(requirements, input.Capabilities.Host, true)) != 0 {
		return nil, business.ErrUnavailable
	}
	for _, c := range bundle.Components {
		if input.Capabilities.Binaries[c.Engine] == "" {
			return nil, business.ErrUnavailable
		}
	}
	sealed, e := inboundexec.SealBundle(key, bundle)
	if e != nil {
		return nil, e
	}
	pins := map[string]string{}
	if len(bundle.NativeConfig) > 0 {
		pins["xray"] = input.Capabilities.Binaries["xray"]
	}
	for _, component := range bundle.Components {
		pins[component.Engine] = input.Capabilities.Binaries[component.Engine]
	}
	if e = s.business.RecordInboundExecutionBinaryApproval(ctx, s.inboundKey, bundle.NodeID, bundle.InstallationID, bundle.Generation, pins); e != nil {
		return nil, e
	}
	now := time.Now()
	lease := inboundruntime.Lease{Version: 1, NodeID: bundle.NodeID, InstallationID: bundle.InstallationID, Generation: bundle.Generation, ConfigSHA: bundle.ConfigSHA, ManifestSHA: bundle.ManifestSHA, Issued: now.UnixNano(), Expires: now.Add(30 * time.Second).UnixNano()}
	if input.ReconciledEpochID != "" {
		operation, approved, err := s.business.InboundExecutionResume(ctx, s.inboundKey, input.NodeID, input.InstallationID, input.ReconciledEpochID, bundle.Generation)
		if err != nil {
			return nil, err
		}
		if approved {
			clearance, found, err := s.business.InboundExecutionClearance(ctx, s.inboundKey, input.NodeID, input.InstallationID, input.ReconciledEpochID)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, business.ErrUnavailable
			}
			lease.ResumeEpochID, lease.ResumeEpochSHA, lease.ResumeOperationID = clearance.EpochID, clearance.EpochSHA, operation
		}
	}
	lease, e = inboundruntime.SignLease(key, lease)
	if e != nil {
		return nil, e
	}
	return &inboundruntime.Delivery{Bundle: sealed, Lease: lease}, nil
}

func (s *Server) registerExecution(ctx context.Context, in inboundexec.EpochInput) (inboundruntime.EpochReceipt, error) {
	if err := s.executionEpochProvenance(ctx, in, false); err != nil {
		return inboundruntime.EpochReceipt{}, err
	}
	registered, e := s.business.RegisterInboundExecutionEpoch(ctx, s.inboundKey, in)
	if e != nil {
		return inboundruntime.EpochReceipt{}, e
	}
	return inboundruntime.EpochReceipt{EpochID: registered.EpochID, ProcessIdentity: registered.ProcessIdentity, Registered: true}, nil
}

// An exact recorded birth preserves its approved binary provenance across a
// release upgrade. Unknown births still require the currently approved pins;
// neither path grants admission without the Store's lifecycle and lease checks.
func (s *Server) executionEpochProvenance(ctx context.Context, in inboundexec.EpochInput, settlement bool) error {
	if !s.executionNode(in.NodeID, in.InstallationID) || len(in.BinarySHAs) == 0 {
		return business.ErrConflict
	}
	if _, found, err := s.business.LookupInboundExecutionEpochReceipt(ctx, s.inboundKey, in); err != nil {
		return err
	} else if found {
		return nil
	}
	if settlement {
		approved, err := s.business.LookupInboundExecutionBinaryApproval(ctx, s.inboundKey, in)
		if err != nil {
			return err
		}
		if approved {
			return nil
		}
	}
	for engine, pin := range in.BinarySHAs {
		expected := s.inboundExecution.config.Native.SHA256
		if engine != "xray" {
			c, ok := s.inboundExecution.config.External[engine]
			if !ok {
				return business.ErrConflict
			}
			expected = c.SHA256
		}
		if pin != expected {
			return business.ErrConflict
		}
	}
	return nil
}

func (s *Server) acceptExecutionUsage(ctx context.Context, in inboundexec.UsageReport) (inboundexec.UsageReceipt, error) {
	if in.InitialEpoch != nil {
		if in.InitialEpoch.EpochID != in.EpochID || in.InitialEpoch.NodeID != in.NodeID || in.InitialEpoch.InstallationID != in.InstallationID || in.InitialEpoch.Generation != in.Generation || in.InitialEpoch.ProcessIdentity != in.ProcessIdentity {
			return inboundexec.UsageReceipt{}, business.ErrConflict
		}
		if err := s.executionEpochProvenance(ctx, *in.InitialEpoch, true); err != nil {
			return inboundexec.UsageReceipt{}, err
		}
	}
	return s.business.AcceptInboundExecutionUsage(ctx, s.inboundKey, in)
}

func (s *Server) observeExecution(ctx context.Context, in inboundexec.Observation) error {
	if in.InitialEpoch != nil {
		if in.InitialEpoch.EpochID != in.EpochID || in.InitialEpoch.NodeID != in.NodeID || in.InitialEpoch.InstallationID != in.InstallationID || in.InitialEpoch.Generation != in.Generation || in.InitialEpoch.ConfigSHA != in.ConfigSHA || in.InitialEpoch.ManifestSHA != in.ManifestSHA {
			return business.ErrConflict
		}
		if err := s.executionEpochProvenance(ctx, *in.InitialEpoch, true); err != nil {
			return err
		}
	}
	return s.business.ObserveInboundExecution(ctx, s.inboundKey, in)
}

func (s *Server) handleInboundExecutionAgent(w http.ResponseWriter, r *http.Request) {
	if s.inboundExecution == nil {
		http.NotFound(w, r)
		return
	}
	if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 {
		respond(w, 426, map[string]string{"error": "tls13_required"})
		return
	}
	var raw json.RawMessage
	if decode(w, r, 8<<20, &raw) != nil {
		businessError(w, business.ErrInvalid)
		return
	}
	var identity struct {
		NodeID         string `json:"node_id"`
		InstallationID string `json:"installation_id"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.NodeID == localHubNodeID {
		businessError(w, business.ErrInvalid)
		return
	}
	token := bearer(r)
	if _, ok := s.registry.Authenticate(identity.NodeID, identity.InstallationID, token); !ok {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/api/v1/agent/inbound-execution/") {
	case "poll":
		var input inboundruntime.PollInput
		if usagewire.Decode(raw, &input) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		out, e := s.executionDelivery(r.Context(), input, []byte(token))
		if e != nil {
			businessError(w, e)
			return
		}
		if out == nil {
			w.WriteHeader(204)
			return
		}
		respond(w, 200, out)
	case "register":
		var input inboundexec.EpochInput
		if usagewire.Decode(raw, &input) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		out, e := s.registerExecution(r.Context(), input)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, out)
	case "usage":
		var input inboundexec.UsageReport
		if usagewire.Decode(raw, &input) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		out, e := s.acceptExecutionUsage(r.Context(), input)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, out)
	case "observation":
		var input inboundexec.Observation
		if usagewire.Decode(raw, &input) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		if e := s.observeExecution(r.Context(), input); e != nil {
			businessError(w, e)
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}

type localInboundExchange struct{ s *Server }

func (x localInboundExchange) Poll(ctx context.Context, p inboundruntime.PollInput) (*inboundruntime.Delivery, error) {
	return x.s.executionDelivery(ctx, p, x.s.inboundExecution.localKey)
}
func (x localInboundExchange) Register(ctx context.Context, p inboundexec.EpochInput) (inboundruntime.EpochReceipt, error) {
	return x.s.registerExecution(ctx, p)
}
func (x localInboundExchange) Usage(ctx context.Context, p inboundexec.UsageReport) (inboundexec.UsageReceipt, error) {
	return x.s.acceptExecutionUsage(ctx, p)
}
func (x localInboundExchange) Observe(ctx context.Context, p inboundexec.Observation) error {
	return x.s.observeExecution(ctx, p)
}
