//go:build linux

package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func enqueueQR(t *testing.T, j *journal, updateID int64) (delivery, Document, DeliveryProof) {
	t.Helper()
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: updateID, Identity: a.Identity, Kind: "message", Text: "deliver", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	proof := qrProof(syntheticQRURI + "\n")
	doc, err := prepareQRDelivery(syntheticQRURI+"\n", proof, "Private configuration QR")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.complete(context.Background(), in, a, Result{Receipt: true, Delivery: &proof, Document: doc}); err != nil {
		t.Fatal(err)
	}
	return nextQueued(t, j, 100), *doc, proof
}

func qrBackupConfig(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{{BotID: 100, Token: syntheticToken}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestQRDurableMultipartReceiptKeepsOriginalNativeProof(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100)
	d, doc, proof := enqueueQR(t, j, 81)
	if len(d.Payload) > 64<<10 || bytes.Contains(d.Payload, []byte(syntheticQRURI)) {
		t.Fatal("QR queue leaked plain configuration or exceeded the payload limit")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(ctx, path, qrBackupConfig(t)); err != nil {
		t.Fatal("pending QR cannot be restored", err)
	}
	reopened, err := openJournal(path, []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored := nextQueued(t, reopened, 100)
	if !bytes.Equal(restored.Payload, d.Payload) || restored.Proof == nil || *restored.Proof != proof {
		t.Fatal("restart changed binary PNG or native proof")
	}
	var calls int
	a, err := newAPI(100, syntheticToken, fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" || r.URL.Path != "/bot"+syntheticToken+"/sendDocument" || r.URL.RawQuery != "" {
			t.Fatal("QR did not use the fixed-origin document endpoint")
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatal("QR was not uploaded as multipart", err)
		}
		fields := map[string]string{}
		var uploaded []byte
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			value, err := io.ReadAll(io.LimitReader(part, maxDocumentBytes+1))
			if err != nil {
				t.Fatal(err)
			}
			if part.FormName() == "document" {
				if uploaded != nil || part.FileName() != "dark-service-qr.png" || part.Header.Get("Content-Type") != "image/png" {
					t.Fatal("QR file metadata changed")
				}
				uploaded = value
			} else {
				if _, duplicate := fields[part.FormName()]; duplicate {
					t.Fatal("duplicate multipart field")
				}
				fields[part.FormName()] = string(value)
			}
		}
		if !bytes.Equal(uploaded, doc.Content) || len(fields) != 4 || fields["chat_id"] != "2001" || fields["caption"] != doc.Caption || fields["parse_mode"] != "HTML" || fields["disable_content_type_detection"] != "true" {
			t.Fatal("lossless PNG upload, recipient, or safe caption changed")
		}
		// The only uploaded credential representation is the exact image. The
		// original SHA/terms are local acknowledgement metadata, not captions.
		for _, value := range fields {
			if strings.Contains(value, "vless://") || value == proof.SHA256 || value == proof.TermsSHA256 || value == doc.ContentSHA256 {
				t.Fatal("private proof or plain native configuration leaked to fields")
			}
		}
		return fakeResponse(200, `{"ok":true,"result":{"message_id":801,"from":{"id":100,"is_bot":true},"chat":{"id":2001,"type":"private"},"document":{"file_id":"test-document","file_unique_id":"test-unique","file_name":"dark-service-qr.png","mime_type":"image/png"}}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	a.verified = true
	if err := reopened.claim(ctx, restored); err != nil {
		t.Fatal(err)
	}
	out, err := a.dispatch(ctx, restored)
	if err != nil || out.State != "sent" || out.MessageID != 801 || calls != 1 {
		t.Fatal("PNG upload did not produce an exact confirmed receipt", out, err)
	}
	if err := reopened.finish(ctx, restored, out); err != nil {
		t.Fatal(err)
	}
	ack, found, err := reopened.nextAck(ctx, 100)
	if err != nil || !found || ack.Proof != proof || ack.Proof.SHA256 == doc.ContentSHA256 || ack.MessageID != 801 || ack.Identity != (Identity{100, 2001, 2001}) {
		t.Fatal("QR receipt failed to preserve the original config and terms proof", err)
	}
	var retained []byte
	if err := reopened.db.QueryRow("SELECT payload_json FROM bot_outbox WHERE id=?", restored.ID).Scan(&retained); err != nil || retained != nil {
		t.Fatal("confirmed PNG credential retained in outbox", err)
	}
	if err := reopened.finishAck(ctx, ack, true); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(ctx, path, qrBackupConfig(t)); err != nil {
		t.Fatal("confirmed QR journal cannot be restored", err)
	}
}

func TestQRUnknownSendNeverAcknowledgesOrResendsAfterRestart(t *testing.T) {
	for _, mode := range []string{"network uncertainty", "server error"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			j, path := testJournal(t, 100)
			d, doc, _ := enqueueQR(t, j, 82)
			a, err := newAPI(100, syntheticToken, fakeRoundTripper(func(*http.Request) (*http.Response, error) {
				if mode == "network uncertainty" {
					return nil, io.ErrUnexpectedEOF
				}
				return fakeResponse(502, `{"ok":false,"error_code":502}`), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer a.close()
			a.verified = true
			if err := j.claim(ctx, d); err != nil {
				t.Fatal(err)
			}
			out, err := a.dispatch(ctx, d)
			if err == nil || out.State != "unknown" || out.MessageID != 0 || out.RetryAfter != 0 {
				t.Fatal("ambiguous image upload became a success or retry", out, err)
			}
			if err := j.finish(ctx, d, out); err != nil {
				t.Fatal(err)
			}
			if err := j.Close(); err != nil {
				t.Fatal(err)
			}
			if err := ValidateOfflineBackup(ctx, path, qrBackupConfig(t)); err != nil {
				t.Fatal("unknown QR evidence failed backup validation", err)
			}
			reopened, err := openJournal(path, []int64{100})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if _, found, err := reopened.nextDelivery(ctx, 100); err != nil || found {
				t.Fatal("ambiguous QR delivery requeued", err)
			}
			if _, found, err := reopened.nextAck(ctx, 100); err != nil || found {
				t.Fatal("ambiguous QR delivery released native acknowledgement", err)
			}
			var raw []byte
			var state string
			if err := reopened.db.QueryRow("SELECT payload_json,state FROM bot_outbox WHERE id=?", d.ID).Scan(&raw, &state); err != nil || state != "unknown" {
				t.Fatal("ambiguous QR evidence lost", err)
			}
			var retained wireDocument
			if decodeJSON(raw, &retained) != nil || !bytes.Equal(retained.Content, doc.Content) {
				t.Fatal("unknown QR evidence was corrupted")
			}
		})
	}
}

func TestQRInvalidBindingsRejectedAtEnqueueDispatchAndOfflineRestore(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"missing proof", "different source proof", "damaged image", "different valid QR"} {
		t.Run(mode, func(t *testing.T) {
			j, path := testJournal(t, 100)
			d, doc, proof := enqueueQR(t, j, 83)
			badDoc, badProof := doc, proof
			var pointer = &badProof
			switch mode {
			case "missing proof":
				pointer = nil
				if _, err := j.db.Exec("DELETE FROM bot_delivery_proofs WHERE outbox_id=?", d.ID); err != nil {
					t.Fatal(err)
				}
			case "different source proof":
				badProof.SHA256 = strings.Repeat("b", 64)
				if _, err := j.db.Exec("UPDATE bot_delivery_proofs SET sha256=? WHERE outbox_id=?", badProof.SHA256, d.ID); err != nil {
					t.Fatal(err)
				}
			case "damaged image":
				badDoc.Content = append([]byte(nil), doc.Content...)
				badDoc.Content[len(badDoc.Content)-1] ^= 1
			case "different valid QR":
				otherURI := strings.Replace(syntheticQRURI, "11111111-2222-4333-8444-555555555555", "99999999-2222-4333-8444-555555555555", 1)
				other, err := prepareQRDelivery(otherURI, qrProof(otherURI), "Other private QR")
				if err != nil || !validDocument(*other) {
					t.Fatal("invalid second QR fixture", err)
				}
				// Preserve the original source, source hash, native proof and
				// terms. Replace a complete valid image and its matching hash.
				badDoc.Content = other.Content
				badDoc.ContentSHA256 = other.ContentSHA256
			}
			if mode == "damaged image" || mode == "different valid QR" {
				var wire wireDocument
				if decodeJSON(d.Payload, &wire) != nil {
					t.Fatal("bad fixture")
				}
				wire.Content = badDoc.Content
				wire.ContentSHA256 = badDoc.ContentSHA256
				d.Payload, _ = json.Marshal(wire)
				if _, err := j.db.Exec("UPDATE bot_outbox SET payload_json=? WHERE id=?", d.Payload, d.ID); err != nil {
					t.Fatal(err)
				}
			}
			d.Proof = pointer
			a, err := newAPI(100, syntheticToken, fakeRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid QR reached the Bot API")
				return nil, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer a.close()
			a.verified = true
			out, err := a.dispatch(ctx, d)
			if !errors.Is(err, ErrProtocol) || out.State != "failed" {
				t.Fatal("transport accepted unbound QR", out, err)
			}
			if _, found, err := j.nextDelivery(ctx, 100); !errors.Is(err, ErrJournal) || found {
				t.Fatal("runtime queue accepted unbound QR", err)
			}
			access := testAccess(100, 2001)
			in := Incoming{UpdateID: 84, Identity: access.Identity, Kind: "message", Text: "deliver", ReceivedUnix: time.Now().Unix()}
			persistInput(t, j, in)
			if err := j.complete(ctx, in, access, Result{Receipt: true, Delivery: pointer, Document: &badDoc}); !errors.Is(err, ErrProtocol) {
				t.Fatal("enqueue accepted unbound QR", err)
			}
			var count int
			if err := j.db.QueryRow("SELECT COUNT(*) FROM bot_outbox WHERE update_id=84").Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected QR enqueue did not roll back", err)
			}
			if err := j.Close(); err != nil {
				t.Fatal(err)
			}
			if err := ValidateOfflineBackup(ctx, path, qrBackupConfig(t)); !errors.Is(err, ErrJournal) {
				t.Fatal("offline backup accepted unbound QR", err)
			}
			if reopened, err := openJournal(path, []int64{100}); err == nil {
				reopened.Close()
				t.Fatal("startup accepted unbound QR")
			}
		})
	}
}

func TestQRExpiredProofStopsBeforeMultipartDispatch(t *testing.T) {
	ctx := context.Background()
	j, _ := testJournal(t, 100)
	d, _, _ := enqueueQR(t, j, 85)
	d.Proof.ExpiresUnixNano = time.Now().Add(-time.Second).UnixNano()
	a, err := newAPI(100, syntheticToken, fakeRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("expired QR proof reached the Bot API")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	a.verified = true
	out, err := a.dispatch(ctx, d)
	if !errors.Is(err, ErrProtocol) || out.State != "failed" || out.MessageID != 0 {
		t.Fatal("expired image proof accepted", out, err)
	}
}

func TestQRPreservesLegacyTextJournalWithoutNewMetadata(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100)
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 86, Identity: a.Identity, Kind: "message", Text: "deliver", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	proof := qrProof(syntheticQRURI + "\n")
	if err := j.complete(ctx, in, a, Result{Receipt: true, Delivery: &proof, Document: &Document{Name: "dark-service.txt", Content: []byte(syntheticQRURI + "\n"), Caption: "Private configuration"}}); err != nil {
		t.Fatal(err)
	}
	d := nextQueued(t, j, 100)
	for _, field := range []string{"content_type", "source_content", "source_sha256", "content_sha256"} {
		if bytes.Contains(d.Payload, []byte(field)) {
			t.Fatal("legacy document gained mandatory PNG fields")
		}
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(ctx, path, qrBackupConfig(t)); err != nil {
		t.Fatal("legacy text backup rejected", err)
	}
	reopened, err := openJournal(path, []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored := nextQueued(t, reopened, 100)
	if !bytes.Equal(restored.Payload, d.Payload) || restored.Proof == nil || *restored.Proof != proof {
		t.Fatal("legacy document changed during reopen")
	}
}
