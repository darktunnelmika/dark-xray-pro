//go:build linux

package corebarrierv2

import (
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"sync"
)

type memoryFinalCapture struct {
	file            *os.File
	binding, config string
	once            sync.Once
	err             error
}

func openFinalCapture(number, binding, config string) (finalCapture, error) {
	if !validNonce(binding) || !validNonce(config) {
		return nil, ErrBarrier
	}
	n, e := strconv.Atoi(number)
	if e != nil || n < 3 || n > 1024 {
		return nil, ErrBarrier
	}
	f := os.NewFile(uintptr(n), "owned-native-final-capture")
	if f == nil {
		return nil, ErrBarrier
	}
	st, e := f.Stat()
	if e != nil || st.Size() != 0 || !st.Mode().IsRegular() {
		return nil, ErrBarrier
	}
	raw, e := f.SyscallConn()
	if e != nil {
		return nil, ErrBarrier
	}
	valid := false
	e = raw.Control(func(fd uintptr) {
		seals, err := unix.FcntlInt(fd, unix.F_GET_SEALS, 0)
		flags, fe := unix.FcntlInt(fd, unix.F_GETFL, 0)
		valid = err == nil && fe == nil && seals == 0 && flags&unix.O_ACCMODE == unix.O_RDWR
	})
	if e != nil || !valid {
		return nil, ErrBarrier
	}
	return &memoryFinalCapture{file: f, binding: binding, config: config}, nil
}
func (c *memoryFinalCapture) Binding() string   { return c.binding }
func (c *memoryFinalCapture) ConfigSHA() string { return c.config }
func (c *memoryFinalCapture) Publish(r FinalRecord) error {
	c.once.Do(func() {
		b, e := json.Marshal(r)
		if e != nil || len(b) > FinalRecordLimit {
			c.err = ErrBarrier
			return
		}
		n, e := c.file.WriteAt(b, 0)
		if e != nil || n != len(b) {
			c.err = ErrBarrier
			return
		}
		raw, e := c.file.SyscallConn()
		if e != nil {
			c.err = ErrBarrier
			return
		}
		var sealErr error
		e = raw.Control(func(fd uintptr) {
			_, sealErr = unix.FcntlInt(fd, unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL)
		})
		if e != nil || sealErr != nil {
			c.err = ErrBarrier
		}
	})
	return c.err
}
