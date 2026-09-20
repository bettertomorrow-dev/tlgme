# Publishing releases

## GitHub repositories

Create a public repository named `bettertomorrow-dev/homebrew-tap`. Initialize
its `main` branch with a README.

Create a fine-grained personal access token with access only to that repository
and grant it `Contents: Read and write`. Add the token to the `tlgme`
repository under **Settings → Secrets and variables → Actions** as
`TAP_GITHUB_TOKEN`.

The release workflow uses the repository's `GITHUB_TOKEN` to create tags and
GitHub Releases. Make sure organization policy allows workflows to request
`actions: read` and `contents: write`.

## Merge and tag rules

Under **Settings → General → Pull Requests**, enable squash merging and disable
merge commits and rebase merging. Add a branch rule for `main` that:

- requires a pull request;
- requires the CI workflow to pass;
- blocks direct and force pushes.

Add a tag ruleset for `v*` that prevents updates and deletion. Allow the
release workflow to create new tags.

## Publish a release

1. Wait for CI to pass on `main`.
2. Open the [Release workflow](https://github.com/bettertomorrow-dev/tlgme/actions/workflows/release.yml).
3. Select **Run workflow**.
4. Check that the workflow created the GitHub Release and updated the Homebrew cask.

The workflow always publishes the current tip of `main`. It refuses to run if
CI for that commit has not passed. A repeat run for an already tagged current
commit retries publication with the same tag.

## Start a new major or minor version

The root [`VERSION`](../VERSION) file contains `X.Y`. Change it in a pull
request, merge the pull request, wait for CI, then publish through the Release
workflow. The first release for a new `X.Y` is `vX.Y.0`.

For example, changing `VERSION` from `0.1` to `0.2` makes the next manual
release `v0.2.0`.

## Verify the release

Before changing release infrastructure, check it locally:

```bash
go test -race ./...
goreleaser check
goreleaser release --snapshot --clean
```

The snapshot should contain six archives and `checksums.txt`.

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

Never move an existing release tag. Publish a new patch release for application
or build defects.
