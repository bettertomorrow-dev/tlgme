package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunSendsMessageToEnvironmentChat(t *testing.T) {
	var gotToken, gotMessage string
	var gotChatID any
	app := testApplication(map[string]string{
		botTokenEnv: "secret",
		chatIDEnv:   "-123",
	})
	app.send = func(_ context.Context, token string, chatID any, message outgoing) (int, error) {
		gotToken, gotChatID, gotMessage = token, chatID, message.text
		return 1, nil
	}

	if err := app.run(context.Background(), []string{"--text", "job finished"}); err != nil {
		t.Fatal(err)
	}
	if gotToken != "secret" || gotChatID != int64(-123) || gotMessage != "job finished" {
		t.Fatalf("unexpected send: token=%q chat=%v message=%q", gotToken, gotChatID, gotMessage)
	}
}

func TestRunUsesSavedChat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, config{ChatID: &chatTarget{value: int64(456)}}); err != nil {
		t.Fatal(err)
	}

	var gotChatID any
	app := testApplication(map[string]string{botTokenEnv: "secret"})
	app.configPath = func() (string, error) { return path, nil }
	app.send = func(_ context.Context, _ string, chatID any, _ outgoing) (int, error) {
		gotChatID = chatID
		return 1, nil
	}

	if err := app.run(context.Background(), []string{"--text", "done"}); err != nil {
		t.Fatal(err)
	}
	if gotChatID != int64(456) {
		t.Fatalf("got chat ID %v", gotChatID)
	}
}

func TestRunAllowsChannelUsername(t *testing.T) {
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "@alerts"})
	app.send = func(_ context.Context, _ string, chatID any, _ outgoing) (int, error) {
		if chatID != "@alerts" {
			t.Fatalf("got chat ID %v", chatID)
		}
		return 1, nil
	}
	if err := app.run(context.Background(), []string{"--text", "done"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "legacy positional send", args: []string{"done"}, want: "unexpected positional arguments"},
		{name: "legacy prompt", args: []string{"--prompt", "question"}, want: "unexpected positional arguments"},
		{name: "prompt without text", args: []string{"--prompt"}, want: "requires --text"},
		{name: "button without prompt", args: []string{"--text", "question", "--button", "Yes"}, want: "requires --prompt"},
		{name: "empty text", args: []string{"--text", ""}, want: "non-empty"},
		{name: "learn chat override", args: []string{"--learn", "--chat-id", "42"}, want: "cannot be combined"},
		{name: "invalid chat", args: []string{"--text", "done", "--token", "secret", "--chat-id", "somewhere"}, want: "integer or an @channel username"},
		{name: "empty set token", args: []string{"--set-token", ""}, want: "non-empty"},
		{name: "zero set chat", args: []string{"--set-chat-id", "0"}, want: "must not be zero"},
		{name: "set with send", args: []string{"--set-token", "secret", "--text", "done"}, want: "cannot be combined"},
		{name: "set with override", args: []string{"--set-chat-id", "42", "--token", "secret"}, want: "cannot be combined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := testApplication(nil)
			err := app.run(context.Background(), tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestRunLearnSavesChatAndConfirms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	startedAt := time.Unix(100, 0)
	var sent []any
	app := testApplication(map[string]string{botTokenEnv: "secret"})
	app.now = func() time.Time { return startedAt }
	app.configPath = func() (string, error) { return path, nil }
	app.validateToken = func(_ context.Context, token string) (string, error) {
		if token != "secret" {
			t.Fatalf("unexpected token %q", token)
		}
		return "learnbot", nil
	}
	app.learn = func(_ context.Context, token string, gotStartedAt time.Time) (int64, error) {
		if token != "secret" || !gotStartedAt.Equal(startedAt) {
			t.Fatal("unexpected learn arguments")
		}
		return 789, nil
	}
	app.send = func(_ context.Context, token string, chatID any, message outgoing) (int, error) {
		sent = []any{token, chatID, message.text}
		return 1, nil
	}

	if err := app.run(context.Background(), []string{"--learn"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatID == nil || cfg.ChatID.value != int64(789) {
		t.Fatalf("got saved chat ID %#v", cfg.ChatID)
	}
	wantSent := []any{"secret", int64(789), testMessageText}
	if !reflect.DeepEqual(sent, wantSent) {
		t.Fatalf("got send %#v, want %#v", sent, wantSent)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("got config permissions %o", info.Mode().Perm())
	}
}

func TestSendNotConfiguredReportsMissing(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{
			name: "nothing set",
			args: []string{"--text", "hi"},
			want: "not configured: missing bot token and chat ID",
		},
		{
			name: "token only",
			env:  map[string]string{botTokenEnv: "secret"},
			args: []string{"--text", "hi"},
			want: "not configured: missing chat ID",
		},
		{
			name: "chat only",
			env:  map[string]string{chatIDEnv: "42"},
			args: []string{"--text", "hi", "--prompt"},
			want: "not configured: missing bot token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout strings.Builder
			app := testApplication(tt.env)
			app.stdout = &stdout

			err := app.run(context.Background(), tt.args)
			var silent silentExitError
			if !errors.As(err, &silent) || silent.code != 1 {
				t.Fatalf("got %v, want silent exit 1", err)
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("stdout %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestLearnNotConfiguredReportsMissingToken(t *testing.T) {
	var stdout strings.Builder
	app := testApplication(map[string]string{chatIDEnv: "42"})
	app.stdout = &stdout

	err := app.run(context.Background(), []string{"--learn"})
	var silent silentExitError
	if !errors.As(err, &silent) || silent.code != 1 {
		t.Fatalf("got %v, want silent exit 1", err)
	}
	if !strings.Contains(stdout.String(), "not configured: missing bot token") {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestSettingsPrecedence(t *testing.T) {
	app := testApplication(map[string]string{botTokenEnv: "env-token", chatIDEnv: "11"})
	cfg := config{BotToken: "config-token", ChatID: &chatTarget{value: int64(22)}}

	resolved, err := app.resolveSettings(cfg, cliOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.token != "config-token" || resolved.chatID.value != int64(22) {
		t.Fatalf("config did not override environment: %#v", resolved)
	}

	opts, err := parseCLI([]string{"--token", "cli-token", "--chat-id", "33"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = app.resolveSettings(cfg, opts)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.token != "cli-token" || resolved.chatID.value != int64(33) {
		t.Fatalf("CLI did not override config: %#v", resolved)
	}
}

func TestConfiguredRunWithoutActionShowsHelp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, config{BotToken: "token", ChatID: &chatTarget{value: int64(1)}}); err != nil {
		t.Fatal(err)
	}
	var stdout strings.Builder
	app := testApplication(nil)
	app.stdout = &stdout
	app.configPath = func() (string, error) { return path, nil }
	if err := app.run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Usage:") || !strings.Contains(stdout.String(), "--set-token") {
		t.Fatalf("help output %q", stdout.String())
	}
}

func TestHelpDoesNotReadConfig(t *testing.T) {
	var stdout strings.Builder
	app := testApplication(nil)
	app.stdout = &stdout
	app.configPath = func() (string, error) { return "", errors.New("should not read config") }
	if err := app.run(context.Background(), []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("help output %q", stdout.String())
	}
}

func TestRedact(t *testing.T) {
	got := redact(`Post "https://api.telegram.org/bot123:secret/sendMessage": failed`, "123:secret")
	if strings.Contains(got, "123:secret") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("secret was not redacted: %s", got)
	}
}

func testApplication(env map[string]string) application {
	redactions := []string{env[botTokenEnv]}
	return application{
		getenv: func(key string) string { return env[key] },
		stdin:  strings.NewReader(""),
		stdout: io.Discard,
		stderr: io.Discard,
		now:    time.Now,
		configPath: func() (string, error) {
			return filepath.Join(os.TempDir(), "missing-tlgme-config"), nil
		},
		validateToken: func(context.Context, string) (string, error) {
			return "testbot", nil
		},
		checkAPI: func(context.Context) error { return nil },
		lookPath: func(string) (string, error) {
			return "/usr/local/bin/tlgme", nil
		},
		learn: func(context.Context, string, time.Time) (int64, error) {
			return 0, errors.New("unexpected learn")
		},
		send: func(context.Context, string, any, outgoing) (int, error) {
			return 0, errors.New("unexpected send")
		},
		awaitAnswer: func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error) {
			return promptAnswer{}, errors.New("unexpected awaitAnswer")
		},
		answerCallback: func(context.Context, string, string) error {
			return errors.New("unexpected answerCallback")
		},
		removeKeyboard: func(context.Context, string, int64, int) error {
			return errors.New("unexpected removeKeyboard")
		},
		appendAnswer: func(context.Context, string, int64, int, string, bool) error {
			return errors.New("unexpected appendAnswer")
		},
		react: func(context.Context, string, int64, int, string) error {
			return errors.New("unexpected react")
		},
		redactions: &redactions,
	}
}
