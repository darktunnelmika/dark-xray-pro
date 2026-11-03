package main

import (
	"bytes"
	"context"
	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	managedxray "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"os"
	"testing"
	"time"
)

func TestQuarantineWorkerPinnedTLS(t *testing.T) {
	if os.Getenv("DARK_PRO_QUARANTINE_WORKER_TEST") != "1" {
		t.Skip("requires isolated live HUB fixture")
	}
	get := os.Getenv
	client, endpoint, err := agenttransport.New(agenttransport.Config{Endpoint: get("QUARANTINE_TEST_URL"), PinSHA256: get("QUARANTINE_TEST_TLS_PIN"), CABundle: []byte(get("QUARANTINE_TEST_CA"))})
	if err != nil {
		t.Fatal(err)
	}
	key, err := quarantineSigner(get("QUARANTINE_TEST_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = pollDarkRestoreQuarantine(context.Background(), client, endpoint, "quarantine-node", get("QUARANTINE_TEST_INSTALL"), get("QUARANTINE_TEST_TOKEN"), get("QUARANTINE_TEST_PLAN"), get("DARK_PRO_TEST_XRAY_BINARY"), get("DARK_PRO_TEST_XRAY_SHA256"), get("QUARANTINE_TEST_DIR"), key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDarkRestoreSupervisorPinnedTLS(t *testing.T) {
	if os.Getenv("DARK_PRO_QUARANTINE_SUPERVISOR_TEST") != "1" {
		t.Skip("requires isolated live HUB and pinned final TCP core")
	}
	get := os.Getenv
	client, endpoint, err := agenttransport.New(agenttransport.Config{Endpoint: get("QUARANTINE_TEST_URL"), PinSHA256: get("QUARANTINE_TEST_TLS_PIN"), CABundle: []byte(get("QUARANTINE_TEST_CA"))})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := quarantineSigner(get("QUARANTINE_TEST_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	options := darkRestoreSupervisorOptions{Client: client, Endpoint: endpoint, Token: get("QUARANTINE_TEST_TOKEN"), PlanSHA: get("QUARANTINE_TEST_PLAN"), TLSPin: get("QUARANTINE_TEST_TLS_PIN"), Signer: signer, Runtime: managedxray.Options{Directory: get("QUARANTINE_TEST_DIR"), StateKey: bytes.Repeat([]byte{2}, 32), LeaseKey: []byte(get("QUARANTINE_TEST_TOKEN")), NodeID: "quarantine-node", InstallationID: get("QUARANTINE_TEST_INSTALL"), ListenIP: "127.0.0.1", Binary: get("DARK_PRO_TEST_XRAY_BINARY"), BinarySHA: get("DARK_PRO_TEST_XRAY_SHA256"), ExperimentalFinalTCP: true}}
	err = runDarkRestoreSupervisor(ctx, options)
	if os.Getenv("QUARANTINE_TEST_EXPECT_STOP") == "1" {
		if err == nil {
			t.Fatal("revoked supervisor continued")
		}
	} else if err != nil {
		t.Fatal(err)
	}
}
