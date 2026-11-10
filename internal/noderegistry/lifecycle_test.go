package noderegistry

import (
	"strings"
	"testing"
)

func TestLifecycleDurableFenceReplayAndManualEnable(t *testing.T) {
	r, path, _ := pairedRenameRegistry(t)
	in := LifecycleInput{EventID: strings.Repeat("a", 32), InstallationID: testInstall, ExpectedRevision: 1, Action: "disable"}
	rec, e := r.BeginLifecycle("lab-1", "owner", in)
	if e != nil || rec.Completed || r.DeploymentAllowed("lab-1", testInstall) {
		t.Fatal(rec, e)
	}
	r, e = Open(path, nil)
	if e != nil || r.DeploymentAllowed("lab-1", testInstall) {
		t.Fatal("crash lost fence", e)
	}
	if _, ok := r.Authenticate("lab-1", testInstall, testToken); !ok {
		t.Fatal("lost settlement authentication")
	}
	if _, e = r.BeginLifecycle("lab-1", "owner", in); e != nil {
		t.Fatal(e)
	}
	if _, e = r.FinishLifecycle(in.EventID, []uint64{501}); e != nil {
		t.Fatal(e)
	}
	if r.DeploymentAllowed("lab-1", testInstall) {
		t.Fatal("disabled node admitted")
	}
	in = LifecycleInput{EventID: strings.Repeat("b", 32), InstallationID: testInstall, ExpectedRevision: 2, Action: "enable"}
	if _, e = r.BeginLifecycle("lab-1", "owner", in); e != nil {
		t.Fatal(e)
	}
	if r.DeploymentAllowed("lab-1", testInstall) {
		t.Fatal("enable bypassed detach recovery")
	}
	if _, e = r.FinishLifecycle(in.EventID, nil); e != nil || !r.DeploymentAllowed("lab-1", testInstall) {
		t.Fatal(e)
	}
	in = LifecycleInput{EventID: strings.Repeat("c", 32), InstallationID: testInstall, ExpectedRevision: 3, Action: "remove"}
	if _, e = r.BeginLifecycle("lab-1", "owner", in); e != nil {
		t.Fatal(e)
	}
	if _, e = r.FinishLifecycle(in.EventID, nil); e != nil {
		t.Fatal(e)
	}
	r, e = Open(path, nil)
	if e != nil || r.Lifecycle("lab-1").Status != "removed" || r.DeploymentAllowed("lab-1", testInstall) {
		t.Fatal(e)
	}
	in = LifecycleInput{EventID: strings.Repeat("d", 32), InstallationID: testInstall, ExpectedRevision: 4, Action: "enable"}
	if _, e = r.BeginLifecycle("lab-1", "owner", in); e == nil {
		t.Fatal("removed identity resurrected")
	}
}
