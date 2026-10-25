package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/darktunnelmika/dark-xray-pro/internal/agentidentity"
	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	"github.com/darktunnelmika/dark-xray-pro/internal/hostmetrics"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

func checkLabEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if endpoint.Scheme != "http" || endpoint.Host != "127.0.0.1:2087" || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("lab agent requires http://127.0.0.1:2087; remote Nodes are disabled")
	}
	return endpoint, nil
}

func consumerEndpointPolicy(enabled, health, metered bool, nodeID, raw string) ([]managedxray.ConsumerEndpointApproval, error) {
	if !enabled {
		if raw != "" {
			return nil, errors.New("consumer endpoint policy requires explicit enablement")
		}
		return nil, nil
	}
	if !health || !metered {
		return nil, errors.New("consumer endpoint proof requires metered health and the isolated metered executor")
	}
	approvals, err := managedxray.ParseConsumerEndpointApprovalsJSON(raw)
	if err != nil {
		return nil, errors.New("invalid consumer endpoint startup policy")
	}
	for _, approval := range approvals {
		if approval.NodeID != nodeID {
			return nil, errors.New("consumer endpoint policy must name this Node")
		}
	}
	return approvals, nil
}

// Enrollment retries preserve the Agent-generated installation and secret.
func enroll(ctx context.Context, client *http.Client, endpoint string, identity agentidentity.Identity) error {
	data, err := json.Marshal(map[string]string{"pair_code": identity.PairCode, "node_id": identity.NodeID, "installation_id": identity.InstallationID, "agent_token": identity.AgentToken})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/enroll", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
		return fmt.Errorf("pairing refused with HTTP %d; identity remains intact for retry", res.StatusCode)
	}
	var ack struct {
		Paired         bool   `json:"paired"`
		NodeID         string `json:"node_id"`
		InstallationID string `json:"installation_id"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1024)).Decode(&ack); err != nil {
		return err
	}
	if !ack.Paired || ack.NodeID != identity.NodeID || ack.InstallationID != identity.InstallationID {
		return errors.New("pairing response identity mismatch")
	}
	return nil
}
func sendPulse(ctx context.Context, client *http.Client, endpoint, token string, pulse telemetry.Pulse) error {
	payload, err := json.Marshal(pulse)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/pulse", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HUB pulse rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}
func transmit(ctx context.Context, client *http.Client, endpoint, token string, sample telemetry.Sample) error {
	payload, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/heartbeat", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HUB rejected telemetry: HTTP %d", resp.StatusCode)
	}
	return nil
}
func main() {
	nodeID := flag.String("id", "", "unique lab Node ID")
	name := flag.String("name", "", "display name")
	hub := flag.String("hub", "http://127.0.0.1:2087", "local lab HUB")
	identityPath := flag.String("identity-file", "", "private local Agent enrollment identity file")
	probeOnly := flag.Bool("probe-only", false, "verify pinned Agent-only HUB TLS without loading an identity or pairing")
	enrollOnly := flag.Bool("enroll-only", false, "pair the new Agent identity and exit; do not start telemetry or native runtime")
	stageXrayBinary := flag.String("stage-xray-binary", "", "explicit trusted Xray binary for OFFLINE validation only")
	stageXraySHA := flag.String("stage-xray-sha256", "", "SHA256 of separately trusted Xray binary")
	pin := flag.String("hub-cert-sha256", "", "required SHA256 fingerprint of the verified HTTPS HUB leaf certificate")
	caFile := flag.String("hub-ca-file", "", "optional trusted LAB CA certificate bundle")
	runtimeDir := flag.String("managed-runtime-dir", "", "NEW private PRO-owned runtime directory; never an old Xray directory")
	runtimeIP := flag.String("managed-listen-ip", "", "explicit assigned local IP for NEW native Xray listeners")
	flag.Parse()
	if os.Getenv("DARK_PRO_LAB_MODE") != "1" {
		log.Fatal("node agent is lab-only; set DARK_PRO_LAB_MODE=1")
	}
	if *nodeID == "" {
		log.Fatal("specify -id for a new LAB node; do not reuse old node identities")
	}
	routeEnabled := os.Getenv("DARK_PRO_ENABLE_ROUTE_TRANSACTIONS") == "1"
	meteredRouteEnabled := os.Getenv("DARK_PRO_ENABLE_METERED_ROUTE_TRANSACTIONS") == "1"
	healthEnabled := os.Getenv("DARK_PRO_ENABLE_METERED_HEALTH") == "1"
	consumerApprovals, policyErr := consumerEndpointPolicy(os.Getenv("DARK_PRO_ENABLE_CONSUMER_ENDPOINTS") == "1", healthEnabled, meteredRouteEnabled, *nodeID, os.Getenv("DARK_PRO_CONSUMER_ENDPOINTS_JSON"))
	if policyErr != nil {
		log.Fatal(policyErr)
	}
	remote := strings.HasPrefix(*hub, "https://")
	if remote && os.Getenv("DARK_PRO_ENABLE_REMOTE_AGENT") != "1" {
		log.Fatal("remote Node connection requires DARK_PRO_ENABLE_REMOTE_AGENT=1 in the isolated LAB")
	}
	if !remote {
		if _, err := checkLabEndpoint(*hub); err != nil {
			log.Fatal(err)
		}
	}
	if *name == "" {
		*name = *nodeID
	}
	if len(*name) > 80 {
		log.Fatal("name too long")
	}
	var boot [16]byte
	if _, err := rand.Read(boot[:]); err != nil {
		log.Fatal(err)
	}
	bootID := hex.EncodeToString(boot[:])
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var caPEM []byte
	var err error
	if *caFile != "" {
		caPEM, err = os.ReadFile(*caFile)
		if err != nil {
			log.Fatal("cannot load trusted Agent CA: ", err)
		}
	}
	client, endpoint, err := agenttransport.New(agenttransport.Config{Endpoint: *hub, PinSHA256: *pin, CABundle: caPEM})
	if err != nil {
		log.Fatal("unsafe HUB transport: ", err)
	}
	if *probeOnly && !remote {
		log.Fatal("probe-only requires explicitly verified remote HTTPS TLS")
	}
	if remote {
		probe, cancel := context.WithTimeout(ctx, 6*time.Second)
		err = agenttransport.Probe(probe, client, endpoint)
		cancel()
		if err != nil {
			log.Fatal("verified HTTPS LAB Agent endpoint is not ready: ", err)
		}
	}
	if *probeOnly {
		log.Printf("verified dedicated PRO Agent TLS1.3 transport and certificate pin; no identity or installation created")
		return
	}
	location := *identityPath
	if location == "" {
		location = "./lab-state/agent-" + *nodeID + ".json"
	}
	identity, err := agentidentity.LoadOrCreate(location, *nodeID, os.Getenv("DARK_PRO_PAIR_CODE"))
	if err != nil {
		log.Fatal("Node identity unavailable: ", err)
	}
	for _, approval := range consumerApprovals {
		if approval.InstallationID != identity.InstallationID {
			log.Fatal("consumer endpoint policy must name this exact installation")
		}
	}
	if !identity.Paired {
		attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
		err = enroll(attempt, client, endpoint, identity)
		cancel()
		if err != nil {
			log.Fatal("LAB pairing incomplete: ", err)
		}
		identity, err = agentidentity.MarkPaired(location, identity)
		if err != nil {
			log.Fatal("LAB pairing succeeded but local receipt was not saved; retry with the same identity: ", err)
		}
	}
	if *enrollOnly {
		if !remote || !identity.Paired {
			log.Fatal("enroll-only requires successfully paired remote TLS Agent")
		}
		log.Printf("PRO isolated Agent paired; exiting before any native runtime or telemetry service")
		return
	}
	var managedInboundDone chan struct{}
	if os.Getenv("DARK_PRO_ENABLE_INBOUND_EXECUTION") == "1" {
		if routeEnabled || meteredRouteEnabled || os.Getenv("DARK_PRO_ENABLE_NATIVE_RUNTIME") == "1" || os.Getenv("DARK_PRO_ENABLE_NATIVE_METERED_ADMISSION") == "1" {
			log.Fatal("managed inbound execution requires exclusive new PRO process ownership")
		}
		engines, e := inboundruntime.ParseExternalBinaries(os.Getenv("DARK_PRO_INBOUND_ENGINES_JSON"))
		if e != nil {
			log.Fatal("invalid managed inbound engine configuration")
		}
		worker, e := inboundruntime.Open(inboundruntime.Config{Directory: os.Getenv("DARK_PRO_INBOUND_RUNTIME_DIR"), NodeID: identity.NodeID, InstallationID: identity.InstallationID, Key: []byte(identity.AgentToken), Native: inboundruntime.BinaryConfig{Path: os.Getenv("DARK_PRO_INBOUND_CORE_PATH"), SHA256: os.Getenv("DARK_PRO_INBOUND_CORE_SHA256")}, External: engines})
		if e != nil {
			log.Fatal("managed inbound execution unavailable: ", e)
		}
		exchange, e := inboundruntime.NewHTTPExchange(client, endpoint, identity.AgentToken, identity.NodeID, identity.InstallationID)
		if e != nil {
			worker.Close()
			log.Fatal("managed inbound HUB transport invalid")
		}
		managedInboundDone = make(chan struct{})
		go func() { defer close(managedInboundDone); worker.Run(ctx, exchange) }()
	}
	var nativeRuntime *managedxray.Runtime
	var transactionRuntime *managedxray.TransactionRuntime
	if healthEnabled && !meteredRouteEnabled {
		log.Fatal("protocol health requires the separate metered LAB executor")
	}
	var usageSender *uc.Sender
	remoteMetered := os.Getenv("DARK_PRO_ENABLE_NATIVE_METERED_ADMISSION") == "1"
	if remoteMetered && (os.Getenv("DARK_PRO_ENABLE_NATIVE_RUNTIME") != "1" || !remote) {
		log.Fatal("metered admission needs explicit native runtime and pinned remote TLS")
	}
	if meteredRouteEnabled {
		if routeEnabled || remoteMetered || os.Getenv("DARK_PRO_ENABLE_NATIVE_RUNTIME") == "1" || os.Getenv("DARK_PRO_ENABLE_FINAL_TCP") != "1" || !remote || !identity.Paired || *stageXrayBinary == "" || len(*stageXraySHA) != 64 || *runtimeDir == "" {
			log.Fatal("metered route executor requires exclusive independent loopback state, paired pinned TLS and explicit custom final-TCP core")
		}
		stateKey, keyErr := hex.DecodeString(os.Getenv("DARK_PRO_NATIVE_RUNTIME_KEY_HEX"))
		if keyErr != nil || len(stateKey) != 32 {
			log.Fatal("metered route private state key required")
		}
		usageSender, err = uc.NewSender(uc.SenderOptions{Endpoint: endpoint, NodeID: *nodeID, InstallationID: identity.InstallationID, Token: identity.AgentToken, PinSHA256: *pin, Client: client})
		if err != nil {
			log.Fatal("metered route usage transport refused")
		}
		defer usageSender.Close()
		authority, e := managedxray.NewMeteredRemoteAuthority(client, endpoint, identity.AgentToken, *nodeID, identity.InstallationID)
		if e != nil {
			log.Fatal("metered command authority refused")
		}
		transactionRuntime, err = managedxray.OpenMeteredTransactions(managedxray.Options{Directory: *runtimeDir, StateKey: stateKey, LeaseKey: []byte(identity.AgentToken), NodeID: *nodeID, InstallationID: identity.InstallationID, ListenIP: *runtimeIP, Binary: *stageXrayBinary, BinarySHA: *stageXraySHA, ReservedPorts: []uint16{2087, 8443, 24443}, ExperimentalFinalTCP: true}, managedxray.MeteredTransactionOptions{Authority: authority, Sender: usageSender, Interval: 2 * time.Second, MaxSampleGap: 12 * time.Second, MaxPendingAge: 20 * time.Second, ConsumerEndpointApprovals: consumerApprovals})
		if err != nil {
			log.Fatal("metered route executor refused private state: ", err)
		}
		defer transactionRuntime.Close()
	} else if os.Getenv("DARK_PRO_ENABLE_FINAL_TCP") == "1" {
		log.Fatal("final TCP core flag requires the separate metered route executor")
	}
	if routeEnabled {
		if !remote || !identity.Paired || remoteMetered || os.Getenv("DARK_PRO_ENABLE_NATIVE_RUNTIME") == "1" || *stageXrayBinary == "" || len(*stageXraySHA) != 64 || *runtimeDir == "" {
			log.Fatal("route executor requires exclusive loopback runtime, pinned TLS/core and paired installation")
		}
		stateKey, keyErr := hex.DecodeString(os.Getenv("DARK_PRO_NATIVE_RUNTIME_KEY_HEX"))
		if keyErr != nil || len(stateKey) != 32 {
			log.Fatal("route executor requires independent private state key")
		}
		transactionRuntime, err = managedxray.OpenTransactions(managedxray.Options{Directory: *runtimeDir, StateKey: stateKey, LeaseKey: []byte(identity.AgentToken), NodeID: *nodeID, InstallationID: identity.InstallationID, ListenIP: *runtimeIP, Binary: *stageXrayBinary, BinarySHA: *stageXraySHA, ReservedPorts: []uint16{2087, 8443, 24443}})
		if err != nil {
			log.Fatal("route executor refused private loopback state: ", err)
		}
		defer transactionRuntime.Close()
	}
	if os.Getenv("DARK_PRO_ENABLE_NATIVE_RUNTIME") == "1" {
		if !remote || !identity.Paired || *stageXrayBinary == "" || len(*stageXraySHA) != 64 || *runtimeDir == "" {
			log.Fatal("native runtime requires verified TLS, a freshly paired installation, a pinned core and private state directory")
		}
		stateKey, keyErr := hex.DecodeString(os.Getenv("DARK_PRO_NATIVE_RUNTIME_KEY_HEX"))
		if keyErr != nil || len(stateKey) != 32 {
			log.Fatal("native runtime requires an independent private 32-byte state key")
		}
		if err := managedxray.CheckLocalAddress(*runtimeIP); err != nil {
			log.Fatal("native runtime IP must belong to this host")
		}
		nativeRuntime, err = managedxray.Open(managedxray.Options{Directory: *runtimeDir, StateKey: stateKey, LeaseKey: []byte(identity.AgentToken), NodeID: *nodeID, InstallationID: identity.InstallationID, ListenIP: *runtimeIP, Binary: *stageXrayBinary, BinarySHA: *stageXraySHA, ReservedPorts: []uint16{2087, 8443, 24443}})
		if err != nil {
			log.Fatal("native Xray supervisor refused private state: ", err)
		}
		defer nativeRuntime.Close()
		usageSender, err = uc.NewSender(uc.SenderOptions{Endpoint: endpoint, NodeID: *nodeID, InstallationID: identity.InstallationID, Token: identity.AgentToken, PinSHA256: *pin, Client: client})
		if err != nil {
			log.Fatal("private usage recovery transport refused")
		}
		defer usageSender.Close()
	} else if !routeEnabled && !meteredRouteEnabled && (*runtimeDir != "" || *runtimeIP != "") {
		log.Fatal("native runtime flags supplied without explicit opt-in")
	}
	// Liveness is independent of resource sampling, staging and runtime Apply.
	var workers sync.WaitGroup
	if remote && identity.Paired {
		for i := 0; i < 4; i++ {
			workers.Add(1)
			go func() { defer workers.Done(); runInboundSecurityWorker(ctx, client, endpoint, identity) }()
		}
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		var pulseSeq uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pulseSeq++
				pulse := telemetry.Pulse{NodeID: *nodeID, InstallationID: identity.InstallationID, BootID: bootID, Sequence: pulseSeq}
				if err := sendPulse(ctx, client, endpoint, identity.AgentToken, pulse); err != nil && ctx.Err() == nil {
					log.Printf("HUB liveness pulse unavailable: %v", err)
				}
			}
		}
	}()
	if transactionRuntime != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
				var err error
				if meteredRouteEnabled {
					// Historical usage recovery cannot resume an interrupted
					// command. A fresh explicit Stage/Apply is still required.
					if !transactionRuntime.Status().Observed.ProcessReady {
						_, _ = transactionRuntime.ReplayMeteredUsage(attempt)
					}
					err = transactionRuntime.SyncMeteredTransaction(attempt, client, endpoint)
				} else {
					err = transactionRuntime.SyncTransaction(attempt, client, endpoint)
				}
				cancel()
				if err != nil && ctx.Err() == nil {
					log.Printf("route command/receipt pending; short lease and exact replay retained")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	// Independent of the command worker: verifier delay must never block
	// delivery of an emergency drain or the runtime's lease expiry.
	if transactionRuntime != nil && healthEnabled {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				attempt, cancel := context.WithTimeout(ctx, 12*time.Second)
				// Sync drains successive health challenges immediately with its
				// own work bound; the ticker is only an idle/error retry delay.
				err := transactionRuntime.SyncMeteredHealth(attempt, client, endpoint)
				cancel()
				if err != nil && ctx.Err() == nil {
					log.Printf("protocol health pending or unknown; no lease renewed")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	if nativeRuntime != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				attempt, cancel := context.WithTimeout(ctx, managedxray.MeteredSyncTimeout)
				blocked, replayErr := replayUsageBeforeSync(attempt, nativeRuntime, usageSender)
				if blocked {
					cancel()
					if ctx.Err() == nil {
						if replayErr != nil {
							log.Printf("interrupted usage replay unavailable; recorded bytes and runtime fence retained")
						} else {
							log.Printf("recorded usage replayed; previous epoch still requires explicit settlement")
						}
					}
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
					}
					continue
				}
				var err error
				if remoteMetered {
					_, err = nativeRuntime.MeteredRemoteSync(attempt, client, endpoint, usageSender)
				} else {
					_, err = nativeRuntime.Sync(attempt, client, endpoint)
				}
				cancel()
				if err != nil && ctx.Err() == nil {
					log.Printf("native runtime synchronization unavailable; short lease remains enforced")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	if os.Getenv("DARK_PRO_ENABLE_TCP_ROUTE_PROBES") == "1" {
		workers.Add(1)
		go func() {
			defer workers.Done()
			timer := time.NewTicker(12 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if err := pollPreflight(ctx, client, endpoint, *nodeID, identity.InstallationID, identity.AgentToken); err != nil && ctx.Err() == nil {
						log.Printf("replacement TCP preflight failed: %v", err)
					}
				}
			}
		}()
	}
	if os.Getenv("DARK_PRO_ENABLE_XRAY_STAGE_VALIDATION") == "1" {
		if !remote || *stageXrayBinary == "" || len(*stageXraySHA) != 64 {
			log.Fatal("offline Xray stage worker requires HTTPS and a separately pinned executable")
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			timer := time.NewTicker(15 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if err := pollConfigStage(ctx, client, endpoint, *nodeID, identity.InstallationID, identity.AgentToken, *stageXrayBinary, *stageXraySHA); err != nil && ctx.Err() == nil {
						log.Printf("offline Xray stage validation unavailable: %v", err)
					}
				}
			}
		}()
	}
	initial, err := hostmetrics.Read()
	hasInitial := err == nil
	if err != nil {
		log.Printf("initial stats unavailable; liveness continues independently: %v", err)
	}
	previous := initial
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var metricSeq uint64
	if transactionRuntime != nil {
		if meteredRouteEnabled {
			log.Printf("NEW independent metered TCP loopback executor enabled for %s; no migration or subscription publication", *nodeID)
		} else {
			log.Printf("NEW loopback route executor enabled for %s; customer bindings and subscriptions disabled", *nodeID)
		}
	} else if nativeRuntime == nil {
		log.Printf("DARK PRO LAB-only telemetry for %s; native runtime disabled", *nodeID)
	} else {
		log.Printf("DARK PRO native runtime explicitly enabled for NEW paired node %s; no subscription publication", *nodeID)
	}
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			if managedInboundDone != nil {
				<-managedInboundDone
			}
			return
		case <-ticker.C:
			now, err := hostmetrics.Read()
			if err != nil {
				log.Printf("telemetry unavailable; Node liveness unaffected: %v", err)
				continue
			}
			if !hasInitial {
				previous = now
				hasInitial = true
				continue
			}
			cpu, available := hostmetrics.CPUPercent(previous, now)
			previous = now
			if !available {
				log.Printf("CPU sample pending; no invented 0%%")
				continue
			}
			metricSeq++
			sample := telemetry.Sample{NodeID: *nodeID, InstallationID: identity.InstallationID, Name: *name, BootID: bootID, Sequence: metricSeq, Interface: now.Interface, CPUCores: runtime.NumCPU(), CPUPercent: cpu, RAMTotal: now.RAMTotal, RAMUsed: now.RAMUsed, RXBytes: now.RXBytes, TXBytes: now.TXBytes, DiskTotalBytes: now.DiskTotal, DiskUsedBytes: now.DiskUsed, Load1: now.Load1, Load5: now.Load5, Load15: now.Load15, UptimeSeconds: now.UptimeSec, TCPInUse: now.TCPInUse}
			if err := transmit(ctx, client, endpoint, identity.AgentToken, sample); err != nil && ctx.Err() == nil {
				log.Printf("HUB stats unavailable; liveness runs separately: %v", err)
			}
		}
	}
}
