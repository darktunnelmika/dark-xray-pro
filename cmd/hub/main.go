package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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

func separateAgentPort(agent, owner string) error {
	_, a, err := net.SplitHostPort(agent)
	if err != nil { return err }
	_, b, err := net.SplitHostPort(owner)
	if err != nil { return err }
	port, err := strconv.Atoi(a)
	if err != nil || port < 1024 || port > 65535 || a == b {
		return errors.New("Agent TLS must use an explicit unprivileged port distinct from the owner UI")
	}
	return nil
}

// A separately opened public TLS listener must not load a world-readable
// private key or follow a symlink to an unrelated host secret.
func privateTLSKey(path string) error {
	info, err := os.Lstat(path)
	if err != nil { return err }
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("Agent TLS key must be a regular, private (0600/0400) file without symlink")
	}
	return nil
}

func main() {
	address := flag.String("listen", "127.0.0.1:2087", "loopback-only LAB owner UI listener")
	static := flag.String("static", "web/dist", "optional compiled frontend directory")
	registryPath := flag.String("registry", "./lab-state/registry.json", "private durable LAB node enrollment")
	agentListen := flag.String("agent-tls-listen", "", "optional dedicated HTTPS LAB Node listener (disabled by default)")
	agentCert := flag.String("agent-tls-cert", "", "certificate with valid IP/DNS SAN for new LAB Node listener")
	agentKey := flag.String("agent-tls-key", "", "private key for dedicated LAB Node listener")
	flag.Parse()

	if os.Getenv("DARK_PRO_LAB_MODE") != "1" {
		log.Fatal("DARK XRAY PRO is not production-ready; set DARK_PRO_LAB_MODE=1 for isolated testing only")
	}
	if !loopbackAddr(*address) {
		log.Fatal("lab HUB refuses non-loopback owner listeners; use an SSH tunnel")
	}
	var agentTLS *http.Server
	if *agentListen != "" {
		if os.Getenv("DARK_PRO_ENABLE_REMOTE_AGENT") != "1" {
			log.Fatal("remote LAB Agent listener requires explicit DARK_PRO_ENABLE_REMOTE_AGENT=1")
		}
		if err := separateAgentPort(*agentListen, *address); err != nil { log.Fatal(err) }
		if *agentCert == "" || *agentKey == "" {
			log.Fatal("dedicated Agent TLS certificate and key are both required")
		}
		if err := privateTLSKey(*agentKey); err != nil { log.Fatal(err) }
		if _, err := tls.LoadX509KeyPair(*agentCert, *agentKey); err != nil {
			log.Fatal("invalid Agent TLS keypair: ", err)
		}
	} else if *agentCert != "" || *agentKey != "" {
		log.Fatal("Agent TLS cert/key cannot be configured without -agent-tls-listen")
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
	admin := &http.Server{
		Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 45 * time.Second, WriteTimeout: 15 * time.Second,
	}
	if *agentListen != "" {
		agentTLS = &http.Server{
			Addr: *agentListen, Handler: service.AgentHandler(),
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13},
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
			MaxHeaderBytes: 16 * 1024,
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 2)
	go func() { errs <- admin.ListenAndServe() }()
	if agentTLS != nil {
		go func() { errs <- agentTLS.ListenAndServeTLS(*agentCert, *agentKey) }()
	}
	log.Printf("DARK XRAY PRO LAB owner UI: %s (loopback; no customer data plane)", *address)
	if agentTLS != nil {
		log.Printf("DARK XRAY PRO isolated HTTPS Agent transport enabled on %s (no owner API)", *agentListen)
	}
	var unexpected error
	select {
	case <-ctx.Done():
	case unexpected = <-errs:
		if errors.Is(unexpected, http.ErrServerClosed) { unexpected = nil }
	}
	graceful, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = admin.Shutdown(graceful)
	if agentTLS != nil { _ = agentTLS.Shutdown(graceful) }
	if unexpected != nil { log.Fatal(fmt.Errorf("LAB listener terminated: %w", unexpected)) }
}
