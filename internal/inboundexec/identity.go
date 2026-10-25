package inboundexec

import (
	"encoding/hex"
	"encoding/json"
	"strings"
)

// ProcessMember binds the complete owned native/external incarnation. ConfigSHA
// is the actual private process configuration, including loopback API and
// bridge material added by the supervisor after the reviewed bundle is signed.
type ProcessMember struct {
	BootID     string `json:"boot_id"`
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	BinarySHA  string `json:"binary_sha"`
	ConfigSHA  string `json:"config_sha"`
}

func validIdentitySHA(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// ValidateEpochIdentity requires exactly every worker described by the signed
// bundle and exactly its release binary set. Canonical encodings make duplicate
// JSON keys, extra process attributes and alternate identities fail closed.
func ValidateEpochIdentity(b Bundle, e EpochInput) error {
	if e.NodeID != b.NodeID || e.InstallationID != b.InstallationID || e.Generation != b.Generation || e.ConfigSHA != b.ConfigSHA || e.ManifestSHA != b.ManifestSHA {
		return ErrInvalid
	}
	expected := map[string]string{}
	engines := map[string]bool{}
	if len(b.NativeConfig) > 0 {
		expected["xray"] = "xray"
		engines["xray"] = true
	}
	for _, c := range b.Components {
		if c.Engine == "" || c.Engine == "xray" || c.Tag == "" {
			return ErrInvalid
		}
		name := c.Engine + "/" + c.Tag
		if _, exists := expected[name]; exists {
			return ErrInvalid
		}
		expected[name] = c.Engine
		engines[c.Engine] = true
	}
	if len(expected) == 0 || len(expected) > 256 || len(e.BinarySHAs) != len(engines) || len(e.ProcessIdentity) > 65536 {
		return ErrInvalid
	}
	for engine, pin := range e.BinarySHAs {
		if !engines[engine] || !validIdentitySHA(pin) {
			return ErrInvalid
		}
	}
	var group map[string]string
	if json.Unmarshal([]byte(e.ProcessIdentity), &group) != nil || len(group) != len(expected) {
		return ErrInvalid
	}
	canonical, _ := json.Marshal(group)
	if string(canonical) != e.ProcessIdentity {
		return ErrInvalid
	}
	for name, raw := range group {
		engine, ok := expected[name]
		if !ok {
			return ErrInvalid
		}
		var member ProcessMember
		if json.Unmarshal([]byte(raw), &member) != nil {
			return ErrInvalid
		}
		canonical, _ = json.Marshal(member)
		if string(canonical) != raw || len(member.BootID) != 36 || member.PID <= 0 || member.StartTicks == 0 || member.BinarySHA != e.BinarySHAs[engine] || !validIdentitySHA(member.ConfigSHA) {
			return ErrInvalid
		}
		for i, c := range member.BootID {
			if i == 8 || i == 13 || i == 18 || i == 23 {
				if c != '-' {
					return ErrInvalid
				}
				continue
			}
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return ErrInvalid
			}
		}
	}
	return nil
}
