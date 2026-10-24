package xrayconfig

import (
	"net"
	"strconv"
	"strings"
)

// ProfileOptions contains the explicitly supported advanced transport settings.
// Empty options preserve the historical candidate bytes and profile identity.
// The structure contains no secret material; REALITY keys remain in Profile.
type ProfileOptions struct {
	GRPCAuthority       string           `json:"grpc_authority,omitempty"`
	GRPCMultiMode       bool             `json:"grpc_multi_mode,omitempty"`
	RealityXver         int              `json:"reality_xver,omitempty"`
	RealityMaxTimeDiff  int64            `json:"reality_max_time_diff,omitempty"`
	RealityMinClientVer string           `json:"reality_min_client_ver,omitempty"`
	RealityMaxClientVer string           `json:"reality_max_client_ver,omitempty"`
	RealityShow         bool             `json:"reality_show,omitempty"`
	Sniffing            *SniffingOptions `json:"sniffing,omitempty"`
	Fallbacks           []Fallback       `json:"fallbacks,omitempty"`
}

// The pinned core converts milliseconds to time.Duration (signed nanoseconds).
// Larger values overflow despite fitting in its unsigned JSON input type.
const MaxRealityTimeDiffMilliseconds int64 = (1<<63 - 1) / 1_000_000

// NormalizeProfileOptions validates without silently trimming or dropping any
// submitted field. The returned value is owned by the caller; a zero-valued
// object becomes nil so old profiles and an explicitly cleared editor agree.
func NormalizeProfileOptions(transport string, options *ProfileOptions) (*ProfileOptions, error) {
	if options == nil {
		return nil, nil
	}
	o := *options
	if (transport != "tcp" && transport != "grpc") ||
		o.RealityXver < 0 || o.RealityXver > 2 ||
		o.RealityMaxTimeDiff < 0 || o.RealityMaxTimeDiff > MaxRealityTimeDiffMilliseconds ||
		(transport != "grpc" && (o.GRPCAuthority != "" || o.GRPCMultiMode)) ||
		(o.GRPCAuthority != "" && !validGRPCAuthority(o.GRPCAuthority)) {
		return nil, ErrInvalid
	}
	minimum, minOK := realityClientVersion(o.RealityMinClientVer)
	maximum, maxOK := realityClientVersion(o.RealityMaxClientVer)
	if !minOK || !maxOK || (o.RealityMinClientVer != "" && o.RealityMaxClientVer != "" && minimum > maximum) {
		return nil, ErrInvalid
	}
	var err error
	if o.Sniffing, err = normalizeSniffing(o.Sniffing); err != nil {
		return nil, err
	}
	if o.Fallbacks, err = normalizeFallbacks(transport, o.Fallbacks); err != nil {
		return nil, err
	}
	if o.GRPCAuthority == "" && !o.GRPCMultiMode && o.RealityXver == 0 && o.RealityMaxTimeDiff == 0 &&
		o.RealityMinClientVer == "" && o.RealityMaxClientVer == "" && !o.RealityShow && o.Sniffing == nil && len(o.Fallbacks) == 0 {
		return nil, nil
	}
	return &o, nil
}

func realityClientVersion(value string) (uint32, bool) {
	if value == "" {
		return 0, true
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return 0, false
	}
	var version uint32
	for _, part := range parts {
		n, err := strconv.ParseUint(part, 10, 8)
		if err != nil || strconv.FormatUint(n, 10) != part {
			return 0, false
		}
		version = version<<8 | uint32(n)
	}
	return version, true
}

func validGRPCAuthority(value string) bool {
	if len(value) == 0 || len(value) > 259 || strings.ContainsAny(value, " /\\@%?#\t\r\n") {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	host := value
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		ip := net.ParseIP(value[1 : len(value)-1])
		return ip != nil && strings.Contains(value, ":")
	}
	if strings.Contains(value, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(value)
		if err != nil {
			return false
		}
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != port {
			return false
		}
		if strings.HasPrefix(value, "[") && (net.ParseIP(host) == nil || !strings.Contains(host, ":")) {
			return false
		}
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

type grpcSettings struct {
	ServiceName string `json:"serviceName"`
	Authority   string `json:"authority,omitempty"`
	MultiMode   bool   `json:"multiMode,omitempty"`
}

func profileStream(p Profile) stream {
	s := stream{Network: p.Transport, Security: "reality", Reality: reality{
		Dest: p.RealityTarget, ServerNames: append([]string(nil), p.ServerNames...),
		PrivateKey: p.RealityPrivateKey, ShortIDs: append([]string(nil), p.ShortIDs...),
	}}
	if p.Transport == "grpc" {
		s.GRPC = &grpcSettings{ServiceName: p.GRPCService}
	}
	if o := p.Options; o != nil {
		s.Reality.Xver = o.RealityXver
		s.Reality.MaxTimeDiff = o.RealityMaxTimeDiff
		s.Reality.MinClientVer = o.RealityMinClientVer
		s.Reality.MaxClientVer = o.RealityMaxClientVer
		s.Reality.Show = o.RealityShow
		if s.GRPC != nil {
			s.GRPC.Authority = o.GRPCAuthority
			s.GRPC.MultiMode = o.GRPCMultiMode
		}
	}
	return s
}

func optionsFromStream(s stream) *ProfileOptions {
	o := &ProfileOptions{
		RealityXver: s.Reality.Xver, RealityMaxTimeDiff: s.Reality.MaxTimeDiff,
		RealityMinClientVer: s.Reality.MinClientVer, RealityMaxClientVer: s.Reality.MaxClientVer,
		RealityShow: s.Reality.Show,
	}
	if s.GRPC != nil {
		o.GRPCAuthority = s.GRPC.Authority
		o.GRPCMultiMode = s.GRPC.MultiMode
	}
	return o
}
