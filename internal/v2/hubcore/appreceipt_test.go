package hubcore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"testing"
)

func TestAppReceiptUpload(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	const mainTok = "111:main-bot-token-main-bot-token-000"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	var got struct {
		seller, kind string
		chat, id     int64
		n            int
	}
	SetAppReceipt(func(_ context.Context, seller string, chat int64, _, kind string, id int64, img []byte) error {
		got.seller, got.kind, got.chat, got.id, got.n = seller, kind, chat, id, len(img)
		return nil
	})
	defer SetAppReceipt(nil)
	var pic bytes.Buffer
	m := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range m.Pix {
		m.Pix[i] = byte(i * 7)
	}
	_ = png.Encode(&pic, m)
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pic.Bytes())
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890"}); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateTopup(ctx, "", 700, 50000)
	if err != nil {
		t.Fatal(err)
	}
	h := SubHandler(s)
	post := func(chat string, body any) int {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "https://hub.example/app/api/receipt", bytes.NewReader(b))
		req.Header.Set("X-Init-Data", signInitData(mainTok, `{"id":`+chat+`,"first_name":"A"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("701", map[string]any{"kind": "topup", "id": id, "image": data}); code != 404 {
		t.Fatalf("someone else's top-up %d", code)
	}
	if code := post("700", map[string]any{"kind": "topup", "id": id, "image": "aGVsbG8gd29ybGQ="}); code != 400 {
		t.Fatalf("not an image %d", code)
	}
	if code := post("700", map[string]any{"kind": "topup", "id": id, "image": data}); code != 200 || got.kind != "topup" || got.id != id || got.chat != 700 || got.seller != "" || got.n != pic.Len() {
		t.Fatalf("upload %d %+v", code, got)
	}
}
