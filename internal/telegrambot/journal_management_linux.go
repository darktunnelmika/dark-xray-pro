//go:build linux

package telegrambot

import "context"

// ensureManagedBot adds only a cursor and extends the historical binding. It
// never deletes old bots or reinitializes their financial/delivery history.
func (j *journal) ensureManagedBot(ctx context.Context, botID int64) error {
	if botID <= 0 || botID > maxManagedInteger {
		return ErrConfig
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	known, err := knownBotIDs(ctx, j.db)
	if err != nil {
		return err
	}
	for _, id := range known {
		if id == botID {
			return nil
		}
	}
	if len(known) >= 256 {
		return ErrCapacity
	}
	known = append(known, botID)
	binding := bindingDigest(known)
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrJournal
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO bot_cursors(bot_id,cursor) VALUES(?,0)", botID); err != nil {
		return ErrJournal
	}
	if _, err = tx.ExecContext(ctx, "UPDATE bot_meta SET binding=? WHERE id=1", binding); err != nil {
		return ErrJournal
	}
	if tx.Commit() != nil {
		return ErrJournal
	}
	j.binding = binding
	return nil
}

// recoverManagedBot is called only after its old loop (including poll and
// dispatch completion) has joined. Uncertain sends stay unknown, never queued
// for blind resend; durable receipts can resume their existing capture path.
func (j *journal) recoverManagedBot(ctx context.Context, botID int64) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrJournal
	}
	defer tx.Rollback()
	for _, statement := range []string{
		"UPDATE bot_outbox SET state='unknown',reason='restart_during_dispatch' WHERE bot_id=? AND state='dispatching'",
		"UPDATE bot_delivery_proofs SET state='review' WHERE bot_id=? AND state='waiting' AND outbox_id IN (SELECT id FROM bot_outbox WHERE state='unknown')",
		"UPDATE bot_inbox SET state='pending' WHERE bot_id=? AND state='processing'",
	} {
		if _, err = tx.ExecContext(ctx, statement, botID); err != nil {
			return ErrJournal
		}
	}
	if tx.Commit() != nil {
		return ErrJournal
	}
	return nil
}
