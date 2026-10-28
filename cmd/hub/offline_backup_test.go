package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
)

func privateStateTemp(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if e := os.Chmod(d, 0700); e != nil {
		t.Fatal(e)
	}
	return d
}
func testStateOptions(root string) hubstate.Options {
	return hubstate.Options{Root: root, Registry: filepath.Join(root, "registry.json"), Replacements: filepath.Join(root, "replacements.json"), BusinessDB: filepath.Join(root, "business.sqlite"), SourceRevision: strings.Repeat("a", 40), Environment: []string{"DARK_PRO_LAB_MODE=1", "DARK_PRO_ENABLE_BUSINESS_CORE=1", "DARK_PRO_LAB_TOKEN=" + strings.Repeat("b", 64)}}
}
func TestOfflineStateLockAppliesEvenWhenBackupDisabled(t *testing.T) {
	root := privateStateTemp(t)
	o := testStateOptions(root)
	if e := os.WriteFile(filepath.Join(root, hubstate.InventoryName), []byte("stale"), 0600); e != nil {
		t.Fatal(e)
	}
	l, inv, e := prepareOfflineState(o)
	if e != nil || l == nil || inv != nil {
		t.Fatal("business writer omitted lock", e)
	}
	defer l.Close()
	if _, _, e = prepareOfflineState(o); !errors.Is(e, hubstate.ErrBusy) {
		t.Fatal("second startup not blocked", e)
	}
	if _, e = os.Stat(filepath.Join(root, hubstate.InventoryName)); !os.IsNotExist(e) {
		t.Fatal("stale inventory retained")
	}
	if _, e = os.Stat(o.BusinessDB); !os.IsNotExist(e) {
		t.Fatal("preparation opened business writer")
	}
}
func TestOfflineStateQuarantineAndStrictFlagsBeforeWriters(t *testing.T) {
	for _, kind := range []string{"quarantine", "invalidflag", "unrecognized"} {
		t.Run(kind, func(t *testing.T) {
			o := testStateOptions(privateStateTemp(t))
			o.Environment = append(o.Environment, hubstate.EnableEnv+"=1")
			switch kind {
			case "quarantine":
				if e := os.WriteFile(filepath.Join(o.Root, hubstate.QuarantineName), []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
			case "invalidflag":
				o.Environment[len(o.Environment)-1] = hubstate.EnableEnv + "=yes"
			case "unrecognized":
				o.Environment = append(o.Environment, "DARK_PRO_ENABLE_OTHER_PERSISTENCE=1")
			}
			l, _, e := prepareOfflineState(o)
			if l != nil {
				l.Close()
			}
			if e == nil {
				t.Fatal("unsafe startup accepted")
			}
			if _, e = os.Stat(o.BusinessDB); !os.IsNotExist(e) {
				t.Fatal("unsafe startup opened business database")
			}
		})
	}
}

func TestOfflineStateNonbusinessRegistryWriterParticipates(t *testing.T) {
	o := testStateOptions(privateStateTemp(t))
	o.Environment = []string{"DARK_PRO_ENABLE_BUSINESS_CORE=0", hubstate.EnableEnv + "=0"}
	if err := os.WriteFile(filepath.Join(o.Root, hubstate.InventoryName), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	lock, inv, err := prepareOfflineState(o)
	if err != nil || lock == nil || inv != nil {
		t.Fatal("nonbusiness writer bypassed fence", err)
	}
	defer lock.Close()
	if _, err = hubstate.Acquire(o.Root); !errors.Is(err, hubstate.ErrBusy) {
		t.Fatal("backup raced registry writer", err)
	}
	if _, err = os.Lstat(filepath.Join(o.Root, hubstate.InventoryName)); !os.IsNotExist(err) {
		t.Fatal("stale inventory survived", err)
	}
}

func TestOfflineStateQuarantineIncludesConfiguredAssets(t *testing.T) {
	for _, role := range []string{"cert", "key", "bot", "botjournal"} {
		t.Run(role, func(t *testing.T) {
			o := testStateOptions(privateStateTemp(t))
			restored := privateStateTemp(t)
			if role == "botjournal" {
				restored = filepath.Join(o.Root, "owner-report-bot")
				if err := os.Mkdir(restored, 0700); err != nil {
					t.Fatal(err)
				}
				o.OwnerReportConfig = "/etc/dark-xray-pro-lab/owner-report-bot/config.json"
			}
			if err := os.WriteFile(filepath.Join(restored, hubstate.QuarantineName), []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			asset := filepath.Join(restored, "config", "asset")
			switch role {
			case "cert":
				o.AgentTLSCert = asset
			case "key":
				o.AgentTLSKey = asset
			case "bot":
				o.OwnerReportConfig = asset
			}
			if lock, _, err := prepareOfflineState(o); err == nil {
				if lock != nil {
					lock.Close()
				}
				t.Fatal("quarantined asset admitted")
			}
			if _, err := os.Lstat(o.BusinessDB); !os.IsNotExist(err) {
				t.Fatal("writer opened before quarantine check")
			}
		})
	}
}
