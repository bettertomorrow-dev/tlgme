# Release setup

Every successful merge to `main` creates a release. Complete this setup before
merging the release workflow for the first time.

## GitHub repositories

Create a public repository named `bettertomorrow-dev/homebrew-tap`. Initialize
its `main` branch with a README.

Create a fine-grained personal access token with access only to that repository
and grant it `Contents: Read and write`. Add the token to the `tlgme`
repository under **Settings → Secrets and variables → Actions** as
`TAP_GITHUB_TOKEN`.

The release workflow uses the repository's `GITHUB_TOKEN` to create tags and
GitHub Releases. Make sure organization policy allows workflows to request
`contents: write`.

## Merge and tag rules

Under **Settings → General → Pull Requests**, enable squash merging and disable
merge commits and rebase merging. Add a branch rule for `main` that:

- requires a pull request;
- requires the CI workflow to pass;
- blocks direct and force pushes.

Add a tag ruleset for `v*` that prevents updates and deletion. Allow the
release workflow to create new tags.

These settings matter because the patch number is the number of first-parent
commits since `VERSION` last changed. With squash merging, one merged pull
request adds one patch release.

## First release

Check the release configuration locally:

```bash
go test -race ./...
goreleaser check
goreleaser release --snapshot --clean
```

The snapshot should contain six archives and `checksums.txt`. After the setup
pull request is squash-merged and CI passes, the release workflow creates
`v0.1.0`, publishes the GitHub Release, and writes `Casks/tlgme.rb` to the tap.

The CLI updater depends on these asset names remaining stable:

```text
tlgme_VERSION_darwin_amd64.tar.gz
tlgme_VERSION_darwin_arm64.tar.gz
tlgme_VERSION_linux_amd64.tar.gz
tlgme_VERSION_linux_arm64.tar.gz
tlgme_VERSION_windows_amd64.zip
tlgme_VERSION_windows_arm64.zip
checksums.txt
```

Each archive must contain `tlgme`, or `tlgme.exe` on Windows, at its root.
`checksums.txt` must include the SHA-256 checksum for every archive. Changing
this contract requires a backward-compatible updater change in an earlier
release.

Verify the published package:

```bash
brew install --cask bettertomorrow-dev/tap/tlgme
tlgme --version
```

Use the Release workflow's manual trigger with the original commit SHA if a
temporary GitHub or Homebrew failure needs a retry. Never move an existing
release tag. Publish a new patch release for application or build defects.
