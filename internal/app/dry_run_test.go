package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countingTransport struct {
	requests atomic.Int32
}

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.requests.Add(1)
	return nil, errors.New("unexpected HTTP request")
}

func TestDryRunOutput(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	path := filepath.Join(t.TempDir(), "chart.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, stdin string
		args        []string
		want        string
	}{
		{
			name: "text",
			args: []string{"--text", "hello", "--token", "temporary-token", "--chat-id", "99", "--dry-run"},
			want: "dry-run: no network requests made\n" +
				"target: 99\nmessage-length: 5\nattachment-kind: none\nprompt: false\nbutton-count: 0\n",
		},
		{
			name: "local image",
			args: []string{"--text", "chart", "--image", path, "--dry-run"},
			want: "dry-run: no network requests made\n" +
				"target: 42\ncaption-length: 5\nattachment-kind: photo\nfilename: chart.png\nbyte-size: 8\nprompt: false\nbutton-count: 0\n",
		},
		{
			name: "data URI",
			args: []string{"--file", "data:application/pdf;base64,JVBERi0=", "--dry-run"},
			want: "dry-run: no network requests made\n" +
				"target: 42\ncaption-length: 0\nattachment-kind: document\nfilename: file.pdf\nbyte-size: 5\nprompt: false\nbutton-count: 0\n",
		},
		{
			name:  "stdin",
			stdin: "iVBORw0KGgo=",
			args:  []string{"--image", "-", "--filename", "stdin.png", "--dry-run"},
			want: "dry-run: no network requests made\n" +
				"target: 42\ncaption-length: 0\nattachment-kind: photo\nfilename: stdin.png\nbyte-size: 8\nprompt: false\nbutton-count: 0\n",
		},
		{
			name: "URL",
			args: []string{"--image", "https://example.com/build.png", "--dry-run"},
			want: "dry-run: no network requests made\n" +
				"target: 42\ncaption-length: 0\nattachment-kind: photo\nfilename: build.png\nbyte-size: unknown\nprompt: false\nbutton-count: 0\n",
		},
		{
			name: "fallback document",
			args: []string{"--image", "data:application/pdf;base64,JVBERi0=", "--dry-run"},
			want: "dry-run: no network requests made\n" +
				"target: 42\ncaption-length: 0\nattachment-kind: document\nfilename: file.pdf\nbyte-size: 5\nprompt: false\nbutton-count: 0\n",
		},
		{
			name: "prompt",
			args: []string{"--text", "Proceed?", "--prompt", "--button", "Yes", "--button", "No", "--dry-run"},
			want: "dry-run: no network requests made\n" +
				"target: 42\nmessage-length: 8\nattachment-kind: none\nprompt: true\nbutton-count: 2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, stdout := dryRunApplication(t, map[string]string{botTokenEnv: "secret-token", chatIDEnv: "42"}, tt.stdin)
			var stderr strings.Builder
			app.stderr = &stderr
			if err := app.run(context.Background(), tt.args); err != nil {
				t.Fatal(err)
			}
			if got := stdout.String(); got != tt.want {
				t.Fatalf("stdout:\n%s\nwant:\n%s", got, tt.want)
			}
			if strings.Contains(stdout.String(), "secret-token") || strings.Contains(stdout.String(), "temporary-token") {
				t.Fatalf("dry run exposed token: %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("dry run wrote to stderr: %q", stderr.String())
			}
		})
	}
}

func TestDryRunRejectsInvalidModes(t *testing.T) {
	for _, args := range [][]string{
		{"--dry-run"},
		{"--dry-run", "--learn"},
		{"--dry-run", "--set-token", "secret"},
		{"--dry-run", "--version"},
		{"--dry-run", "--update"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("expected validation error for %v", args)
		}
	}
}

func TestDryRunMissingConfigurationAndPromptTarget(t *testing.T) {
	app, stdout := dryRunApplication(t, nil, "")
	err := app.run(context.Background(), []string{"--text", "hello", "--dry-run"})
	code, quiet := exitResult(err)
	if code != exitMissingConfig || !quiet || stdout.Len() != 0 {
		t.Fatalf("code=%d quiet=%v stdout=%q", code, quiet, stdout.String())
	}

	app, stdout = dryRunApplication(t, map[string]string{botTokenEnv: "secret", chatIDEnv: "@alerts"}, "")
	err = app.run(context.Background(), []string{"--text", "hello", "--prompt", "--dry-run"})
	code, _ = exitResult(err)
	if code != exitInvalidInput || stdout.Len() != 0 || !strings.Contains(err.Error(), "private/group chat") {
		t.Fatalf("code=%d stdout=%q err=%v", code, stdout.String(), err)
	}
}

func TestDryRunMakesNoHTTPRequests(t *testing.T) {
	transport := &countingTransport{}
	previousTransport := http.DefaultTransport
	previousClientTransport := http.DefaultClient.Transport
	http.DefaultTransport = transport
	http.DefaultClient.Transport = transport
	t.Cleanup(func() {
		http.DefaultTransport = previousTransport
		http.DefaultClient.Transport = previousClientTransport
	})

	t.Setenv(botTokenEnv, "")
	t.Setenv(chatIDEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	commands := [][]string{
		{"--text", "hello", "--dry-run", "--token", "secret", "--chat-id", "42"},
		{"--image", "https://example.com/chart.png", "--dry-run", "--token", "secret", "--chat-id", "42"},
		{"--text", "Proceed?", "--prompt", "--dry-run", "--token", "secret", "--chat-id", "42"},
	}
	for _, args := range commands {
		var stdout, stderr strings.Builder
		if code := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
	if got := transport.requests.Load(); got != 0 {
		t.Fatalf("dry runs made %d HTTP requests", got)
	}
}

// dryRunApplication turns every operation that could contact a service or
// mutate state into a test failure. The config directory stays absent after a
// dry run, proving the normal setup and update paths did not run.
func dryRunApplication(t *testing.T, env map[string]string, stdin string) (application, *strings.Builder) {
	t.Helper()
	app := testApplication(env)
	var stdout strings.Builder
	configPath := filepath.Join(t.TempDir(), "missing", "config.json")
	app.stdin = strings.NewReader(stdin)
	app.stdout = &stdout
	app.configPath = func() (string, error) { return configPath, nil }
	app.send = func(context.Context, string, any, outgoing) (int, error) {
		t.Fatal("dry run sent a Telegram message")
		return 0, errors.New("unreachable")
	}
	app.validateToken = func(context.Context, string) (string, error) {
		t.Fatal("dry run validated a Telegram token")
		return "", errors.New("unreachable")
	}
	app.learn = func(context.Context, string, time.Time) (int64, error) {
		t.Fatal("dry run polled Telegram")
		return 0, errors.New("unreachable")
	}
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time) (promptAnswer, error) {
		t.Fatal("dry run polled for a prompt answer")
		return promptAnswer{}, errors.New("unreachable")
	}
	app.checkAPI = func(context.Context) error {
		t.Fatal("dry run checked Telegram")
		return errors.New("unreachable")
	}
	app.updateCachePath = func() (string, error) {
		t.Fatal("dry run accessed the update cache")
		return "", errors.New("unreachable")
	}
	app.latestRelease = func(context.Context) (releaseInfo, error) {
		t.Fatal("dry run checked for updates")
		return releaseInfo{}, errors.New("unreachable")
	}
	t.Cleanup(func() {
		if _, err := os.Stat(filepath.Dir(configPath)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("dry run wrote configuration data: %v", err)
		}
	})
	return app, &stdout
}
