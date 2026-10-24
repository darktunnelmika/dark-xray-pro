package business

import "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"

// These sections are currently supported by paused creation plans only. Native
// delivery has a narrower metering grammar, and fallback forwarding can bypass
// its ordinary outbound barrier.
func hasInboundTrafficOptions(options *xrayconfig.ProfileOptions) bool {
	return options != nil && (options.Sniffing != nil || len(options.Fallbacks) != 0)
}

// New network/client sections belong to the complete creation-plan editor.
// Existing metered delivery has an independently enforced, narrower grammar.
func hasInboundNetworkOptions(transport string, options *xrayconfig.ProfileOptions) bool {
	return transport == "xhttp" || hasInboundTLSAuthoring(transport, options) || options != nil && (options.XHTTP != nil || options.Client != nil)
}

// Presence is captured before normalization. A browser that only understands
// older scalar options must not erase a newer section by omitting it.
type inboundOptionPresence struct {
	Options     bool
	Sniffing    bool
	Fallbacks   bool
	XHTTP       bool
	Client      bool
	WebSocket   bool
	HTTPUpgrade bool
}

func (p inboundOptionPresence) include(options *xrayconfig.ProfileOptions) inboundOptionPresence {
	if options != nil {
		p.Options = true
		p.Sniffing = p.Sniffing || options.Sniffing != nil
		p.Fallbacks = p.Fallbacks || options.Fallbacks != nil
		p.XHTTP = p.XHTTP || options.XHTTP != nil
		p.Client = p.Client || options.Client != nil
		p.WebSocket = p.WebSocket || options.WebSocket != nil
		p.HTTPUpgrade = p.HTTPUpgrade || options.HTTPUpgrade != nil
	}
	return p
}

func (p inboundOptionPresence) missing(existing *xrayconfig.ProfileOptions) (field, code string) {
	if existing == nil {
		return "", ""
	}
	if !p.Options {
		return "profile.options", "profile_options_required"
	}
	if existing.Sniffing != nil && !p.Sniffing {
		return "profile.options.sniffing", "profile_sniffing_required"
	}
	if len(existing.Fallbacks) != 0 && !p.Fallbacks {
		return "profile.options.fallbacks", "profile_fallbacks_required"
	}
	if existing.XHTTP != nil && !p.XHTTP {
		return "profile.options.xhttp", "profile_xhttp_required"
	}
	if existing.Client != nil && !p.Client {
		return "profile.options.client", "profile_client_options_required"
	}
	if existing.WebSocket != nil && !p.WebSocket {
		return "profile.options.websocket", "profile_websocket_required"
	}
	if existing.HTTPUpgrade != nil && !p.HTTPUpgrade {
		return "profile.options.http_upgrade", "profile_http_upgrade_required"
	}
	return "", ""
}
