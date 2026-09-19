// Package releaseversion calculates deterministic release tags from git history.
package releaseversion

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var basePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Calculate returns vX.Y.Z for revision. X.Y comes from VERSION and Z is the
// first-parent distance from the commit that last changed VERSION.
func Calculate(repo, revision string) (string, error) {
	commit, err := git(repo, "rev-parse", revision+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve revision %q: %w", revision, err)
	}

	rawVersion, err := git(repo, "show", commit+":VERSION")
	if err != nil {
		return "", fmt.Errorf("read VERSION at %s: %w", commit, err)
	}
	base := strings.TrimSpace(rawVersion)
	if !basePattern.MatchString(base) {
		return "", fmt.Errorf("VERSION must contain X.Y, got %q", base)
	}

	start, err := git(repo, "log", "--first-parent", "--format=%H", "-n", "1", commit, "--", "VERSION")
	if err != nil {
		return "", fmt.Errorf("find start of version %s: %w", base, err)
	}
	if start == "" {
		return "", fmt.Errorf("VERSION has no history at %s", commit)
	}

	rawPatch, err := git(repo, "rev-list", "--first-parent", "--count", start+".."+commit)
	if err != nil {
		return "", fmt.Errorf("count releases since %s: %w", start, err)
	}
	patch, err := strconv.Atoi(rawPatch)
	if err != nil {
		return "", fmt.Errorf("parse patch number %q: %w", rawPatch, err)
	}

	return fmt.Sprintf("v%s.%d", base, patch), nil
}

func git(repo string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repo}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return strings.TrimSpace(string(output)), nil
}
