package app

import (
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestParsePromptTimeout(t *testing.T) {
	opts, err := parseCLI([]string{"--text", "Proceed?", "--prompt", "--timeout", "90s"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.timeout != 90*time.Second || !opts.timeoutSet {
		t.Fatalf("got timeout=%s set=%v", opts.timeout, opts.timeoutSet)
	}

	opts, err = parseCLI([]string{"--text", "Proceed?", "--prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.timeout != promptTimeout || opts.timeoutSet {
		t.Fatalf("got default timeout=%s set=%v", opts.timeout, opts.timeoutSet)
	}

	for _, args := range [][]string{
		{"--text", "Proceed?", "--prompt", "--timeout", "bad"},
		{"--text", "Proceed?", "--prompt", "--timeout", "0s"},
		{"--text", "Proceed?", "--prompt", "--timeout", "-1s"},
		{"--text", "message", "--timeout", "90s"},
		{"--timeout", "90s"},
		{"--version", "--timeout", "90s"},
		{"--learn", "--timeout", "90s"},
		{"--set-token", "token", "--timeout", "90s"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("expected validation error for %v", args)
		}
	}
}
