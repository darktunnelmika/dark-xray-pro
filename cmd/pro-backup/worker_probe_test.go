package main

import (
	"bytes"
	"context"
	"testing"
)

func TestWorkerCredentialProbeRejectsEmptyArguments(t *testing.T) {
	t.Setenv("DARK_PRO_LAB_MODE", "1")
	var out, diagnostic bytes.Buffer
	if code := execute(context.Background(), nil, &out, &diagnostic); code != 2 || out.Len() != 0 {
		t.Fatalf("no-argument credential probe contract changed: exit=%d stdout=%q", code, out.String())
	}
}
