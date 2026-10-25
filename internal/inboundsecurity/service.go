package inboundsecurity

import (
	"context"
	"net"
	"regexp"
	"time"
)

type Service struct {
	config Config
	probes chan struct{}
	finds  chan struct{}
	core   *pinnedCore
}

var nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$`)

// These are the bounded defaults shipped by MHSanaei/3x-ui at commit
// 39935ed9ef29b375184113983580052c6d970e97, internal/web/service/reality_scan.go.
// Listing them is not a claim about current reachability or compatibility.
var sanaeiTargets = []Target{
	{"www.cloudflare.com:443", "www.cloudflare.com"},
	{"www.microsoft.com:443", "www.microsoft.com"},
	{"www.amazon.com:443", "www.amazon.com"},
	{"aws.amazon.com:443", "aws.amazon.com"},
	{"www.samsung.com:443", "www.samsung.com"},
	{"www.nvidia.com:443", "www.nvidia.com"},
	{"www.amd.com:443", "www.amd.com"},
	{"www.intel.com:443", "www.intel.com"},
	{"www.sony.com:443", "www.sony.com"},
	{"dl.google.com:443", "dl.google.com"},
}

func New(config Config) (*Service, error) {
	if config.ProbeTimeout == 0 {
		config.ProbeTimeout = 10 * time.Second
	}
	if config.FindTimeout == 0 {
		config.FindTimeout = 45 * time.Second
	}
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 8
	}
	if config.ProbeTimeout < time.Millisecond || config.ProbeTimeout > 15*time.Second ||
		config.FindTimeout < config.ProbeTimeout || config.FindTimeout > 90*time.Second ||
		config.MaxConcurrency < 1 || config.MaxConcurrency > 16 ||
		(config.LocalNodeID != "" && !nodeIDPattern.MatchString(config.LocalNodeID)) {
		return nil, failure("invalid_security_config")
	}
	if config.Resolver == nil {
		config.Resolver = net.DefaultResolver
	}
	if config.DialContext == nil {
		dialer := &net.Dialer{Timeout: config.ProbeTimeout, KeepAlive: -1}
		config.DialContext = dialer.DialContext
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.RootCAs != nil {
		config.RootCAs = config.RootCAs.Clone()
	}
	if config.DefaultTargets == nil {
		config.DefaultTargets = sanaeiTargets
	}
	if len(config.DefaultTargets) > MaxTargets {
		return nil, failure("too_many_default_targets")
	}
	config.DefaultTargets = append([]Target(nil), config.DefaultTargets...)
	for _, target := range config.DefaultTargets {
		if _, _, err := parseTarget(target.Target, target.ServerName); err != nil {
			return nil, failure("invalid_default_target")
		}
	}
	var core *pinnedCore
	if config.CorePath != "" || config.CoreSHA256 != "" {
		var err error
		core, err = newPinnedCore(config.CorePath, config.CoreSHA256)
		if err != nil {
			return nil, err
		}
	}
	return &Service{config: config, probes: make(chan struct{}, config.MaxConcurrency), finds: make(chan struct{}, 2), core: core}, nil
}

func (s *Service) Capabilities() Capabilities {
	core := s.core != nil
	result := Capabilities{
		Enabled:      true,
		Keys:         map[string]bool{"x25519": true, "mldsa65": core, "ech": core},
		Certificates: map[string]bool{"inspect": true, "generate": true, "remote": true},
		Reality: RealityCapability{Scan: true, Find: true, PublicTargetsOnly: true, MaxTargets: MaxTargets,
			MaxCIDRAddresses: MaxCIDRAddresses, MaxConcurrency: s.config.MaxConcurrency},
		NodeProbeAvailable: s.config.NodeProbe != nil,
		DefaultTargets:     append([]Target{}, s.config.DefaultTargets...),
		UnavailableReasons: map[string]string{},
	}
	if !core {
		result.UnavailableReasons["mldsa65"] = "pinned_xray_core_required"
		result.UnavailableReasons["ech"] = "pinned_xray_core_required"
	}
	if s.config.NodeProbe == nil {
		result.UnavailableReasons["node_probe"] = "selected_node_transport_unavailable"
	}
	return result
}

func validateScope(scope, nodeID string) error {
	if scope == "hub" && nodeID == "" {
		return nil
	}
	if scope == "node" && nodeIDPattern.MatchString(nodeID) {
		return nil
	}
	return failure("invalid_probe_scope")
}

func (s *Service) scopeAvailable(scope, nodeID string) error {
	if err := validateScope(scope, nodeID); err != nil {
		return err
	}
	if scope == "node" && nodeID != s.config.LocalNodeID && s.config.NodeProbe == nil {
		return failure("selected_node_transport_unavailable")
	}
	if scope == "hub" && s.config.LocalNodeID != "" {
		return failure("hub_probe_unavailable_on_node")
	}
	return nil
}

func (s *Service) acquire(ctx context.Context) error {
	if ctx.Err() != nil {
		return failure("probe_cancelled")
	}
	select {
	case s.probes <- struct{}{}:
		return nil
	case <-ctx.Done():
		return failure("probe_cancelled")
	}
}
