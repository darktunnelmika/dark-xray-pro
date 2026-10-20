//go:build linux

package usagecheckpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

// ConfirmSettlement uses the same CA-verified, leaf-pinned TLS transport as
// usage ingestion. A generic report ACK cannot authorize a replacement epoch.
func (s *Sender) ConfirmSettlement(ctx context.Context, e Epoch, r Receipt) error {
	if _, _, err := CanonicalEpoch(e); err != nil {
		return err
	}
	if !ValidReceipt(r) || r.EpochID != e.ID {
		return ErrReceipt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.NodeID != s.node || e.InstallationID != s.installation {
		return ErrEpoch
	}
	endpoint, err := url.Parse(s.url)
	if err != nil {
		return ErrTransport
	}
	endpoint.Path += "/settled"
	raw, _ := json.Marshal(r)
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(call, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return ErrTransport
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	res, err := s.client.Do(req)
	if err != nil {
		return ErrTransport
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrTransport
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return ErrTransport
	}
	var proof SettlementReceipt
	if usagewire.Decode(raw, &proof) != nil || !proof.Sealed || proof.Receipt != r {
		return ErrReceipt
	}
	return nil
}
