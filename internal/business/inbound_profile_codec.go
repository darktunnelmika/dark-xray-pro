package business

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// Each extension uses a distinct authenticated envelope. An older binary must
// fail to open settings it cannot preserve during a rollback. Ordinary profiles
// retain v1; the original REALITY/gRPC scalar options retain v2. Sniffing and
// fallbacks require v3. XHTTP transport or typed client/network options require
// v4, including when combined with recognized settings from earlier versions.
// TLS credentials and WS/HTTPUpgrade authoring require v5.
var extendedProfilePrefix = []byte("DXPPRF2\x00")
var profileV3Prefix = []byte("DXPPRF3\x00")
var profileV4Prefix = []byte("DXPPRF4\x00")
var profileV5Prefix = []byte("DXPPRF5\x00")

func extendedProfileAAD(id uint64) []byte {
	return []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v2:%d", id))
}

func profileV3AAD(id uint64) []byte {
	return []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v3:%d", id))
}
func profileV4AAD(id uint64) []byte {
	return []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v4:%d", id))
}
func profileV5AAD(id uint64) []byte {
	return []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v5:%d", id))
}

// options must already be normalized so an empty object does not force a new
// format. A disabled but configured sniffing object still contains settings.
func profileEnvelopeVersion(transport string, options *xrayconfig.ProfileOptions) int {
	if hasInboundTLSAuthoring(transport, options) {
		return 5
	}
	if hasInboundNetworkOptions(transport, options) {
		return 4
	}
	if options == nil {
		return 1
	}
	if options.Sniffing != nil || len(options.Fallbacks) != 0 {
		return 3
	}
	return 2
}

func privateProfileEnvelopeVersion(p xrayconfig.Profile) int {
	if p.Security == "tls" || p.TLS != nil {
		return 5
	}
	return profileEnvelopeVersion(p.Transport, p.Options)
}

func normalizePrivateProfile(p xrayconfig.Profile) (xrayconfig.Profile, error) {
	security, err := xrayconfig.NormalizeProfileSecurity(p.Security)
	if err != nil {
		return xrayconfig.Profile{}, ErrInvalid
	}
	p.Security = security
	if security == "tls" {
		material, _, err := xrayconfig.NormalizeTLSMaterial(p.TLS)
		if err != nil || p.RealityPrivateKey != "" {
			return xrayconfig.Profile{}, ErrInvalid
		}
		p.TLS = material
	} else if p.TLS != nil {
		return xrayconfig.Profile{}, ErrInvalid
	}
	options, err := xrayconfig.NormalizeProfileOptions(p.Transport, p.Options)
	if err != nil {
		return xrayconfig.Profile{}, ErrInvalid
	}
	p.Options = options
	if p.Security == "tls" || hasInboundTLSAuthoring(p.Transport, p.Options) {
		// New storage must not authenticate a security/transport/SNI mismatch.
		// This validation is clock-independent: expired material remains
		// readable for renewal, and historical envelopes retain their behavior.
		if err := xrayconfig.ValidateProfile(p); err != nil {
			return xrayconfig.Profile{}, ErrInvalid
		}
	}
	return p, nil
}

func sealInboundProfile(key []byte, p xrayconfig.Profile) ([]byte, error) {
	p, err := normalizePrivateProfile(p)
	if err != nil {
		return nil, ErrInvalid
	}
	a, err := profileAEAD(key)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := profileAAD(p.InboundID)
	var prefix []byte
	switch privateProfileEnvelopeVersion(p) {
	case 2:
		aad = extendedProfileAAD(p.InboundID)
		prefix = extendedProfilePrefix
	case 3:
		aad = profileV3AAD(p.InboundID)
		prefix = profileV3Prefix
	case 4:
		aad = profileV4AAD(p.InboundID)
		prefix = profileV4Prefix
	case 5:
		aad = profileV5AAD(p.InboundID)
		prefix = profileV5Prefix
	}
	sealed := a.Seal(nonce, nonce, raw, aad)
	if prefix != nil {
		sealed = append(append([]byte(nil), prefix...), sealed...)
	}
	return sealed, nil
}

func openInboundProfile(key, encrypted []byte, id uint64) (xrayconfig.Profile, error) {
	var p xrayconfig.Profile
	a, err := profileAEAD(key)
	if err != nil {
		return p, err
	}
	original := encrypted
	aad := profileAAD(id)
	version := 1
	switch {
	case bytes.HasPrefix(encrypted, profileV5Prefix):
		encrypted = encrypted[len(profileV5Prefix):]
		aad = profileV5AAD(id)
		version = 5
	case bytes.HasPrefix(encrypted, profileV4Prefix):
		encrypted = encrypted[len(profileV4Prefix):]
		aad = profileV4AAD(id)
		version = 4
	case bytes.HasPrefix(encrypted, profileV3Prefix):
		encrypted = encrypted[len(profileV3Prefix):]
		aad = profileV3AAD(id)
		version = 3
	case bytes.HasPrefix(encrypted, extendedProfilePrefix):
		encrypted = encrypted[len(extendedProfilePrefix):]
		aad = extendedProfileAAD(id)
		version = 2
	}
	open := func(value, binding []byte) ([]byte, error) {
		if len(value) < a.NonceSize()+a.Overhead() {
			return nil, ErrUnavailable
		}
		return a.Open(nil, value[:a.NonceSize()], value[a.NonceSize():], binding)
	}
	raw, err := open(encrypted, aad)
	if err != nil && version != 1 {
		// A valid v1 random nonce can start with an extension marker. Authenticate that
		// entire legacy envelope before considering it v1; never infer a
		// downgrade from JSON alone.
		raw, err = open(original, profileAAD(id))
		version = 1
	}
	// Fail closed on fields introduced by a future binary, as well as duplicate
	// names and trailing JSON. Authentication alone does not establish that this
	// binary understands every setting in the authenticated payload.
	if err != nil || usagewire.Decode(raw, &p) != nil || p.InboundID != id {
		return xrayconfig.Profile{}, ErrUnavailable
	}
	p, err = normalizePrivateProfile(p)
	if err != nil || version != privateProfileEnvelopeVersion(p) {
		return xrayconfig.Profile{}, ErrUnavailable
	}
	return p, nil
}
