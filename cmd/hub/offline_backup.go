package main

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegramhub"
)

// Retain the file descriptor until the operating system ends this HUB process.
// HTTP/worker shutdown has bounded waits; an explicit unlock in main's defers
// could otherwise admit backup while a timed-out writer is still finishing.
// The global reference also prevents an os.File finalizer from releasing it.
var hubStateProcessLock *hubstate.Lock

func buildProvenance() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	var revision string
	var modified bool
	for _, v := range info.Settings {
		if v.Key == "vcs.revision" {
			revision = v.Value
		}
		if v.Key == "vcs.modified" {
			modified = v.Value == "true"
		}
	}
	return revision, modified
}

func prepareOfflineState(o hubstate.Options) (*hubstate.Lock, *hubstate.Inventory, error) {
	env := map[string]string{}
	for _, item := range o.Environment {
		k, v, ok := strings.Cut(item, "=")
		if ok {
			env[k] = v
		}
	}
	flag := env[hubstate.EnableEnv]
	if flag != "" && flag != "0" && flag != "1" {
		return nil, nil, hubstate.ErrUnsafe
	}
	paths := []string{o.Root, o.Registry, o.Replacements, o.Staging, o.BusinessDB, o.LabRevisions, o.NodeHistoryDB, env["DARK_PRO_INBOUND_RUNTIME_DIR"], o.AgentTLSCert, o.AgentTLSKey, o.OwnerReportConfig}
	if env[telegramhub.OwnerReportLABEnableEnv] == "1" || o.OwnerReportConfig != "" {
		paths = append(paths, filepath.Join(o.Root, "owner-report-bot/inbox.sqlite"), telegramhub.OwnerReportLABTokenPath)
	}
	if e := hubstate.CheckQuarantine(paths...); e != nil {
		return nil, nil, e
	}
	needLock := env["DARK_PRO_ENABLE_BUSINESS_CORE"] == "1" || flag == "1"
	// Installed nonbusiness HUBs also write the registry/replacement files;
	// they participate whenever their paths are in the fixed LAB root.
	for _, p := range paths[1:] {
		if p == "" {
			continue
		}
		abs, e := filepath.Abs(p)
		if e != nil {
			return nil, nil, hubstate.ErrUnsafe
		}
		rel, e := filepath.Rel(o.Root, abs)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			needLock = true
		}
	}
	if !needLock {
		return nil, nil, nil
	}
	lock, e := hubstate.Acquire(o.Root)
	if e != nil {
		return nil, nil, e
	}
	if e = hubstate.InvalidateInventory(lock); e != nil {
		lock.Close()
		return nil, nil, e
	}
	if flag != "1" {
		return lock, nil, nil
	}
	inv, e := hubstate.BuildInventory(o)
	if e != nil {
		lock.Close()
		return nil, nil, e
	}
	return lock, &inv, nil
}

func offlineOptions(registry, replacements, staging, business, versions, history, cert, key, bot, listen, agent, static string) hubstate.Options {
	revision, modified := buildProvenance()
	return hubstate.Options{Root: hubstate.ProductionRoot, Registry: registry, Replacements: replacements, Staging: staging, BusinessDB: business, LabRevisions: versions, NodeHistoryDB: history, AgentTLSCert: cert, AgentTLSKey: key, OwnerReportConfig: bot, Listen: listen, AgentTLSListen: agent, Static: static, SourceRevision: revision, BuildModified: modified, Environment: os.Environ()}
}
