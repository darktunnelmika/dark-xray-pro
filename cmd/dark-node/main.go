// Command dark-node is the v2 Node service: it runs stock Xray with the last
// good config, meters users through the Xray stats API, enforces limits
// locally and syncs with the HUB when it can. A HUB outage never stops
// customer traffic.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/nodeagent"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func main() {
	hubURL := flag.String("hub", "", "HUB base URL, https://host:port")
	nodeID := flag.String("node-id", "", "logical node id assigned by the HUB")
	tokenFile := flag.String("token-file", "/etc/dark-node/token", "file holding the per-node token")
	caFile := flag.String("hub-ca", "/etc/dark-node/hub-ca.pem", "PEM CA that signs the HUB certificate (empty = system roots)")
	serverName := flag.String("hub-name", "", "TLS server name for the HUB (default: host from -hub)")
	xrayBin := flag.String("xray", "/usr/local/bin/xray", "stock Xray binary")
	stateDir := flag.String("state", "/var/lib/dark-node", "durable state directory")
	statsAddr := flag.String("stats", "127.0.0.1:10085", "Xray API (StatsService) address from the HUB config")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("dark-node", nodeagent.Version)
		return
	}

	if *hubURL == "" || *nodeID == "" {
		log.Fatal("-hub and -node-id are required")
	}
	tok, err := os.ReadFile(*tokenFile)
	if err != nil {
		log.Fatalf("token: %v", err)
	}
	hub, err := nodeagent.NewHTTPHub(*hubURL, *nodeID, strings.TrimSpace(string(tok)), *caFile, *serverName)
	if err != nil {
		log.Fatal(err)
	}
	sup, err := nodeagent.NewSupervisor(*xrayBin, filepath.Join(*stateDir, "xray"), log.Printf)
	if err != nil {
		log.Fatal(err)
	}
	stats := &nodeagent.XrayStats{Addr: *statsAddr}
	defer stats.Close()
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	selfSHA, _ := nodeagent.FileSHA(exe)
	agent, err := nodeagent.New(nodeagent.Config{NodeID: *nodeID, StateDir: *stateDir, Logf: log.Printf, AgentSHA: selfSHA}, hub, sup, stats)
	if err != nil {
		log.Fatal(err)
	}
	users := &nodeagent.XrayUsers{Addr: *statsAddr}
	defer users.Close()
	agent.SetUserAPI(users)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	agent.SetTask(wire.TaskSelfUpdate, nodeagent.SelfUpdateTask(hub, exe))
	// After a self-update: stop cleanly; systemd (Restart=always) starts
	// the new binary.
	agent.SetRestart(func() {
		log.Printf("dark-node updated, restarting")
		stop()
	})
	go sup.Run(ctx)
	log.Printf("dark-node %s started: node=%s hub=%s", nodeagent.Version, *nodeID, *hubURL)
	agent.Run(ctx)
}
