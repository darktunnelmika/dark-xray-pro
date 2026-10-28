package probackup

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"filippo.io/age"
)

var magic = [...]byte{'D', 'A', 'R', 'K', 'P', 'R', 'O', 'B', 'A', 'C', 'K', 'U', 'P', 0, 0, Version}

const (
	recordFile     byte = 1
	recordManifest byte = 2
	recordEnd      byte = 255
)

// Write writes a single binary age file containing one gzip member and a
// versioned record stream. It closes every source, but never closes dst.
// Successful return includes successful gzip and age finalization. Callers
// must separately sync and atomically publish their ciphertext destination.
func Write(ctx context.Context, dst io.Writer, recipient string, meta Metadata, files []FileSource, limits Limits) (Manifest, error) {
	var empty Manifest
	limits, err := limits.resolve()
	if err != nil || ctx == nil || dst == nil || !validMetadata(meta) || len(files) == 0 || len(files) > limits.MaxFiles {
		return empty, ErrOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if len(recipient) > 256 {
		return empty, ErrRecipient
	}
	key, err := age.ParseX25519Recipient(strings.TrimSpace(recipient))
	if err != nil {
		return empty, ErrRecipient
	}
	files = append([]FileSource(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	m := cloneManifest(Manifest{Version: Version, Metadata: meta, Files: make([]FileEntry, 0, len(files))})
	var total int64
	seen := make(map[string]bool, len(files))
	previous := ""
	for _, f := range files {
		_, roleOK := roleByte(f.Role)
		if !addOrderedName(f.Name, previous, seen) || !roleOK || f.Open == nil || f.Size < 0 || f.Size > limits.MaxFileBytes {
			return empty, ErrOptions
		}
		previous = f.Name
		if f.Size > limits.MaxPlaintextBytes-total {
			return empty, ErrLimit
		}
		total += f.Size
		m.Files = append(m.Files, FileEntry{Name: f.Name, Role: f.Role, Size: f.Size, SHA256: strings.Repeat("0", 64)})
	}
	// Preflight bounded metadata before opening any source or writing output.
	encoded, err := json.Marshal(m)
	if err != nil || len(encoded) > limits.MaxManifestBytes {
		return empty, ErrLimit
	}
	cipher := &budgetWriter{ctx: ctx, w: dst, remaining: limits.MaxCiphertextBytes}
	encrypted, err := age.Encrypt(cipher, key)
	if err != nil {
		return empty, safeError(err, ErrIO)
	}
	z, err := gzip.NewWriterLevel(encrypted, gzip.BestSpeed)
	if err != nil {
		return empty, ErrIO
	}
	plain := &budgetWriter{ctx: ctx, w: z, remaining: limits.MaxPlaintextBytes}
	if _, err = plain.Write(magic[:]); err != nil {
		return empty, safeError(err, ErrIO)
	}
	buf := make([]byte, copyBufferSize)
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		var hdr [12]byte
		hdr[0] = recordFile
		hdr[1], _ = roleByte(f.Role)
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(f.Name)))
		binary.BigEndian.PutUint64(hdr[4:12], uint64(f.Size))
		if _, err = plain.Write(hdr[:]); err != nil {
			return empty, safeError(err, ErrIO)
		}
		if _, err = io.WriteString(plain, f.Name); err != nil {
			return empty, safeError(err, ErrIO)
		}
		src, openErr := f.Open(ctx)
		if openErr != nil || src == nil {
			if src != nil {
				_ = src.Close()
			}
			return empty, safeError(openErr, ErrSource)
		}
		h := sha256.New()
		copyErr := copyExact(ctx, io.MultiWriter(plain, h), src, f.Size, buf)
		if copyErr == nil {
			copyErr = requireEOF(ctx, src)
		}
		closeErr := src.Close()
		if copyErr != nil {
			return empty, safeError(copyErr, ErrSource)
		}
		if closeErr != nil {
			return empty, ErrSource
		}
		m.Files[i].SHA256 = hex.EncodeToString(h.Sum(nil))
	}
	encoded, err = json.Marshal(m)
	if err != nil || len(encoded) > limits.MaxManifestBytes {
		return empty, ErrLimit
	}
	var hdr [5]byte
	hdr[0] = recordManifest
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(encoded)))
	for _, part := range [][]byte{hdr[:], encoded, {recordEnd}} {
		if _, err = plain.Write(part); err != nil {
			return empty, safeError(err, ErrIO)
		}
	}
	if err = z.Close(); err != nil {
		return empty, safeError(err, ErrIO)
	}
	if err = encrypted.Close(); err != nil {
		return empty, safeError(err, ErrIO)
	}
	if cipher.err != nil {
		return empty, cipher.err
	}
	return m, nil
}
