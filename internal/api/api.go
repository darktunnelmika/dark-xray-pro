package api

import (
    "crypto/subtle"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "strings"

    "github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

const maxHeartbeatBytes = 4096

// Lab API only. Production will replace its lab token with per-Node enrollment
// and mutual authentication; do NOT deploy this endpoint to a public listener.
type Server struct {
    fleet *telemetry.Store
    token string
}

func NewServer(store *telemetry.Store, token string) (*Server, error) {
    if store == nil || len(token) < 32 {
        return nil, errors.New("a telemetry Store and >=32-byte lab token are required")
    }
    return &Server{fleet: store, token: token}, nil
}

func respond(w http.ResponseWriter, code int, body any) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(code)
    _ = json.NewEncoder(w).Encode(body)
}

func (s *Server) Handler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
        respond(w, http.StatusOK, map[string]string{
            "status": "ok", "mode": "isolated-lab", "data_plane": "not_enabled",
        })
    })
    mux.HandleFunc("GET /api/v1/fleet", func(w http.ResponseWriter, _ *http.Request) {
        respond(w, http.StatusOK, s.fleet.Snapshot())
    })
    mux.HandleFunc("POST /api/v1/agent/heartbeat", func(w http.ResponseWriter, r *http.Request) {
        supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
        if supplied == r.Header.Get("Authorization") ||
           subtle.ConstantTimeCompare([]byte(supplied), []byte(s.token)) != 1 {
            respond(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
            return
        }
        r.Body = http.MaxBytesReader(w, r.Body, maxHeartbeatBytes)
        defer r.Body.Close()
        decoder := json.NewDecoder(r.Body)
        decoder.DisallowUnknownFields()
        var sample telemetry.Sample
        if err := decoder.Decode(&sample); err != nil {
            respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
            return
        }
        var trailing any
        if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
            respond(w, http.StatusBadRequest, map[string]string{"error": "trailing_data"})
            return
        }
        if err := s.fleet.Record(sample); err != nil {
            if errors.Is(err, telemetry.ErrOutOfOrder) {
                respond(w, http.StatusConflict, map[string]string{"error": "out_of_order"})
            } else {
                respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_telemetry"})
            }
            return
        }
        w.WriteHeader(http.StatusNoContent)
    })
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Cache-Control", "no-store")
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Referrer-Policy", "no-referrer")
        mux.ServeHTTP(w, r)
    })
}
