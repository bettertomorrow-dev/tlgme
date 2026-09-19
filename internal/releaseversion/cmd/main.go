package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bettertomorrow-dev/tlgme/internal/releaseversion"
)

func main() {
	repo := flag.String("repo", ".", "repository path")
	revision := flag.String("revision", "HEAD", "commit to version")
	flag.Parse()

	tag, err := releaseversion.Calculate(*repo, *revision)
	if err != nil {
		fmt.Fprintf(os.Stderr, "release-version: %s\n", err)
		os.Exit(1)
	}
	fmt.Println(tag)
}
