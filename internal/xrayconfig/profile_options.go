package xrayconfig

import (
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
)

// ProfileOptions contains the explicitly supported advanced transport settings.
// Empty options preserve the historical candidate bytes and profile identity.
// The structure contains no secret material; REALITY keys remain in Profile.
type ProfileOptions struct {
	GRPCAuthority       string              `json:"grpc_authority,omitempty"`
	GRPCMultiMode       bool                `json:"grpc_multi_mode,omitempty"`
	RealityXver         int                 `json:"reality_xver,omitempty"`
	RealityMaxTimeDiff  int64               `json:"reality_max_time_diff,omitempty"`
	RealityMinClientVer string              `json:"reality_min_client_ver,omitempty"`
	RealityMaxClientVer string              `json:"reality_max_client_ver,omitempty"`
	RealityShow         bool                `json:"reality_show,omitempty"`
	Sniffing            *SniffingOptions    `json:"sniffing,omitempty"`
	Fallbacks           []Fallback          `json:"fallbacks,omitempty"`
	XHTTP               *XHTTPOptions       `json:"xhttp,omitempty"`
	Client              *ClientOptions      `json:"client,omitempty"`
	WebSocket           *WebSocketOptions   `json:"websocket,omitempty"`
	HTTPUpgrade         *HTTPUpgradeOptions `json:"http_upgrade,omitempty"`
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
	if (transport == "ws" || transport == "httpupgrade") &&
		(o.RealityXver != 0 || o.RealityMaxTimeDiff != 0 || o.RealityMinClientVer != "" || o.RealityMaxClientVer != "" || o.RealityShow) {
		return nil, &ProfileValidationError{Field: "security", Code: "tls_reality_options_invalid"}
	}
	if (transport != "tcp" && transport != "grpc" && transport != "xhttp" && transport != "ws" && transport != "httpupgrade") ||
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
	if o.XHTTP, err = NormalizeXHTTPOptions(o.XHTTP); err != nil {
		return nil, err
	}
	if transport != "xhttp" && o.XHTTP != nil {
		return nil, &ProfileValidationError{Field: "options.xhttp", Code: "xhttp_invalid"}
	}
	if o.Client, err = NormalizeClientOptions(o.Client); err != nil {
		return nil, err
	}
	if o.WebSocket, err = NormalizeWebSocketOptions(o.WebSocket); err != nil {
		return nil, err
	}
	if o.HTTPUpgrade, err = NormalizeHTTPUpgradeOptions(o.HTTPUpgrade); err != nil {
		return nil, err
	}
	if transport != "ws" && o.WebSocket != nil {
		return nil, &ProfileValidationError{Field: "options.websocket", Code: "websocket_invalid"}
	}
	if transport != "httpupgrade" && o.HTTPUpgrade != nil {
		return nil, &ProfileValidationError{Field: "options.http_upgrade", Code: "http_upgrade_invalid"}
	}
	if o.GRPCAuthority == "" && !o.GRPCMultiMode && o.RealityXver == 0 && o.RealityMaxTimeDiff == 0 &&
		o.RealityMinClientVer == "" && o.RealityMaxClientVer == "" && !o.RealityShow && o.Sniffing == nil && len(o.Fallbacks) == 0 && o.XHTTP == nil && o.Client == nil && o.WebSocket == nil && o.HTTPUpgrade == nil {
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
	return subscriptioncodec.ValidAuthority(value)
}

type grpcSettings struct {
	ServiceName string `json:"serviceName"`
	Authority   string `json:"authority,omitempty"`
	MultiMode   bool   `json:"multiMode,omitempty"`
}

func profileStream(p Profile) stream {
	s := stream{Network: p.Transport, Security: "reality", Reality: &reality{
		Dest: p.RealityTarget, ServerNames: append([]string(nil), p.ServerNames...),
		PrivateKey: p.RealityPrivateKey, ShortIDs: append([]string(nil), p.ShortIDs...),
	}}
	if p.Security == "tls" {
		s.Security, s.Reality, s.TLS = "tls", nil, profileTLSSettings(p)
	}
	if p.Transport == "grpc" {
		s.GRPC = &grpcSettings{ServiceName: p.GRPCService}
	}
	if p.Transport == "xhttp" {
		s.XHTTP = &xhttpSettings{}
	}
	if p.Transport == "ws" {
		s.WebSocket = &WebSocketOptions{}
	}
	if p.Transport == "httpupgrade" {
		s.HTTPUpgrade = &HTTPUpgradeOptions{}
	}
	if o := p.Options; o != nil {
		if s.Reality != nil {
			s.Reality.Xver = o.RealityXver
			s.Reality.MaxTimeDiff = o.RealityMaxTimeDiff
			s.Reality.MinClientVer = o.RealityMinClientVer
			s.Reality.MaxClientVer = o.RealityMaxClientVer
			s.Reality.Show = o.RealityShow
		}
		if s.GRPC != nil {
			s.GRPC.Authority = o.GRPCAuthority
			s.GRPC.MultiMode = o.GRPCMultiMode
		}
		if s.XHTTP != nil && o.XHTTP != nil {
			s.XHTTP = &xhttpSettings{Path: o.XHTTP.Path, Host: o.XHTTP.Host, Mode: o.XHTTP.Mode, XPaddingBytes: o.XHTTP.XPaddingBytes}
		}
		if s.WebSocket != nil && o.WebSocket != nil {
			value := *o.WebSocket
			s.WebSocket = &value
		}
		if s.HTTPUpgrade != nil && o.HTTPUpgrade != nil {
			value := *o.HTTPUpgrade
			s.HTTPUpgrade = &value
		}
	}
	return s
}

func optionsFromStream(s stream) *ProfileOptions {
	o := &ProfileOptions{}
	if s.Reality != nil {
		o.RealityXver, o.RealityMaxTimeDiff = s.Reality.Xver, s.Reality.MaxTimeDiff
		o.RealityMinClientVer, o.RealityMaxClientVer = s.Reality.MinClientVer, s.Reality.MaxClientVer
		o.RealityShow = s.Reality.Show
	}
	if s.GRPC != nil {
		o.GRPCAuthority = s.GRPC.Authority
		o.GRPCMultiMode = s.GRPC.MultiMode
	}
	if s.XHTTP != nil {
		o.XHTTP = &XHTTPOptions{Path: s.XHTTP.Path, Host: s.XHTTP.Host, Mode: s.XHTTP.Mode, XPaddingBytes: s.XHTTP.XPaddingBytes}
	}
	if s.WebSocket != nil {
		value := *s.WebSocket
		o.WebSocket = &value
	}
	if s.HTTPUpgrade != nil {
		value := *s.HTTPUpgrade
		o.HTTPUpgrade = &value
	}
	return o
}
