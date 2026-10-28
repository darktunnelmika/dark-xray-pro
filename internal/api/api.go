package api

import (
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubmonitor"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundsecurity"
	"github.com/darktunnelmika/dark-xray-pro/internal/labrevision"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/nodebootstrap"
	"github.com/darktunnelmika/dark-xray-pro/internal/nodehistory"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type Server struct {
	inboundExecution            *inboundExecutionService
	inboundSecurity             *inboundsecurity.Service
	inboundProbeJobs            inboundProbeQueue
	healthEndpoint              *managedxray.ProtocolHealthEndpoint
	healthRoundsEnabled         bool // startup-only; requires the pinned health transport
	initialApplyHealthEnabled   bool // startup-only; one promoted first Apply, no handover authority
	labSubscriptionsEnabled     bool // startup-only; bounded N17-backed consumer delivery
	consumerManifestsEnabled    bool // startup-only; exact approved consumer endpoint proofs
	meteredRouteNode            string
	meteredRouteInstall         string
	meteredRouteNodes           map[string]string // immutable after startup: node -> installation
	inboundPlanPromotionEnabled bool              // startup-only, one previously empty metered LAB Node
	routeNode                   string
	routeInstall                string
	fleet                       *telemetry.Store
	registry                    *noderegistry.Registry
	ownerKey                    string
	replacements                *replacement.Journal
	replacementLifecycle        sync.Mutex // serializes cancel and dependent private-stage/LAB mutations
	stages                      *configstage.Store
	labRevisions                *labrevision.Store
	business                    *business.Store
	sellerQuotaV2Enabled        bool // opt-in separate authorization and private migration gate
	loginGate                   *loginGate
	inboundKey                  []byte
	nodeHistory                 *nodehistory.Store
	hubMonitor                  *hubmonitor.Monitor
	nodeBundle                  *nodebootstrap.Bundle
	nodeTransportScope          string
	meteredNode                 string // explicitly allowlisted NEW PRO Node; disabled by default
}

func NewServer(fleet *telemetry.Store, registry *noderegistry.Registry, ownerKey string) (*Server, error) {
	if fleet == nil || registry == nil || len(ownerKey) < 32 {
		return nil, errors.New("telemetry Store, private registry and >=32-byte lab owner token required")
	}
	return &Server{fleet: fleet, registry: registry, ownerKey: ownerKey}, nil
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func bearer(r *http.Request) string {
	const prefix = "Bearer "
	token := r.Header.Get("Authorization")
	if !strings.HasPrefix(token, prefix) {
		return ""
	}
	return strings.TrimPrefix(token, prefix)
}
func (s *Server) isOwner(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(s.ownerKey)) == 1
}
func decode(w http.ResponseWriter, r *http.Request, max int64, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return err
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing data")
	}
	return nil
}

// Cached telemetry never queries financial history or contacts Agents.
func (s *Server) snapshot() telemetry.Fleet {
	fleet := s.fleet.Snapshot()
	known := make(map[string]bool, len(fleet.Nodes))
	authoritative := make(map[string]noderegistry.NodeInfo)
	for _, n := range s.registry.List() {
		authoritative[n.NodeID] = n
	}
	for i := range fleet.Nodes {
		node := &fleet.Nodes[i]
		if paired, ok := authoritative[node.NodeID]; ok {
			node.Name = paired.Name
		}
		known[node.NodeID] = true
	}
	for _, n := range authoritative {
		if known[n.NodeID] {
			continue
		}
		fleet.Nodes = append(fleet.Nodes, telemetry.Node{NodeID: n.NodeID, Name: n.Name, Status: "offline"})
		fleet.Offline++
	}
	sort.Slice(fleet.Nodes, func(i, j int) bool { return fleet.Nodes[i].NodeID < fleet.Nodes[j].NodeID })
	fleet.Total = len(fleet.Nodes)
	return fleet
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		dataPlane := "not_enabled"
		if s.inboundExecution != nil {
			dataPlane = "managed_inbounds_enabled"
		}
		respond(w, http.StatusOK, map[string]string{"status": "ok", "mode": "isolated-lab", "data_plane": dataPlane})
	})
	mux.HandleFunc("GET /api/v1/fleet", func(w http.ResponseWriter, _ *http.Request) { respond(w, http.StatusOK, s.snapshot()) })
	s.attachEnrollment(mux)
	s.attachNodeManager(mux)
	s.attachHubMonitor(mux)
	s.attachReplacement(mux)
	s.attachConfigStaging(mux)
	s.attachLabRevisions(mux)
	s.attachBusiness(mux)
	s.attachBrowserAuth(mux)
	s.attachWorkspace(mux)
	s.attachRepresentativeControls(mux)
	s.attachSellerQuotaV2(mux)
	s.attachOwnerRepLedgerReports(mux)
	s.attachCatalog(mux)
	s.attachCatalogPolicy(mux)
	s.attachGroups(mux)
	s.attachClientLifecycle(mux)
	s.attachClientProfileEdits(mux)
	s.attachNativeInbounds(mux)
	s.attachNativeInboundPlans(mux)
	s.attachInboundExecution(mux)
	s.attachInboundExecutionSubscriptions(mux)
	s.attachInboundSecurity(mux)
	s.attachNativeInboundPlanPromotions(mux)
	s.attachInboundInitialHealth(mux)
	s.attachNativeRouteDrafts(mux)
	s.attachRouteReadinessP0G(mux)
	s.attachRouteTransactions(mux)
	s.attachMeteredRouteTransactions(mux)
	s.attachOwnerNativeCanary(mux)
	s.attachRuntimeReports(mux)
	s.attachInboundLifecycle(mux)
	s.attachUsageRead(mux)
	s.attachLabSubscriptions(mux)
	s.attachConsumerManifests(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

// Only the strict Agent allowlist is routed on the separately verified TLS listener.
func (s *Server) AgentHandler() http.Handler {
	local := s.Handler()
	usage := s.nativeUsageHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 {
			respond(w, http.StatusUpgradeRequired, map[string]string{"error": "tls13_required"})
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/agent/route-transactions/poll" || r.URL.Path == "/api/v1/agent/route-transactions/receipt") {
			s.handleRouteTransactionAgent(w, r)
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/agent/inbound-security/poll" || r.URL.Path == "/api/v1/agent/inbound-security/result") {
			s.handleInboundProbeAgent(w, r)
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/agent/inbound-execution/poll" || r.URL.Path == "/api/v1/agent/inbound-execution/register" || r.URL.Path == "/api/v1/agent/inbound-execution/usage" || r.URL.Path == "/api/v1/agent/inbound-execution/observation") {
			s.handleInboundExecutionAgent(w, r)
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/agent/metered-health/poll" || r.URL.Path == "/api/v1/agent/metered-health/receipt") {
			s.handleMeteredHealthAgent(w, r)
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/agent/metered-route-transactions/poll" || r.URL.Path == "/api/v1/agent/metered-route-transactions/receipt" || r.URL.Path == "/api/v1/agent/metered-route-transactions/check" || r.URL.Path == "/api/v1/agent/metered-route-transactions/register") {
			s.handleMeteredRouteTransactionAgent(w, r)
			return
		}
		if r.URL.Path == "/api/v1/agent/native-usage" || r.URL.Path == "/api/v1/agent/native-usage/interrupted" || r.URL.Path == "/api/v1/agent/native-usage/settled" {
			if usage == nil {
				http.NotFound(w, r)
			} else {
				usage.ServeHTTP(w, r)
			}
			return
		}
		if (r.Method == http.MethodGet && r.URL.Path == "/api/v1/agent/native-metered-runtime") ||
			(r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/native-metered-register") {
			s.handleRemoteMetered(w, r)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/agent/health" {
			dataPlane := "disabled"
			if s.inboundExecution != nil {
				dataPlane = "managed_inbounds_enabled"
			}
			respond(w, http.StatusOK, map[string]string{"status": "ok", "mode": "new-node-lab", "data_plane": dataPlane})
			return
		}
		if !((r.Method == http.MethodPost && (r.URL.Path == "/api/v1/agent/enroll" || r.URL.Path == "/api/v1/agent/config-stage-report" || r.URL.Path == "/api/v1/agent/native-runtime-report" || r.URL.Path == "/api/v1/agent/pulse" || r.URL.Path == "/api/v1/agent/heartbeat" || r.URL.Path == "/api/v1/agent/replacement-report")) || (r.Method == http.MethodGet && (r.URL.Path == "/api/v1/agent/replacement-task" || r.URL.Path == "/api/v1/agent/config-stage" || r.URL.Path == "/api/v1/agent/native-runtime"))) {
			http.NotFound(w, r)
			return
		}
		local.ServeHTTP(w, r)
	})
}
