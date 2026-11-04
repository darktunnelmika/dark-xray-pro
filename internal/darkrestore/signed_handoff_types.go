package darkrestore

import "errors"

var ErrLocalHandoff = errors.New("untrusted dark restore local handoff")

// This value can be constructed only by the signature verifier on the
// destination. An ordinary parsed JSON value cannot pass the attachment gate.
type VerifiedLocalHandoff struct {
	settlement   LocalSourceSettlement
	digest       string
	publicKeySHA string
	valid        bool
}

func (v VerifiedLocalHandoff) Settlement() (LocalSourceSettlement, error) {
	if !v.valid || v.settlement.Scope != "single_local_source_epoch" {
		return LocalSourceSettlement{}, ErrLocalHandoff
	}
	return v.settlement, nil
}

func (v VerifiedLocalHandoff) Evidence() (string, string, error) {
	if _, err := v.Settlement(); err != nil || len(v.digest) != 64 || len(v.publicKeySHA) != 64 {
		return "", "", ErrLocalHandoff
	}
	return v.digest, v.publicKeySHA, nil
}
