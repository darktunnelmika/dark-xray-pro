package xrayconfig

import (
	"context"
	"os"
	"testing"
	"time"
)

// This test is opt-in because it requires a *separately trusted* real Xray
// binary on an isolated NEW LAB machine. Unit CI does not download or trust
// an arbitrary latest release. It must never use the old production binary.
func TestRealPinnedXrayCoreOptIn(t *testing.T){
	bin:=os.Getenv("DARK_PRO_TEST_XRAY_BINARY")
	pin:=os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if bin==""&&pin==""{t.Skip("no pinned real Xray binary configured for isolated LAB")}
	if os.Getenv("DARK_PRO_LAB_MODE")!="1"{t.Fatal("real Xray integration is permitted only in explicit isolated LAB mode")}
	if bin==""||pin==""{t.Fatal("real Xray validation requires BOTH absolute path and trusted SHA256")}
	profile:=testProfile(1)
	candidate,err:=Render(planFor(t,18443,0),[]Profile{profile})
	if err!=nil{t.Fatal(err)}
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	out,err:=ValidateOffline(ctx,candidate,bin,pin)
	if err!=nil{t.Fatalf("pinned real Xray rejected offline Reality config: %v",err)}
	if !out.XraySyntaxValidated||out.CanActivate||!out.NeedsDataPlaneProof||
		!out.NeedsSubscriptionCommit{
		t.Fatal("real Xray syntax test cannot authorize deployment")
	}
}
