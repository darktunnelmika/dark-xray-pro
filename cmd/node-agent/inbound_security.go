package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agentidentity"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundsecurity"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

type inboundSecurityTask struct {
	ID      string                       `json:"id"`
	Request inboundsecurity.ProbeRequest `json:"request"`
	Expires int64                        `json:"expires_unix"`
}

func inboundSecurityExchange(ctx context.Context, client *http.Client, endpoint, operation string, identity agentidentity.Identity, task string, result *inboundsecurity.ProbeResult) (*http.Response, error) {
	body, err := json.Marshal(struct {
		NodeID         string                       `json:"node_id"`
		InstallationID string                       `json:"installation_id"`
		ID             string                       `json:"id,omitempty"`
		Result         *inboundsecurity.ProbeResult `json:"result,omitempty"`
	}{identity.NodeID, identity.InstallationID, task, result})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/inbound-security/"+operation, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+identity.AgentToken)
	request.Header.Set("Content-Type", "application/json")
	return client.Do(request)
}

// This worker is independent of telemetry and runtime Apply. It only executes
// explicit, expiring HUB jobs; the local scanner revalidates public addresses.
func runInboundSecurityWorker(ctx context.Context, client *http.Client, endpoint string, identity agentidentity.Identity) {
	service, err := inboundsecurity.New(inboundsecurity.Config{LocalNodeID: identity.NodeID, ProbeTimeout: 8 * time.Second})
	if err != nil {
		return
	}
	runInboundSecurityWorkerWithService(ctx, client, endpoint, identity, service)
}

func runInboundSecurityWorkerWithService(ctx context.Context, client *http.Client, endpoint string, identity agentidentity.Identity, service *inboundsecurity.Service) {
	if service == nil {
		return
	}
	// Independent workers let a selected-Node finder test several candidates
	// without head-of-line blocking a single scan or liveness reports.
	for ctx.Err() == nil {
		response, err := inboundSecurityExchange(ctx, client, endpoint, "poll", identity, "", nil)
		var task inboundSecurityTask
		ready := false
		if err == nil {
			if response.StatusCode == http.StatusOK {
				raw, e := io.ReadAll(io.LimitReader(response.Body, 8193))
				ready = e == nil && len(raw) <= 8192 && usagewire.Decode(raw, &task) == nil
			}
			_ = response.Body.Close()
		}
		if ready && len(task.ID) == 32 && task.Request.Scope == "node" && task.Request.NodeID == identity.NodeID && task.Expires > time.Now().Unix() && task.Expires <= time.Now().Add(30*time.Second).Unix() {
			probeCtx, cancel := context.WithDeadline(ctx, time.Unix(task.Expires, 0))
			result, e := service.ProbeLocal(probeCtx, task.Request)
			cancel()
			if e != nil {
				result = inboundsecurity.ProbeResult{Scope: "node", NodeID: identity.NodeID, Target: task.Request.Target, ServerName: task.Request.ServerName, Error: "node_probe_failed", Reason: "node_probe_failed"}
			}
			response, err = inboundSecurityExchange(ctx, client, endpoint, "result", identity, task.ID, &result)
			if err == nil {
				_ = response.Body.Close()
			}
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
