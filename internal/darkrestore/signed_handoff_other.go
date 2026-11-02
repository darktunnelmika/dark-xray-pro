//go:build !linux

package darkrestore

func VerifySignedLocalHandoff(string, string, []byte) (VerifiedLocalHandoff, error) {
	return VerifiedLocalHandoff{}, ErrLocalHandoff
}
