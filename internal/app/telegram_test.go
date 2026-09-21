package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestFreshPrivateStart(t *testing.T) {
	startedAt := time.Unix(100, 0)
	start := &models.Update{Message: &models.Message{
		Date: 100,
		Text: "/start payload",
		Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate},
	}}
	if !isFreshPrivateStart(start, startedAt) {
		t.Fatal("expected fresh private /start to match")
	}

	old := *start.Message
	old.Date = 99
	group := *start.Message
	group.Chat.Type = models.ChatTypeGroup
	other := *start.Message
	other.Text = "hello"
	for name, message := range map[string]*models.Message{"old": &old, "group": &group, "other": &other} {
		t.Run(name, func(t *testing.T) {
			if isFreshPrivateStart(&models.Update{Message: message}, startedAt) {
				t.Fatal("unexpected match")
			}
		})
	}
}

func TestInlineKeyboard(t *testing.T) {
	if inlineKeyboard(nil) != nil {
		t.Fatal("expected nil for empty buttons")
	}
	m := inlineKeyboard([]string{"Yes", "No"})
	if m == nil || len(m.InlineKeyboard) != 1 || len(m.InlineKeyboard[0]) != 2 {
		t.Fatalf("unexpected markup %#v", m)
	}
	if m.InlineKeyboard[0][0].Text != "Yes" || m.InlineKeyboard[0][0].CallbackData != "opt:0" {
		t.Fatalf("first button %#v", m.InlineKeyboard[0][0])
	}
}

func TestRetryTelegramRetriesWithFreshAttemptsAndBackoff(t *testing.T) {
	var attempts int
	var contexts []context.Context
	var waits []time.Duration
	result, err := retryTelegram(context.Background(), 2, 10*time.Second, func(ctx context.Context) (int, error) {
		attempts++
		contexts = append(contexts, ctx)
		if attempts < 3 {
			return 0, io.ErrUnexpectedEOF
		}
		return 42, nil
	}, func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	})
	if err != nil || result != 42 || attempts != 3 {
		t.Fatalf("result=%d attempts=%d err=%v", result, attempts, err)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 2*time.Second {
		t.Fatalf("waits=%v", waits)
	}
	if contexts[0] == contexts[1] || contexts[1] == contexts[2] {
		t.Fatal("each attempt must use a fresh context")
	}
	for _, ctx := range contexts {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("attempt context has no deadline")
		}
	}
}

func TestRetryTelegramRetryAfterAndLimits(t *testing.T) {
	var attempts int
	var waits []time.Duration
	_, err := retryTelegram(context.Background(), 1, time.Second, func(context.Context) (int, error) {
		attempts++
		return 0, &bot.TooManyRequestsError{RetryAfter: 7}
	}, func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	})
	if err == nil || attempts != 2 || len(waits) != 1 || waits[0] != 7*time.Second {
		t.Fatalf("attempts=%d waits=%v err=%v", attempts, waits, err)
	}

	attempts = 0
	_, err = retryTelegram(context.Background(), 0, time.Second, func(context.Context) (int, error) {
		attempts++
		return 0, io.ErrUnexpectedEOF
	}, func(context.Context, time.Duration) error {
		t.Fatal("zero retries must not wait")
		return nil
	})
	if err == nil || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestRetryTelegramDoesNotRetryPermanentOrCancelledErrors(t *testing.T) {
	for name, err := range map[string]error{
		"bad request": errors.New("bad request"),
		"cancelled":   context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			attempts := 0
			_, got := retryTelegram(context.Background(), 2, time.Second, func(context.Context) (int, error) {
				attempts++
				return 0, err
			}, func(context.Context, time.Duration) error {
				t.Fatal("permanent errors must not wait")
				return nil
			})
			if !errors.Is(got, err) || attempts != 1 {
				t.Fatalf("attempts=%d err=%v", attempts, got)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, err := retryTelegram(ctx, 2, time.Second, func(context.Context) (int, error) {
		attempts++
		return 0, io.ErrUnexpectedEOF
	}, func(waitCtx context.Context, _ time.Duration) error {
		cancel()
		<-waitCtx.Done()
		return waitCtx.Err()
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestRetryClassificationAndUploadReplay(t *testing.T) {
	if !isRetryableTelegramError(context.Background(), &telegramHTTPStatusError{statusCode: 503}) {
		t.Fatal("HTTP 503 should retry")
	}
	if isRetryableTelegramError(context.Background(), &telegramHTTPStatusError{statusCode: 400}) {
		t.Fatal("HTTP 400 must not retry")
	}
	if retryDelay(7, io.ErrUnexpectedEOF) != 30*time.Second {
		t.Fatal("backoff must cap at 30 seconds")
	}

	attachment, err := resolveAttachmentFrom("data:application/pdf;base64,JVBERi0=", "report.pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		upload, ok := attachment.inputFile().(*models.InputFileUpload)
		if !ok {
			t.Fatalf("input file type %T", attachment.inputFile())
		}
		data, err := io.ReadAll(upload.Data)
		if err != nil || string(data) != "%PDF-" {
			t.Fatalf("attempt %d data=%q err=%v", i, data, err)
		}
	}
}
