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
    "net"
    "net/http"
    "net/url"
    "os"
    "os/signal"
    "runtime"
    "syscall"
    "time"

    "github.com/darktunnelmika/dark-xray-pro/internal/agentidentity"
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
    flag.Parse()

    if os.Getenv("DARK_PRO_LAB_MODE") != "1" {
        log.Fatal("node agent is lab-only; set DARK_PRO_LAB_MODE=1")
    }
    if *nodeID == "" { log.Fatal("specify -id for a new LAB node; do not reuse old node identities") }
    if _, err := checkLabEndpoint(*hub); err != nil { log.Fatal(err) }
    if *name == "" { *name = *nodeID }
    if len(*name) > 80 { log.Fatal("name too long") }

    var boot [16]byte
    if _, err := rand.Read(boot[:]); err != nil { log.Fatal(err) }
    bootID := hex.EncodeToString(boot[:])
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    dialer := &net.Dialer{Timeout: 2 * time.Second}
    client := &http.Client{
        Timeout: 4 * time.Second,
        Transport: &http.Transport{DialContext: dialer.DialContext},
        CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
    }
    location := *identityPath
    if location == "" {
        location = "./lab-state/agent-" + *nodeID + ".json"
    }
    identity, err := agentidentity.LoadOrCreate(location, *nodeID, os.Getenv("DARK_PRO_PAIR_CODE"))
    if err != nil { log.Fatal("Node identity unavailable: ", err) }
    if !identity.Paired {
        attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
        err = enroll(attempt, client, *hub, identity)
        cancel()
        if err != nil { log.Fatal("LAB pairing incomplete: ", err) }
        identity, err = agentidentity.MarkPaired(location, identity)
        if err != nil { log.Fatal("LAB pairing succeeded but local receipt was not saved; retry with the same identity: ", err) }
    }
    initial, err := hostmetrics.Read()
    if err != nil { log.Fatal("cannot sample host telemetry: ", err) }
    ticker := time.NewTicker(3 * time.Second)
    defer ticker.Stop()
    previous := initial
    var seq uint64
    log.Printf("DARK PRO LAB-only telemetry for %s; no Xray config or customer data access", *nodeID)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            now, err := hostmetrics.Read()
            if err != nil {
                log.Printf("telemetry unavailable: %v", err)
                continue
            }
            cpu, available := hostmetrics.CPUPercent(previous, now)
            previous = now
            if !available { log.Printf("CPU sample pending; not reporting invented 0%%"); continue }
            seq++
            sample := telemetry.Sample{
                NodeID: *nodeID, InstallationID: identity.InstallationID,
                Name: *name, BootID: bootID, Sequence: seq,
                Interface: now.Interface, CPUCores: runtime.NumCPU(), CPUPercent: cpu,
                RAMTotal: now.RAMTotal, RAMUsed: now.RAMUsed,
                RXBytes: now.RXBytes, TXBytes: now.TXBytes,
            }
            if err := transmit(ctx, client, *hub, identity.AgentToken, sample); err != nil {
                log.Printf("HUB telemetry failed: %v", err)
            }
        }
    }
}
