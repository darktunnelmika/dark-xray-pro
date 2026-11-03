package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

type darkRestoreSupervisorOptions struct {
	Client                           *http.Client
	Endpoint, Token, PlanSHA, TLSPin string
	Runtime                          mx.Options
	Signer                           ed25519.PrivateKey
}

func runDarkRestoreSupervisor(ctx context.Context, o darkRestoreSupervisorOptions) error {
	if o.Client == nil || !strings.HasPrefix(o.Endpoint, "https://") || o.Runtime.ListenIP != "127.0.0.1" || !o.Runtime.ExperimentalFinalTCP || len(o.PlanSHA) != 64 {
		return errors.New("invalid metered quarantine supervisor policy")
	}
	var listenerApply *business.DarkRestoreListenerApply
	call := func(phase string, birth *business.DarkRestoreQuarantineBirth, out any) (int, error) {
		raw, err := json.Marshal(struct {
			NodeID, InstallationID, PlanSHA, Phase string
			Birth                                  *business.DarkRestoreQuarantineBirth
			ListenerApply                          *business.DarkRestoreListenerApply
		}{o.Runtime.NodeID, o.Runtime.InstallationID, o.PlanSHA, phase, birth, func() *business.DarkRestoreListenerApply {
			if phase == "applied" {
				return listenerApply
			}
			return nil
		}()})
		if err != nil {
			return 0, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Endpoint+"/api/v1/agent/darkrestore-runtime", bytes.NewReader(raw))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+o.Token)
		req.Header.Set("Content-Type", "application/json")
		res, err := o.Client.Do(req)
		if err != nil {
			return 0, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			io.Copy(io.Discard, io.LimitReader(res.Body, 2048))
			return res.StatusCode, fmt.Errorf("runtime authority refused phase %s HTTP %d", phase, res.StatusCode)
		}
		decoder := json.NewDecoder(io.LimitReader(res.Body, 4<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(out); err != nil {
			return res.StatusCode, err
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return res.StatusCode, errors.New("unexpected runtime authority data")
		}
		return res.StatusCode, nil
	}
	runtime, err := mx.Open(o.Runtime)
	if err != nil {
		return err
	}
	defer runtime.Close()
	sender, err := uc.NewSender(uc.SenderOptions{Endpoint: o.Endpoint, NodeID: o.Runtime.NodeID, InstallationID: o.Runtime.InstallationID, Token: o.Token, PinSHA256: o.TLSPin, Client: o.Client})
	if err != nil {
		return err
	}
	defer sender.Close()

	if runtime.Snapshot().Usage != nil {
		var recovery struct {
			Epoch uc.Epoch `json:"epoch"`
		}
		if _, err = call("recovery", nil, &recovery); err != nil {
			return err
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		if _, err = runtime.SettleActiveTCP(cleanup, sender); err != nil {
			sender.NotifyInterrupted(cleanup, recovery.Epoch)
			return err
		}
		return nil
	}
	var preparation struct {
		Delivery business.DarkRestoreQuarantineDelivery `json:"delivery"`
		Lease    mx.Lease                               `json:"lease"`
	}
	if _, err := call("prepare", nil, &preparation); err != nil {
		return err
	}
	plan, err := preparation.Delivery.Open(o.Runtime.NodeID, o.Runtime.InstallationID, o.PlanSHA)
	if err != nil {
		return err
	}
	var registered struct {
		Epoch uc.Epoch `json:"epoch"`
	}
	settled := false
	defer func() {
		if !settled && registered.Epoch.ID != "" {
			runtime.Close()
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
			defer cancel()
			sender.NotifyInterrupted(cleanup, registered.Epoch)
		}
	}()
	birth, err := plan.PrepareRuntime(ctx, runtime, o.PlanSHA, preparation.Lease, o.Signer)
	if err != nil {
		return err
	}
	for {
		status, e := call("birth", &birth, &registered)
		if e == nil {
			break
		}
		if (status != 0 && status != 503 && status != 504) || time.Now().UnixNano() >= preparation.Lease.Expires {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	var admission struct {
		Epoch uc.Epoch `json:"epoch"`
		Lease mx.Lease `json:"lease"`
	}
	for {
		status, e := call("admit", nil, &admission)
		if e == nil {
			break
		}
		if status != 404 || time.Now().UnixNano() >= preparation.Lease.Expires {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	expected, _ := json.Marshal(registered.Epoch)
	actual, _ := json.Marshal(admission.Epoch)
	if !bytes.Equal(expected, actual) || admission.Lease != preparation.Lease {
		return errors.New("admission changed registered runtime")
	}
	if _, err = runtime.ActivateUsageAdmission(ctx, mx.UsageObservation{Epoch: registered.Epoch, StatsEndpoint: mx.MeteredStatsEndpoint, Sender: sender, Interval: time.Second, MaxSampleGap: 5 * time.Second, MaxPendingAge: 8 * time.Second}); err != nil {
		return err
	}

	registration := business.DarkRestoreRegisteredBirth{Plan: plan, Birth: birth, Epoch: registered.Epoch, Signer: o.Signer.Public().(ed25519.PublicKey)}
	applied, err := registration.ObserveListenerApply(ctx, runtime, o.Signer)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		_, sealErr := runtime.SettleActiveTCP(cleanup, sender)
		cancel()
		settled = sealErr == nil
		return err
	}
	listenerApply = &applied
	for attempt := 0; ; attempt++ {
		var ack struct {
			SHA string `json:"receipt_sha256"`
		}
		status, e := call("applied", nil, &ack)
		if e == nil && ack.SHA == applied.Digest() {
			break
		}
		if e == nil {
			e = errors.New("listener apply receipt acknowledgment mismatch")
		}
		if attempt >= 2 || (status != 0 && status != 503 && status != 504) || ctx.Err() != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
			_, sealErr := runtime.SettleActiveTCP(cleanup, sender)
			cancel()
			settled = sealErr == nil
			return e
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
			_, err = runtime.SettleActiveTCP(cleanup, sender)
			cancel()
			if err == nil {
				settled = true
			}
			return err
		case <-ticker.C:
			var renewal struct {
				Epoch uc.Epoch `json:"epoch"`
				Lease mx.Lease `json:"lease"`
			}
			if _, err = call("renew", nil, &renewal); err != nil {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
				_, sealErr := runtime.SettleActiveTCP(cleanup, sender)
				cancel()
				settled = sealErr == nil
				if ctx.Err() != nil && settled {
					return nil
				}
				return err
			}
			actual, _ = json.Marshal(renewal.Epoch)
			if !bytes.Equal(expected, actual) {
				return errors.New("renewal changed accounting epoch")
			}
			if _, err = runtime.Refresh(renewal.Lease); err != nil {
				return err
			}
		}
	}
}
