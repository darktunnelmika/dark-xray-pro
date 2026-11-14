// Package tgbot is the HUB's Telegram bot: a storefront and support desk for
// customers and an operations console for the owner. It talks to the Bot API
// with plain HTTPS long polling and needs no third-party library.
package tgbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

// API is the subset of the Bot API the bot uses. Tests replace it.
type API interface {
	Updates(ctx context.Context, offset int64) ([]Update, error)
	Send(ctx context.Context, chat int64, text string, kb *Keyboard, replyTo int64) (int64, error)
	SendPhoto(ctx context.Context, chat int64, png []byte, caption string) error
	Answer(ctx context.Context, callbackID, text string) error
}

type Update struct {
	ID       int64          `json:"update_id"`
	Message  *Message       `json:"message"`
	Callback *CallbackQuery `json:"callback_query"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Message struct {
	ID      int64    `json:"message_id"`
	From    *User    `json:"from"`
	Chat    Chat     `json:"chat"`
	Text    string   `json:"text"`
	ReplyTo *Message `json:"reply_to_message"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// Keyboard is an inline keyboard.
type Keyboard struct {
	Rows [][]Button `json:"inline_keyboard"`
}

type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data,omitempty"`
	URL  string `json:"url,omitempty"`
}

// ErrBlocked means the user blocked the bot (403).
var ErrBlocked = errors.New("telegram: chat unavailable")

// HTTPAPI is the real Bot API client.
type HTTPAPI struct {
	Token  string
	Base   string // default https://api.telegram.org
	Client *http.Client
}

func (a *HTTPAPI) url(method string) string {
	base := a.Base
	if base == "" {
		base = "https://api.telegram.org"
	}
	return base + "/bot" + a.Token + "/" + method
}

func (a *HTTPAPI) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: 70 * time.Second}
}

type apiResp struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

func (a *HTTPAPI) call(ctx context.Context, method string, body io.Reader, ctype string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(method), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	res, err := a.client().Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: request failed", method) // never echo the URL: it holds the token
	}
	defer res.Body.Close()
	var r apiResp
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&r); err != nil {
		return fmt.Errorf("telegram %s: %s", method, res.Status)
	}
	if !r.OK {
		if r.ErrorCode == 403 || r.ErrorCode == 400 && bytes.Contains([]byte(r.Description), []byte("chat not found")) {
			return ErrBlocked
		}
		return fmt.Errorf("telegram %s: %d %s", method, r.ErrorCode, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func (a *HTTPAPI) callJSON(ctx context.Context, method string, payload, out any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return a.call(ctx, method, bytes.NewReader(b), "application/json", out)
}

func (a *HTTPAPI) Updates(ctx context.Context, offset int64) ([]Update, error) {
	var ups []Update
	err := a.callJSON(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 50, "allowed_updates": []string{"message", "callback_query"}}, &ups)
	return ups, err
}

func (a *HTTPAPI) Send(ctx context.Context, chat int64, text string, kb *Keyboard, replyTo int64) (int64, error) {
	p := map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	if kb != nil {
		p["reply_markup"] = kb
	}
	if replyTo != 0 {
		p["reply_parameters"] = map[string]any{"message_id": replyTo, "allow_sending_without_reply": true}
	}
	var m Message
	err := a.callJSON(ctx, "sendMessage", p, &m)
	return m.ID, err
}

func (a *HTTPAPI) SendPhoto(ctx context.Context, chat int64, png []byte, caption string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", strconv.FormatInt(chat, 10))
	_ = w.WriteField("caption", caption)
	_ = w.WriteField("parse_mode", "HTML")
	fw, err := w.CreateFormFile("photo", "qr.png")
	if err != nil {
		return err
	}
	if _, err := fw.Write(png); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return a.call(ctx, "sendPhoto", &buf, w.FormDataContentType(), nil)
}

func (a *HTTPAPI) Answer(ctx context.Context, id, text string) error {
	return a.callJSON(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}
