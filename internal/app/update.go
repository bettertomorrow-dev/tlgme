package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/minio/selfupdate"
)

const (
	noUpdateCheckEnv   = "TLGME_NO_UPDATE_CHECK"
	latestReleaseURL   = "https://api.github.com/repos/bettertomorrow-dev/tlgme/releases/latest"
	updateCacheName    = "update-check.json"
	updateCheckPeriod  = 24 * time.Hour
	updateCheckTimeout = 2 * time.Second
	updateRunTimeout   = 2 * time.Minute
	maxUpdateAssetSize = 100 << 20
	githubAPIVersion   = "2022-11-28"
	homebrewCask       = "bettertomorrow-dev/tap/tlgme"
)

type semanticVersion struct {
	major uint64
	minor uint64
	patch uint64
}

func parseSemanticVersion(value string) (semanticVersion, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semanticVersion{}, fmt.Errorf("invalid version %q", value)
	}
	values := make([]uint64, len(parts))
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semanticVersion{}, fmt.Errorf("invalid version %q", value)
		}
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return semanticVersion{}, fmt.Errorf("invalid version %q", value)
		}
		values[i] = parsed
	}
	return semanticVersion{major: values[0], minor: values[1], patch: values[2]}, nil
}

func (v semanticVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v semanticVersion) compare(other semanticVersion) int {
	left := [...]uint64{v.major, v.minor, v.patch}
	right := [...]uint64{other.major, other.minor, other.patch}
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

type releaseInfo struct {
	Version semanticVersion
	URL     string
	Assets  []releaseAsset
}

type githubRelease struct {
	TagName    string         `json:"tag_name"`
	HTMLURL    string         `json:"html_url"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type updateCache struct {
	LastAttemptAt time.Time `json:"last_attempt_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
	ReleaseURL    string    `json:"release_url,omitempty"`
}

func fetchLatestRelease(ctx context.Context) (releaseInfo, error) {
	return fetchRelease(ctx, http.DefaultClient, latestReleaseURL)
}

func fetchRelease(ctx context.Context, client *http.Client, url string) (releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "tlgme/"+version)

	resp, err := client.Do(req)
	if err != nil {
		return releaseInfo{}, externalError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, externalError(fmt.Errorf("GitHub returned %s", resp.Status))
	}

	var payload githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return releaseInfo{}, fmt.Errorf("decode release: %w", err)
	}
	if payload.Draft || payload.Prerelease {
		return releaseInfo{}, errors.New("latest release is not stable")
	}
	latest, err := parseSemanticVersion(payload.TagName)
	if err != nil {
		return releaseInfo{}, err
	}
	if strings.TrimSpace(payload.HTMLURL) == "" {
		return releaseInfo{}, errors.New("latest release has no URL")
	}
	return releaseInfo{Version: latest, URL: payload.HTMLURL, Assets: payload.Assets}, nil
}

func defaultUpdateCachePath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find cache directory: %w", err)
	}
	return filepath.Join(base, configDirName, updateCacheName), nil
}

func loadUpdateCache(path string) updateCache {
	file, err := os.Open(path)
	if err != nil {
		return updateCache{}
	}
	defer file.Close()
	var cache updateCache
	if err := json.NewDecoder(io.LimitReader(file, 64<<10)).Decode(&cache); err != nil {
		return updateCache{}
	}
	return cache
}

func saveUpdateCache(path string, cache updateCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := json.NewEncoder(tmp).Encode(cache); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	return os.Rename(tmpName, path)
}

func (app application) notifyUpdate(ctx context.Context) {
	if ctx.Err() != nil || version == "dev" || envEnabled(app.getenv(noUpdateCheckEnv)) {
		return
	}
	current, err := parseSemanticVersion(version)
	if err != nil || app.updateCachePath == nil || app.latestRelease == nil {
		return
	}
	path, err := app.updateCachePath()
	if err != nil {
		return
	}
	cache := loadUpdateCache(path)
	now := app.now()
	if cache.LastAttemptAt.IsZero() || now.Sub(cache.LastAttemptAt) >= updateCheckPeriod || now.Before(cache.LastAttemptAt) {
		checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
		latest, fetchErr := app.latestRelease(checkCtx)
		cancel()
		cache.LastAttemptAt = now
		if fetchErr == nil {
			cache.LatestVersion = latest.Version.String()
			cache.ReleaseURL = latest.URL
		}
		_ = saveUpdateCache(path, cache)
	}
	latest, err := parseSemanticVersion(cache.LatestVersion)
	if err != nil || latest.compare(current) <= 0 || strings.TrimSpace(cache.ReleaseURL) == "" {
		return
	}
	renderer := lipgloss.NewRenderer(app.stderr)
	fmt.Fprint(app.stderr, renderUpdateNotice(current, latest, cache.ReleaseURL, renderer))
}

func envEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func renderUpdateNotice(current, latest semanticVersion, url string, renderer *lipgloss.Renderer) string {
	return fmt.Sprintf(
		"TlgMe update available: %s → %s\n%s\n\nRun:\ntlgme --update\n",
		current,
		latest,
		renderHyperlink(renderer, url),
	)
}

func (app application) runUpdate(ctx context.Context) error {
	if version == "dev" {
		return errors.New("--update is unavailable for development builds")
	}
	current, err := parseSemanticVersion(version)
	if err != nil {
		return fmt.Errorf("parse installed version: %w", err)
	}
	if app.latestRelease == nil || app.installRelease == nil {
		return errors.New("updater is unavailable")
	}
	updateCtx, cancel := context.WithTimeout(ctx, updateRunTimeout)
	defer cancel()
	latest, err := app.latestRelease(updateCtx)
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	if latest.Version.compare(current) <= 0 {
		fmt.Fprintf(app.stdout, "TlgMe is up to date (%s).\n", current)
		return nil
	}
	if err := app.installRelease(updateCtx, latest); err != nil {
		return fmt.Errorf("update tlgme: %w", err)
	}
	fmt.Fprintf(app.stdout, "TlgMe updated: %s → %s\n", current, latest.Version)
	return nil
}

type releaseInstaller struct {
	client         *http.Client
	goos           string
	goarch         string
	stdout         io.Writer
	stderr         io.Writer
	executablePath func() (string, error)
	evalSymlinks   func(string) (string, error)
	lookPath       func(string) (string, error)
	runCommand     func(context.Context, string, []string, io.Writer, io.Writer) error
	replace        func(io.Reader, selfupdate.Options) error
}

func newReleaseInstaller(stdout, stderr io.Writer) releaseInstaller {
	return releaseInstaller{
		client:         http.DefaultClient,
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
		stdout:         stdout,
		stderr:         stderr,
		executablePath: os.Executable,
		evalSymlinks:   filepath.EvalSymlinks,
		lookPath:       exec.LookPath,
		runCommand: func(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			return cmd.Run()
		},
		replace: selfupdate.Apply,
	}
}

func (installer releaseInstaller) install(ctx context.Context, release releaseInfo) error {
	executablePath, err := installer.executablePath()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	resolvedPath := executablePath
	if resolved, resolveErr := installer.evalSymlinks(executablePath); resolveErr == nil {
		resolvedPath = resolved
	}
	if isHomebrewExecutable(resolvedPath) {
		brew, err := installer.lookPath("brew")
		if err != nil {
			return errors.New("this installation is managed by Homebrew, but brew is not on PATH")
		}
		if err := installer.runCommand(ctx, brew, []string{"upgrade", "--cask", "--no-ask", homebrewCask}, installer.stdout, installer.stderr); err != nil {
			return fmt.Errorf("run Homebrew upgrade: %w", err)
		}
		return nil
	}

	archive, checksums, err := selectUpdateAssets(release, installer.goos, installer.goarch)
	if err != nil {
		return err
	}
	archiveData, err := installer.download(ctx, archive)
	if err != nil {
		return fmt.Errorf("download %s: %w", archive.Name, err)
	}
	checksumData, err := installer.download(ctx, checksums)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyArchiveChecksum(archive.Name, archiveData, checksumData); err != nil {
		return err
	}
	binary, err := extractUpdateBinary(archive.Name, archiveData, installer.goos)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o755)
	if info, statErr := os.Stat(resolvedPath); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := installer.replace(bytes.NewReader(binary), selfupdate.Options{TargetPath: resolvedPath, TargetMode: mode}); err != nil {
		if rollbackErr := selfupdate.RollbackError(err); rollbackErr != nil {
			return fmt.Errorf("replace executable: %v; rollback failed: %w", err, rollbackErr)
		}
		return fmt.Errorf("replace executable: %w", err)
	}
	return nil
}

func isHomebrewExecutable(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	return strings.Contains(normalized, "/Caskroom/tlgme/")
}

func selectUpdateAssets(release releaseInfo, goos, goarch string) (releaseAsset, releaseAsset, error) {
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	switch goos {
	case "darwin", "linux", "windows":
	default:
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("unsupported operating system %q", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("unsupported architecture %q", goarch)
	}
	wanted := fmt.Sprintf("tlgme_%s_%s_%s%s", release.Version, goos, goarch, extension)
	var archive, checksums releaseAsset
	for _, asset := range release.Assets {
		switch asset.Name {
		case wanted:
			archive = asset
		case "checksums.txt":
			checksums = asset
		}
	}
	if archive.Name == "" {
		return releaseAsset{}, releaseAsset{}, fmt.Errorf("release does not contain %s", wanted)
	}
	if checksums.Name == "" {
		return releaseAsset{}, releaseAsset{}, errors.New("release does not contain checksums.txt")
	}
	return archive, checksums, nil
}

func (installer releaseInstaller) download(ctx context.Context, asset releaseAsset) ([]byte, error) {
	if strings.TrimSpace(asset.DownloadURL) == "" {
		return nil, errors.New("asset has no download URL")
	}
	if asset.Size < 0 || asset.Size > maxUpdateAssetSize {
		return nil, fmt.Errorf("asset size %d exceeds limit", asset.Size)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tlgme/"+version)
	resp, err := installer.client.Do(req)
	if err != nil {
		return nil, externalError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, externalError(fmt.Errorf("server returned %s", resp.Status))
	}
	if resp.ContentLength > maxUpdateAssetSize {
		return nil, fmt.Errorf("asset exceeds %d bytes", maxUpdateAssetSize)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateAssetSize+1))
	if err != nil {
		return nil, externalError(err)
	}
	if len(data) > maxUpdateAssetSize {
		return nil, fmt.Errorf("asset exceeds %d bytes", maxUpdateAssetSize)
	}
	return data, nil
}

func verifyArchiveChecksum(name string, archive, checksums []byte) error {
	var expected []byte
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("invalid checksum for %s", name)
		}
		expected = decoded
		break
	}
	if expected == nil {
		return fmt.Errorf("checksums.txt has no entry for %s", name)
	}
	actual := sha256.Sum256(archive)
	if !bytes.Equal(actual[:], expected) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func extractUpdateBinary(archiveName string, archive []byte, goos string) ([]byte, error) {
	binaryName := "tlgme"
	if goos == "windows" {
		binaryName = "tlgme.exe"
	}
	if strings.HasSuffix(archiveName, ".zip") {
		return extractZipBinary(archive, binaryName)
	}
	if strings.HasSuffix(archiveName, ".tar.gz") {
		return extractTarBinary(archive, binaryName)
	}
	return nil, fmt.Errorf("unsupported release archive %q", archiveName)
}

func extractZipBinary(archive []byte, binaryName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open zip archive: %w", err)
	}
	for _, file := range reader.File {
		if file.Name != binaryName || !file.Mode().IsRegular() {
			continue
		}
		if file.UncompressedSize64 > maxUpdateAssetSize {
			return nil, errors.New("executable exceeds extraction limit")
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, maxUpdateAssetSize+1))
		closeErr := stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(data) > maxUpdateAssetSize {
			return nil, errors.New("executable exceeds extraction limit")
		}
		if len(data) == 0 {
			return nil, errors.New("archive contains an empty executable")
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive does not contain %s", binaryName)
}

func extractTarBinary(archive []byte, binaryName string) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar archive: %w", err)
		}
		if header.Name != binaryName || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) {
			continue
		}
		if header.Size < 0 || header.Size > maxUpdateAssetSize {
			return nil, errors.New("executable exceeds extraction limit")
		}
		data, err := io.ReadAll(io.LimitReader(tarReader, maxUpdateAssetSize+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxUpdateAssetSize {
			return nil, errors.New("executable exceeds extraction limit")
		}
		if len(data) == 0 {
			return nil, errors.New("archive contains an empty executable")
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive does not contain %s", binaryName)
}
