//go:build linux

package restoreapply

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const restoreHeadroom int64 = 64 << 20

type deviceBudget struct {
	path              string
	copies, temporary int64
}

// CheckSpace budgets the ORIGINAL databases and WAL as well as the candidate.
// The decrypted candidate already occupies disk. Preparing a transaction adds
// both retained originals and candidate copies, and each atomic replacement
// temporarily needs one additional file. Rollback needs the original-file
// temporary allowance too. Independent filesystems receive separate budgets.
func CheckSpace(root, directory string, replacements []Replacement, uid int) error {
	needs, err := spaceNeeds(root, directory, replacements, uid)
	if err != nil {
		return err
	}
	for _, need := range needs {
		var fs syscall.Statfs_t
		if syscall.Statfs(need.path, &fs) != nil || fs.Bsize <= 0 || fs.Flags&syscall.MS_RDONLY != 0 {
			return ErrUnsafe
		}
		required := need.copies + need.temporary + restoreHeadroom
		blocks := uint64((required + fs.Bsize - 1) / fs.Bsize)
		if fs.Bavail < blocks {
			return ErrUnsafe
		}
	}
	return nil
}

func spaceNeeds(root, directory string, replacements []Replacement, uid int) (map[uint64]deviceBudget, error) {
	if !canonical(root) || !canonical(directory) || len(replacements) == 0 || len(replacements) > 128 {
		return nil, ErrUnsafe
	}
	device := func(path string) (uint64, error) {
		st, err := os.Stat(path)
		if err != nil || !st.IsDir() {
			return 0, ErrUnsafe
		}
		s, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, ErrUnsafe
		}
		return uint64(s.Dev), nil
	}
	staging, err := device(directory)
	if err != nil {
		return nil, err
	}
	needs := map[uint64]deviceBudget{staging: {path: directory}}
	seen := map[string]bool{}
	var copies int64
	for _, replacement := range replacements {
		wanted := image{Present: replacement.Present, Size: replacement.Size, SHA256: replacement.SHA256}
		if !validRel(replacement.Destination) || seen[replacement.Destination] || !validImage(wanted) {
			return nil, ErrUnsafe
		}
		seen[replacement.Destination] = true
		destination := filepath.Join(root, replacement.Destination)
		var oldSize int64
		if _, err := os.Lstat(destination); err == nil {
			file, err := checked(destination, uid)
			if err != nil {
				return nil, err
			}
			st, err := file.Stat()
			file.Close()
			if err != nil {
				return nil, ErrUnsafe
			}
			oldSize = st.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, ErrUnsafe
		}
		copies += oldSize + wanted.Size
		if copies > 2*maxBytes {
			return nil, ErrUnsafe
		}
		if oldSize == 0 && !wanted.Present {
			continue
		}
		parent := filepath.Dir(destination)
		if !canonical(parent) {
			return nil, ErrUnsafe
		}
		dev, err := device(parent)
		if err != nil {
			return nil, err
		}
		need := needs[dev]
		if need.path == "" {
			need.path = parent
		}
		need.temporary = max(need.temporary, oldSize, wanted.Size)
		needs[dev] = need
	}
	need := needs[staging]
	need.copies = copies
	needs[staging] = need
	return needs, nil
}
