//go:build linux

package telegrambot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Worker owns one private, bounded journal and independent polling loops for
// the explicit bot allowlist. No bot failure grants another bot its authority.
type Worker struct {
	backend         Backend
	journal         *journal
	apis            map[int64]*api
	mu              sync.Mutex
	started, closed bool
	cancel          context.CancelFunc
	done            chan struct{}
	runError        error
	statuses        map[int64]BotStatus
	closeOnce       sync.Once
	closeErr        error
}

func (w *Worker) Run(ctx context.Context) (result error) {
	if w == nil || ctx == nil {
		return ErrState
	}
	w.mu.Lock()
	if w.started || w.closed {
		w.mu.Unlock()
		return ErrState
	}
	w.started = true
	ctx, w.cancel = context.WithCancel(ctx)
	w.mu.Unlock()
	defer func() { w.cancel(); w.mu.Lock(); w.runError = result; close(w.done); w.mu.Unlock() }()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	errorsOut := make(chan error, len(w.apis))
	var group sync.WaitGroup
	for id, a := range w.apis {
		group.Add(1)
		go func(id int64, a *api) {
			defer group.Done()
			err := w.runBot(ctx, a)
			if ctx.Err() == nil && err != nil {
				w.status(id, "error", 0)
				w.problem(id, "worker_stopped")
			} else {
				w.status(id, "stopped", 0)
			}
			errorsOut <- err
		}(id, a)
	}
	group.Wait()
	close(errorsOut)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for err := range errorsOut {
		if err != nil {
			return err
		}
	}
	return ErrState
}

func (w *Worker) runBot(parent context.Context, a *api) error {
	w.status(a.botID, "starting", 0)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = w.refreshBacklog(ctx, a.botID)
	}()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if err := w.refreshBacklog(ctx, a.botID); err != nil {
		return err
	}
	if err := a.verify(ctx); err != nil {
		return err
	}
	w.status(a.botID, "identity_verified", 0)
	pollDone := make(chan error, 1)
	go func() { pollDone <- w.poll(ctx, a) }()
	defer func() { cancel(); <-pollDone }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	chatNext := map[int64]time.Time{}
	var botNext, campaignNext, lastMaintenance, lastBacklog time.Time
	lastPrune := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-pollDone:
			// Put the value back for the deferred join. The producer has exited.
			pollDone <- err
			return err
		case <-ticker.C:
		}
		if time.Since(lastPrune) >= time.Minute {
			if err := w.journal.prune(ctx); err != nil {
				return err
			}
			lastPrune = time.Now()
		}
		if time.Since(lastBacklog) >= time.Second {
			if err := w.refreshBacklog(ctx, a.botID); err != nil {
				return err
			}
			lastBacklog = time.Now()
		}
		if maintenance, ok := w.backend.(maintenanceBackend); ok && time.Since(lastMaintenance) >= time.Minute {
			maintCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			err := maintenance.Maintain(maintCtx, a.botID)
			stop()
			lastMaintenance = time.Now()
			if err != nil {
				w.problem(a.botID, "rewards_maintenance_unavailable")
			} else {
				w.problem(a.botID, "")
			}
		}
		in, exists, err := w.journal.nextInput(ctx, a.botID)
		if err != nil {
			return err
		}
		if exists {
			if err := w.processInput(ctx, in); err != nil {
				return err
			}
		}
		if acknowledger, ok := w.backend.(deliveryAcknowledger); ok {
			ack, found, err := w.journal.nextAck(ctx, a.botID)
			if err != nil {
				return err
			}
			if found {
				ackCtx, stop := context.WithTimeout(ctx, 3*time.Second)
				accepted := acknowledger.AcknowledgeDelivery(ackCtx, ack.Identity, ack.Proof, ack.MessageID) == nil
				stop()
				if err = w.journal.finishAck(ctx, ack, accepted); err != nil {
					return err
				}
			}
		}
		if time.Now().Before(botNext) {
			continue
		}
		d, exists, err := w.journal.nextDelivery(ctx, a.botID)
		if err != nil {
			return err
		}
		if !exists {
			if campaigns, ok := w.backend.(campaignBackend); ok && !time.Now().Before(campaignNext) {
				claimCtx, stop := context.WithTimeout(ctx, 3*time.Second)
				task, found, claimErr := campaigns.ClaimCampaign(claimCtx, a.botID)
				stop()
				campaignNext = time.Now().Add(250 * time.Millisecond)
				if claimErr != nil {
					w.problem(a.botID, "campaign_queue_unavailable")
					continue
				}
				if found {
					out := DeliveryOutcome{State: "unknown", Reason: "campaign_claim_expired"}
					var sendErr error = ErrProtocol
					if task.Delivery.BotID == a.botID && task.Delivery.ChatID > 0 && task.LeaseUntil > time.Now().Unix() {
						sendCtx, stop := context.WithTimeout(ctx, 30*time.Second)
						out, sendErr = a.dispatch(sendCtx, task.Delivery)
						stop()
					}
					finishCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
					finishErr := campaigns.FinishCampaign(finishCtx, task, out, sendErr)
					stop()
					if finishErr != nil {
						return ErrJournal
					}
					botNext = time.Now().Add(50 * time.Millisecond)
					chatNext[task.Delivery.ChatID] = time.Now().Add(time.Second)
					if out.RetryAfter > 0 {
						botNext = time.Now().Add(out.RetryAfter)
					}
					var failure *APIError
					if errors.As(sendErr, &failure) && failure.Fatal {
						return sendErr
					}
				}
			}
			continue
		}
		if d.ExpiresUnix <= time.Now().Unix() {
			if err = w.journal.cancelDelivery(ctx, d, "delivery_expired"); err != nil {
				return err
			}
			continue
		}
		if d.Kind != "checkout" && d.Kind != "callback_answer" && time.Now().Before(chatNext[d.ChatID]) {
			if err = w.journal.delay(ctx, d, chatNext[d.ChatID]); err != nil {
				return err
			}
			continue
		}
		if !d.Anonymous {
			checkCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			current, authErr := w.backend.Resolve(checkCtx, d.Access.Identity, false)
			stop()
			if authErr != nil || !current.valid() || current.digest() != d.Access.digest() {
				if err = w.journal.cancelDelivery(ctx, d, "authority_changed"); err != nil {
					return err
				}
				continue
			}
		}
		if d.Proof != nil && d.Proof.ExpiresUnixNano <= time.Now().UnixNano() {
			if err = w.journal.cancelDelivery(ctx, d, "native_proof_expired"); err != nil {
				return err
			}
			if err = w.journal.expiredProofNotice(ctx, d); err != nil {
				return err
			}
			continue
		}
		if binding, needed, guardErr := financialBinding(d); guardErr != nil {
			return guardErr
		} else if needed {
			validator, ok := w.backend.(financialDispatchValidator)
			var checkErr error = ErrAccess
			if ok {
				checkCtx, stop := context.WithTimeout(ctx, time.Second)
				checkErr = validator.ValidateFinancialDispatch(checkCtx, d.Access.Identity, binding)
				stop()
			}
			if checkErr != nil {
				if err = w.journal.cancelDelivery(ctx, d, "financial_terms_changed"); err != nil {
					return err
				}
				continue
			}
		}
		if d.ExpiresUnix <= time.Now().Unix() {
			if err = w.journal.cancelDelivery(ctx, d, "delivery_expired"); err != nil {
				return err
			}
			continue
		}
		if err = w.journal.claim(ctx, d); err != nil {
			return err
		}
		sendCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		if d.Kind == "checkout" || d.Kind == "callback_answer" {
			stop()
			sendCtx, stop = context.WithTimeout(ctx, 3*time.Second)
		}
		out, sendErr := a.dispatch(sendCtx, d)
		stop()
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = w.journal.finish(finishCtx, d, out)
		finishCancel()
		if err != nil {
			return err
		}
		botNext = time.Now().Add(50 * time.Millisecond)
		if d.Kind != "checkout" && d.Kind != "callback_answer" {
			chatNext[d.ChatID] = time.Now().Add(time.Second)
			campaignNext = time.Now().Add(time.Second)
		}
		if out.RetryAfter > 0 {
			botNext = time.Now().Add(out.RetryAfter)
		}
		var failure *APIError
		if errors.As(sendErr, &failure) && failure.Fatal {
			return sendErr
		}
	}
}

func financialBinding(d delivery) (Payment, bool, error) {
	switch d.Kind {
	case "invoice":
		var m wireInvoice
		if decodeJSON(d.Payload, &m) != nil || len(m.Prices) != 1 {
			return Payment{}, false, ErrJournal
		}
		p := Payment{Payload: m.Payload, Currency: m.Currency, Amount: m.Prices[0].Amount}
		if !validPayment(p, false) {
			return Payment{}, false, ErrJournal
		}
		return p, true, nil
	case "checkout":
		var m wireCheckout
		if decodeJSON(d.Payload, &m) != nil {
			return Payment{}, false, ErrJournal
		}
		if !m.OK {
			return Payment{}, false, nil
		}
		if m.Binding == nil || !validPayment(*m.Binding, false) {
			return Payment{}, false, ErrJournal
		}
		return *m.Binding, true, nil
	}
	return Payment{}, false, nil
}

func (w *Worker) poll(ctx context.Context, a *api) error {
	consecutive := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cursor, err := w.journal.cursor(ctx, a.botID)
		if err != nil {
			return err
		}
		batch, err := a.updates(ctx, cursor)
		if err != nil {
			var failure *APIError
			if !errors.As(err, &failure) || failure.Fatal {
				return err
			}
			consecutive++
			delay := time.Duration(1<<min(consecutive, 5)) * time.Second
			if failure.RetryAfter > delay {
				delay = failure.RetryAfter
			}
			if err = wait(ctx, delay); err != nil {
				return err
			}
			continue
		}
		consecutive = 0
		// Commit the entire bounded batch before acknowledging its cursor in a
		// later getUpdates request. Persist unsupported updates as rejections.
		if err = w.journal.ingest(ctx, a.botID, batch); err != nil {
			return err
		}
		w.status(a.botID, "polling", time.Now().Unix())
		if len(batch) == 0 {
			if err = wait(ctx, 500*time.Millisecond); err != nil {
				return err
			}
		}
	}
}

func (w *Worker) processInput(ctx context.Context, in Incoming) error {
	lang := w.journal.preference(ctx, in.Identity, in.Language)
	if in.Kind == "payment" || in.Kind == "refund" {
		recorder, ok := w.backend.(financialRecorder)
		if !ok {
			return w.journal.rejectInput(ctx, in, "financial_capture_unavailable", true)
		}
		captureCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		err := recorder.CaptureFinancialEvent(captureCtx, in)
		stop()
		if err != nil {
			if errors.Is(err, ErrFinancialMismatch) || errors.Is(err, ErrAccess) || errors.Is(err, ErrProtocol) {
				return w.journal.rejectInput(ctx, in, "financial_capture_requires_review", true)
			}
			return w.journal.retryInput(ctx, in)
		}
	}
	start := in.Kind == "message" && (in.Text == "/start" || strings.HasPrefix(in.Text, "/start "))
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	access, err := w.backend.Resolve(checkCtx, in.Identity, start)
	cancel()
	if err != nil || !access.valid() || access.Identity != in.Identity {
		if in.Kind == "callback" {
			if err := w.journal.answer(ctx, in, tr(lang, "دسترسی این منو تغییر کرده؛ /start را بزنید.", "Access changed. Send /start to open a new menu."), false); err != nil {
				return err
			}
		}
		if in.Kind == "pre_checkout" {
			if err := w.journal.answer(ctx, in, tr(lang, "این سفارش قابل پرداخت نیست. دوباره منوی خرید را باز کنید.", "This order cannot be paid. Open the purchase menu again."), true); err != nil {
				return err
			}
		}
		return w.journal.rejectInput(ctx, in, "access_denied", in.Kind == "payment" || in.Kind == "refund")
	}
	action := Action{}
	event := stableEvent(in.Identity.BotID, fmt.Sprintf("update:%d", in.UpdateID))
	if in.Kind == "message" && !strings.HasPrefix(in.Text, "/") {
		stored, found, err := w.journal.inputAction(ctx, access)
		if err != nil {
			return err
		}
		if found {
			action = stored
		}
	}
	if in.Kind == "callback" {
		action, event, err = w.journal.callback(ctx, in, access)
		if err != nil {
			if err := w.journal.answer(ctx, in, tr(lang, "این منو منقضی شده؛ /start را بزنید.", "This menu expired. Send /start for a new menu."), false); err != nil {
				return err
			}
			return w.journal.rejectInput(ctx, in, "callback_rejected", false)
		}
		if err := w.journal.answer(ctx, in, "", false); err != nil {
			return err
		}
		if action.Name == "language" {
			if action.Value != "fa" && action.Value != "en" {
				return w.journal.rejectInput(ctx, in, "language_rejected", false)
			}
			if err := w.journal.setPreference(ctx, in.Identity, action.Value); err != nil {
				return err
			}
			lang = action.Value
			action = Action{Name: "home"}
		}
	}
	if in.Kind == "pre_checkout" && time.Now().Unix() >= in.ReceivedUnix+7 {
		if err := w.journal.answer(ctx, in, tr(lang, "زمان تأیید پرداخت گذشته است؛ دوباره تلاش کنید.", "Checkout confirmation expired. Please try again."), true); err != nil {
			return err
		}
		return w.journal.rejectInput(ctx, in, "checkout_expired", false)
	}
	handleCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	result, handleErr := w.backend.Handle(handleCtx, Request{Incoming: in, Access: access, Action: action, EventID: event, Language: lang, financialCaptured: in.Kind == "payment" || in.Kind == "refund"})
	stop()
	if handleErr != nil {
		if in.Kind == "payment" || in.Kind == "refund" {
			return w.journal.rejectInput(ctx, in, "payment_requires_review", true)
		}
		if in.Kind == "pre_checkout" {
			result = Result{Checkout: &CheckoutDecision{ErrorMessage: tr(lang, "سفارش تأیید نشد؛ پرداختی انجام ندهید و دوباره تلاش کنید.", "The order could not be verified. Please try again.")}}
		} else {
			result = Result{Screen: &Screen{Text: tr(lang, "این درخواست اکنون قابل انجام نیست. از منوی اصلی دوباره بررسی کنید.", "This request is not available now. Check again from the main menu."), Buttons: [][]Button{{actionButton(tr(lang, "🏠 منوی اصلی", "🏠 Main menu"), "primary", "home", "", "")}}}}
		}
	}
	if result.Access != nil {
		refreshed, err := w.backend.Resolve(ctx, in.Identity, false)
		if err != nil || result.Access.Identity != in.Identity || result.Access.digest() != refreshed.digest() {
			return w.journal.rejectInput(ctx, in, "changed_access_unverified", false)
		}
		access = refreshed
	}
	return w.journal.complete(ctx, in, access, result)
}

func (w *Worker) status(id int64, state string, poll int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.statuses[id]
	s.BotID = id
	s.State = state
	if poll > 0 {
		s.LastPollUnix = poll
	}
	w.statuses[id] = s
}
func (w *Worker) problem(id int64, reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.statuses[id]
	s.BotID = id
	s.LastError = reason
	w.statuses[id] = s
}

func (w *Worker) refreshBacklog(ctx context.Context, id int64) error {
	counts, err := w.journal.backlog(ctx, id)
	if err != nil {
		w.mu.Lock()
		s := w.statuses[id]
		s.BotID = id
		s.QueueStatusAvailable = false
		w.statuses[id] = s
		w.mu.Unlock()
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.statuses[id]
	s.BotID = id
	s.InboxPending = counts.InboxPending
	s.FinancialRetry = counts.FinancialRetry
	s.ReviewPending = counts.ReviewPending
	s.OutboxPending = counts.OutboxPending
	s.OutboxUnknown = counts.OutboxUnknown
	s.DeliveryReview = counts.DeliveryReview
	s.QueueStatusAvailable = true
	w.statuses[id] = s
	return nil
}
func (w *Worker) Status() []BotStatus {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]BotStatus, 0, len(w.apis))
	for id := range w.apis {
		s := w.statuses[id]
		if s.BotID == 0 {
			s = BotStatus{BotID: id, State: "configured"}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BotID < out[j].BotID })
	return out
}
func (w *Worker) Statuses() []BotStatus { return w.Status() }
func (w *Worker) Wait(ctx context.Context) error {
	if w == nil || ctx == nil {
		return ErrState
	}
	w.mu.Lock()
	started, closed, done := w.started, w.closed, w.done
	w.mu.Unlock()
	if !started && !closed {
		return ErrState
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
func (w *Worker) Close() error {
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
		for _, a := range w.apis {
			a.close()
		}
		w.closeErr = w.journal.Close()
	})
	return w.closeErr
}
func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
