package main

import (
	"context"
	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	"os"
	"testing"
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
