//go:build !linux

package business

import (
	"context"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func (s *Store) AttachSignedLocalSettlementV1(context.Context, string, darkrestore.VerifiedLocalHandoff) (DarkRestoreLocalAttachmentReceiptV1, error) {
	return DarkRestoreLocalAttachmentReceiptV1{}, ErrUnavailable
}
