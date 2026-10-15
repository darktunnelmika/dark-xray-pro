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

    "github.com/darktunnelmika/dark-xray-pro/internal/agentidentity"
    "github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
    "github.com/darktunnelmika/dark-xray-pro/internal/hostmetrics"
    "github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func checkLabEndpoint(raw string) (*url.URL, error) {
    endpoint, err := url.Parse(raw)
    if err != nil { return nil, err }
    // Phase 0 is strictly same-host: real remote enrollment/TLS is P0 work.
    if endpoint.Scheme != "http" || endpoint.Host != "127.0.0.1:2087" ||
       endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
        return nil, errors.New("lab agent requires http://127.0.0.1:2087; remote Nodes are disabled")
    }
    return endpoint, nil
}

// The Agent generates and durably saves its own secret before first enrollment.
// A lost response safely retries with exactly the same installation and token.
func enroll(ctx context.Context, client *http.Client, endpoint string, identity agentidentity.Identity) error {
    data, err := json.Marshal(map[string]string{
        "pair_code": identity.PairCode, "node_id": identity.NodeID,
        "installation_id": identity.InstallationID, "agent_token": identity.AgentToken,
    })
    if err != nil { return err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/enroll", bytes.NewReader(data))
    if err != nil { return err }
    req.Header.Set("Content-Type", "application/json")
    res, err := client.Do(req)
    if err != nil { return err }
    defer res.Body.Close()
    if res.StatusCode != http.StatusOK {
        _, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
        return fmt.Errorf("pairing refused with HTTP %d; identity remains intact for retry", res.StatusCode)
    }
    var ack struct {
        Paired bool `json:"paired"`
        NodeID string `json:"node_id"`
        InstallationID string `json:"installation_id"`
    }
    if err := json.NewDecoder(io.LimitReader(res.Body, 1024)).Decode(&ack); err != nil { return err }
    if !ack.Paired || ack.NodeID != identity.NodeID || ack.InstallationID != identity.InstallationID {
        return errors.New("pairing response identity mismatch")
    }
    return nil
}

// sendPulse never collects CPU, RAM, disk, Xray or interface telemetry.
// It is independent of the slower and failure-prone stats sampling loop.
func sendPulse(ctx context.Context, client *http.Client, endpoint, token string, pulse telemetry.Pulse) error {
    payload, err := json.Marshal(pulse)
    if err != nil { return err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/pulse", bytes.NewReader(payload))
    if err != nil { return err }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err := client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusNoContent {
        _, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
        return fmt.Errorf("HUB pulse rejected: HTTP %d", resp.StatusCode)
    }
    return nil
}

func transmit(ctx context.Context, client *http.Client, endpoint, token string, sample telemetry.Sample) error {
    payload, err := json.Marshal(sample)
    if err != nil { return err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/heartbeat", bytes.NewReader(payload))
    if err != nil { return err }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+token)
    resp, err := client.Do(req)
    if err != nil { return err }
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
    stageXrayBinary := flag.String("stage-xray-binary", "", "explicit trusted Xray binary for OFFLINE validation only")
    stageXraySHA := flag.String("stage-xray-sha256", "", "SHA256 of separately trusted Xray binary")
    pin := flag.String("hub-cert-sha256", "", "required SHA256 fingerprint of the verified HTTPS HUB leaf certificate")
    caFile := flag.String("hub-ca-file", "", "optional trusted LAB CA certificate bundle")
    flag.Parse()

    if os.Getenv("DARK_PRO_LAB_MODE") != "1" {
        log.Fatal("node agent is lab-only; set DARK_PRO_LAB_MODE=1")
    }
    if *nodeID == "" { log.Fatal("specify -id for a new LAB node; do not reuse old node identities") }
    // The legacy unencrypted lab path is accepted ONLY on this machine.
    // Remote enrollment requires opt-in, TLS1.3, normal cert verification
    // and an explicit pinned certificate fingerprint.
    remote := strings.HasPrefix(*hub, "https://")
    if remote && os.Getenv("DARK_PRO_ENABLE_REMOTE_AGENT") != "1" {
        log.Fatal("remote Node connection requires DARK_PRO_ENABLE_REMOTE_AGENT=1 in the isolated LAB")
    }
    if !remote {
        if _, err := checkLabEndpoint(*hub); err != nil { log.Fatal(err) }
    }

    if *name == "" { *name = *nodeID }
    if len(*name) > 80 { log.Fatal("name too long") }

    var boot [16]byte
    if _, err := rand.Read(boot[:]); err != nil { log.Fatal(err) }
    bootID := hex.EncodeToString(boot[:])
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    var caPEM []byte
    var err error
    if *caFile != "" {
        caPEM, err = os.ReadFile(*caFile)
        if err != nil { log.Fatal("cannot load trusted Agent CA: ", err) }
    }
    client, endpoint, err := agenttransport.New(agenttransport.Config{
        Endpoint: *hub, PinSHA256: *pin, CABundle: caPEM,
    })
    if err != nil { log.Fatal("unsafe HUB transport: ", err) }
    if remote {
        probe, cancel := context.WithTimeout(ctx, 6*time.Second)
        err = agenttransport.Probe(probe, client, endpoint)
        cancel()
        if err != nil { log.Fatal("verified HTTPS LAB Agent endpoint is not ready: ", err) }
    }
    location := *identityPath
    if location == "" {
        location = "./lab-state/agent-" + *nodeID + ".json"
    }
    identity, err := agentidentity.LoadOrCreate(location, *nodeID, os.Getenv("DARK_PRO_PAIR_CODE"))
    if err != nil { log.Fatal("Node identity unavailable: ", err) }
    if !identity.Paired {
        attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
        err = enroll(attempt, client, endpoint, identity)
        cancel()
        if err != nil { log.Fatal("LAB pairing incomplete: ", err) }
        identity, err = agentidentity.MarkPaired(location, identity)
        if err != nil { log.Fatal("LAB pairing succeeded but local receipt was not saved; retry with the same identity: ", err) }
    }
    // Heartbeats are independent of stats. A busy CPU, broken /proc, or
    // unavailable network counters must not manufacture Offline state.
    var workers sync.WaitGroup
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
                pulse := telemetry.Pulse{
                    NodeID: *nodeID, InstallationID: identity.InstallationID,
                    BootID: bootID, Sequence: pulseSeq,
                }
                if err := sendPulse(ctx, client, endpoint, identity.AgentToken, pulse); err != nil &&
                    ctx.Err() == nil {
                    log.Printf("HUB liveness pulse unavailable: %v", err)
                }
            }
        }
    }()

    // Explicit LAB-only opt-in. Preflight never changes Xray or subscriptions.
    // Keep it in its own worker so a slow TCP test cannot block authenticated
    // liveness pulses or CPU/memory sampling.
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
                    if err := pollPreflight(ctx,client,endpoint,*nodeID,
                        identity.InstallationID,identity.AgentToken); err != nil && ctx.Err() == nil {
                        log.Printf("replacement TCP preflight failed: %v",err)
                    }
                }
            }
        }()
    }

    // Private Reality staging is disabled by default. It always uses the
    // verified HTTPS Agent connection and never writes live Xray config.
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
                    if err := pollConfigStage(ctx,client,endpoint,*nodeID,
                        identity.InstallationID,identity.AgentToken,*stageXrayBinary,
                        *stageXraySHA); err != nil && ctx.Err() == nil {
                        log.Printf("offline Xray stage validation unavailable: %v",err)
                    }
                }
            }
        }()
    }

    initial, err := hostmetrics.Read()
    hasInitial := err == nil
    if err != nil { log.Printf("initial stats unavailable; liveness continues independently: %v", err) }
    previous := initial
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    var metricSeq uint64
    log.Printf("DARK PRO LAB-only telemetry for %s; no Xray config or customer data access", *nodeID)
    for {
        select {
        case <-ctx.Done():
            workers.Wait()
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
            if !available { log.Printf("CPU sample pending; no invented 0%%"); continue }
            metricSeq++
            sample := telemetry.Sample{
                NodeID: *nodeID, InstallationID: identity.InstallationID,
                Name: *name, BootID: bootID, Sequence: metricSeq,
                Interface: now.Interface, CPUCores: runtime.NumCPU(), CPUPercent: cpu,
                RAMTotal: now.RAMTotal, RAMUsed: now.RAMUsed,
                RXBytes: now.RXBytes, TXBytes: now.TXBytes,
            }
            if err := transmit(ctx, client, endpoint, identity.AgentToken, sample); err != nil && ctx.Err() == nil {
                log.Printf("HUB stats unavailable; liveness runs separately: %v", err)
            }
        }
    }
}
