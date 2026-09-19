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
