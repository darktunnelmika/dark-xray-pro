package business

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// Profiles with extended settings use a distinct authenticated envelope. An
// earlier binary knows only the v1 AAD and must fail to open these profiles;
// otherwise json.Unmarshal would silently discard options during a rollback.
// Ordinary profiles retain the original v1 encoding and fingerprints.
var extendedProfilePrefix = []byte("DXPPRF2\x00")

func extendedProfileAAD(id uint64) []byte {
	return []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v2:%d", id))
}

func sealInboundProfile(key []byte, p xrayconfig.Profile) ([]byte, error) {
	options, err := xrayconfig.NormalizeProfileOptions(p.Transport, p.Options)
	if err != nil {
		return nil, ErrInvalid
	}
	p.Options = options
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
	if options != nil {
		aad = extendedProfileAAD(p.InboundID)
	}
	sealed := a.Seal(nonce, nonce, raw, aad)
	if options != nil {
		sealed = append(append([]byte(nil), extendedProfilePrefix...), sealed...)
	}
	return sealed, nil
}

func openInboundProfile(key, encrypted []byte, id uint64) (xrayconfig.Profile, error) {
	var p xrayconfig.Profile
	a, err := profileAEAD(key)
	if err != nil {
		return p, err
	}
	aad := profileAAD(id)
	extended := bytes.HasPrefix(encrypted, extendedProfilePrefix)
	if extended {
		encrypted = encrypted[len(extendedProfilePrefix):]
		aad = extendedProfileAAD(id)
	}
	if len(encrypted) < a.NonceSize()+a.Overhead() {
		return p, ErrUnavailable
	}
	raw, err := a.Open(nil, encrypted[:a.NonceSize()], encrypted[a.NonceSize():], aad)
	if err != nil && extended {
		// A valid v1 random nonce can start with the marker. Authenticate that
		// entire legacy envelope before considering it v1; never infer a
		// downgrade from JSON alone.
		legacy := append(append([]byte(nil), extendedProfilePrefix...), encrypted...)
		raw, err = a.Open(nil, legacy[:a.NonceSize()], legacy[a.NonceSize():], profileAAD(id))
		extended = false
	}
	if err != nil || json.Unmarshal(raw, &p) != nil || p.InboundID != id {
		return xrayconfig.Profile{}, ErrUnavailable
	}
	options, err := xrayconfig.NormalizeProfileOptions(p.Transport, p.Options)
	if err != nil || extended != (options != nil) {
		return xrayconfig.Profile{}, ErrUnavailable
	}
	p.Options = options
	return p, nil
}
