//go:build linux

package xraycanary

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in only. The Xray binary must already be separately checksum-verified.
// This starts a disposable process on loopback with throwaway Reality users.
// A successful bound listener is still NOT a verified VLESS data-plane route.
func TestRealPinnedXrayCanaryOptIn(t *testing.T){
	binary,pin:=os.Getenv("DARK_PRO_TEST_XRAY_BINARY"),os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if binary==""&&pin==""{t.Skip("no trusted Xray binary configured")}
	if binary==""||pin==""{t.Fatal("real canary requires explicit binary and SHA256")}
	lab(t)
	c:=candidate(t,true)
	ctx,cancel:=context.WithTimeout(context.Background(),14*time.Second)
	defer cancel()
	result,err:=Run(ctx,c,binary,pin)
	if err!=nil{t.Fatalf("pinned real Xray could not start a disposable localhost canary: %v",err)}
	if !result.SyntaxValidated||!result.LoopbackBound||result.RemappedListeners!=2||
		result.CanActivate||result.DataPlaneVerified||!result.NeedsSubscriptionCommit{
		t.Fatalf("real Xray incorrectly claimed live customer connectivity: %+v",result)
	}
}
