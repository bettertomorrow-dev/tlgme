package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func TestFormattingCLI(t *testing.T) {
	for _, mode := range []string{"HTML", "MarkdownV2"} {
		app, stdout := dryRunApplication(t, map[string]string{botTokenEnv: "secret", chatIDEnv: "42"}, "")
		if err := app.run(context.Background(), []string{"--text", "formatted", "--parse-mode", mode, "--dry-run"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "parse-mode: "+mode+"\n") {
			t.Fatal(stdout.String())
		}
	}
	for _, args := range [][]string{
		{"--parse-mode", "HTML"}, {"--text", "x", "--parse-mode", ""},
		{"--text", "x", "--parse-mode", "Markdown"}, {"--text", "x", "--parse-mode", "html"},
		{"--text", "x", "--parse-mode", "HTML", "--help"},
		{"--text", "x", "--parse-mode", "HTML", "--version"},
		{"--text", "x", "--parse-mode", "HTML", "--update"},
		{"--text", "x", "--parse-mode", "HTML", "--learn"},
		{"--text", "x", "--parse-mode", "HTML", "--set-chat-id", "42"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestFormattedPromptAnswers(t *testing.T) {
	for _, tt := range []struct{ mode, question, label, escaped string }{
		{"", "Proceed?", "<yes> & \\!", "<yes> & \\!"},
		{"HTML", "<b>Proceed?</b>", `<yes> & "ok"`, `&lt;yes&gt; &amp; &#34;ok&#34;`},
		{"MarkdownV2", "*Proceed?*", `C:\tmp\ *yes*_[x](y)~` + "`" + `>#+-=|{}.!`, `C:\\tmp\\ \*yes\*\_\[x\]\(y\)\~` + "\\`" + `\>\#\+\-\=\|\{\}\.\!`},
	} {
		for _, withAttachment := range []bool{false, true} {
			t.Run(tt.mode, func(t *testing.T) {
				app := testApplication(nil)
				var stdout strings.Builder
				app.stdout = &stdout
				message := outgoing{text: tt.question, parseMode: tt.mode, buttons: []string{tt.label}}
				if withAttachment {
					message.attachment = &attachment{}
				}
				app.send = func(_ context.Context, _ string, _ any, got outgoing) (int, error) {
					if got.parseMode != tt.mode {
						t.Fatal(got)
					}
					return 11, nil
				}
				app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, bool) (promptAnswer, error) {
					return promptAnswer{text: tt.label, callbackID: "cb"}, nil
				}
				app.answerCallback = func(context.Context, string, string) error { return nil }
				app.react = func(context.Context, string, int64, int, string) error { return nil }
				edited := false
				app.appendAnswer = func(_ context.Context, _ string, _ int64, _ int, text string, hasAttachment bool, mode string) error {
					edited = true
					if text != tt.question+"\n\nAnswer: "+tt.escaped || mode != tt.mode || hasAttachment != withAttachment {
						t.Fatalf("text=%q mode=%q attachment=%v", text, mode, hasAttachment)
					}
					return nil
				}
				if err := app.runPrompt(context.Background(), "secret", int64(42), message); err != nil {
					t.Fatal(err)
				}
				if !edited || stdout.String() != tt.label+"\n" {
					t.Fatalf("edited=%v stdout=%q", edited, stdout.String())
				}
			})
		}
	}
}

type formattingTransport func(*http.Request) (*http.Response, error)

func (f formattingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFormattingTelegramRequests(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, mode := range []string{"", "HTML", "MarkdownV2"} {
		for _, method := range []string{"sendMessage", "sendPhoto", "sendDocument", "editMessageText", "editMessageCaption"} {
			t.Run(mode+"/"+method, func(t *testing.T) {
				calls := 0
				http.DefaultTransport = formattingTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if !strings.HasSuffix(r.URL.Path, "/"+method) {
						t.Fatalf("path=%s", r.URL.Path)
					}
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Fatal(err)
					}
					defer r.MultipartForm.RemoveAll()
					if got := r.FormValue("parse_mode"); got != mode {
						t.Fatalf("mode=%q want %q", got, mode)
					}
					if mode == "" {
						if _, ok := r.MultipartForm.Value["parse_mode"]; ok {
							t.Fatal("plain request includes parse_mode")
						}
					}
					field := "text"
					if method == "sendPhoto" || method == "sendDocument" || method == "editMessageCaption" {
						field = "caption"
					}
					if r.FormValue(field) != "payload" {
						t.Fatalf("%s=%q", field, r.FormValue(field))
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":11}}`))}, nil
				})
				message := outgoing{text: "payload", parseMode: mode}
				var err error
				switch method {
				case "editMessageText", "editMessageCaption":
					err = appendAnswer(context.Background(), "123:fake", 42, 11, "payload", method == "editMessageCaption", mode)
				default:
					if method != "sendMessage" {
						message.attachment = &attachment{file: &models.InputFileString{Data: "https://example.com/file"}}
						message.asDocument = method == "sendDocument"
					}
					_, err = sendOutgoing(context.Background(), "123:fake", int64(42), message)
				}
				if err != nil {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Fatalf("calls=%d", calls)
				}
			})
		}
	}
}

func TestCaptionUnicodeAndFormattingLimits(t *testing.T) {
	for _, tt := range []struct {
		text, mode string
		wantErr    bool
	}{
		{strings.Repeat("界", 1024), "", false}, {strings.Repeat("界", 1025), "", true},
		{strings.Repeat("😀", 1024), "", false},
		{"<b>" + strings.Repeat("界", 1024) + "</b>", "HTML", false},
		{"*" + strings.Repeat("界", 1024) + "*", "MarkdownV2", false},
	} {
		opts := cliOptions{text: stringOption{value: tt.text, set: true}, parseMode: stringOption{value: tt.mode}, file: stringOption{value: "https://example.com/file", set: true}}
		_, err := opts.outgoing(strings.NewReader(""))
		if (err != nil) != tt.wantErr {
			t.Fatalf("mode=%q error=%v", tt.mode, err)
		}
	}
}

func TestRejectedMarkupDoesNotRetry(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = formattingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))}, nil
	})
	_, err := sendOutgoing(context.Background(), "123:fake", int64(42), outgoing{text: "<b>unclosed", parseMode: "HTML"})
	if err == nil || !strings.Contains(err.Error(), "can't parse entities") {
		t.Fatalf("error=%v", err)
	}
	if calls != 1 {
		t.Fatalf("made %d requests, want 1", calls)
	}
}
