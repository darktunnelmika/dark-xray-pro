//go:build linux

package telegrambot

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Only the final service-owned directory may be created. Existing permissions,
// owners and ancestors are validated, never repaired or broadened here.
func ensureManagedParent(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrConfig
	}
	dir := filepath.Dir(path)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		for p := filepath.Dir(dir); ; p = filepath.Dir(p) {
			info, err := os.Lstat(p)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return ErrConfig
			}
			if p == "/" {
				break
			}
		}
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return ErrConfig
		}
	} else if err != nil {
		return ErrConfig
	}
	return privateParent(path)
}

func readManagedConfig(path string) (fileConfig, [32]byte, bool, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return fileConfig{}, [32]byte{}, false, nil
	} else if err != nil {
		return fileConfig{}, [32]byte{}, false, ErrConfig
	}
	raw, err := readPrivate(path, 16<<10)
	if err != nil {
		return fileConfig{}, [32]byte{}, true, ErrConfig
	}
	defer clear(raw)
	config, err := parseConfig(raw)
	if err != nil {
		return fileConfig{}, [32]byte{}, true, ErrConfig
	}
	return config, sha256.Sum256(raw), true, nil
}

// publishManagedConfig uses a private same-directory temporary inode, fsync,
// atomic rename and directory fsync. The digest is a CAS fence in addition to
// the manager's serialized revision check and the HUB/journal lifetime locks.
// A true published result means rename happened, including an uncertain final
// directory-sync failure: callers must retain the visible revision, not resume
// an obsolete token or claim that a durable activation succeeded.
func publishManagedConfig(path string, config fileConfig, expected *[32]byte) (digest [32]byte, published bool, result error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return digest, false, ErrConfig
	}
	defer clear(raw)
	if config.Version != 2 {
		return digest, false, ErrConfig
	}
	if _, err := parseConfig(raw); err != nil || privateParent(path) != nil {
		return digest, false, ErrConfig
	}
	digest = sha256.Sum256(raw)
	fd, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return digest, false, ErrConfig
	}
	dir := os.NewFile(uintptr(fd), "private-telegram-config-directory")
	defer dir.Close()
	before, err := dir.Stat()
	if err != nil {
		return digest, false, ErrConfig
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return digest, false, ErrConfig
	}
	name := ".telegram-config-" + hex.EncodeToString(random[:])
	tempFD, err := unix.Openat(fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return digest, false, ErrConfig
	}
	defer unix.Unlinkat(fd, name, 0)
	temp := os.NewFile(uintptr(tempFD), "private-telegram-config")
	defer temp.Close()
	if n, err := temp.Write(raw); err != nil || n != len(raw) || temp.Sync() != nil {
		return digest, false, ErrConfig
	}
	info, err := temp.Stat()
	if err != nil || !privateRegular(info) {
		return digest, false, ErrConfig
	}
	current, err := os.Lstat(filepath.Dir(path))
	if err != nil || !os.SameFile(before, current) || privateParent(path) != nil {
		return digest, false, ErrConfig
	}
	_, actual, exists, err := readManagedConfig(path)
	if err != nil {
		return digest, false, err
	}
	if (expected == nil && exists) || (expected != nil && (!exists || actual != *expected)) {
		return digest, false, ErrConflict
	}
	flags := uint(0)
	if expected == nil {
		flags = unix.RENAME_NOREPLACE
	}
	if err := unix.Renameat2(fd, name, fd, filepath.Base(path), flags); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return digest, false, ErrConflict
		}
		return digest, false, ErrConfig
	}
	if dir.Sync() != nil {
		return digest, true, ErrConfig
	}
	return digest, true, nil
}
