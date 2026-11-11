//go:build linux

package inboundruntime

import (
	"bytes"
	"context"
	"encoding/json"
	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"time"
)

func newNativeFinalCapture() (*os.File, string, error) {
	binding, e := randomID(32)
	if e != nil {
		return nil, "", e
	}
	fd, e := unix.MemfdCreate("dark-pro-owned-native-final", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if e != nil {
		return nil, "", e
	}
	return os.NewFile(uintptr(fd), "owned-native-final-capture"), binding, nil
}
func finalCaptureSealed(f *os.File) bool {
	if f == nil {
		return false
	}
	raw, e := f.SyscallConn()
	if e != nil {
		return false
	}
	valid := false
	e = raw.Control(func(fd uintptr) {
		s, err := unix.FcntlInt(fd, unix.F_GET_SEALS, 0)
		needed := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
		valid = err == nil && s&needed == needed
	})
	return e == nil && valid
}
func (p *Native) readFinalCapture(nonce string) (map[string]uint64, error) {
	if !p.autonomousLease || !validNonce(nonce) || !finalCaptureSealed(p.finalCapture) {
		return nil, ErrProof
	}
	st, e := p.finalCapture.Stat()
	if e != nil || st.Size() <= 0 || st.Size() > barrier.FinalRecordLimit {
		return nil, ErrProof
	}
	b := make([]byte, int(st.Size()))
	n, e := p.finalCapture.ReadAt(b, 0)
	if (e != nil && e != io.EOF) || n != len(b) {
		return nil, ErrProof
	}
	var r barrier.FinalRecord
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil {
		return nil, ErrProof
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrProof
	}
	canonical, e := json.Marshal(r)
	if e != nil || !bytes.Equal(b, canonical) {
		return nil, ErrProof
	}
	if r.Version != barrier.FinalRecordVersion || !r.Final || r.PID != p.identity.PID || r.Binding != p.finalBinding || r.ConfigSHA != p.configSHA || r.Counters == nil || (!r.Autonomous && !validNonce(r.Nonce)) || (r.Nonce != "" && r.Nonce != nonce) || (p.recoveredFinalNonce != "" && p.recoveredFinalNonce != nonce) {
		return nil, ErrProof
	}
	out := make(map[string]uint64, len(r.Counters))
	for name, v := range r.Counters {
		if name == "" || v < 0 {
			return nil, ErrProof
		}
		out[name] = uint64(v)
	}
	p.recoveredFinalNonce = nonce
	return out, nil
}
func (p *Native) hasFinalCapture() bool {
	return p != nil && p.autonomousLease && finalCaptureSealed(p.finalCapture)
}
func (p *Native) waitFinalCapture() {
	if !p.autonomousLease {
		return
	}
	until := time.Now().Add(3 * time.Second)
	for !finalCaptureSealed(p.finalCapture) && time.Now().Before(until) {
		time.Sleep(5 * time.Millisecond)
	}
}

func (p *Native) awaitFinalCapture(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !finalCaptureSealed(p.finalCapture) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
