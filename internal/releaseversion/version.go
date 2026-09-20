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

// Calculate returns vX.Y.Z for revision. X.Y comes from VERSION. Z increments
// from the latest reachable release tag with the same X.Y version.
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

	tags, err := git(repo, "tag", "--merged", commit)
	if err != nil {
		return "", fmt.Errorf("list release tags for %s: %w", commit, err)
	}
	pattern := regexp.MustCompile(`^v` + regexp.QuoteMeta(base) + `\.(0|[1-9][0-9]*)$`)
	current := -1
	currentTag := ""
	latest := -1
	for _, tag := range strings.Fields(tags) {
		match := pattern.FindStringSubmatch(tag)
		if match == nil {
			continue
		}
		patch, err := strconv.Atoi(match[1])
		if err != nil {
			return "", fmt.Errorf("parse patch number in tag %q: %w", tag, err)
		}
		tagCommit, err := git(repo, "rev-parse", tag+"^{commit}")
		if err != nil {
			return "", fmt.Errorf("resolve release tag %q: %w", tag, err)
		}
		if tagCommit == commit {
			if patch > current {
				current = patch
				currentTag = tag
			}
			continue
		}
		if patch > latest {
			latest = patch
		}
	}
	if currentTag != "" {
		return currentTag, nil
	}

	return fmt.Sprintf("v%s.%d", base, latest+1), nil
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
