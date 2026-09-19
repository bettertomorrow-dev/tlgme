package app

import (
	"context"
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

	t.Run("ordinary error", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"--unknown"}, strings.NewReader(""), &stdout, &stderr)
		if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("silent error", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := Run(context.Background(), []string{"--text", "hello"}, strings.NewReader(""), &stdout, &stderr)
		if code != 1 || !strings.Contains(stdout.String(), "not configured: missing bot token and chat ID") || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr strings.Builder
		code := Run(ctx, nil, strings.NewReader(""), &stdout, &stderr)
		if code != 130 || stderr.Len() != 0 {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
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
