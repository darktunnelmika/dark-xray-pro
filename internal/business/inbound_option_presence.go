package business

import "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"

// These sections are currently supported by paused creation plans only. Native
// delivery has a narrower metering grammar, and fallback forwarding can bypass
// its ordinary outbound barrier.
func hasInboundTrafficOptions(options *xrayconfig.ProfileOptions) bool {
	return options != nil && (options.Sniffing != nil || len(options.Fallbacks) != 0)
}

// Presence is captured before normalization. A browser that only understands
// older scalar options must not erase a newer section by omitting it.
type inboundOptionPresence struct {
	Options   bool
	Sniffing  bool
	Fallbacks bool
}

func (p inboundOptionPresence) include(options *xrayconfig.ProfileOptions) inboundOptionPresence {
	if options != nil {
		p.Options = true
		p.Sniffing = p.Sniffing || options.Sniffing != nil
		p.Fallbacks = p.Fallbacks || options.Fallbacks != nil
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
	return "", ""
}
