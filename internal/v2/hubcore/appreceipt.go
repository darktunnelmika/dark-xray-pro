package hubcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Receipt photos uploaded in the Mini App. Telegram file ids belong to one
// bot, so the bot that handles the payment (the seller's, or the main bot)
// sends the photo to the buyer's own chat, which gives it its file id, and
// then takes it exactly as if the buyer had sent it there: the duplicate
// check, the approvers' message with its buttons, everything stays the bot's.

// AppReceiptFunc hands a receipt to a seller's bot. kind is order | topup |
// panel (rep_orders); id is that payment's id.
type AppReceiptFunc func(ctx context.Context, seller string, chat int64, name, kind string, id int64, img []byte) error

var appReceipt AppReceiptFunc

func SetAppReceipt(f AppReceiptFunc) { appReceipt = f }

// ErrNoBotChat: the bot cannot write to this chat (never started).
var ErrNoBotChat = errors.New("the bot cannot write to this chat")

// receiptTarget checks that the payment is the chat's and still waits for a
// receipt, and returns the seller whose bot takes it.
func (s *Store) receiptTarget(ctx context.Context, chat int64, kind string, id int64) (string, error) {
	switch kind {
	case "order":
		o, err := s.OrderByID(ctx, id)
		if err != nil || o.Buyer != TgBuyer(chat) || o.Status != "pending" || o.Method != "manual" || o.HasReceipt {
			return "", ErrNotFound
		}
		return o.Seller, nil
	case "topup":
		t, err := s.TopupByID(ctx, id)
		if err != nil || t.Chat != chat || t.Status != "awaiting" || t.HasReceipt {
			return "", ErrNotFound
		}
		return t.Seller, nil
	case "panel":
		o, err := s.RepOrderByID(ctx, id)
		if err != nil || o.Chat != chat || o.Status != "await_receipt" {
			return "", ErrNotFound
		}
		return o.Seller, nil
	}
	return "", ErrInvalid
}

// decodeImage takes a data: URL or plain base64 of a JPEG, PNG or WebP.
func decodeImage(v string) ([]byte, bool) {
	if i := strings.Index(v, ","); strings.HasPrefix(v, "data:") && i > 0 {
		v = v[i+1:]
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(b) < 100 {
		return nil, false
	}
	switch ct := http.DetectContentType(b); ct {
	case "image/jpeg", "image/png", "image/webp":
		return b, true
	}
	return nil, false
}

func registerAppReceipt(mux *http.ServeMux, s *Store) {
	mux.HandleFunc("POST /app/api/receipt", func(w http.ResponseWriter, r *http.Request) {
		u, _, err := s.verifyMiniUser(r.Context(), r.Header.Get("X-Init-Data"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
		var in struct {
			Kind  string `json:"kind"`
			ID    int64  `json:"id"`
			Image string `json:"image"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "عکس خیلی بزرگ است یا خراب است.", http.StatusBadRequest)
			return
		}
		img, good := decodeImage(in.Image)
		if !good || len(img) > 8<<20 {
			http.Error(w, "فقط عکس (JPG یا PNG) تا ۸ مگابایت.", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		seller, err := s.receiptTarget(ctx, u.ID, in.Kind, in.ID)
		if err != nil {
			http.Error(w, "این پرداخت پیدا نشد یا رسیدش قبلاً رسیده.", http.StatusNotFound)
			return
		}
		if appReceipt == nil {
			http.Error(w, "ربات خاموش است؛ رسید را در ربات بفرستید.", http.StatusServiceUnavailable)
			return
		}
		if err := appReceipt(ctx, seller, u.ID, u.FirstName, in.Kind, in.ID, img); err != nil {
			msg := "رسید فرستاده نشد؛ کمی بعد دوباره امتحان کنید یا عکس را در ربات بفرستید."
			if errors.Is(err, ErrNoBotChat) {
				if b := s.BotUsernameFor(ctx, seller); b != "" {
					msg = "اول یک بار ربات @" + b + " را باز کنید و /start بزنید، بعد دوباره رسید را بفرستید."
				}
			}
			http.Error(w, msg, http.StatusBadGateway)
			return
		}
		reply(w, map[string]bool{"ok": true}, nil)
	})
}
