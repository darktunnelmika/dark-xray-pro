//go:build linux

package usagecheckpoint

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
)

// VerifyEpochStopped is a read-only recovery check, never permission to kill a
// PID. A journal can contain final counters before its old child has exited.
// Missing/unreadable /proc evidence must not be confused: only a new boot,
// absent PID, different start tick or exited kernel state proves that birth is
// no longer running. A recycled PID belongs to somebody else.
func VerifyEpochStopped(epoch Epoch) error {
	e, err := normalize(epoch)
	if err != nil {
		return err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ErrEpoch
	}
	if strings.TrimSpace(string(boot)) != e.KernelBootID {
		return nil
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(e.PID) + "/stat")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ErrEpoch
	}
	if !stoppedBirth(raw, e.ProcessStartTicks) {
		return ErrEpoch
	}
	return nil
}

func stoppedBirth(raw []byte, start uint64) bool {
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return false
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) <= 19 {
		return false
	}
	actual, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || actual == 0 {
		return false
	}
	return actual != start || fields[0] == "Z" || fields[0] == "X"
}
