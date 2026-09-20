[CmdletBinding()]
param(
    [switch]$Preview,
    [ValidateSet('First', 'Update')]
    [string]$Scenario = 'First',
    [ValidateSet('amd64', 'arm64')]
    [string]$Architecture,
    [ValidatePattern('^v\d+\.\d+\.\d+$')]
    [string]$Version
)

$ErrorActionPreference = 'Stop'
$repo = 'bettertomorrow-dev/tlgme'
$latestReleaseUrl = "https://api.github.com/repos/$repo/releases/latest"
$supportsColor = -not [Console]::IsOutputRedirected -and $env:TERM -ne 'dumb'

function Write-InstallerText {
    param([string]$Text, [ConsoleColor]$Color = [ConsoleColor]::Gray, [switch]$NoNewline)
    if ($supportsColor) {
        Write-Host $Text -ForegroundColor $Color -NoNewline:$NoNewline
    } else {
        Write-Host $Text -NoNewline:$NoNewline
    }
}

function Write-Title {
    Write-InstallerText '>' Cyan -NoNewline
    Write-Host ' ' -NoNewline
    Write-InstallerText 'TlgMe' White -NoNewline
    Write-Host ' ' -NoNewline
    Write-InstallerText "installer for Windows, $Architecture" Gray
    Write-Host ''
}

function Write-SetupPrompt {
    Write-InstallerText 'Run TlgMe first-time setup now?' White -NoNewline
    Write-Host ' ' -NoNewline
    Write-InstallerText '[Y/n]:' Gray -NoNewline
    return Read-Host
}

function Get-InstallerArchitecture {
    if ($Architecture) { return $Architecture }
    $machine = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch ($machine.ToUpperInvariant()) {
        'ARM64' { return 'arm64' }
        'AMD64' { return 'amd64' }
        default { throw "Unsupported architecture: $machine" }
    }
}

function Get-LatestRelease {
    $release = Invoke-RestMethod -Uri $latestReleaseUrl
    if ($release.draft -or $release.prerelease -or $release.tag_name -notmatch '^v\d+\.\d+\.\d+$') {
        throw 'Could not determine the latest stable TlgMe release.'
    }
    return $release
}

function Get-Checksum {
    param([string]$ChecksumsPath, [string]$AssetName)
    $line = Get-Content -LiteralPath $ChecksumsPath | Where-Object { $_ -match "^([a-fA-F0-9]{64})\s+\*?$([regex]::Escape($AssetName))$" } | Select-Object -First 1
    if (-not $line) { return $null }
    return ($line -split '\s+')[0].ToLowerInvariant()
}

function Add-UserPath {
    param([string]$InstallDir)
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @($current -split ';' | Where-Object { $_ })
    if ($entries | Where-Object { $_.TrimEnd('\\') -ieq $InstallDir.TrimEnd('\\') }) { return }
    [Environment]::SetEnvironmentVariable('Path', (($entries + $InstallDir) -join ';'), 'User')
    $env:Path = "$InstallDir;$env:Path"
}

function Invoke-Preview {
    $latest = if ($Version) { $Version } elseif ($Scenario -eq 'First') { 'v0.1.3' } else { 'v0.1.4' }
    if ($Scenario -eq 'First') {
        Write-InstallerText "Downloading TlgMe $latest..."
        Write-InstallerText 'Verifying download...'
        Write-InstallerText 'Installing TlgMe...'
        Write-InstallerText "TlgMe $latest installed successfully."
        Write-Host ''
        $answer = Write-SetupPrompt
        Write-Host ''
        if ($answer -notmatch '^[Nn]$') {
            Write-InstallerText 'Preview would now launch TlgMe setup.' White
        } else {
            Write-InstallerText 'Run `tlgme` whenever you are ready to finish setup.' White
        }
    } else {
        $installDir = Join-Path $env:LOCALAPPDATA 'tlgme'
        Write-InstallerText "Existing installation found at $(Join-Path $installDir 'tlgme.exe')." White
        Write-InstallerText 'This will replace it with the latest release.' White
        Write-Host ''
        Write-InstallerText "Downloading TlgMe $latest..."
        Write-InstallerText 'Verifying download...'
        Write-InstallerText 'Updating TlgMe...'
        Write-InstallerText "TlgMe updated successfully: v0.1.3 → $latest." White
    }
    Write-Host ''
    Write-InstallerText 'Preview complete. Nothing was downloaded or changed.' Gray
}

function Install-TlgMe {
    $installDir = Join-Path $env:LOCALAPPDATA 'tlgme'
    $binaryPath = Join-Path $installDir 'tlgme.exe'
    $existing = Test-Path -LiteralPath $binaryPath
    $currentVersion = if ($existing) { try { & $binaryPath --version 2>$null | Select-Object -First 1 } catch { $null } }
    if ($currentVersion -match '(\d+\.\d+\.\d+)') {
        $currentVersion = "v$($Matches[1])"
    } else {
        $currentVersion = $null
    }
    if ($existing) {
        Write-InstallerText "Existing installation found at $binaryPath." White
        Write-InstallerText 'This will replace it with the latest release.' White
        Write-Host ''
    }

    $release = Get-LatestRelease
    $version = $release.tag_name
    $assetName = "tlgme_$($version.TrimStart('v'))_windows_$Architecture.zip"
    $asset = $release.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
    $checksums = $release.assets | Where-Object { $_.name -eq 'checksums.txt' } | Select-Object -First 1
    if (-not $asset -or -not $checksums) { throw "Release does not contain $assetName and checksums.txt." }

    $tempDir = Join-Path ([IO.Path]::GetTempPath()) ("tlgme-install-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    try {
        $archivePath = Join-Path $tempDir $assetName
        $checksumsPath = Join-Path $tempDir 'checksums.txt'
        Write-InstallerText "Downloading TlgMe $version..."
        Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $archivePath
        Invoke-WebRequest -Uri $checksums.browser_download_url -OutFile $checksumsPath
        Write-InstallerText 'Verifying download...'
        $expected = Get-Checksum $checksumsPath $assetName
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
        if (-not $expected -or $expected -ne $actual) {
            Write-InstallerText 'Verification failed. The downloaded archive does not match the published SHA-256 checksum.' Red
            Write-InstallerText 'TlgMe was not installed or changed.' Red
            return
        }

        $expanded = Join-Path $tempDir 'expanded'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $expanded -Force
        $source = Join-Path $expanded 'tlgme.exe'
        if (-not (Test-Path -LiteralPath $source)) { throw 'The release archive does not contain tlgme.exe.' }
        if ($existing) {
            Write-InstallerText 'Updating TlgMe...'
        } else {
            Write-InstallerText 'Installing TlgMe...'
        }
        New-Item -ItemType Directory -Force -Path $installDir | Out-Null
        Copy-Item -LiteralPath $source -Destination $binaryPath -Force
        Add-UserPath $installDir
    } finally {
        Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }

    if ($existing) {
        $from = if ($currentVersion) { $currentVersion } else { 'the installed version' }
        Write-InstallerText "TlgMe updated successfully: $from → $version." White
        return
    }
    Write-InstallerText "TlgMe $version installed successfully."
    Write-Host ''
    $answer = Write-SetupPrompt
    Write-Host ''
    if ($answer -notmatch '^[Nn]$') {
        & $binaryPath
    } else {
        Write-InstallerText 'Run `tlgme` whenever you are ready to finish setup.' White
    }
}

Clear-Host
Write-Host ''
Write-Host ''
if (-not $Preview -and ($PSBoundParameters.ContainsKey('Scenario') -or $PSBoundParameters.ContainsKey('Architecture') -or $PSBoundParameters.ContainsKey('Version'))) {
    throw 'Preview-only options require -Preview.'
}
$Architecture = Get-InstallerArchitecture
Write-Title
if ($Preview) {
    Invoke-Preview
} else {
    Install-TlgMe
}
