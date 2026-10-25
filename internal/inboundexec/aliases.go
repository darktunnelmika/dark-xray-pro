package inboundexec

// PublicAliasProtocol is the managed userspace origin marker in Linux IFA_PROTO.
// It is one part of recorded kernel identity, never sufficient ownership alone.
const PublicAliasProtocol = 215

type PublicAliasInterface struct {
	Name  string `json:"name"`
	Index uint32 `json:"index"`
	MAC   string `json:"mac"`
}

type PublicAliasBinding struct {
	PrincipalID      string `json:"principal_id"`
	Email            string `json:"email"`
	PeerPublicKeySHA string `json:"peer_public_key_sha"`
	Address          string `json:"address"`
	PrefixLength     uint8  `json:"prefix_length"`
	Protocol         uint8  `json:"protocol"`
	// CreatedTicks may genuinely be zero. The presence of this binding means
	// that IFA_CACHEINFO was observed after an acknowledged exclusive add.
	CreatedTicks uint32 `json:"created_ticks"`
}

// PublicAliasAuthority is injected only by the owning Agent into a private
// adapter configuration. Its exact bytes are bound by the process ConfigSHA.
// The signed bundle determines peers/interface; this records their measured
// kernel incarnation. Lifetimes may renew while this birth identity stays fixed.
type PublicAliasAuthority struct {
	Version                 int                  `json:"version"`
	IntentID                string               `json:"intent_id"`
	NodeID                  string               `json:"node_id"`
	InstallationID          string               `json:"installation_id"`
	Generation              uint64               `json:"generation"`
	ConfigSHA               string               `json:"config_sha"`
	ManifestSHA             string               `json:"manifest_sha"`
	ComponentTag            string               `json:"component_tag"`
	ComponentConfigSHA      string               `json:"component_config_sha"`
	BootID                  string               `json:"boot_id"`
	NetworkNamespace        uint64               `json:"network_namespace,string"`
	Interface               PublicAliasInterface `json:"interface"`
	InitialLeaseExpiresNano int64                `json:"initial_lease_expires_unix_nano"`
	Aliases                 []PublicAliasBinding `json:"aliases"`
}
