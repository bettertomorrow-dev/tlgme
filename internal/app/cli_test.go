package app

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCLI(t *testing.T) {
	opts, err := parseCLI([]string{"--text", "Proceed?", "--prompt", "--button", "Yes", "--button", "No"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.text.value != "Proceed?" || !opts.prompt || !reflect.DeepEqual([]string(opts.buttons), []string{"Yes", "No"}) {
		t.Fatalf("got text=%q prompt=%v buttons=%v", opts.text.value, opts.prompt, opts.buttons)
	}

	_, err = parseCLI([]string{"--text", "hello", "--button"})
	if err == nil || !strings.Contains(err.Error(), "needs an argument") {
		t.Fatalf("got %v", err)
	}

	for _, args := range [][]string{
		{"--image", "a", "--file", "b"},
		{"--filename", "report.pdf"},
		{"--learn", "--file", "report.pdf"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("expected validation error for %v", args)
		}
	}
}

func TestParseVersion(t *testing.T) {
	opts, err := parseCLI([]string{"--version"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.version {
		t.Fatal("version flag was not set")
	}

	if _, err := parseCLI([]string{"--version", "--text", "hello"}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("got %v", err)
	}
}

func TestParseUpdate(t *testing.T) {
	opts, err := parseCLI([]string{"--update"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.update {
		t.Fatal("update flag was not set")
	}

	for _, args := range [][]string{
		{"--update", "--text", "hello"},
		{"--update", "--version"},
		{"--update", "--help"},
		{"--update", "--set-chat-id", "42"},
	} {
		if _, err := parseCLI(args); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("args=%v error=%v", args, err)
		}
	}
}

func TestParseRetry(t *testing.T) {
	opts, err := parseCLI([]string{"--text", "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.retry.value != defaultRetryAttempts || opts.retry.set {
		t.Fatalf("default retry=%#v", opts.retry)
	}

	opts, err = parseCLI([]string{"--text", "hello", "--retry", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.retry.value != 0 || !opts.retry.set {
		t.Fatalf("explicit retry=%#v", opts.retry)
	}

	for _, args := range [][]string{
		{"--text", "hello", "--retry", "-1"},
		{"--text", "hello", "--retry", "many"},
		{"--retry", "1"},
		{"--learn", "--retry", "1"},
		{"--version", "--retry", "1"},
		{"--set-token", "token", "--retry", "1"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("expected retry validation error for %v", args)
		}
	}
	if _, err := parseCLI([]string{"--help", "--retry", "1"}); err == nil || !strings.Contains(err.Error(), "--retry cannot be combined with --help") {
		t.Fatalf("help retry error=%v", err)
	}
}
