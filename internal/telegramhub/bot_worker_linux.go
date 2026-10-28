//go:build linux

package telegramhub

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// OwnerReportWorker reads the shared business reporter and owns only a separate
// delivery journal. It has no balance mutation or customer/seller send API.
// An external send cannot be made exactly-once across process/network failure:
// dispatching is durable before the request, and uncertain sends stay unknown.
type OwnerReportWorker struct {
	api     *botAPI
	handler *OwnerReportHandler
	journal *inboxJournal

	mu        sync.Mutex
	started   bool
	closed    bool
	cancel    context.CancelFunc
	done      chan struct{}
	runError  error
	closeOnce sync.Once
	closeErr  error
}

// The only caller that supplies a RoundTripper is the fake-network test suite.
// Production startup enters through OpenLABOwnerReportBot's fixed LAB paths.
func openOwnerReportWorker(binding botBinding, token, journalPath string, reporter RepresentativeReporter, transport http.RoundTripper) (*OwnerReportWorker, error) {
	handler, err := NewOwnerReportHandler(OwnerReportConfig{
		OwnerUserID: binding.OwnerUserID, OwnerChatID: binding.OwnerChatID, BotUsername: binding.BotUsername,
	}, reporter)
	if err != nil {
		return nil, err
	}
	api, err := newBotAPI(token, binding, transport)
	if err != nil {
		return nil, err
	}
	journal, err := openInboxJournal(journalPath, binding)
	if err != nil {
		api.close()
		return nil, err
	}
	return &OwnerReportWorker{api: api, handler: handler, journal: journal, done: make(chan struct{})}, nil
}

// Run starts once, verifies the configured bot, drains durable pending work,
// then polls. It returns only static errors or context cancellation. Startup
// should cancel Run before closing the shared business Store and call Close.
func (w *OwnerReportWorker) Run(ctx context.Context) (result error) {
	if w == nil || ctx == nil {
		return ErrBotWorkerState
	}
	w.mu.Lock()
	if w.started || w.closed {
		w.mu.Unlock()
		return ErrBotWorkerState
	}
	w.started = true
	ctx, w.cancel = context.WithCancel(ctx)
	w.mu.Unlock()
	defer func() {
		w.cancel()
		w.mu.Lock()
		w.runError = result
		close(w.done)
		w.mu.Unlock()
	}()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := w.api.verify(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	consecutivePollErrors := 0
	for {
		if err := w.drain(ctx); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		offset, err := w.journal.Cursor(ctx)
		if err != nil {
			return err
		}
		updates, err := w.api.updates(ctx, offset)
		if err != nil {
			consecutivePollErrors++
			if err := waitAfterBotError(ctx, err, consecutivePollErrors); err != nil {
				return err
			}
			continue
		}
		consecutivePollErrors = 0
		// A later getUpdates call acknowledges the higher cursor. Commit the
		// whole bounded batch before it can be acknowledged, then drain it.
		if err := w.journal.Ingest(ctx, updates); err != nil {
			return err
		}
		if len(updates) == 0 {
			// Long polling normally waits 20 seconds; also bound request rate if
			// the remote server repeatedly returns empty results immediately.
			if err := waitBot(ctx, time.Second); err != nil {
				return err
			}
		}
	}
}

func (w *OwnerReportWorker) drain(ctx context.Context) error {
	consecutiveSendErrors := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entry, exists, err := w.journal.Next(ctx)
		if err != nil || !exists {
			return err
		}
		reply, err := w.handler.Prepare(ctx, entry.Message)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if err := w.journal.Reject(ctx, entry.UpdateID, rejectedReportReason(err)); err != nil {
				return err
			}
			continue
		}
		if err := w.journal.BeginDispatch(ctx, entry, reply); err != nil {
			return err
		}
		outcome, sendErr := w.api.send(ctx, reply)
		// The send may finish at the same time as shutdown. Persist its outcome
		// with a separate short context; failure leaves dispatching, which a
		// later open conservatively recovers to unknown without resending.
		finishCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = w.journal.Finish(finishCtx, entry.UpdateID, outcome.State, outcome.Reason, outcome.MessageID)
		cancel()
		if err != nil {
			return err
		}
		if sendErr != nil {
			var failure *botAPIError
			if errors.As(sendErr, &failure) && failure.perMessage {
				// A deleted reply target or blocked chat rejects this command,
				// not the bot's identity. Preserve failed, bound the send rate,
				// and continue with later commands without retrying this one.
				consecutiveSendErrors = 0
				if err := waitBot(ctx, time.Second); err != nil {
					return err
				}
				continue
			}
			consecutiveSendErrors++
			if err := waitAfterBotError(ctx, sendErr, consecutiveSendErrors); err != nil {
				return err
			}
		} else {
			consecutiveSendErrors = 0
		}
	}
}

func rejectedReportReason(err error) string {
	switch {
	case errors.Is(err, ErrUnauthorized):
		return "owner_scope_rejected"
	case errors.Is(err, ErrInvalidCommand):
		return "command_rejected"
	case errors.Is(err, ErrReportNotFound):
		return "report_not_found"
	default:
		return "report_unavailable"
	}
}

func waitAfterBotError(ctx context.Context, err error, consecutive int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var failure *botAPIError
	if !errors.As(err, &failure) || failure.fatal || consecutive >= 5 {
		return err
	}
	delay := time.Duration(1<<min(consecutive, 5)) * time.Second
	if failure.retryAfter > delay {
		delay = failure.retryAfter
	}
	return waitBot(ctx, delay)
}

func waitBot(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Wait does not start the worker. It lets HUB shutdown coordinate Run with the
// lifetime of its shared Store without exposing any update or credential data.
func (w *OwnerReportWorker) Wait(ctx context.Context) error {
	if w == nil || ctx == nil {
		return ErrBotWorkerState
	}
	w.mu.Lock()
	started, closed, done := w.started, w.closed, w.done
	w.mu.Unlock()
	if !started && !closed {
		return ErrBotWorkerState
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.runError
	}
}

func (w *OwnerReportWorker) Close() error {
	if w == nil {
		return nil
	}
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		cancel, started := w.cancel, w.started
		if !started {
			close(w.done)
		}
		w.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		<-w.done
		w.api.close()
		w.closeErr = w.journal.Close()
	})
	return w.closeErr
}
