package probackup

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"filippo.io/age"
)

const (
	maxAgeHeader  = 64 << 10
	maxAgeLine    = 1024
	maxAgeStanzas = 32
)

// age's header parser accepts extensible stanza sizes. This independent input
// bound runs before any age parsing or key operation, then replays exactly the
// original header. age still performs all syntax and cryptographic validation.
func boundedAgeInput(src io.Reader) (io.Reader, error) {
	r := bufio.NewReaderSize(src, maxAgeLine)
	header := make([]byte, 0, 1024)
	stanzas := 0
	for {
		line, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			return nil, ErrLimit
		}
		if err != nil {
			return nil, safeError(err, ErrIntegrity)
		}
		if len(header)+len(line) > maxAgeHeader {
			return nil, ErrLimit
		}
		if len(header) == 0 && !bytes.Equal(line, []byte("age-encryption.org/v1\n")) {
			return nil, ErrFormat
		}
		header = append(header, line...)
		if bytes.HasPrefix(line, []byte("->")) {
			stanzas++
			if stanzas > maxAgeStanzas {
				return nil, ErrLimit
			}
		}
		if bytes.HasPrefix(line, []byte("---")) {
			if stanzas == 0 {
				return nil, ErrFormat
			}
			return io.MultiReader(bytes.NewReader(header), r), nil
		}
	}
}

type fileSink func(FileEntry) (io.WriteCloser, error)
type discardCloser struct{}

func (discardCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardCloser) Close() error                { return nil }

// Verify authenticates and consumes the complete archive without writing any
// files. It verifies container integrity, not application/database semantics;
// Restore additionally requires an application validator before publication.
func Verify(ctx context.Context, src io.Reader, identity string, limits Limits) (Manifest, error) {
	return readArchive(ctx, src, identity, limits, func(FileEntry) (io.WriteCloser, error) { return discardCloser{}, nil })
}

func readArchive(ctx context.Context, src io.Reader, identity string, limits Limits, sink fileSink) (Manifest, error) {
	var empty Manifest
	limits, err := limits.resolve()
	if err != nil || ctx == nil || src == nil || sink == nil {
		return empty, ErrOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if len(identity) > 256 {
		return empty, ErrIdentity
	}
	key, err := age.ParseX25519Identity(strings.TrimSpace(identity))
	if err != nil {
		return empty, ErrIdentity
	}
	cipher := &budgetReader{ctx: ctx, r: src, remaining: limits.MaxCiphertextBytes}
	bounded, err := boundedAgeInput(cipher)
	if err != nil {
		return empty, err
	}
	decrypted, err := age.Decrypt(bounded, key)
	if err != nil {
		return empty, safeError(err, ErrIntegrity)
	}
	compressed := bufio.NewReaderSize(decrypted, 4096)
	z, err := gzip.NewReader(compressed)
	if err != nil {
		return empty, safeError(err, ErrIntegrity)
	}
	defer z.Close()
	z.Multistream(false)
	if z.Name != "" || z.Comment != "" || len(z.Extra) != 0 || !z.ModTime.IsZero() {
		return empty, ErrFormat
	}
	plain := &budgetReader{ctx: ctx, r: z, remaining: limits.MaxPlaintextBytes}
	m, err := readRecords(ctx, plain, sink, limits)
	if err != nil {
		return empty, err
	}
	// Each layer must reach its own real EOF. In particular gzip EOF is not
	// age EOF, and Close alone does not check gzip checksums or age finality.
	if err := requireEOF(ctx, plain); err != nil {
		return empty, err
	}
	if err := z.Close(); err != nil {
		return empty, safeError(err, ErrIntegrity)
	}
	if err := requireEOF(ctx, compressed); err != nil {
		return empty, err
	}
	if err := requireEOF(ctx, cipher); err != nil {
		return empty, err
	}
	return m, nil
}

func readRecords(ctx context.Context, plain *budgetReader, sink fileSink, limits Limits) (Manifest, error) {
	var empty Manifest
	var intro [len(magic)]byte
	if _, err := io.ReadFull(plain, intro[:]); err != nil {
		return empty, safeError(err, ErrIntegrity)
	}
	if intro != magic {
		return empty, ErrFormat
	}
	observed := make([]FileEntry, 0)
	seen := make(map[string]bool)
	previous := ""
	buf := make([]byte, copyBufferSize)
	for {
		var tag [1]byte
		if _, err := io.ReadFull(plain, tag[:]); err != nil {
			return empty, safeError(err, ErrIntegrity)
		}
		switch tag[0] {
		case recordFile:
			if len(observed) >= limits.MaxFiles {
				return empty, ErrLimit
			}
			var hdr [11]byte
			if _, err := io.ReadFull(plain, hdr[:]); err != nil {
				return empty, safeError(err, ErrIntegrity)
			}
			role := int(hdr[0])
			nameSize := int(binary.BigEndian.Uint16(hdr[1:3]))
			size := binary.BigEndian.Uint64(hdr[3:11])
			if role == 0 || role >= len(wireRoles) || nameSize < 1 || nameSize > 240 {
				return empty, ErrFormat
			}
			if size > uint64(limits.MaxFileBytes) || size > uint64(plain.remaining) {
				return empty, ErrLimit
			}
			nameBytes := make([]byte, nameSize)
			if _, err := io.ReadFull(plain, nameBytes); err != nil {
				return empty, safeError(err, ErrIntegrity)
			}
			f := FileEntry{Name: string(nameBytes), Role: wireRoles[role], Size: int64(size)}
			if !addOrderedName(f.Name, previous, seen) {
				return empty, ErrFormat
			}
			previous = f.Name
			w, err := sink(f)
			if err != nil || w == nil {
				if w != nil {
					_ = w.Close()
				}
				return empty, ErrIO
			}
			h := sha256.New()
			copyErr := copyExact(ctx, io.MultiWriter(w, h), plain, f.Size, buf)
			closeErr := w.Close()
			if copyErr != nil {
				return empty, safeError(copyErr, ErrIntegrity)
			}
			if closeErr != nil {
				return empty, ErrIO
			}
			f.SHA256 = hex.EncodeToString(h.Sum(nil))
			observed = append(observed, f)
		case recordManifest:
			if len(observed) == 0 {
				return empty, ErrFormat
			}
			var length [4]byte
			if _, err := io.ReadFull(plain, length[:]); err != nil {
				return empty, safeError(err, ErrIntegrity)
			}
			n := uint64(binary.BigEndian.Uint32(length[:]))
			if n == 0 {
				return empty, ErrFormat
			}
			if n > uint64(limits.MaxManifestBytes) || n > uint64(plain.remaining) {
				return empty, ErrLimit
			}
			raw := make([]byte, int(n))
			if _, err := io.ReadFull(plain, raw); err != nil {
				return empty, safeError(err, ErrIntegrity)
			}
			m, err := readManifest(raw, observed)
			if err != nil {
				return empty, err
			}
			if _, err := io.ReadFull(plain, tag[:]); err != nil {
				return empty, safeError(err, ErrIntegrity)
			}
			if tag[0] != recordEnd {
				return empty, ErrFormat
			}
			return m, nil
		default:
			return empty, ErrFormat
		}
	}
}

func readManifest(raw []byte, observed []FileEntry) (Manifest, error) {
	var m Manifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return Manifest{}, ErrFormat
	}
	// Canonical re-encoding rejects duplicate JSON keys, trailing JSON and
	// ambiguous representations without implementing another JSON parser.
	canonical, err := json.Marshal(m)
	if err != nil || !bytes.Equal(raw, canonical) || m.Version != Version || !validMetadata(m.Metadata) || len(m.Files) != len(observed) {
		return Manifest{}, ErrFormat
	}
	for i, f := range m.Files {
		if !hexString(f.SHA256, 64) || f != observed[i] {
			return Manifest{}, ErrIntegrity
		}
	}
	return m, nil
}
