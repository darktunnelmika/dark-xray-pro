package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"io"
	"net/http"
	"strings"
)

func pollDarkRestoreQuarantine(ctx context.Context, client *http.Client, endpoint, node, installation, token, pin, binary, core, dir string, key ed25519.PrivateKey) error {
	if client == nil || !strings.HasPrefix(endpoint, "https://") {
		return errors.New("quarantine requires verified Agent HTTPS")
	}
	call := func(receipt *business.DarkRestoreQuarantineRegistration, out any) error {
		raw, err := json.Marshal(struct {
			NodeID, InstallationID, PlanSHA string
			Receipt                         *business.DarkRestoreQuarantineRegistration
		}{node, installation, pin, receipt})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/darkrestore-quarantine", bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return errors.New("HUB refused quarantine transport")
		}
		d := json.NewDecoder(io.LimitReader(res.Body, 4<<20))
		d.DisallowUnknownFields()
		return d.Decode(out)
	}
	var delivery business.DarkRestoreQuarantineDelivery
	if err := call(nil, &delivery); err != nil {
		return err
	}
	plan, err := delivery.Open(node, installation, pin)
	if err != nil {
		return err
	}
	receipt, err := plan.RegisterOffline(ctx, node, installation, pin, binary, core, dir, key)
	if err != nil {
		return err
	}
	var ack struct {
		Registered     bool   `json:"registered"`
		PlanSHA        string `json:"plan_sha256"`
		Scope          string `json:"scope"`
		RuntimeChanged bool   `json:"runtime_changed"`
	}
	if err = call(&receipt, &ack); err != nil {
		return err
	}
	if !ack.Registered || ack.PlanSHA != pin || ack.Scope != "offline_syntax_only_v1" || ack.RuntimeChanged {
		return errors.New("invalid quarantine acknowledgment")
	}
	return nil
}
func quarantineSigner(raw string) (ed25519.PrivateKey, error) {
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid quarantine signing key")
	}
	return ed25519.PrivateKey(key), nil
}
