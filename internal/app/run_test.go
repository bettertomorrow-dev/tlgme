package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInterface(t *testing.T) {
	t.Setenv(botTokenEnv, "")
	t.Setenv(chatIDEnv, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("success", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 || !strings.Contains(stdout.String(), "Usage:") || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("version does not read config", func(t *testing.T) {
		configParent := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(configParent, []byte("occupied"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_CONFIG_HOME", configParent)
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"--version"}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 || stdout.String() != "tlgme "+version+"\n" || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("invalid arguments", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"--unknown"}, strings.NewReader(""), &stdout, &stderr)
		if code != exitInvalidInput || stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("missing configuration", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"--text", "hello"}, strings.NewReader(""), &stdout, &stderr)
		if code != exitMissingConfig || stdout.Len() != 0 || stderr.String() != "tlgme: not configured: missing bot token and chat ID\n" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr strings.Builder
		code := Run(ctx, nil, strings.NewReader(""), &stdout, &stderr)
		if code != exitInterrupted || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("cancellation takes precedence over invalid arguments", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr strings.Builder
		code := Run(ctx, []string{"--unknown"}, strings.NewReader(""), &stdout, &stderr)
		if code != exitInterrupted || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
}

func TestExitResult(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		code  int
		quiet bool
	}{
		{name: "success", code: exitOK},
		{name: "generic", err: errors.New("broken"), code: exitUnexpected},
		{name: "input", err: inputError(errors.New("bad flag")), code: exitInvalidInput},
		{name: "missing config", err: quietExit(exitMissingConfig), code: exitMissingConfig, quiet: true},
		{name: "timeout", err: errPromptTimeout, code: exitPromptTimeout},
		{name: "external", err: externalError(errors.New("offline")), code: exitExternal},
		{name: "wrapped cancellation wins", err: externalError(context.Canceled), code: exitInterrupted, quiet: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, quiet := exitResult(tt.err)
			if code != tt.code || quiet != tt.quiet {
				t.Fatalf("exitResult(%v) = (%d, %v), want (%d, %v)", tt.err, code, quiet, tt.code, tt.quiet)
			}
		})
	}
}

func TestApplicationReadsAttachmentFromInjectedStdin(t *testing.T) {
	var got outgoing
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.stdin = strings.NewReader("iVBORw0KGgo=")
	app.send = func(_ context.Context, _ string, _ any, message outgoing) (int, error) {
		got = message
		return 1, nil
	}

	if err := app.run(context.Background(), []string{"--image", "-", "--filename", "stdin.png"}); err != nil {
		t.Fatal(err)
	}
	if got.attachment == nil || got.attachment.filename != "stdin.png" || got.attachment.contentType != "image/png" {
		t.Fatalf("unexpected attachment %#v", got.attachment)
	}
}
