# Publishing TlgMe with WinGet

The first WinGet submission and the automatic updates are separate processes.
This repository's release workflow publishes the application. The WinGet
community repository hosts the manifests that make `winget install
BetterTomorrowDev.TlgMe` available.

## One-time setup

Create a GitHub personal access token that can create a fork and pull request
in the public `microsoft/winget-pkgs` repository. Store it as the
`WINGET_CREATE_TOKEN` Actions secret in `bettertomorrow-dev/tlgme`. Do not add
the token to a workflow file, shell history, or command line outside GitHub
Actions.

The release workflow uses the token only after the package's first manifest
has been merged into WinGet. It downloads the official Winget-Create utility,
updates `BetterTomorrowDev.TlgMe` with the two Windows release archives, and
opens a pull request in `microsoft/winget-pkgs`.

If that submission fails, the WinGet job fails. The GitHub Release remains
published. Check the job log and the WinGet pull requests before rerunning the
Release workflow, so a retry does not create a duplicate pull request.

## First manifest

The first package version needs a manual submission because it establishes the
permanent identifier and package metadata. Run the generator from a checkout
of this repository after a GitHub Release exists:

```bash
scripts/generate-winget-manifest v0.1.3 /tmp/tlgme-winget
```

It downloads the release metadata and `checksums.txt`, verifies the `amd64`
and `arm64` Windows archives, then writes the three required WinGet YAML files
to:

```text
/tmp/tlgme-winget/manifests/b/BetterTomorrowDev/TlgMe/0.1.3/
```

On a Windows machine, validate and install the generated manifest before
opening the external pull request:

```powershell
winget settings --enable LocalManifestFiles
winget validate --manifest .\manifests\b\BetterTomorrowDev\TlgMe\0.1.3
winget install --manifest .\manifests\b\BetterTomorrowDev\TlgMe\0.1.3
tlgme --version
```

Fork `microsoft/winget-pkgs`, copy only the generated version directory into
the same path in the fork, and open a pull request. The pull request must
contain one package version and no unrelated files. Wait for it to merge before
relying on automatic updates.

Complete that external submission and add `WINGET_CREATE_TOKEN` before merging
the TlgMe pull request that enables this workflow. Its merge creates the next
TlgMe release, which immediately attempts the first automated WinGet update.

## Later releases

After the first manifest is merged, each successful TlgMe release runs the
`Publish WinGet manifest` job. Winget-Create derives the new hashes from the
two versioned ZIP URLs and submits a pull request to `microsoft/winget-pkgs`.
Review that external pull request and wait for the WinGet validation checks to
pass.

Use `winget search TlgMe` after a merge, then install or update with:

```powershell
winget install BetterTomorrowDev.TlgMe
winget upgrade BetterTomorrowDev.TlgMe
```

The WinGet pull request is external to this repository. It does not replace the
pull request that adds this automation to `bettertomorrow-dev/tlgme`.
