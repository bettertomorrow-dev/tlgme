package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAttachment(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	path := filepath.Join(t.TempDir(), "chart.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, source, filename string
		stdin                  string
		wantName, wantType     string
	}{
		{"URL", "https://example.com/chart.png", "", "", "chart.png", ""},
		{"data URI", "data:image/png;base64,iVBORw0KGgo=", "", "", "image.png", "image/png"},
		{"path", path, "", "", "chart.png", "image/png"},
		{"stdin", "-", "stdin.png", "iVBORw0KGgo=", "stdin.png", "image/png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAttachmentFrom(tt.source, tt.filename, strings.NewReader(tt.stdin))
			if err != nil {
				t.Fatal(err)
			}
			if got.filename != tt.wantName || got.contentType != tt.wantType {
				t.Fatalf("got filename=%q contentType=%q", got.filename, got.contentType)
			}
		})
	}
	raw := base64.StdEncoding.EncodeToString(bytes.Repeat(png, 10))
	if got, err := resolveAttachmentFrom(raw, "upload.png", strings.NewReader("")); err != nil || got.filename != "upload.png" || got.contentType != "image/png" {
		t.Fatalf("raw base64 attachment: %#v, %v", got, err)
	}
	if _, err := resolveAttachmentFrom("not a source", "", strings.NewReader("")); err == nil {
		t.Fatal("expected invalid source error")
	}
}

func TestRunSendsAttachment(t *testing.T) {
	var got outgoing
	var stderr strings.Builder
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.stderr = &stderr
	app.send = func(_ context.Context, _ string, _ any, message outgoing) (int, error) {
		got = message
		return 1, nil
	}
	if err := app.run(context.Background(), []string{"--text", "chart", "--image", "data:image/png;base64,iVBORw0KGgo=", "--silent"}); err != nil {
		t.Fatal(err)
	}
	if got.attachment == nil || got.asDocument || got.text != "chart" || !got.silent {
		t.Fatalf("unexpected image outgoing: %#v", got)
	}
	if err := app.run(context.Background(), []string{"--file", "data:application/pdf;base64,JVBERi0="}); err != nil {
		t.Fatal(err)
	}
	if got.attachment == nil || !got.asDocument {
		t.Fatalf("unexpected file outgoing: %#v", got)
	}
	if err := app.run(context.Background(), []string{"--image", "data:application/pdf;base64,JVBERi0="}); err != nil {
		t.Fatal(err)
	}
	if !got.asDocument || !strings.Contains(stderr.String(), "sent as a file") {
		t.Fatalf("fallback document=%v warning=%q", got.asDocument, stderr.String())
	}
}
