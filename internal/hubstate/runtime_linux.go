//go:build linux

package hubstate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
)

func runtimeKeys(native string) (key []byte, install string, err error) {
	raw, e := hex.DecodeString(native)
	if e != nil || len(raw) != 32 || hex.EncodeToString(raw) != native {
		return nil, "", ErrUnsafe
	}
	h := hmac.New(sha256.New, raw)
	h.Write([]byte("DARK-PRO-EXPLICIT-INBOUND-HUB-v1"))
	install = hex.EncodeToString(h.Sum(nil)[:16])
	h = hmac.New(sha256.New, raw)
	h.Write([]byte("DARK-PRO-HUB-LOCAL-EXECUTION-v1"))
	return []byte(hex.EncodeToString(h.Sum(nil))), install, nil
}
func holdRuntimeStopped(ctx context.Context, inv Inventory) (func(), error) {
	for _, r := range inv.Resources {
		if r.Role != "inbound_runtime" {
			continue
		}
		if _, e := os.Lstat(r.Path); errors.Is(e, os.ErrNotExist) && !r.Required {
			entries, de := os.ReadDir(filepath.Dir(r.Path))
			if errors.Is(de, os.ErrNotExist) || de == nil && len(entries) == 0 {
				return func() {}, nil
			}
			return nil, ErrUnsafe
		} else if e != nil {
			return nil, ErrUnsafe
		}
		dir := filepath.Dir(r.Path)
		st, e := os.Lstat(dir)
		if e != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || !owned(st) {
			return nil, ErrUnsafe
		}
		lock, e := openPrivate(filepath.Join(dir, "writer.lock"), syscall.O_RDONLY, 0, true)
		if e != nil {
			return nil, ErrUnsafe
		}
		if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			lock.Close()
			return nil, ErrBusy
		}
		release := func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }
		entries, e := os.ReadDir(dir)
		if e != nil || len(entries) > 2 {
			release()
			return nil, ErrUnsafe
		}
		for _, entry := range entries {
			if entry.Name() != "state.enc" && entry.Name() != "writer.lock" {
				release()
				return nil, ErrUnsafe
			}
		}
		key, install, e := runtimeKeys(inv.Environment["DARK_PRO_NATIVE_INBOUND_KEY_HEX"])
		if e != nil {
			release()
			return nil, e
		}
		state, e := inboundruntime.ValidateOfflineJournal(r.Path, key, "hub-local", install)
		if e != nil || state.Phase == "preparing" {
			release()
			return nil, ErrUnsafe
		}
		if len(state.Epochs) > 0 {
			self, readErr := os.ReadFile("/proc/self/stat")
			fields := strings.Fields(string(self))
			if readErr != nil || len(fields) == 0 || fields[0] != strconv.Itoa(os.Getpid()) {
				release()
				return nil, ErrUnsafe // A different PID namespace cannot prove absence.
			}
		}
		for _, epoch := range state.Epochs {
			var members map[string]string
			if json.Unmarshal([]byte(epoch.ProcessIdentity), &members) != nil {
				release()
				return nil, ErrUnsafe
			}
			for _, raw := range members {
				if ctx.Err() != nil {
					release()
					return nil, ctx.Err()
				}
				var member inboundexec.ProcessMember
				if json.Unmarshal([]byte(raw), &member) != nil || verifyStoppedMember(member, os.ReadFile) != nil {
					release()
					return nil, ErrBusy
				}
			}
		}
		return release, nil
	}
	return func() {}, nil
}

// Only explicit stopped evidence succeeds. A permission or parser failure is
// not absence; a PID reused by another process is never killed or adopted.
func verifyStoppedMember(m inboundexec.ProcessMember, read func(string) ([]byte, error)) error {
	if m.PID <= 0 || m.StartTicks == 0 || len(m.BootID) != 36 {
		return ErrUnsafe
	}
	boot, e := read("/proc/sys/kernel/random/boot_id")
	if e != nil || len(strings.TrimSpace(string(boot))) != 36 {
		return ErrUnsafe
	}
	if strings.TrimSpace(string(boot)) != m.BootID {
		return nil
	}
	raw, e := read("/proc/" + strconv.Itoa(m.PID) + "/stat")
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return ErrUnsafe
	}
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return ErrUnsafe
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) <= 19 {
		return ErrUnsafe
	}
	ticks, e := strconv.ParseUint(fields[19], 10, 64)
	if e != nil || ticks == 0 {
		return ErrUnsafe
	}
	if ticks != m.StartTicks || fields[0] == "Z" || fields[0] == "X" {
		return nil
	}
	return ErrBusy
}
