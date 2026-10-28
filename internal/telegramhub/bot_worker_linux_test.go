//go:build linux

package telegramhub

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func openFakeOwnerBot(t *testing.T, path string, reader RepresentativeReporter, transport ownerBotRoundTrip) *OwnerReportWorker {
	t.Helper()
	worker, err := openOwnerReportWorker(ownerBotBinding(), fakeOwnerBotToken, path, reader, transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	return worker
}

func matchingOwnerBotMe() *http.Response {
	return ownerBotHTTP(200, `{"ok":true,"result":{"id":123456,"is_bot":true,"username":"DarkLabHubBot"}}`)
}

func fakeOwnerBotBatch(values ...any) *http.Response {
	if values == nil {
		values = []any{}
	}
	return ownerBotHTTP(200, ownerBotJSON(map[string]any{"ok": true, "result": values}))
}

func ingestOwnerBotUpdate(t *testing.T, worker *OwnerReportWorker, id int64) {
	t.Helper()
	update, err := normalizeBotUpdate([]byte(ownerBotJSON(ownerBotUpdate(id))))
	if err != nil || worker.journal.Ingest(context.Background(), []receivedUpdate{update}) != nil {
		t.Fatal("could not stage fake pending command", err)
	}
}

func assertOwnerBotOutcome(t *testing.T, worker *OwnerReportWorker, id int64, want string) dispatchOutcome {
	t.Helper()
	got, err := worker.journal.Outcome(context.Background(), id)
	if err != nil || got.State != want {
		t.Fatal("unexpected durable outcome", got, err)
	}
	return got
}

func TestOwnerBotWorkerDurableDispatchAndDuplicatePolling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var worker *OwnerReportWorker
	var order []string
	var offsets []int64
	reads, sends, polls := 0, 0, 0
	worker = openFakeOwnerBot(t, filepath.Join(privateOwnerBotDir(t), "inbox.sqlite"),
		reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
			reads++
			return validReport(), nil
		}), func(request *http.Request) (*http.Response, error) {
			method := filepath.Base(request.URL.Path)
			order = append(order, method)
			switch method {
			case "getMe":
				return matchingOwnerBotMe(), nil
			case "getUpdates":
				polls++
				fields := assertOwnerBotRequest(t, request, "getUpdates")
				var offset int64
				if readRequired(fields, "offset", &offset) != nil {
					t.Fatal("missing offset")
				}
				offsets = append(offsets, offset)
				if polls <= 2 {
					return fakeOwnerBotBatch(ownerBotUpdate(100)), nil
				}
				cancel()
				return nil, request.Context().Err()
			case "sendMessage":
				sends++
				// A real network send is not performed. The fake observes the
				// durable claim at exactly the transport boundary.
				assertOwnerBotOutcome(t, worker, 100, "dispatching")
				cursor, err := worker.journal.Cursor(context.Background())
				if err != nil || cursor != 101 {
					t.Fatal("send preceded durable poll batch", cursor, err)
				}
				return ownerBotHTTP(200, ownerBotJSON(map[string]any{"ok": true, "result": ownerBotSentMessage()})), nil
			default:
				t.Fatal("unexpected bot operation")
				return nil, errors.New("unexpected operation")
			}
		})
	if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("worker did not stop on cancellation", err)
	}
	if reads != 1 || sends != 1 || !reflect.DeepEqual(offsets, []int64{0, 101, 101}) ||
		!reflect.DeepEqual(order, []string{"getMe", "getUpdates", "sendMessage", "getUpdates", "getUpdates"}) {
		t.Fatal("duplicate caused report or send replay", reads, sends, offsets, order)
	}
	if outcome := assertOwnerBotOutcome(t, worker, 100, "sent"); outcome.MessageID != 301 {
		t.Fatal("sent receipt lost")
	}
	if err := worker.Run(context.Background()); err != ErrBotWorkerState {
		t.Fatal("same worker started twice", err)
	}
}

func TestOwnerBotWorkerDrainsPendingAfterRestartBeforePolling(t *testing.T) {
	path := filepath.Join(privateOwnerBotDir(t), "inbox.sqlite")
	ctx, cancel := context.WithCancel(context.Background())
	first := openFakeOwnerBot(t, path, reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		cancel() // Preparing a report must not mark it sent or dispatching.
		return validReport(), nil
	}), func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/getMe") {
			return matchingOwnerBotMe(), nil
		}
		t.Fatal("cancelled report preparation performed transport")
		return nil, context.Canceled
	})
	ingestOwnerBotUpdate(t, first, 100)
	if err := first.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertOwnerBotOutcome(t, first, 100, "pending")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	var order []string
	second := openFakeOwnerBot(t, path, reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		return validReport(), nil
	}), func(request *http.Request) (*http.Response, error) {
		method := filepath.Base(request.URL.Path)
		order = append(order, method)
		switch method {
		case "getMe":
			return matchingOwnerBotMe(), nil
		case "sendMessage":
			return ownerBotHTTP(200, ownerBotJSON(map[string]any{"ok": true, "result": ownerBotSentMessage()})), nil
		case "getUpdates":
			fields := assertOwnerBotRequest(t, request, method)
			var offset int64
			if readRequired(fields, "offset", &offset) != nil || offset != 101 {
				t.Fatal("restart cursor changed", offset)
			}
			cancel()
			return nil, context.Canceled
		default:
			t.Fatal("unexpected operation")
			return nil, context.Canceled
		}
	})
	if err := second.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"getMe", "sendMessage", "getUpdates"}) {
		t.Fatal("poll acknowledged pending command before drain", order)
	}
	assertOwnerBotOutcome(t, second, 100, "sent")
}

func TestOwnerBotWorkerUnknownSendNeverResendsAfterRestart(t *testing.T) {
	path := filepath.Join(privateOwnerBotDir(t), "inbox.sqlite")
	ctx, cancel := context.WithCancel(context.Background())
	reads, sends := 0, 0
	first := openFakeOwnerBot(t, path, reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		reads++
		return validReport(), nil
	}), func(request *http.Request) (*http.Response, error) {
		switch filepath.Base(request.URL.Path) {
		case "getMe":
			return matchingOwnerBotMe(), nil
		case "sendMessage":
			sends++
			// Simulates a request with an ambiguous external result and a lost
			// response. It is not evidence of a real Telegram delivery.
			cancel()
			return nil, errors.New("lost reply " + fakeOwnerBotToken)
		default:
			t.Fatal("pending work should precede polling")
			return nil, context.Canceled
		}
	})
	ingestOwnerBotUpdate(t, first, 100)
	if err := first.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	before := assertOwnerBotOutcome(t, first, 100, "unknown")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	polls := 0
	second := openFakeOwnerBot(t, path, reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		t.Fatal("unknown delivery replay reached ledger")
		return validReport(), nil
	}), func(request *http.Request) (*http.Response, error) {
		switch filepath.Base(request.URL.Path) {
		case "getMe":
			return matchingOwnerBotMe(), nil
		case "getUpdates":
			polls++
			if polls == 1 {
				return fakeOwnerBotBatch(ownerBotUpdate(100)), nil
			}
			cancel()
			return nil, context.Canceled
		default:
			t.Fatal("unknown delivery was blindly retried")
			return nil, context.Canceled
		}
	})
	if err := second.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if after := assertOwnerBotOutcome(t, second, 100, "unknown"); after != before || reads != 1 || sends != 1 {
		t.Fatal("unknown outcome changed or request repeated", after, reads, sends)
	}
}

func TestOwnerBotWorkerIdentityAndScopePrecedeLedgerAccess(t *testing.T) {
	reader := reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		t.Fatal("unauthorized command reached ledger")
		return business.OwnerRepLedgerReport{}, nil
	})
	path := filepath.Join(privateOwnerBotDir(t), "inbox.sqlite")
	wrongBot := openFakeOwnerBot(t, path, reader, func(request *http.Request) (*http.Response, error) {
		assertOwnerBotRequest(t, request, "getMe")
		return ownerBotHTTP(200, `{"ok":true,"result":{"id":999,"is_bot":true,"username":"DarkLabHubBot"}}`), nil
	})
	ingestOwnerBotUpdate(t, wrongBot, 100)
	if err := wrongBot.Run(context.Background()); err != ErrBotIdentity {
		t.Fatal("wrong bot polled or prepared a reply", err)
	}
	assertOwnerBotOutcome(t, wrongBot, 100, "pending")
	if err := wrongBot.Close(); err != nil {
		t.Fatal(err)
	}
	changed := ownerBotBinding()
	changed.OwnerUserID++
	if _, err := openOwnerReportWorker(changed, fakeOwnerBotToken, path, reader,
		ownerBotRoundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("binding change called network"); return nil, nil })); err != ErrBotJournalBinding {
		t.Fatal("binding change reused old inbox", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var batch []any
	for i, mutate := range []func(map[string]any){
		func(m map[string]any) { m["from"].(map[string]any)["id"] = 999 },
		func(m map[string]any) { m["chat"].(map[string]any)["id"] = 999 },
		func(m map[string]any) { m["chat"].(map[string]any)["type"] = "group" },
		func(m map[string]any) { m["from"].(map[string]any)["is_bot"] = true },
		func(m map[string]any) { m["text"] = "/rep_credit@OtherBot repA" },
		func(m map[string]any) { m["sender_business_bot"] = map[string]any{"id": 123456} },
	} {
		update := ownerBotUpdate(int64(200 + i))
		mutate(update["message"].(map[string]any))
		batch = append(batch, update)
	}
	polls := 0
	worker := openFakeOwnerBot(t, filepath.Join(privateOwnerBotDir(t), "inbox.sqlite"), reader,
		func(request *http.Request) (*http.Response, error) {
			switch filepath.Base(request.URL.Path) {
			case "getMe":
				return matchingOwnerBotMe(), nil
			case "getUpdates":
				polls++
				if polls == 1 {
					return fakeOwnerBotBatch(batch...), nil
				}
				cancel()
				return nil, context.Canceled
			default:
				t.Fatal("unauthorized input sent a message")
				return nil, context.Canceled
			}
		})
	if err := worker.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for i := range batch {
		assertOwnerBotOutcome(t, worker, int64(200+i), "rejected")
	}
}

func TestOwnerBotWorkerPollCancellationAndFloodWait(t *testing.T) {
	for _, rateLimited := range []bool{false, true} {
		t.Run(ownerBotJSON(rateLimited), func(t *testing.T) {
			polls := 0
			worker := openFakeOwnerBot(t, filepath.Join(privateOwnerBotDir(t), "inbox.sqlite"),
				reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
					t.Fatal("empty polling read a ledger")
					return validReport(), nil
				}), func(request *http.Request) (*http.Response, error) {
					if strings.HasSuffix(request.URL.Path, "/getMe") {
						return matchingOwnerBotMe(), nil
					}
					assertOwnerBotRequest(t, request, "getUpdates")
					polls++
					if rateLimited {
						return ownerBotHTTP(429, `{"ok":false,"error_code":429,"parameters":{"retry_after":10}}`), nil
					}
					<-request.Context().Done()
					return nil, request.Context().Err()
				})
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := worker.Run(ctx); !errors.Is(err, context.DeadlineExceeded) || polls != 1 {
				t.Fatal("polling ignored cancellation or flood delay", err, polls)
			}
			if err := worker.Wait(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("wait lost worker result", err)
			}
		})
	}
}

func TestOwnerBotWorkerMessageRejectionsDoNotStopLaterCommands(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusForbidden} {
		t.Run(ownerBotJSON(code), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			reads, sends := 0, 0
			worker := openFakeOwnerBot(t, filepath.Join(privateOwnerBotDir(t), "inbox.sqlite"),
				reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
					reads++
					return validReport(), nil
				}), func(request *http.Request) (*http.Response, error) {
					switch filepath.Base(request.URL.Path) {
					case "getMe":
						return matchingOwnerBotMe(), nil
					case "sendMessage":
						sends++
						if sends == 1 {
							return ownerBotHTTP(code, ownerBotJSON(map[string]any{"ok": false, "error_code": code,
								"description": "private remote detail must not escape"})), nil
						}
						message := ownerBotSentMessage()
						message["reply_to_message"].(map[string]any)["message_id"] = 201
						return ownerBotHTTP(200, ownerBotJSON(map[string]any{"ok": true, "result": message})), nil
					case "getUpdates":
						cancel()
						return nil, context.Canceled
					default:
						t.Fatal("unexpected operation")
						return nil, context.Canceled
					}
				})
			ingestOwnerBotUpdate(t, worker, 100)
			second := ownerBotUpdate(101)
			second["message"].(map[string]any)["message_id"] = 201
			update, err := normalizeBotUpdate([]byte(ownerBotJSON(second)))
			if err != nil || worker.journal.Ingest(context.Background(), []receivedUpdate{update}) != nil {
				t.Fatal("could not queue later command", err)
			}
			if err := worker.Run(ctx); !errors.Is(err, context.Canceled) || reads != 2 || sends != 2 {
				t.Fatal("message rejection stopped or replayed work", err, reads, sends)
			}
			assertOwnerBotOutcome(t, worker, 100, "failed")
			assertOwnerBotOutcome(t, worker, 101, "sent")
		})
	}
}

func TestOwnerBotWorkerCloseCancelsAndWaitsBeforeReleasingJournal(t *testing.T) {
	polling := make(chan struct{})
	worker := openFakeOwnerBot(t, filepath.Join(privateOwnerBotDir(t), "inbox.sqlite"),
		reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
			t.Fatal("empty poll reached reporter")
			return validReport(), nil
		}), func(request *http.Request) (*http.Response, error) {
			if strings.HasSuffix(request.URL.Path, "/getMe") {
				return matchingOwnerBotMe(), nil
			}
			close(polling)
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
	result := make(chan error, 1)
	go func() { result <- worker.Run(context.Background()) }()
	select {
	case <-polling:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never entered fake polling")
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("Close did not finish Run", err)
	}
	if err := worker.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal("Wait lost cancellation", err)
	}
	if err := worker.Run(context.Background()); err != ErrBotWorkerState {
		t.Fatal("closed worker restarted", err)
	}
}
