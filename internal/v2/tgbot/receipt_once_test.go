package tgbot

import (
	"testing"
	"time"
)

func TestReceiptOnce(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := &Bot{Now: func() time.Time { return now }}
	if !b.receiptOnce("o:1") || b.receiptOnce("o:1") {
		t.Fatal("a second photo of the same order within minutes must not reach the admins")
	}
	if !b.receiptOnce("o:2") {
		t.Fatal("another order is its own")
	}
	now = now.Add(11 * time.Minute)
	if !b.receiptOnce("o:1") {
		t.Fatal("a new receipt much later goes through")
	}
}
