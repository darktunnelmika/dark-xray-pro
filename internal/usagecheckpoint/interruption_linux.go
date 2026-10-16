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

// NotifyInterrupted uses exactly the same CA+pin checked transport as usage
// receipts. It is idempotent, can be retried after restart, and cannot clear a
// hold, invent a final counter, or register a different installation.
func (s *Sender) NotifyInterrupted(ctx context.Context, epoch Epoch) error {
	e, _, err := CanonicalEpoch(epoch)
	if err != nil {
		return err
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
	endpoint.Path += "/interrupted"
	notice := Interruption{EpochID: e.ID}
	raw, _ := json.Marshal(notice)
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
	var receipt InterruptionReceipt
	if usagewire.Decode(raw, &receipt) != nil || receipt.EpochID != e.ID || !receipt.ReconciliationRequired {
		return ErrReceipt
	}
	return nil
}
