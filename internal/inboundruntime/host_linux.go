//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

const tunDNSActivationMarker = "darkpro-inbound-v2/tun-dns-activation-v1"

func processNetworkCapabilities(status []byte, root bool) (effective, child bool) {
	var eff, ambient, bounding uint64
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.ParseUint(fields[1], 16, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "CapEff:":
			eff = n
		case "CapAmb:":
			ambient = n
		case "CapBnd:":
			bounding = n
		}
	}
	const netAdmin = uint64(1) << 12
	effective = eff&netAdmin != 0
	child = effective && (ambient&netAdmin != 0 || root && bounding&netAdmin != 0)
	return
}

// DetectHostCapabilities does not create interfaces, add addresses, change DNS
// or acquire privileges. Each actual operation must still verify authority and
// its exact owned resource before admission.
func DetectHostCapabilities(b *Binary) inboundexec.HostCapabilities {
	h := inboundexec.HostCapabilities{Version: 1}
	if raw, err := os.ReadFile("/proc/self/status"); err == nil && len(raw) <= 65536 {
		h.NetworkAdmin, h.ChildNetworkAdmin = processNetworkCapabilities(raw, os.Geteuid() == 0)
	}
	fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err == nil {
		var st syscall.Stat_t
		// Linux's TUN clone device is the fixed character device 10:200.
		h.TUNDevice = syscall.Fstat(fd, &st) == nil && st.Mode&syscall.S_IFMT == syscall.S_IFCHR && st.Rdev == 10<<8|200
		_ = syscall.Close(fd)
	}
	if raw, e := os.ReadFile("/proc/sys/net/ipv6/conf/all/disable_ipv6"); e == nil {
		h.IPv6 = strings.TrimSpace(string(raw)) == "0"
	}
	bus, busErr := os.Stat("/run/dbus/system_bus_socket")
	control, controlErr := os.Stat("/usr/bin/resolvectl")
	h.SystemDNSControl = busErr == nil && bus.Mode()&os.ModeSocket != 0 && controlErr == nil && control.Mode().IsRegular() && control.Mode().Perm()&0111 != 0 && control.Mode().Perm()&0022 == 0
	if b != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		h.TUNDNSActivation = b.SupportsTUNDNSActivation(ctx)
		cancel()
	}
	return h
}

// SupportsTUNDNSActivation runs only the version command on the already pinned
// executable descriptor. It must precede even offline config validation: an
// older engine may perform host DNS changes while constructing a TUN handler.
func (b *Binary) SupportsTUNDNSActivation(ctx context.Context) bool {
	if b == nil || b.file == nil {
		return false
	}
	b.featureMu.Lock()
	defer b.featureMu.Unlock()
	if b.featuresChecked {
		return b.tunDNSActivation
	}
	check, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := b.command(check, "version")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	var out boundedOutput
	out.limit = 16 << 10
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	err := cmd.Run()
	if check.Err() != nil {
		return false
	}
	if err == nil {
		b.featuresChecked = true
		for _, field := range strings.Fields(out.String()) {
			if field == tunDNSActivationMarker {
				b.tunDNSActivation = true
			}
		}
	}
	return b.tunDNSActivation
}

func guardTUNDNSActivation(ctx context.Context, b *Binary, bundle inboundexec.Bundle) error {
	requirements, err := inboundexec.BundleHostRequirements(bundle)
	if err != nil {
		return ErrRuntime
	}
	for _, requirement := range requirements {
		if requirement == inboundexec.HostTUNSystemDNS && !b.SupportsTUNDNSActivation(ctx) {
			return errors.New("inbound_tun_dns_activation_unsupported")
		}
	}
	return nil
}
