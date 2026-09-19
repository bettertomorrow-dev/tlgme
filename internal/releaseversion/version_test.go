package releaseversion

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCalculate(t *testing.T) {
	repo := newRepository(t)

	writeFile(t, repo, "VERSION", "0.1\n")
	commit(t, repo, "start 0.1")
	assertVersion(t, repo, "v0.1.0")

	writeFile(t, repo, "one.txt", "one\n")
	commit(t, repo, "first change")
	assertVersion(t, repo, "v0.1.1")

	writeFile(t, repo, "two.txt", "two\n")
	commit(t, repo, "second change")
	assertVersion(t, repo, "v0.1.2")

	writeFile(t, repo, "VERSION", "0.2\n")
	commit(t, repo, "start 0.2")
	assertVersion(t, repo, "v0.2.0")
}

func TestCalculateRejectsInvalidVersion(t *testing.T) {
	for _, value := range []string{"", "0", "0.1.2", "v0.1", "01.2", "1.02"} {
		t.Run(value, func(t *testing.T) {
			repo := newRepository(t)
			writeFile(t, repo, "VERSION", value+"\n")
			commit(t, repo, "invalid version")
			if _, err := Calculate(repo, "HEAD"); err == nil {
				t.Fatalf("Calculate accepted %q", value)
			}
		})
	}
}

func assertVersion(t *testing.T, repo, want string) {
	t.Helper()
	got, err := Calculate(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Calculate() = %q, want %q", got, want)
	}
}

func newRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "config", "user.email", "test@example.com")
	return repo
}

func writeFile(t *testing.T, repo, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, repo, message string) {
	t.Helper()
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", message)
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
