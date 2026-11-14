package nodeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Spool is a durable FIFO of usage batches. Each batch is one file written
// with write-temp + fsync + rename, so a crash leaves either the whole batch
// or nothing. Batches are deleted only after the HUB acknowledges them.
type Spool struct {
	dir     string
	mu      sync.Mutex
	nextSeq uint64
	max     int
}

const spoolSuffix = ".batch.json"

// OpenSpool opens (or creates) a spool directory. max bounds the number of
// retained batches; when full, the two oldest batches are merged so usage is
// never dropped, only coarsened.
func OpenSpool(dir string, max int) (*Spool, error) {
	if max < 2 {
		max = 2
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Spool{dir: dir, max: max, nextSeq: 1}
	seqs, err := s.list()
	if err != nil {
		return nil, err
	}
	if n := len(seqs); n > 0 {
		s.nextSeq = seqs[n-1] + 1
	}
	if last, err := s.readCursor(); err == nil && last >= s.nextSeq {
		s.nextSeq = last + 1
	}
	return s, nil
}

func (s *Spool) path(seq uint64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%020d%s", seq, spoolSuffix))
}

func (s *Spool) list() ([]uint64, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var seqs []uint64
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, spoolSuffix) {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(name, spoolSuffix), 10, 64)
		if err != nil {
			continue
		}
		seqs = append(seqs, n)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	return seqs, nil
}

// cursor persists the highest sequence ever issued, so sequences stay
// monotonic even after every batch has been acknowledged and deleted.
func (s *Spool) cursorPath() string { return filepath.Join(s.dir, "cursor") }

func (s *Spool) readCursor() (uint64, error) {
	b, err := os.ReadFile(s.cursorPath())
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err = os.Chmod(name, 0o600); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// Append assigns the next sequence to b and stores it durably.
func (s *Spool) Append(b wire.UsageBatch) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seqs, err := s.list()
	if err != nil {
		return 0, err
	}
	if len(seqs) >= s.max {
		if err := s.mergeOldest(seqs[0], seqs[1]); err != nil {
			return 0, err
		}
	}
	b.Seq = s.nextSeq
	data, err := json.Marshal(b)
	if err != nil {
		return 0, err
	}
	if err := writeFileAtomic(s.path(b.Seq), data); err != nil {
		return 0, err
	}
	if err := writeFileAtomic(s.cursorPath(), []byte(strconv.FormatUint(b.Seq, 10))); err != nil {
		return 0, err
	}
	s.nextSeq++
	return b.Seq, nil
}

// mergeOldest folds batch a into batch b (b keeps its sequence). The HUB
// therefore never sees a gap that would look like lost usage.
func (s *Spool) mergeOldest(a, b uint64) error {
	first, err := s.read(a)
	if err != nil {
		return err
	}
	second, err := s.read(b)
	if err != nil {
		return err
	}
	merged := MergeBatches(first, second)
	merged.Seq = b
	data, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.path(b), data); err != nil {
		return err
	}
	return os.Remove(s.path(a))
}

func (s *Spool) read(seq uint64) (wire.UsageBatch, error) {
	var b wire.UsageBatch
	data, err := os.ReadFile(s.path(seq))
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("spool batch %d corrupt: %w", seq, err)
	}
	b.Seq = seq
	return b, nil
}

// Pending returns all unacknowledged batches, oldest first.
func (s *Spool) Pending() ([]wire.UsageBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seqs, err := s.list()
	if err != nil {
		return nil, err
	}
	out := make([]wire.UsageBatch, 0, len(seqs))
	for _, seq := range seqs {
		b, err := s.read(seq)
		if err != nil {
			return out, err
		}
		out = append(out, b)
	}
	return out, nil
}

// Len reports the number of unacknowledged batches.
func (s *Spool) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	seqs, _ := s.list()
	return len(seqs)
}

// Ack deletes every batch with sequence <= seq.
func (s *Spool) Ack(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seqs, err := s.list()
	if err != nil {
		return err
	}
	var errs []error
	for _, n := range seqs {
		if n > seq {
			break
		}
		if err := os.Remove(s.path(n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// UnackedTraffic sums per-user bytes still waiting in the spool. The local
// enforcer subtracts it from the HUB's last known remaining volume.
func (s *Spool) UnackedTraffic() (map[string]int64, error) {
	pending, err := s.Pending()
	out := map[string]int64{}
	for _, b := range pending {
		for _, t := range b.Traffic {
			out[t.Email] += t.Up + t.Down
		}
	}
	return out, err
}

// MergeBatches combines two batches; traffic is summed per user, the newest
// online snapshot wins, and disconnect records are concatenated.
func MergeBatches(a, b wire.UsageBatch) wire.UsageBatch {
	out := wire.UsageBatch{NodeID: b.NodeID, Seq: b.Seq, FromUnix: a.FromUnix, ToUnix: b.ToUnix}
	if out.FromUnix == 0 || (b.FromUnix != 0 && b.FromUnix < out.FromUnix) {
		out.FromUnix = b.FromUnix
	}
	sums := map[string]*wire.UserTraffic{}
	var order []string
	for _, list := range [][]wire.UserTraffic{a.Traffic, b.Traffic} {
		for _, t := range list {
			x, ok := sums[t.Email]
			if !ok {
				x = &wire.UserTraffic{Email: t.Email}
				sums[t.Email] = x
				order = append(order, t.Email)
			}
			x.Up += t.Up
			x.Down += t.Down
		}
	}
	for _, e := range order {
		out.Traffic = append(out.Traffic, *sums[e])
	}
	out.Online = b.Online
	if out.Online == nil {
		out.Online = a.Online
	}
	out.Disconnects = append(append([]wire.Disconnect{}, a.Disconnects...), b.Disconnects...)
	return out
}
