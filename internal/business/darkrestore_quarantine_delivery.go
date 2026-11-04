package business

import (
	"encoding/json"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// Private delivery belongs only on the paired Agent TLS channel.
type DarkRestoreQuarantineDelivery struct {
	Plan      DarkRestoreQuarantinePlan
	Candidate xrayconfig.Candidate
	Config    []byte
}

func (p DarkRestoreQuarantinePlan) PrivateDelivery() DarkRestoreQuarantineDelivery {
	c := p.candidate
	c.Config = append([]byte(nil), c.Config...)
	return DarkRestoreQuarantineDelivery{Plan: p, Candidate: c, Config: c.Config}
}
func (d DarkRestoreQuarantineDelivery) Open(node, installation, planSHA string) (DarkRestoreQuarantinePlan, error) {
	p := d.Plan
	sha, err := p.Digest()
	if err != nil || sha != planSHA || p.NodeID != node || p.InstallationID != installation || p.ConfigSHA != d.Candidate.SHA256 || p.ManifestSHA != d.Candidate.ManifestSHA256 || len(d.Config) == 0 || len(d.Config) > xrayconfig.MaxCandidateBytes {
		return DarkRestoreQuarantinePlan{}, ErrDarkRestoreCutover
	}
	// Copy all private bytes so the receiver owns a frozen candidate.
	raw, err := json.Marshal(d.Candidate)
	if err != nil {
		return DarkRestoreQuarantinePlan{}, err
	}
	if err = json.Unmarshal(raw, &p.candidate); err != nil {
		return DarkRestoreQuarantinePlan{}, err
	}
	p.candidate.Config = append([]byte(nil), d.Config...)
	return p, nil
}
