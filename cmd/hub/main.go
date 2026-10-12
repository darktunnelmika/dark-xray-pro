package main

import (
    "context"
    "errors"
    "flag"
    "log"
    "net"
    "net/http"
    "os"
    "os/signal"
    "path/filepath"
    "strings"
    "syscall"
    "time"

    "github.com/darktunnelmika/dark-xray-pro/internal/api"
    "github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
    "github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func loopbackAddr(addr string) bool {
    host, _, err := net.SplitHostPort(addr)
    if err != nil { return false }
    ip := net.ParseIP(host)
    return ip != nil && ip.IsLoopback()
}

func main() {
    address := flag.String("listen", "127.0.0.1:2087", "local lab HTTP listener")
    static := flag.String("static", "web/dist", "optional compiled frontend directory")
    registryPath := flag.String("registry", "./lab-state/registry.json", "private durable LAB node enrollment file")
    flag.Parse()

    if os.Getenv("DARK_PRO_LAB_MODE") != "1" {
        log.Fatal("DARK XRAY PRO is not production-ready; set DARK_PRO_LAB_MODE=1 for isolated testing only")
    }
    if !loopbackAddr(*address) {
        log.Fatal("lab HUB refuses non-loopback listeners; use an SSH tunnel")
    }
    store := telemetry.NewStore(telemetry.DefaultOfflineAfter, nil)
    registry, err := noderegistry.Open(*registryPath, nil)
    if err != nil { log.Fatal("cannot load private node registry: ", err) }
    service, err := api.NewServer(store, registry, os.Getenv("DARK_PRO_LAB_TOKEN"))
    if err != nil { log.Fatal(err) }

    apiHandler := service.Handler()
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "/api/") {
            apiHandler.ServeHTTP(w, r)
            return
        }
        // Static assets are optional; missing frontend must never look like a healthy UI.
        index := filepath.Join(*static, "index.html")
        if _, err := os.Stat(index); err != nil {
            http.Error(w, "Frontend not built: run npm install && npm run build in web/", http.StatusServiceUnavailable)
            return
        }
        if r.URL.Path != "/" && filepath.Ext(r.URL.Path) == "" {
            r2 := r.Clone(r.Context())
            r2.URL.Path = "/"
            http.FileServer(http.Dir(*static)).ServeHTTP(w, r2)
            return
        }
        http.FileServer(http.Dir(*static)).ServeHTTP(w, r)
    })
    server := &http.Server{
        Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
        IdleTimeout: 45 * time.Second, WriteTimeout: 15 * time.Second,
    }
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    go func() {
        <-ctx.Done()
        graceful, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        _ = server.Shutdown(graceful)
    }()
    log.Printf("DARK XRAY PRO isolated LAB listening on %s (NO customer data plane)", *address)
    if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Fatal(err)
    }
}
