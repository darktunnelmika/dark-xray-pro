//go:build linux

package corebarrierv2

import (
	"encoding/json"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestNativeFinalCaptureIsWriteOnceAndKernelSealed(t *testing.T) {
	fd, e := unix.MemfdCreate("test-only-final-capture", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if e != nil {
		t.Fatal(e)
	}
	f := os.NewFile(uintptr(fd), "test-parent-final")
	defer f.Close()
	child, e := unix.Dup(fd)
	if e != nil {
		t.Fatal(e)
	}
	binding, config := strings.Repeat("a", 64), strings.Repeat("b", 64)
	raw, e := openFinalCapture(strconv.Itoa(child), binding, config)
	if e != nil {
		unix.Close(child)
		t.Fatal(e)
	}
	defer raw.(*memoryFinalCapture).file.Close()
	record := FinalRecord{Version: FinalRecordVersion, Final: true, PID: os.Getpid(), Binding: binding, ConfigSHA: config, Autonomous: true, Counters: map[string]int64{"positive": 37}}
	if e = raw.Publish(record); e != nil {
		t.Fatal(e)
	}
	if _, e = f.WriteAt([]byte("overwrite"), 0); e == nil {
		t.Fatal("published capture remained writable")
	}
	if e = f.Truncate(0); e == nil {
		t.Fatal("published capture could be truncated")
	}
	record.Counters["positive"] = 99
	if e = raw.Publish(record); e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(f)
	if e != nil {
		t.Fatal(e)
	}
	var restored FinalRecord
	if e = json.Unmarshal(b, &restored); e != nil || restored.Counters["positive"] != 37 {
		t.Fatal("capture changed after publication", e)
	}
	seals, e := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	required := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	if e != nil || seals&required != required {
		t.Fatal("incomplete kernel seals")
	}
}
