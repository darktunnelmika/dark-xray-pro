package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

type Server struct {
	fleet    *telemetry.Store
	registry *noderegistry.Registry
	ownerKey string
}

// This is a localhost-only lab interface. Remote Nodes must NOT be allowed
// until the future authenticated TLS control plane replaces this lab API.
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
	if !strings.HasPrefix(token, prefix) { return "" }
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
	if err := d.Decode(dest); err != nil { return err }
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing data")
	}
	return nil
}

// O(number-of-nodes) cached telemetry; never reads a ledger or contacts Agents.
// A registered Node without fresh telemetry remains visible as Offline.
func (s *Server) snapshot() telemetry.Fleet {
	fleet := s.fleet.Snapshot()
	known := make(map[string]bool, len(fleet.Nodes))
	for _, n := range fleet.Nodes { known[n.NodeID] = true }
	for _, n := range s.registry.List() {
		if known[n.NodeID] { continue }
		fleet.Nodes = append(fleet.Nodes, telemetry.Node{
			NodeID: n.NodeID, Name: n.Name, Status: "offline",
		})
		fleet.Offline++
	}
	sort.Slice(fleet.Nodes, func(i,j int) bool { return fleet.Nodes[i].NodeID < fleet.Nodes[j].NodeID })
	fleet.Total = len(fleet.Nodes)
	return fleet
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, map[string]string{
			"status": "ok", "mode": "isolated-lab", "data_plane": "not_enabled",
		})
	})
	mux.HandleFunc("GET /api/v1/fleet", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, s.snapshot())
	})
	s.attachEnrollment(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}
