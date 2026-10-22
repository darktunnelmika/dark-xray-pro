package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubmonitor"
	"github.com/darktunnelmika/dark-xray-pro/internal/labrevision"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/nodebootstrap"
	"github.com/darktunnelmika/dark-xray-pro/internal/nodehistory"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
	"io"
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
)

func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func separateAgentPort(agent, owner string) error {
	_, a, err := net.SplitHostPort(agent)
	if err != nil {
		return err
	}
	_, b, err := net.SplitHostPort(owner)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(a)
	if err != nil || port < 1024 || port > 65535 || a == b {
		return errors.New("Agent TLS must use an explicit unprivileged port distinct from the owner UI")
	}
	return nil
}
func privateTLSKey(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("Agent TLS key must be a regular, private (0600/0400) file without symlink")
	}
	return nil
}
func main() {
	address := flag.String("listen", "127.0.0.1:2087", "loopback-only LAB owner UI listener")
	static := flag.String("static", "", "optional development UI override; release uses its embedded UI")
	registryPath := flag.String("registry", "./lab-state/registry.json", "private durable LAB node enrollment")
	replacementsPath := flag.String("replacements", "./lab-state/replacements.json", "private nonactivating replacement journal")
	stagingPath := flag.String("staging", "./lab-state/private-staging/configs.json", "optional encrypted Reality candidate journal")
	businessPath := flag.String("business-db", "", "optional private PRO-only business SQLite path (explicit opt-in)")
	labRevisionsPath := flag.String("lab-revisions", "./lab-state/private-staging/lab-revisions.json", "optional LAB-only encrypted revision selection and rollback journal")
	nodeHistoryPath := flag.String("node-history-db", "", "optional NEW PRO-only bounded monitoring SQLite path (requires explicit opt-in)")
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
		if err := separateAgentPort(*agentListen, *address); err != nil {
			log.Fatal(err)
		}
		if *agentCert == "" || *agentKey == "" {
			log.Fatal("dedicated Agent TLS certificate and key are both required")
		}
		if err := privateTLSKey(*agentKey); err != nil {
			log.Fatal(err)
		}
		if _, err := tls.LoadX509KeyPair(*agentCert, *agentKey); err != nil {
			log.Fatal("invalid Agent TLS keypair: ", err)
		}
	} else if *agentCert != "" || *agentKey != "" {
		log.Fatal("Agent TLS cert/key cannot be configured without -agent-tls-listen")
	}
	store := telemetry.NewStore(telemetry.DefaultOfflineAfter, nil)
	registry, err := noderegistry.Open(*registryPath, nil)
	if err != nil {
		log.Fatal("cannot load private node registry: ", err)
	}
	service, err := api.NewServer(store, registry, os.Getenv("DARK_PRO_LAB_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}
	// Sample only this NEW PRO HUB's local Linux host. No remote Agent and no
	// business accounting operation is consulted for these observations.
	hubMonitor := hubmonitor.New(nil, nil, nil)
	if err := service.EnableHubMonitor(hubMonitor); err != nil {
		log.Fatal(err)
	}
	// Export the matching release Agent only to an authenticated owner.
	executable, executableErr := os.Executable()
	if executableErr == nil {
		artifact, artifactErr := nodebootstrap.Open(filepath.Join(filepath.Dir(executable), "dark-pro-agent"))
		if artifactErr == nil {
			if err := service.EnableNodeInstaller(artifact); err != nil {
				log.Fatal(err)
			}
		} else {
			log.Printf("matching NEW PRO Agent bundle unavailable; Node installer download disabled")
		}
	}
	if *agentListen == "" {
		service.SetNodeTransportScope("disabled")
	} else if loopbackAddr(*agentListen) {
		service.SetNodeTransportScope("loopback_only")
	} else {
		// A listener does not prove reachable/verified external transport.
		service.SetNodeTransportScope("external_tls_unverified")
	}
	if os.Getenv("DARK_PRO_ENABLE_NODE_HISTORY") == "1" {
		clean := filepath.Clean(*nodeHistoryPath)
		base := "/var/lib/dark-xray-pro-lab/telemetry"
		relative, pathErr := filepath.Rel(base, clean)
		if *nodeHistoryPath == "" || pathErr != nil || !filepath.IsAbs(clean) || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || filepath.Ext(clean) != ".sqlite" {
			log.Fatal("PRO host history path must be a new private .sqlite file under /var/lib/dark-xray-pro-lab/telemetry")
		}
		history, openErr := nodehistory.Open(clean, nil)
		if openErr != nil {
			log.Fatal("private PRO nonbilling host history unavailable: ", openErr)
		}
		defer history.Close()
		if err := service.EnableNodeHistory(history); err != nil {
			log.Fatal(err)
		}
	} else if *nodeHistoryPath != "" {
		log.Fatal("host history database supplied without DARK_PRO_ENABLE_NODE_HISTORY=1")
	}

	ops, err := replacement.Open(*replacementsPath, nil)
	if err != nil {
		log.Fatal("replacement journal unavailable: ", err)
	}
	if err := service.EnableReplacementLab(ops); err != nil {
		log.Fatal(err)
	}
	// Only explicit NEW PRO business storage may be opened; never legacy paths.
	if os.Getenv("DARK_PRO_ENABLE_BUSINESS_CORE") == "1" {
		clean := filepath.Clean(*businessPath)
		relative, pathErr := filepath.Rel("/var/lib/dark-xray-pro-lab", clean)
		if pathErr != nil || !filepath.IsAbs(clean) || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || !strings.HasSuffix(clean, ".sqlite") {
			log.Fatal("PRO business database must be a NEW private .sqlite file under /var/lib/dark-xray-pro-lab")
		}
		biz, err := business.Open(clean)
		if err != nil {
			log.Fatal("PRO business database initialization failed: ", err)
		}
		defer biz.Close()
		if os.Getenv("DARK_PRO_ENABLE_USAGE_LEDGER") == "1" {
			if err := biz.EnableUsageLedger(context.Background()); err != nil {
				log.Fatal("private usage ledger unavailable: ", err)
			}
		}
		if err := service.EnableBusinessLab(biz); err != nil {
			log.Fatal(err)
		}
		if os.Getenv("DARK_PRO_ENABLE_NATIVE_INBOUNDS") == "1" {
			if *agentListen == "" {
				log.Fatal("native runtime requires a separate verified TLS Agent listener")
			}
			inboundKey, keyErr := hex.DecodeString(os.Getenv("DARK_PRO_NATIVE_INBOUND_KEY_HEX"))
			if keyErr != nil || len(inboundKey) != 32 {
				log.Fatal("native inbounds require an independent private 32-byte key")
			}
			if err := service.EnableNativeInbounds(inboundKey); err != nil {
				log.Fatal(err)
			}
		}
	} else if *businessPath != "" {
		log.Fatal("business database path supplied without explicit DARK_PRO_ENABLE_BUSINESS_CORE=1")
	}
	if os.Getenv("DARK_PRO_ENABLE_USAGE_LEDGER") == "1" && os.Getenv("DARK_PRO_ENABLE_BUSINESS_CORE") != "1" {
		log.Fatal("usage accounting requires explicit independent PRO business storage")
	}
	if os.Getenv("DARK_PRO_ENABLE_NATIVE_INBOUNDS") == "1" && os.Getenv("DARK_PRO_ENABLE_BUSINESS_CORE") != "1" {
		log.Fatal("native inbounds require explicit new PRO business database")
	}
	if os.Getenv("DARK_PRO_ENABLE_REMOTE_METERED_ADMISSION") == "1" {
		if os.Getenv("DARK_PRO_LAB_MODE") != "1" || os.Getenv("DARK_PRO_ENABLE_USAGE_LEDGER") != "1" ||
			os.Getenv("DARK_PRO_ENABLE_NATIVE_INBOUNDS") != "1" {
			log.Fatal("remote metered runtime requires NEW PRO LAB with private usage ledger and native Inbounds")
		}
		if err := service.EnableRemoteMeteredNode(os.Getenv("DARK_PRO_NATIVE_METERED_NODE_ID")); err != nil {
			log.Fatal("remote metered Node opt-in refused: ", err)
		}
	} else if os.Getenv("DARK_PRO_NATIVE_METERED_NODE_ID") != "" {
		log.Fatal("remote metered Node ID present without explicit enable switch")
	}
	if os.Getenv("DARK_PRO_ENABLE_ROUTE_TRANSACTIONS") == "1" {
		if *agentListen == "" || os.Getenv("DARK_PRO_ENABLE_REMOTE_AGENT") != "1" {
			log.Fatal("route transaction queue requires dedicated pinned Agent TLS")
		}
		if err := service.EnableRouteTransactions(os.Getenv("DARK_PRO_ROUTE_NODE_ID"), os.Getenv("DARK_PRO_ROUTE_INSTALLATION_ID"), os.Getenv("DARK_PRO_ROUTE_XRAY_SHA256")); err != nil {
			log.Fatal("empty loopback route Node claim refused: ", err)
		}
	} else if os.Getenv("DARK_PRO_ROUTE_NODE_ID") != "" || os.Getenv("DARK_PRO_ROUTE_INSTALLATION_ID") != "" || os.Getenv("DARK_PRO_ROUTE_XRAY_SHA256") != "" {
		log.Fatal("route transaction scope supplied without explicit enable switch")
	}
	if os.Getenv("DARK_PRO_ENABLE_METERED_ROUTE_TRANSACTIONS") == "1" {
		if os.Getenv("DARK_PRO_LAB_MODE") != "1" || os.Getenv("DARK_PRO_ENABLE_USAGE_LEDGER") != "1" || os.Getenv("DARK_PRO_ENABLE_NATIVE_INBOUNDS") != "1" || *agentListen == "" || os.Getenv("DARK_PRO_ENABLE_REMOTE_AGENT") != "1" {
			log.Fatal("metered route transactions require isolated PRO LAB, usage ledger and dedicated Agent TLS")
		}
		nodes, err := meteredRouteNodes(os.Getenv("DARK_PRO_METERED_ROUTE_NODES_JSON"), os.Getenv("DARK_PRO_METERED_ROUTE_NODE_ID"), os.Getenv("DARK_PRO_METERED_ROUTE_INSTALLATION_ID"), os.Getenv("DARK_PRO_METERED_ROUTE_XRAY_SHA256"))
		if err != nil {
			log.Fatal("metered route allowlist refused: ", err)
		}
		if err := service.EnableMeteredRouteNodes(nodes); err != nil {
			log.Fatal("independent metered loopback channel refused: ", err)
		}
	} else if os.Getenv("DARK_PRO_METERED_ROUTE_NODE_ID") != "" || os.Getenv("DARK_PRO_METERED_ROUTE_INSTALLATION_ID") != "" || os.Getenv("DARK_PRO_METERED_ROUTE_XRAY_SHA256") != "" || os.Getenv("DARK_PRO_METERED_ROUTE_NODES_JSON") != "" {
		log.Fatal("metered route scope supplied without explicit opt-in")
	}
	if os.Getenv("DARK_PRO_ENABLE_METERED_HEALTH") == "1" {
		if os.Getenv("DARK_PRO_ENABLE_METERED_ROUTE_TRANSACTIONS") != "1" {
			log.Fatal("health requires approved LAB metered channels")
		}
		var verifier managedxray.ProtocolHealthEndpoint
		raw := os.Getenv("DARK_PRO_HEALTH_VERIFIER_JSON")
		if len(raw) > 100000 {
			log.Fatal("health verifier configuration too large")
		}
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		var extra any
		if decoder.Decode(&verifier) != nil || decoder.Decode(&extra) != io.EOF {
			log.Fatal("invalid health verifier configuration")
		}
		if err := service.EnableMeteredHealth(verifier); err != nil {
			log.Fatal("health configuration refused: ", err)
		}
	} else if os.Getenv("DARK_PRO_HEALTH_VERIFIER_JSON") != "" {
		log.Fatal("health verifier supplied without explicit enable switch")
	}
	if os.Getenv("DARK_PRO_ENABLE_METERED_HEALTH_ROUNDS") == "1" {
		if os.Getenv("DARK_PRO_ENABLE_METERED_HEALTH") != "1" {
			log.Fatal("health rounds require the explicitly enabled pinned health transport")
		}
		if err := service.EnableMeteredHealthRounds(); err != nil {
			log.Fatal("health rounds configuration refused: ", err)
		}
	}
	if os.Getenv("DARK_PRO_ENABLE_CONFIG_STAGE") == "1" {
		if *agentListen == "" || os.Getenv("DARK_PRO_ENABLE_REMOTE_AGENT") != "1" {
			log.Fatal("encrypted config staging requires dedicated verified remote Agent TLS listener")
		}
		key, err := hex.DecodeString(os.Getenv("DARK_PRO_STAGE_KEY_HEX"))
		if err != nil || len(key) != 32 {
			log.Fatal("staging requires a privately supplied 32-byte hex AES key")
		}
		stage, err := configstage.Open(*stagingPath, key, nil)
		if err != nil {
			log.Fatal("private encrypted stage registry unavailable: ", err)
		}
		if err := service.EnableConfigStaging(stage); err != nil {
			log.Fatal(err)
		}
		if os.Getenv("DARK_PRO_ENABLE_LAB_REVISION_ROLLBACK") == "1" {
			material := append([]byte("DARK-PRO-LABREV-AES-v1:"), key...)
			revisionKey := sha256.Sum256(material)
			versions, err := labrevision.Open(*labRevisionsPath, revisionKey[:], nil)
			if err != nil {
				log.Fatal("private LAB revision journal unavailable: ", err)
			}
			if err := service.EnableLabRevisions(versions); err != nil {
				log.Fatal(err)
			}
		}
	}
	apiHandler := service.BrowserHandler()
	ui := webui.Embedded()
	if *static != "" {
		ui = webui.Directory(*static)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		ui.ServeHTTP(w, r)
	})
	admin := &http.Server{Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 45 * time.Second, WriteTimeout: 15 * time.Second}
	if *agentListen != "" {
		agentTLS = &http.Server{Addr: *agentListen, Handler: service.AgentHandler(), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16 * 1024}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go hubMonitor.Run(ctx)
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
		if errors.Is(unexpected, http.ErrServerClosed) {
			unexpected = nil
		}
	}
	graceful, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = admin.Shutdown(graceful)
	if agentTLS != nil {
		_ = agentTLS.Shutdown(graceful)
	}
	if unexpected != nil {
		log.Fatal(fmt.Errorf("LAB listener terminated: %w", unexpected))
	}
}
