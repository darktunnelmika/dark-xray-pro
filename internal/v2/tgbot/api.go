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
	"strings"
	"time"
)

// API is the subset of the Bot API the bot uses. Tests replace it.
type API interface {
	Updates(ctx context.Context, offset int64) ([]Update, error)
	Send(ctx context.Context, chat int64, text string, kb *Keyboard, replyTo int64) (int64, error)
	SendPhoto(ctx context.Context, chat int64, png []byte, caption string) error
	Answer(ctx context.Context, callbackID, text string) error
	Me(ctx context.Context) (string, error)
	SetCommands(ctx context.Context, chat int64, cmds [][2]string) error
	SendDocument(ctx context.Context, chat int64, name string, data []byte, caption string) error
	SendPhotoID(ctx context.Context, chat int64, fileID, caption string, kb *Keyboard) error
}

type Update struct {
	ID          int64             `json:"update_id"`
	Message     *Message          `json:"message"`
	Callback    *CallbackQuery    `json:"callback_query"`
	PreCheckout *PreCheckoutQuery `json:"pre_checkout_query"`
}

// PreCheckoutQuery asks the bot to confirm a payment before Telegram charges.
type PreCheckoutQuery struct {
	ID       string `json:"id"`
	From     User   `json:"from"`
	Currency string `json:"currency"`
	Total    int64  `json:"total_amount"`
	Payload  string `json:"invoice_payload"`
}

// SuccessfulPayment arrives in a message once Telegram charged the customer.
type SuccessfulPayment struct {
	Currency string `json:"currency"`
	Total    int64  `json:"total_amount"`
	Payload  string `json:"invoice_payload"`
	ChargeID string `json:"telegram_payment_charge_id"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	IsForum bool   `json:"is_forum"`
}

type Message struct {
	ID              int64              `json:"message_id"`
	From            *User              `json:"from"`
	Chat            Chat               `json:"chat"`
	Text            string             `json:"text"`
	Caption         string             `json:"caption"`
	Photo           []PhotoSize        `json:"photo"`
	Contact         *Contact           `json:"contact"`
	ReplyTo         *Message           `json:"reply_to_message"`
	Thread          int64              `json:"message_thread_id"`
	Paid            *SuccessfulPayment `json:"successful_payment"`
	Entities        []MessageEntity    `json:"entities"`
	CaptionEntities []MessageEntity    `json:"caption_entities"`
}

// PhotoSize is one size of a received photo.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	UniqueID string `json:"file_unique_id"`
}

// Contact is a shared phone number (request_contact button).
type Contact struct {
	PhoneNumber string `json:"phone_number"`
	UserID      int64  `json:"user_id"`
}

// MessageEntity marks a span of text (offsets in UTF-16 units).
type MessageEntity struct {
	Type          string `json:"type"`
	Offset        int    `json:"offset"`
	Length        int    `json:"length"`
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
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
	Text   string  `json:"text"`
	Data   string  `json:"callback_data,omitempty"`
	URL    string  `json:"url,omitempty"`
	WebApp *WebApp `json:"web_app,omitempty"` // opens a Mini App
	Style  string  `json:"style,omitempty"`   // danger | success | primary (set automatically by meaning)
	Icon   string  `json:"icon_custom_emoji_id,omitempty"`
}

// WebApp is a Mini App address for a button.
type WebApp struct {
	URL string `json:"url"`
}

// SendInvoice asks for a Telegram Stars payment (currency XTR, no provider).
func (a *HTTPAPI) SendInvoice(ctx context.Context, chat int64, title, desc, payload string, stars int64) error {
	return a.callJSON(ctx, "sendInvoice", map[string]any{"chat_id": chat, "title": title, "description": desc, "payload": payload,
		"currency": "XTR", "prices": []map[string]any{{"label": title, "amount": stars}}}, nil)
}

// InvoiceLink makes a Stars invoice link (the Mini App opens it in place).
func (a *HTTPAPI) InvoiceLink(ctx context.Context, title, desc, payload string, stars int64) (string, error) {
	var link string
	err := a.callJSON(ctx, "createInvoiceLink", map[string]any{"title": title, "description": desc, "payload": payload,
		"currency": "XTR", "prices": []map[string]any{{"label": title, "amount": stars}}}, &link)
	return link, err
}

// AnswerPreCheckout confirms (ok) or refuses a payment about to happen.
func (a *HTTPAPI) AnswerPreCheckout(ctx context.Context, id string, ok bool, msg string) error {
	p := map[string]any{"pre_checkout_query_id": id, "ok": ok}
	if !ok {
		p["error_message"] = msg
	}
	return a.callJSON(ctx, "answerPreCheckoutQuery", p, nil)
}

// RefundStars gives a Stars payment back to the customer.
func (a *HTTPAPI) RefundStars(ctx context.Context, user int64, chargeID string) error {
	return a.callJSON(ctx, "refundStarPayment", map[string]any{"user_id": user, "telegram_payment_charge_id": chargeID}, nil)
}

// SetMenuButton points the chat's menu button at a Mini App ("" restores
// the command list).
func (a *HTTPAPI) SetMenuButton(ctx context.Context, url, text string) error {
	mb := map[string]any{"type": "commands"}
	if url != "" {
		mb = map[string]any{"type": "web_app", "text": text, "web_app": map[string]string{"url": url}}
	}
	return a.callJSON(ctx, "setChatMenuButton", map[string]any{"menu_button": mb}, nil)
}

// ErrBlocked means the user blocked the bot (403).
var ErrBlocked = errors.New("telegram: chat unavailable")

// HTTPAPI is the real Bot API client.
type HTTPAPI struct {
	Token  string
	Base   string // default https://api.telegram.org
	Client *http.Client
	// Emoji is the custom emoji theme (plain emoji → custom emoji id).
	Emoji func() map[string]string
	es    emojiState
}

// sendStyled sends a text-or-caption method with the look applied: animated
// emoji, coloured buttons and an effect from ctx; when Telegram refuses the
// custom emoji it is sent again plain.
func (a *HTTPAPI) sendStyled(ctx context.Context, method, textKey string, p map[string]any, kb *Keyboard, out any) error {
	theme := a.emojiTheme()
	raw, _ := p[textKey].(string)
	if len(theme) > 0 {
		p[textKey] = decorate(raw, theme)
	}
	if kb != nil {
		p["reply_markup"] = styleKeyboard(kb, theme)
	}
	if fx := effectOf(ctx); fx != "" && method != "editMessageText" {
		p["message_effect_id"] = fx
	}
	err := a.callJSON(ctx, method, p, out)
	if err != nil && len(theme) > 0 && emojiError(err) {
		a.emojiRefused()
		p[textKey] = raw
		if kb != nil {
			p["reply_markup"] = plainKeyboard(styleKeyboard(kb, nil))
		}
		err = a.callJSON(ctx, method, p, out)
	}
	if err != nil && p["message_effect_id"] != nil && strings.Contains(strings.ToUpper(err.Error()), "EFFECT") {
		delete(p, "message_effect_id")
		err = a.callJSON(ctx, method, p, out)
	}
	return err
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
	err := a.callJSON(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 50, "allowed_updates": []string{"message", "callback_query", "pre_checkout_query"}}, &ups)
	return ups, err
}

func (a *HTTPAPI) Send(ctx context.Context, chat int64, text string, kb *Keyboard, replyTo int64) (int64, error) {
	p := map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	if replyTo != 0 {
		p["reply_parameters"] = map[string]any{"message_id": replyTo, "allow_sending_without_reply": true}
	}
	var m Message
	err := a.sendStyled(ctx, "sendMessage", "text", p, kb, &m)
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

// ChatMember reports a user's status in a channel ("member", "left", ...).
// The bot must be an administrator of the channel to ask.
func (a *HTTPAPI) ChatMember(ctx context.Context, channel string, user int64) (string, error) {
	var out struct {
		Status   string `json:"status"`
		IsMember bool   `json:"is_member"`
	}
	if err := a.callJSON(ctx, "getChatMember", map[string]any{"chat_id": channel, "user_id": user}, &out); err != nil {
		return "", err
	}
	if out.Status == "restricted" && out.IsMember {
		return "member", nil
	}
	return out.Status, nil
}

// UploadPhoto sends an image with a caption and buttons and returns the file
// id Telegram gave it, so later sends can reuse it without uploading again.
func (a *HTTPAPI) UploadPhoto(ctx context.Context, chat int64, name string, img []byte, caption string, kb *Keyboard) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", strconv.FormatInt(chat, 10))
	_ = w.WriteField("caption", caption)
	_ = w.WriteField("parse_mode", "HTML")
	if kb != nil {
		if m, err := json.Marshal(styleKeyboard(kb, nil)); err == nil {
			_ = w.WriteField("reply_markup", string(m))
		}
	}
	fw, err := w.CreateFormFile("photo", name)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(img); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	var out struct {
		Photo []struct {
			FileID string `json:"file_id"`
		} `json:"photo"`
	}
	if err := a.call(ctx, "sendPhoto", &buf, w.FormDataContentType(), &out); err != nil {
		return "", err
	}
	if len(out.Photo) == 0 {
		return "", nil
	}
	return out.Photo[len(out.Photo)-1].FileID, nil
}

// EditText redraws a message in place (one screen per chat).
func (a *HTTPAPI) EditText(ctx context.Context, chat, msgID int64, text string, kb *Keyboard) error {
	p := map[string]any{"chat_id": chat, "message_id": msgID, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	err := a.sendStyled(ctx, "editMessageText", "text", p, kb, nil)
	if err != nil && bytes.Contains([]byte(err.Error()), []byte("message is not modified")) {
		return errNotModified
	}
	return err
}

func (a *HTTPAPI) Answer(ctx context.Context, id, text string) error {
	return a.callJSON(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

func (a *HTTPAPI) Me(ctx context.Context) (string, error) {
	var u User
	err := a.callJSON(ctx, "getMe", map[string]any{}, &u)
	return u.Username, err
}

// SetCommands sets the "/" menu; chat 0 is the default for everyone.
func (a *HTTPAPI) SetCommands(ctx context.Context, chat int64, cmds [][2]string) error {
	list := make([]map[string]string, 0, len(cmds))
	for _, c := range cmds {
		list = append(list, map[string]string{"command": c[0], "description": c[1]})
	}
	p := map[string]any{"commands": list}
	if chat != 0 {
		p["scope"] = map[string]any{"type": "chat", "chat_id": chat}
	}
	return a.callJSON(ctx, "setMyCommands", p, nil)
}

func (a *HTTPAPI) SendDocument(ctx context.Context, chat int64, name string, data []byte, caption string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", strconv.FormatInt(chat, 10))
	_ = w.WriteField("caption", caption)
	_ = w.WriteField("parse_mode", "HTML")
	fw, err := w.CreateFormFile("document", name)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return a.call(ctx, "sendDocument", &buf, w.FormDataContentType(), nil)
}

// SendPhotoID re-sends a photo Telegram already has (by file_id).
func (a *HTTPAPI) SendPhotoID(ctx context.Context, chat int64, fileID, caption string, kb *Keyboard) error {
	p := map[string]any{"chat_id": chat, "photo": fileID, "caption": caption, "parse_mode": "HTML"}
	return a.sendStyled(ctx, "sendPhoto", "caption", p, kb, nil)
}

// CreateForumTopic opens a topic in a forum group and returns its thread id.
func (a *HTTPAPI) CreateForumTopic(ctx context.Context, chat int64, name string, color int) (int64, error) {
	var out struct {
		Thread int64 `json:"message_thread_id"`
	}
	p := map[string]any{"chat_id": chat, "name": name}
	if color != 0 {
		p["icon_color"] = color
	}
	err := a.callJSON(ctx, "createForumTopic", p, &out)
	return out.Thread, err
}

// SendThread sends a message into one topic of a forum group.
func (a *HTTPAPI) SendThread(ctx context.Context, chat, thread int64, text string, kb *Keyboard) (int64, error) {
	p := map[string]any{"chat_id": chat, "message_thread_id": thread, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	var m Message
	err := a.sendStyled(ctx, "sendMessage", "text", p, kb, &m)
	return m.ID, err
}

// SendPhotoIDThread re-sends a known photo into a topic.
func (a *HTTPAPI) SendPhotoIDThread(ctx context.Context, chat, thread int64, fileID, caption string, kb *Keyboard) error {
	p := map[string]any{"chat_id": chat, "message_thread_id": thread, "photo": fileID, "caption": caption, "parse_mode": "HTML"}
	return a.sendStyled(ctx, "sendPhoto", "caption", p, kb, nil)
}

// SendDocumentThread uploads a file into a topic.
func (a *HTTPAPI) SendDocumentThread(ctx context.Context, chat, thread int64, name string, data []byte, caption string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", strconv.FormatInt(chat, 10))
	_ = w.WriteField("message_thread_id", strconv.FormatInt(thread, 10))
	_ = w.WriteField("caption", caption)
	_ = w.WriteField("parse_mode", "HTML")
	fw, err := w.CreateFormFile("document", name)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return a.call(ctx, "sendDocument", &buf, w.FormDataContentType(), nil)
}
