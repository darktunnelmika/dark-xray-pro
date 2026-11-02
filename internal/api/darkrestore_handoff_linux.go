//go:build linux

package api

import (
	"context"
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

// StageDarkRestoreLocalHandoff is startup-only. The configured public key and
// file digest are independent operator pins, never browser supplied.
func (s *Server) StageDarkRestoreLocalHandoff(ctx context.Context, path, sha, publicPath, publicSHA string) error {
	if s.darkRestoreHandoff != nil || s.business == nil || !s.clientOwnershipV1Enabled || len(s.inboundKey) != 32 {
		return errors.New("invalid Dark Restore handoff staging configuration")
	}
	public, err := darkrestore.ReadLocalHandoffPublicKey(publicPath, publicSHA)
	if err != nil {
		return err
	}
	if _, err := darkrestore.VerifySignedLocalHandoff(path, sha, public); err != nil {
		return err
	}
	if err = s.business.EnableDarkRestoreImportV1(ctx); err != nil {
		return err
	}
	s.darkRestoreReceipts = true
	s.darkRestoreHandoff = &restoreHandoffStage{path: path, digest: sha, public: public}
	return nil
}
