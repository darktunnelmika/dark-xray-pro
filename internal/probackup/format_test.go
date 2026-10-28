package probackup

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"filippo.io/age"
)

func testKey(t *testing.T) *age.X25519Identity {
	t.Helper()
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func testMetadata() Metadata {
	return Metadata{BackupID: "test-backup-1", CreatedUnix: 1793203200, SourceRevision: strings.Repeat("a", 40), BusinessSchemaVersion: 7, FeatureVersions: map[string]int{"refund": 1}}
}
func bytesSource(name string, data []byte) FileSource {
	return FileSource{Name: name, Role: RoleBusinessDB, Size: int64(len(data)), Open: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}
func testArchive(t *testing.T, key *age.X25519Identity, files []FileSource) ([]byte, Manifest) {
	t.Helper()
	var out bytes.Buffer
	m, err := Write(context.Background(), &out, key.Recipient().String(), testMetadata(), files, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes(), m
}
func sealCompressed(t *testing.T, key *age.X25519Identity, compressed []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := age.Encrypt(&out, key.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(compressed); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func compressRecords(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func unsealRecords(t *testing.T, key *age.X25519Identity, raw []byte) []byte {
	t.Helper()
	r, err := age.Decrypt(bytes.NewReader(raw), key)
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	return plain
}
func rawRecords(t *testing.T, names []string, manifestMutate func(*Manifest)) []byte {
	t.Helper()
	var out bytes.Buffer
	out.Write(magic[:])
	m := Manifest{Version: Version, Metadata: testMetadata()}
	for _, name := range names {
		data := []byte("synthetic snapshot " + name)
		sum := sha256.Sum256(data)
		var h [12]byte
		h[0] = recordFile
		h[1], _ = roleByte(RoleBusinessDB)
		binary.BigEndian.PutUint16(h[2:], uint16(len(name)))
		binary.BigEndian.PutUint64(h[4:], uint64(len(data)))
		out.Write(h[:])
		out.WriteString(name)
		out.Write(data)
		m.Files = append(m.Files, FileEntry{Name: name, Role: RoleBusinessDB, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})
	}
	if manifestMutate != nil {
		manifestMutate(&m)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var h [5]byte
	h[0] = recordManifest
	binary.BigEndian.PutUint32(h[1:], uint32(len(raw)))
	out.Write(h[:])
	out.Write(raw)
	out.WriteByte(recordEnd)
	return out.Bytes()
}

func TestRoundTripBoundariesAndEncryptedMetadata(t *testing.T) {
	key := testKey(t)
	for _, size := range []int{0, 1, 65535, 65536, 65537, 131072} {
		t.Run(string(rune('a'+size%26))+"-"+strings.Repeat("x", size%7), func(t *testing.T) {
			data := bytes.Repeat([]byte("private fixture data and tokens never logged!"), size/44+1)
			data = data[:size]
			files := []FileSource{bytesSource("z.db", data), bytesSource("data/business.sqlite", []byte("small"))}
			raw, want := testArchive(t, key, files)
			got, err := Verify(context.Background(), bytes.NewReader(raw), key.String(), Limits{})
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("round trip failed: %v", err)
			}
			if want.Files[0].Name != "data/business.sqlite" || files[0].Name != "z.db" {
				t.Fatal("writer changed caller order or did not canonicalize")
			}
			if bytes.Contains(raw, []byte("data/business.sqlite")) || bytes.Contains(raw, []byte(testMetadata().BackupID)) {
				t.Fatal("unencrypted metadata")
			}
		})
	}
}

func TestUntrustedHeaderBoundsAndWrongIdentity(t *testing.T) {
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("db", []byte("x"))})
	other := testKey(t)
	if _, err := Verify(context.Background(), bytes.NewReader(raw), other.String(), Limits{}); !errors.Is(err, ErrIntegrity) {
		t.Fatal("wrong identity accepted")
	}
	cases := map[string]string{
		"long-line": "age-encryption.org/v1\n" + strings.Repeat("x", maxAgeLine+1),
		"stanzas":   "age-encryption.org/v1\n" + strings.Repeat("-> X\n\n", maxAgeStanzas+1) + "--- x\n",
		"total":     "age-encryption.org/v1\n-> X\n" + strings.Repeat(strings.Repeat("A", 64)+"\n", maxAgeHeader/65+1),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(context.Background(), strings.NewReader(s), key.String(), Limits{}); !errors.Is(err, ErrLimit) {
				t.Fatalf("header bound: %v", err)
			}
		})
	}
}

func TestEveryTruncationAndCiphertextTamperFails(t *testing.T) {
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("db", []byte("short synthetic fixture"))})
	for n := 0; n < len(raw); n++ {
		if _, err := Verify(context.Background(), bytes.NewReader(raw[:n]), key.String(), Limits{}); err == nil {
			t.Fatalf("accepted truncation at %d", n)
		}
	}
	for _, at := range []int{0, 30, len(raw) / 2, len(raw) - 17, len(raw) - 1} {
		mutated := append([]byte(nil), raw...)
		mutated[at] ^= 1
		if _, err := Verify(context.Background(), bytes.NewReader(mutated), key.String(), Limits{}); err == nil {
			t.Fatalf("accepted tamper at %d", at)
		}
	}
	for _, suffix := range [][]byte{{0}, raw} {
		if _, err := Verify(context.Background(), bytes.NewReader(append(append([]byte(nil), raw...), suffix...)), key.String(), Limits{}); err == nil {
			t.Fatal("accepted ciphertext trailing data")
		}
	}
}

func TestContainerLimitsAndEveryLayerEOF(t *testing.T) {
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("db", bytes.Repeat([]byte("x"), 1024))})
	plain := unsealRecords(t, key, raw)
	if _, err := Verify(context.Background(), bytes.NewReader(raw), key.String(), Limits{MaxCiphertextBytes: int64(len(raw)), MaxPlaintextBytes: int64(len(plain))}); err != nil {
		t.Fatalf("exact byte bounds rejected: %v", err)
	}
	for name, limits := range map[string]Limits{"cipher": {MaxCiphertextBytes: int64(len(raw) - 1)}, "plain": {MaxPlaintextBytes: int64(len(plain) - 1)}, "file": {MaxFileBytes: 1023}, "manifest": {MaxManifestBytes: 32}} {
		t.Run(name, func(t *testing.T) {
			if _, err := Verify(context.Background(), bytes.NewReader(raw), key.String(), limits); !errors.Is(err, ErrLimit) {
				t.Fatalf("limit not enforced: %v", err)
			}
		})
	}
	validGzip := compressRecords(t, plain)
	badCRC := append([]byte(nil), validGzip...)
	badCRC[len(badCRC)-8] ^= 1
	for name, b := range map[string][]byte{
		"container-tail":  compressRecords(t, append(append([]byte(nil), plain...), 0)),
		"second-gzip":     append(append([]byte(nil), validGzip...), compressRecords(t, nil)...),
		"compressed-tail": append(append([]byte(nil), validGzip...), 1, 2, 3),
		"gzip-crc":        badCRC,
		"gzip-truncated":  validGzip[:len(validGzip)-1],
	} {
		t.Run(name, func(t *testing.T) {
			sealed := sealCompressed(t, key, b)
			if _, err := Verify(context.Background(), bytes.NewReader(sealed), key.String(), Limits{}); err == nil {
				t.Fatal("accepted malformed inner stream")
			}
		})
	}
}

func TestNamesRolesAndManifestAreStrict(t *testing.T) {
	key := testKey(t)
	for _, names := range [][]string{{"a", "a-.txt", "a/x"}, {"a", "a"}, {"z", "a"}, {"../db"}, {"/db"}, {"a/./b"}, {"a//b"}, {"a\\b"}, {QuarantineName}, {QuarantineName + "/x"}, {"fóó"}, {strings.Repeat("a/", 8) + "z"}, {strings.Repeat("a", 241)}} {
		t.Run(strings.Join(names, "_"), func(t *testing.T) {
			sealed := sealCompressed(t, key, compressRecords(t, rawRecords(t, names, nil)))
			if _, err := Verify(context.Background(), bytes.NewReader(sealed), key.String(), Limits{}); err == nil {
				t.Fatal("reader accepted invalid paths/order")
			}
			if len(names) == 2 && names[0] == "z" {
				return
			} // writer deliberately sorts source order
			var out bytes.Buffer
			var sources []FileSource
			for _, n := range names {
				sources = append(sources, bytesSource(n, nil))
			}
			if _, err := Write(context.Background(), &out, key.Recipient().String(), testMetadata(), sources, Limits{}); err == nil || out.Len() != 0 {
				t.Fatal("writer did not reject before I/O")
			}
		})
	}
	mutations := map[string]func(*Manifest){
		"hash":    func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("0", 64) },
		"size":    func(m *Manifest) { m.Files[0].Size++ },
		"role":    func(m *Manifest) { m.Files[0].Role = RoleAgentTLSKey },
		"missing": func(m *Manifest) { m.Files = nil },
		"version": func(m *Manifest) { m.Version++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			raw := sealCompressed(t, key, compressRecords(t, rawRecords(t, []string{"a"}, mutate)))
			if _, err := Verify(context.Background(), bytes.NewReader(raw), key.String(), Limits{}); err == nil {
				t.Fatal("manifest mismatch accepted")
			}
		})
	}
	unknown := rawRecords(t, []string{"a"}, nil)
	unknown[len(magic)+1] = 255
	if _, err := Verify(context.Background(), bytes.NewReader(sealCompressed(t, key, compressRecords(t, unknown))), key.String(), Limits{}); err == nil {
		t.Fatal("unknown role accepted")
	}
	valid := rawRecords(t, []string{"a"}, nil)
	// Alter a fixed-width file size to exercise uint64 overflow before allocation.
	binary.BigEndian.PutUint64(valid[len(magic)+4:], ^uint64(0))
	if _, err := Verify(context.Background(), bytes.NewReader(sealCompressed(t, key, compressRecords(t, valid))), key.String(), Limits{}); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversized declaration: %v", err)
	}
}

type watchedReader struct {
	io.Reader
	closed *bool
}

func (r watchedReader) Close() error { *r.closed = true; return nil }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("secret-token-in-underlying-error")
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }

func TestSourcesOutputFailureCancellationAndRedaction(t *testing.T) {
	key := testKey(t)
	for _, actual := range []string{"a", "abc"} {
		closed := false
		src := FileSource{Name: "db", Role: RoleBusinessDB, Size: 2, Open: func(context.Context) (io.ReadCloser, error) {
			return watchedReader{strings.NewReader(actual), &closed}, nil
		}}
		if _, err := Write(context.Background(), io.Discard, key.Recipient().String(), testMetadata(), []FileSource{src}, Limits{}); err == nil || !closed {
			t.Fatal("source length or close not enforced")
		}
	}
	for _, w := range []io.Writer{failingWriter{}, shortWriter{}} {
		_, err := Write(context.Background(), w, key.Recipient().String(), testMetadata(), []FileSource{bytesSource("db", nil)}, Limits{})
		if err == nil || strings.Contains(err.Error(), "secret-token") {
			t.Fatal("output failure not sanitized")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened := false
	src := FileSource{Name: "db", Role: RoleBusinessDB, Open: func(context.Context) (io.ReadCloser, error) { opened = true; return nil, nil }}
	if _, err := Write(ctx, io.Discard, key.Recipient().String(), testMetadata(), []FileSource{src}, Limits{}); !errors.Is(err, context.Canceled) || opened {
		t.Fatal("cancelled write performed I/O")
	}
	src.Open = func(context.Context) (io.ReadCloser, error) { return nil, errors.New("secret-token-in-source-error") }
	if _, err := Write(context.Background(), io.Discard, key.Recipient().String(), testMetadata(), []FileSource{src}, Limits{}); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("source error not sanitized")
	}
	if _, err := Verify(ctx, strings.NewReader(""), key.String(), Limits{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled verify ignored context")
	}
}

func FuzzVerifyBounded(f *testing.F) {
	key, err := age.GenerateX25519Identity()
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte("age-encryption.org/v1\n"))
	f.Add([]byte("not an archive"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 128<<10 {
			t.Skip()
		}
		_, _ = Verify(context.Background(), bytes.NewReader(raw), key.String(), Limits{MaxCiphertextBytes: 128 << 10, MaxPlaintextBytes: 128 << 10, MaxFileBytes: 64 << 10, MaxFiles: 8, MaxManifestBytes: 4096})
	})
}
