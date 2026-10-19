package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRefuseWithoutOfflineCopyAcknowledgement(t *testing.T) {
	tests := [][]string{
		nil,
		{"-source", "/tmp/snapshot.sqlite"},
		{"-offline-copy-verified"},
		{"-offline-copy-verified", "-source", "relative.sqlite"},
		{"-offline-copy-verified", "-source", "/does-not-exist/old.sqlite"},
	}
	for _, args := range tests {
		var output, errors bytes.Buffer
		result := execute(args, &output, &errors)
		if result == 0 || output.Len() != 0 {
			t.Fatalf("unsafe CLI args were accepted: %d", result)
		}
		if strings.Contains(errors.String(), "/does-not-exist") ||
			strings.Contains(errors.String(), "relative.sqlite") ||
			strings.Contains(errors.String(), "secret") ||
			errors.Len() == 0 {
			t.Fatal("an error message leaked untrusted source values")
		}
	}
}
func TestRejectUnknownFlagsAndInvalidAsOf(t *testing.T) {
	for _, args := range [][]string{
		{"-secret-password", "dont-print-it"},
		{"-offline-copy-verified", "-source", "/tmp/test.db", "-as-of-unix", "-1"},
		{"-offline-copy-verified", "-source", "/tmp/test.db", "-as-of-unix", "253402300800"},
	} {
		var stdout, stderr bytes.Buffer
		code := execute(args, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "dont-print-it") {
			t.Fatalf("CLI should fail closed without leaking sensitive flag data, status %d", code)
		}
	}
}
