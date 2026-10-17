package business

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// Equal Direct and Dark Swap ports have no unambiguous route attribution and
// cannot produce distinct listeners. Reject BEFORE touching any stored target.
func TestRejectSharedDirectAndSwapPortWithoutChangingStoredTarget(t *testing.T) {
	ctx := context.Background()
	store, _ := opened(t)
	key := bytes.Repeat([]byte{7}, 32)
	if _, err := store.CreateInbound(ctx, key, nativeInput()); err != nil {
		t.Fatal(err)
	}
	initial := InboundTarget{InboundID: 10, NodeID: "new-node", InstallationID: inboundInstall,
		ListenIP: "127.0.0.1", DirectPort: 19443, SwapPort: 19444}
	if err := store.PutInboundTarget(ctx, initial); err != nil {
		t.Fatal(err)
	}
	bad := initial
	bad.SwapPort = bad.DirectPort
	if validNativeTarget(bad) {
		t.Fatal("duplicated listener considered valid")
	}
	if err := store.PutInboundTarget(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("shared Direct/Swap port accepted or wrong error: %v", err)
	}
	targets, err := store.ListInboundTargets(ctx)
	if err != nil || len(targets) != 1 || targets[0].DirectPort != 19443 || targets[0].SwapPort != 19444 {
		t.Fatalf("invalid save mutated last safe config: %+v %v", targets, err)
	}
	cfg, err := store.BuildNodeConfig(ctx, key, "new-node", inboundInstall)
	if err != nil || len(cfg.Candidate.Listeners) != 2 {
		t.Fatal("known-good distinct listeners damaged", err)
	}
	for _, port := range []uint16{1024, 8569, 5900, 65535} {
		distinct := initial
		distinct.DirectPort = port
		distinct.SwapPort = port
		if validNativeTarget(distinct) {
			t.Fatalf("shared port %d admitted", port)
		}
	}
	directOnly := initial
	directOnly.SwapPort = 0
	if !validNativeTarget(directOnly) {
		t.Fatal("direct-only route refused")
	}
	swapOnly := initial
	swapOnly.DirectPort = 0
	if !validNativeTarget(swapOnly) {
		t.Fatal("swap-only route refused")
	}
}
