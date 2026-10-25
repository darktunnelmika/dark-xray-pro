//go:build linux

package inboundruntime

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"golang.org/x/sys/unix"
)

const aliasIFAProto = 11 // Linux UAPI IFA_PROTO; absent from older x/sys headers.
const aliasBadFlags = unix.IFA_F_NODAD | unix.IFA_F_OPTIMISTIC | unix.IFA_F_DADFAILED | unix.IFA_F_DEPRECATED

var aliasNetlinkSequence atomic.Uint32

type publicAliasHost struct {
	BootID    string                  `json:"boot_id"`
	Namespace uint64                  `json:"network_namespace,string"`
	Interface ix.PublicAliasInterface `json:"interface"`
}

type publicAliasAddress struct {
	Address      netip.Addr
	PrefixLength uint8
	Flags        uint32
	Protocol     uint8
	HasProtocol  bool
	HasCache     bool
	CreatedTicks uint32
	Preferred    uint32
	Valid        uint32
}

// No delete operation is exposed. Linux RTM_DELADDR has no identity CAS for
// IFA_PROTO/cstamp, so cleanup stops renewal and observes kernel expiry instead
// of risking removal of an address replaced by another privileged host owner.
type publicAliasKernel interface {
	Host(context.Context, string) (publicAliasHost, error)
	HostIndex(context.Context, uint32) (publicAliasHost, bool, error)
	Address(context.Context, ix.PublicAliasInterface, netip.Addr) (publicAliasAddress, bool, error)
	Add(context.Context, ix.PublicAliasInterface, netip.Addr, uint32) (bool, error)
	Renew(context.Context, ix.PublicAliasInterface, netip.Addr, uint32) (bool, error)
}

type linuxPublicAliasKernel struct{}

func (linuxPublicAliasKernel) Host(ctx context.Context, name string) (publicAliasHost, error) {
	out, err := publicAliasKernelIdentity(ctx)
	if ctx.Err() != nil || !validAliasInterfaceName(name) {
		return out, ErrPublicAliasUnavailable
	}
	if err != nil {
		return out, err
	}
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Index <= 0 || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
		return out, ErrPublicAliasUnavailable
	}
	out.Interface = ix.PublicAliasInterface{Name: iface.Name, Index: uint32(iface.Index), MAC: iface.HardwareAddr.String()}
	return out, nil
}

func publicAliasKernelIdentity(ctx context.Context) (publicAliasHost, error) {
	var out publicAliasHost
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return out, ErrPublicAliasUnavailable
	}
	out.BootID = strings.TrimSpace(string(boot))
	var ns unix.Stat_t
	if len(out.BootID) != 36 || unix.Stat("/proc/self/ns/net", &ns) != nil || ns.Ino == 0 {
		return out, ErrPublicAliasUnavailable
	}
	out.Namespace = ns.Ino
	return out, nil
}

func (linuxPublicAliasKernel) HostIndex(ctx context.Context, index uint32) (publicAliasHost, bool, error) {
	out, err := publicAliasKernelIdentity(ctx)
	if err != nil {
		return out, false, err
	}
	// A complete link dump proves absence; InterfaceByIndex's generic error
	// alone would not distinguish removal from a failed rtnetlink read.
	interfaces, err := net.Interfaces()
	if err != nil {
		return out, false, err
	}
	for _, iface := range interfaces {
		if uint32(iface.Index) == index {
			out.Interface = ix.PublicAliasInterface{Name: iface.Name, Index: index, MAC: iface.HardwareAddr.String()}
			return out, true, nil
		}
	}
	return out, false, nil
}

func (linuxPublicAliasKernel) Address(ctx context.Context, iface ix.PublicAliasInterface, address netip.Addr) (publicAliasAddress, bool, error) {
	var out publicAliasAddress
	data := make([]byte, unix.SizeofIfAddrmsg)
	data[0] = unix.AF_INET6
	binary.NativeEndian.PutUint32(data[4:8], iface.Index)
	messages, _, err := publicAliasNetlink(ctx, unix.RTM_GETADDR, unix.NLM_F_DUMP, data)
	if err != nil {
		return out, false, err
	}
	found := false
	for _, m := range messages {
		if m.Header.Type != unix.RTM_NEWADDR || len(m.Data) < unix.SizeofIfAddrmsg {
			return out, false, ErrPublicAliasUnavailable
		}
		if m.Data[0] != unix.AF_INET6 || binary.NativeEndian.Uint32(m.Data[4:8]) != iface.Index {
			continue
		}
		a, err := parsePublicAliasAddress(m.Data)
		if err != nil {
			return out, false, err
		}
		if a.Address == address {
			if found {
				return out, false, ErrPublicAliasIdentity
			}
			out, found = a, true
		}
	}
	return out, found, nil
}

func parsePublicAliasAddress(data []byte) (publicAliasAddress, error) {
	var out publicAliasAddress
	if len(data) < unix.SizeofIfAddrmsg || data[0] != unix.AF_INET6 {
		return out, ErrPublicAliasUnavailable
	}
	out.PrefixLength, out.Flags = data[1], uint32(data[2])
	attrs := data[unix.SizeofIfAddrmsg:]
	seen := map[uint16]bool{}
	var local, address netip.Addr
	for len(attrs) > 0 {
		if len(attrs) < unix.SizeofRtAttr {
			return out, ErrPublicAliasUnavailable
		}
		n := int(binary.NativeEndian.Uint16(attrs[:2]))
		kind := binary.NativeEndian.Uint16(attrs[2:4]) & 0x3fff
		if n < unix.SizeofRtAttr || n > len(attrs) || (n+3)&^3 > len(attrs) || seen[kind] {
			return out, ErrPublicAliasUnavailable
		}
		seen[kind] = true
		v := attrs[unix.SizeofRtAttr:n]
		switch kind {
		case unix.IFA_ADDRESS, unix.IFA_LOCAL:
			if len(v) != 16 {
				return out, ErrPublicAliasUnavailable
			}
			a := netip.AddrFrom16([16]byte(v))
			if kind == unix.IFA_ADDRESS {
				address = a
			} else {
				local = a
			}
		case unix.IFA_FLAGS:
			if len(v) != 4 {
				return out, ErrPublicAliasUnavailable
			}
			out.Flags = binary.NativeEndian.Uint32(v)
		case aliasIFAProto:
			if len(v) != 1 {
				return out, ErrPublicAliasUnavailable
			}
			out.Protocol, out.HasProtocol = v[0], true
		case unix.IFA_CACHEINFO:
			if len(v) != 16 {
				return out, ErrPublicAliasUnavailable
			}
			out.Preferred = binary.NativeEndian.Uint32(v[:4])
			out.Valid = binary.NativeEndian.Uint32(v[4:8])
			out.CreatedTicks, out.HasCache = binary.NativeEndian.Uint32(v[8:12]), true
		}
		attrs = attrs[(n+3)&^3:]
	}
	if local.IsValid() {
		if address.IsValid() && address != local {
			return out, ErrPublicAliasIdentity // A point-to-point peer is not an alias.
		}
		address = local
	}
	if !address.IsValid() {
		return out, ErrPublicAliasUnavailable
	}
	out.Address = address
	return out, nil
}

func (linuxPublicAliasKernel) Add(ctx context.Context, iface ix.PublicAliasInterface, address netip.Addr, lifetime uint32) (bool, error) {
	return changePublicAlias(ctx, iface, address, lifetime, unix.NLM_F_CREATE|unix.NLM_F_EXCL)
}

func (linuxPublicAliasKernel) Renew(ctx context.Context, iface ix.PublicAliasInterface, address netip.Addr, lifetime uint32) (bool, error) {
	// Deliberately omit NLM_F_CREATE: an expired/removed address is never
	// recreated or adopted by an old process generation.
	return changePublicAlias(ctx, iface, address, lifetime, unix.NLM_F_REPLACE)
}

func changePublicAlias(ctx context.Context, iface ix.PublicAliasInterface, address netip.Addr, lifetime uint32, flags uint16) (bool, error) {
	if !address.Is6() || address.Is4In6() || iface.Index == 0 || lifetime == 0 || lifetime > 30 {
		return false, ErrPublicAliasUnavailable
	}
	data := make([]byte, unix.SizeofIfAddrmsg)
	data[0], data[1], data[3] = unix.AF_INET6, 128, unix.RT_SCOPE_UNIVERSE
	binary.NativeEndian.PutUint32(data[4:8], iface.Index)
	ip := address.As16()
	data = appendPublicAliasAttribute(data, unix.IFA_ADDRESS, ip[:])
	data = appendPublicAliasAttribute(data, aliasIFAProto, []byte{ix.PublicAliasProtocol})
	cache := make([]byte, 16)
	binary.NativeEndian.PutUint32(cache[:4], lifetime)
	binary.NativeEndian.PutUint32(cache[4:8], lifetime)
	data = appendPublicAliasAttribute(data, unix.IFA_CACHEINFO, cache)
	addressFlags := make([]byte, 4)
	binary.NativeEndian.PutUint32(addressFlags, unix.IFA_F_NOPREFIXROUTE)
	data = appendPublicAliasAttribute(data, unix.IFA_FLAGS, addressFlags)
	_, ack, err := publicAliasNetlink(ctx, unix.RTM_NEWADDR, flags|unix.NLM_F_ACK, data)
	return ack, err
}

func appendPublicAliasAttribute(data []byte, kind uint16, value []byte) []byte {
	n := unix.SizeofRtAttr + len(value)
	attr := make([]byte, (n+3)&^3)
	binary.NativeEndian.PutUint16(attr[:2], uint16(n))
	binary.NativeEndian.PutUint16(attr[2:4], kind)
	copy(attr[unix.SizeofRtAttr:], value)
	return append(data, attr...)
}

// The boolean means a kernel ACK was actually received, including a negative
// ACK proving that a failed exclusive add did not mutate anything. A timeout
// after send is ambiguous and must remain a durable, unadoptable intent.
func publicAliasNetlink(ctx context.Context, kind, flags uint16, data []byte) ([]syscall.NetlinkMessage, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, false, err
	}
	defer unix.Close(fd)
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, false, err
	}
	seq := aliasNetlinkSequence.Add(1)
	packet := make([]byte, unix.NLMSG_HDRLEN+len(data))
	binary.NativeEndian.PutUint32(packet[:4], uint32(len(packet)))
	binary.NativeEndian.PutUint16(packet[4:6], kind)
	binary.NativeEndian.PutUint16(packet[6:8], flags|unix.NLM_F_REQUEST)
	binary.NativeEndian.PutUint32(packet[8:12], seq)
	copy(packet[unix.NLMSG_HDRLEN:], data)
	if err = unix.Sendto(fd, packet, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, false, err
	}
	out := []syscall.NetlinkMessage{}
	buffer := make([]byte, 1<<20)
	for {
		if err = ctx.Err(); err != nil {
			return nil, false, err
		}
		n, _, flagsRead, from, recvErr := unix.Recvmsg(fd, buffer, nil, unix.MSG_DONTWAIT)
		if errors.Is(recvErr, unix.EINTR) {
			continue
		}
		if errors.Is(recvErr, unix.EAGAIN) || errors.Is(recvErr, unix.EWOULDBLOCK) {
			_, err = unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 50)
			if err != nil && !errors.Is(err, unix.EINTR) {
				return nil, false, err
			}
			continue
		}
		if recvErr != nil {
			return nil, false, recvErr
		}
		source, ok := from.(*unix.SockaddrNetlink)
		if !ok || source.Pid != 0 || flagsRead&unix.MSG_TRUNC != 0 {
			return nil, false, ErrPublicAliasUnavailable
		}
		messages, err := syscall.ParseNetlinkMessage(buffer[:n])
		if err != nil {
			return nil, false, err
		}
		for _, m := range messages {
			if m.Header.Seq != seq || m.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
				return nil, false, ErrPublicAliasUnavailable
			}
			switch m.Header.Type {
			case unix.NLMSG_ERROR:
				if len(m.Data) < 4 {
					return nil, false, ErrPublicAliasUnavailable
				}
				code := int32(binary.NativeEndian.Uint32(m.Data[:4]))
				if code == 0 {
					return out, true, nil
				}
				if code >= 0 {
					return nil, false, ErrPublicAliasUnavailable
				}
				return nil, true, syscall.Errno(-code)
			case unix.NLMSG_DONE:
				if len(m.Data) >= 4 && binary.NativeEndian.Uint32(m.Data[:4]) != 0 {
					return nil, false, ErrPublicAliasUnavailable
				}
				return out, false, nil
			case unix.NLMSG_OVERRUN:
				return nil, false, ErrPublicAliasUnavailable
			default:
				if len(out) >= 4096 {
					return nil, false, ErrPublicAliasUnavailable
				}
				copyMessage := m
				copyMessage.Data = append([]byte(nil), m.Data...)
				out = append(out, copyMessage)
			}
		}
	}
}
