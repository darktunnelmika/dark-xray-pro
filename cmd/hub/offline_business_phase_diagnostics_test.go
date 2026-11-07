package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
)

func TestOfflineStateWithBusinessPhaseDiagnostics(t *testing.T) {
	const flag = "DARK_PRO_ENABLE_BUSINESS_PHASE_DIAGNOSTICS"
	o := testStateOptions(privateStateTemp(t))
	o.Environment = append(o.Environment, hubstate.EnableEnv+"=1", flag+"=1")
	getenv := func(name string) string {
		for _, item := range o.Environment {
			key, value, ok := strings.Cut(item, "=")
			if ok && key == name {
				return value
			}
		}
		return ""
	}
	if enabled, err := businessPhaseDiagnosticsEnabled(getenv); err != nil || !enabled {
		t.Fatal("business diagnostic opt-in refused the startup fixture", err)
	}
	staleInventory := filepath.Join(o.Root, hubstate.InventoryName)
	if err := os.WriteFile(staleInventory, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	lock, inv, err := prepareOfflineState(o)
	if lock != nil {
		t.Cleanup(func() { lock.Close() })
	}
	if err != nil || lock == nil || inv == nil {
		t.Fatal("offline state fence refused the accepted diagnostic opt-in", err)
	}
	if inv.Environment[flag] != "1" {
		t.Fatal("offline inventory omitted the enabled diagnostic opt-in")
	}
	if err := hubstate.ValidateArchiveInventory(*inv); err != nil {
		t.Fatal("offline diagnostic inventory failed structural validation", err)
	}
	second, err := hubstate.Acquire(o.Root)
	if second != nil {
		second.Close()
	}
	if !errors.Is(err, hubstate.ErrBusy) {
		t.Fatal("diagnostic startup did not retain the exclusive state fence", err)
	}
	if _, err := os.Lstat(staleInventory); !os.IsNotExist(err) {
		t.Fatal("diagnostic startup retained stale inventory", err)
	}
	if _, err := os.Lstat(o.BusinessDB); !os.IsNotExist(err) {
		t.Fatal("state fence opened the business database", err)
	}
}
