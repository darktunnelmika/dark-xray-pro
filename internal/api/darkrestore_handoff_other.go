//go:build !linux

package api

import (
	"context"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func (s *Server) StageDarkRestoreLocalHandoff(context.Context, string, string, string, string) error {
	return business.ErrUnavailable
}
