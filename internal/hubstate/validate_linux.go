//go:build linux

package hubstate

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
	"github.com/darktunnelmika/dark-xray-pro/internal/labrevision"
	"github.com/darktunnelmika/dark-xray-pro/internal/nodehistory"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegramhub"
)

func maxRoleSize(role string) int64 {
	switch role {
	case "business":
		return maxSnapshotPayload
	case "registry":
		return 4 << 20
	case "replacements":
		return 8 << 20
	case "config_stage":
		return 16 << 20
	case "lab_revisions":
		return 32 << 20
	case "node_history":
		return nodehistory.MaxHistoryDBSize
	case "inbound_runtime":
		return 64 << 20
	case "owner_report_db":
		return maxSnapshotPayload
	case "owner_report_config":
		return 4096
	case "owner_report_token":
		return 256
	case "agent_tls_cert", "agent_tls_key", "inventory":
		return 1 << 20
	}
	return -1
}

// ValidateSnapshot is shared by offline creation and quarantined restore.
// It opens ONLY fixed archive names below stage: original Root/Path values are
// provenance and are never dereferenced. It has no activation, recovery,
// migration, network, worker or writer-constructor side effect. Source process
// stopped proof remains a separate pre-snapshot requirement, not a claim about
// processes on the restore host.
func ValidateSnapshot(ctx context.Context, stage string, inv Inventory) error {
	if ctx == nil || ctx.Err() != nil || inv.SnapshotFiles == nil || ValidateArchiveInventory(inv) != nil || canonicalPath(stage) != nil {
		return ErrUnsafe
	}
	st, err := os.Lstat(stage)
	if err != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || !owned(st) {
		return ErrUnsafe
	}
	paths := map[string]string{}
	for _, f := range inv.SnapshotFiles {
		if err = ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(stage, filepath.FromSlash(f.Name))
		file, e := openPrivate(path, syscall.O_RDONLY, 0, true)
		if e != nil {
			return ErrUnsafe
		}
		st, e := file.Stat()
		file.Close()
		if e != nil || st.Size() != f.Size || st.Size() > maxRoleSize(f.Role) {
			return ErrUnsafe
		}
		paths[f.Role] = path
	}
	for _, r := range inv.Resources {
		if !r.Present {
			if _, e := os.Lstat(filepath.Join(stage, filepath.FromSlash(r.Name))); !errors.Is(e, os.ErrNotExist) {
				return ErrUnsafe
			}
		}
	}
	read := func(role string) ([]byte, error) {
		path, exists := paths[role]
		if !exists {
			return nil, ErrUnsafe
		}
		return readPrivate(path, maxRoleSize(role), true)
	}
	nativeKey, err := hex.DecodeString(inv.Environment["DARK_PRO_NATIVE_INBOUND_KEY_HEX"])
	if err != nil {
		return ErrUnsafe
	}
	if _, err = business.ValidateOfflineBackup(ctx, paths["business"]); err != nil || business.ValidateOfflineBackupAssets(ctx, paths["business"], nativeKey) != nil {
		return ErrUnsafe
	}
	stageKey, err := hex.DecodeString(inv.Environment["DARK_PRO_STAGE_KEY_HEX"])
	if err != nil {
		return ErrUnsafe
	}
	checkPayload := business.ValidateOfflineBackupPayloadAssets
	for _, role := range []string{"registry", "replacements", "config_stage", "lab_revisions"} {
		if paths[role] == "" {
			continue
		}
		raw, e := read(role)
		if e != nil {
			return ErrUnsafe
		}
		switch role {
		case "registry":
			e = noderegistry.ValidateOfflineBackup(raw)
		case "replacements":
			e = replacement.ValidateOfflineBackup(raw)
		case "config_stage":
			e = configstage.ValidateOfflineBackup(raw, stageKey, checkPayload)
		case "lab_revisions":
			derived := sha256.Sum256(append([]byte("DARK-PRO-LABREV-AES-v1:"), stageKey...))
			e = labrevision.ValidateOfflineBackup(raw, derived[:], checkPayload)
		}
		if e != nil {
			return ErrUnsafe
		}
	}
	if paths["node_history"] != "" && nodehistory.ValidateOfflineBackup(ctx, paths["node_history"]) != nil {
		return ErrUnsafe
	}
	if paths["inbound_runtime"] != "" {
		key, install, e := runtimeKeys(inv.Environment["DARK_PRO_NATIVE_INBOUND_KEY_HEX"])
		if e != nil {
			return ErrUnsafe
		}
		journal, e := inboundruntime.ValidateOfflineJournal(paths["inbound_runtime"], key, "hub-local", install)
		if e != nil {
			return ErrUnsafe
		}
		for _, bundle := range journal.Bundles {
			if len(bundle.NativeConfig) > 0 && checkPayload(bundle.NativeConfig) != nil {
				return ErrUnsafe
			}
			for _, component := range bundle.Components {
				if checkPayload(component.Config) != nil || len(component.Manifest) > 0 && checkPayload(component.Manifest) != nil {
					return ErrUnsafe
				}
			}
		}
	}
	if paths["owner_report_config"] != "" || paths["owner_report_token"] != "" || paths["owner_report_db"] != "" {
		config, e := read("owner_report_config")
		if e != nil {
			return ErrUnsafe
		}
		token, e := read("owner_report_token")
		if e != nil {
			return ErrUnsafe
		}
		if telegramhub.ValidateOfflineBackup(ctx, paths["owner_report_db"], config, token) != nil {
			return ErrUnsafe
		}
	}
	if paths["agent_tls_cert"] != "" || paths["agent_tls_key"] != "" {
		cert, e := read("agent_tls_cert")
		if e != nil {
			return ErrUnsafe
		}
		key, e := read("agent_tls_key")
		if e != nil {
			return ErrUnsafe
		}
		if _, e = tls.X509KeyPair(cert, key); e != nil {
			return ErrUnsafe
		}
	}
	return ctx.Err()
}
