package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minio/selfupdate"
)

func TestParseSemanticVersion(t *testing.T) {
	for _, value := range []string{"1", "1.2", "1.2.3.4", "1.02.3", "v1.2.3-beta", "dev"} {
		if _, err := parseSemanticVersion(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
	one, err := parseSemanticVersion("v1.2.3")
	if err != nil || one.String() != "1.2.3" {
		t.Fatalf("version=%v error=%v", one, err)
	}
	two, err := parseSemanticVersion("1.3.0")
	if err != nil || one.compare(two) >= 0 || two.compare(one) <= 0 || one.compare(one) != 0 {
		t.Fatalf("unexpected comparison one=%v two=%v error=%v", one, two, err)
	}
}

func TestFetchRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Fatalf("missing GitHub headers: %#v", r.Header)
		}
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName: "v1.2.3",
			HTMLURL: "https://example.test/releases/v1.2.3",
			Assets:  []releaseAsset{{Name: "checksums.txt", DownloadURL: "https://example.test/checksums"}},
		})
	}))
	defer server.Close()

	release, err := fetchRelease(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if release.Version.String() != "1.2.3" || release.URL != "https://example.test/releases/v1.2.3" || len(release.Assets) != 1 {
		t.Fatalf("unexpected release %#v", release)
	}
}

func TestFetchReleaseRejectsPrerelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName:    "v1.2.3",
			HTMLURL:    "https://example.test/releases/v1.2.3",
			Prerelease: true,
		})
	}))
	defer server.Close()
	if _, err := fetchRelease(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("prerelease was accepted")
	}
}

func TestUpdateNoticeRunsAfterSendAndUsesCache(t *testing.T) {
	restoreVersion := setVersionForTest("0.1.2")
	defer restoreVersion()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	var stderr strings.Builder
	var stdout strings.Builder
	var events []string
	fetches := 0
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.stdout = &stdout
	app.stderr = &stderr
	app.updateCachePath = func() (string, error) { return cachePath, nil }
	app.send = func(context.Context, string, any, outgoing) (int, error) {
		events = append(events, "send")
		return 1, nil
	}
	app.latestRelease = func(context.Context) (releaseInfo, error) {
		events = append(events, "check")
		fetches++
		return testRelease("0.1.3"), nil
	}

	if err := app.run(context.Background(), []string{"--text", "done"}); err != nil {
		t.Fatal(err)
	}
	if err := app.run(context.Background(), []string{"--text", "again"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"send", "check", "send"}) {
		t.Fatalf("events=%v", events)
	}
	if fetches != 1 {
		t.Fatalf("fetches=%d", fetches)
	}
	want := "TlgMe update available: 0.1.2 → 0.1.3\nhttps://example.test/releases/v0.1.3\n\nRun:\ntlgme --update\n"
	if stderr.String() != want+want {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if strings.Contains(stderr.String(), "\x1b") {
		t.Fatalf("redirected notice contains escapes: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("notice polluted stdout: %q", stdout.String())
	}
}

func TestPromptAnswerStaysAloneOnStdout(t *testing.T) {
	restoreVersion := setVersionForTest("0.1.2")
	defer restoreVersion()

	var stdout, stderr strings.Builder
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.stdout = &stdout
	app.stderr = &stderr
	app.updateCachePath = func() (string, error) {
		return filepath.Join(t.TempDir(), "update-check.json"), nil
	}
	app.latestRelease = func(context.Context) (releaseInfo, error) { return testRelease("0.1.3"), nil }
	app.send = func(context.Context, string, any, outgoing) (int, error) { return 7, nil }
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, bool) (promptAnswer, error) {
		return promptAnswer{text: "approved", replyMsgID: 8}, nil
	}
	app.react = func(context.Context, string, int64, int, string) error { return nil }

	if err := app.run(context.Background(), []string{"--text", "Proceed?", "--prompt"}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "approved\n" {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "TlgMe update available: 0.1.2 → 0.1.3") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestUpdateNoticeFailureIsSilentAndCached(t *testing.T) {
	restoreVersion := setVersionForTest("0.1.2")
	defer restoreVersion()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	var stderr strings.Builder
	fetches := 0
	app := testApplication(nil)
	app.stderr = &stderr
	app.updateCachePath = func() (string, error) { return cachePath, nil }
	app.latestRelease = func(context.Context) (releaseInfo, error) {
		fetches++
		return releaseInfo{}, errors.New("offline")
	}

	app.notifyUpdate(context.Background())
	app.notifyUpdate(context.Background())
	if fetches != 1 || stderr.Len() != 0 {
		t.Fatalf("fetches=%d stderr=%q", fetches, stderr.String())
	}
	if cache := loadUpdateCache(cachePath); cache.LastAttemptAt.IsZero() {
		t.Fatal("failed attempt was not cached")
	}
}

func TestMalformedUpdateCacheIsAMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-check.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cache := loadUpdateCache(path); !cache.LastAttemptAt.IsZero() || cache.LatestVersion != "" {
		t.Fatalf("cache=%#v", cache)
	}
}

func TestUpdateNoticeCanBeDisabled(t *testing.T) {
	restoreVersion := setVersionForTest("0.1.2")
	defer restoreVersion()

	app := testApplication(map[string]string{noUpdateCheckEnv: "1"})
	app.latestRelease = func(context.Context) (releaseInfo, error) {
		t.Fatal("update check should be disabled")
		return releaseInfo{}, nil
	}
	app.notifyUpdate(context.Background())
}

func TestRenderUpdateNoticeStylesOnlyURL(t *testing.T) {
	current, _ := parseSemanticVersion("0.1.2")
	latest, _ := parseSemanticVersion("0.1.3")
	url := "https://example.test/releases/v0.1.3"
	got := renderUpdateNotice(current, latest, url, styledRenderer())
	if !strings.Contains(got, "0.1.2 → 0.1.3") || !strings.Contains(got, "\x1b]8;;"+url+"\x1b\\") {
		t.Fatalf("notice=%q", got)
	}
	if !strings.Contains(got, "\x1b[3") || !strings.Contains(got, "96m") {
		t.Fatalf("URL is not italic bright cyan: %q", got)
	}
	if strings.Contains(strings.Split(got, "\n")[0], "\x1b") || strings.Contains(strings.Split(got, "\n")[3], "\x1b") {
		t.Fatalf("text outside URL is styled: %q", got)
	}
}

func TestRunUpdate(t *testing.T) {
	restoreVersion := setVersionForTest("0.1.2")
	defer restoreVersion()

	var stdout strings.Builder
	installed := false
	app := testApplication(nil)
	app.stdout = &stdout
	app.latestRelease = func(context.Context) (releaseInfo, error) { return testRelease("0.1.3"), nil }
	app.installRelease = func(_ context.Context, release releaseInfo) error {
		installed = release.Version.String() == "0.1.3"
		return nil
	}
	if err := app.run(context.Background(), []string{"--update"}); err != nil {
		t.Fatal(err)
	}
	if !installed || stdout.String() != "TlgMe updated: 0.1.2 → 0.1.3\n" {
		t.Fatalf("installed=%v stdout=%q", installed, stdout.String())
	}
}

func TestRunUpdateIsNoOpWhenCurrent(t *testing.T) {
	restoreVersion := setVersionForTest("0.1.3")
	defer restoreVersion()

	var stdout strings.Builder
	app := testApplication(nil)
	app.stdout = &stdout
	app.latestRelease = func(context.Context) (releaseInfo, error) { return testRelease("0.1.3"), nil }
	app.installRelease = func(context.Context, releaseInfo) error {
		t.Fatal("up-to-date binary should not be replaced")
		return nil
	}
	if err := app.run(context.Background(), []string{"--update"}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "TlgMe is up to date (0.1.3).\n" {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestDevelopmentBuildDoesNotCheckOrUpdate(t *testing.T) {
	app := testApplication(nil)
	app.latestRelease = func(context.Context) (releaseInfo, error) {
		t.Fatal("development build should not check for releases")
		return releaseInfo{}, nil
	}
	app.notifyUpdate(context.Background())
	if err := app.run(context.Background(), []string{"--update"}); err == nil || !strings.Contains(err.Error(), "development builds") {
		t.Fatalf("error=%v", err)
	}
}

func TestHomebrewInstallDelegatesToBrew(t *testing.T) {
	var gotName string
	var gotArgs []string
	installer := newReleaseInstaller(io.Discard, io.Discard)
	installer.executablePath = func() (string, error) { return "/opt/homebrew/bin/tlgme", nil }
	installer.evalSymlinks = func(string) (string, error) {
		return "/opt/homebrew/Caskroom/tlgme/0.1.2/tlgme", nil
	}
	installer.lookPath = func(name string) (string, error) { return "/opt/homebrew/bin/" + name, nil }
	installer.runCommand = func(_ context.Context, name string, args []string, _, _ io.Writer) error {
		gotName, gotArgs = name, args
		return nil
	}
	installer.replace = func(io.Reader, selfupdate.Options) error {
		t.Fatal("Homebrew installation should not be replaced directly")
		return nil
	}
	if err := installer.install(context.Background(), testRelease("0.1.3")); err != nil {
		t.Fatal(err)
	}
	if gotName != "/opt/homebrew/bin/brew" || !reflect.DeepEqual(gotArgs, []string{"upgrade", "--cask", "--no-ask", homebrewCask}) {
		t.Fatalf("command=%q args=%v", gotName, gotArgs)
	}
}

func TestDirectInstallDownloadsVerifiesAndReplaces(t *testing.T) {
	binary := []byte("new-binary")
	archive := makeTarGzip(t, "tlgme", binary)
	archiveSum := sha256.Sum256(archive)
	archiveName := "tlgme_0.1.3_linux_amd64.tar.gz"
	checksums := []byte(fmt.Sprintf("%x  %s\n", archiveSum, archiveName))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/archive":
			_, _ = w.Write(archive)
		case "/checksums":
			_, _ = w.Write(checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	release := testRelease("0.1.3")
	release.Assets = []releaseAsset{
		{Name: archiveName, DownloadURL: server.URL + "/archive", Size: int64(len(archive))},
		{Name: "checksums.txt", DownloadURL: server.URL + "/checksums", Size: int64(len(checksums))},
	}
	installer := newReleaseInstaller(io.Discard, io.Discard)
	installer.client = server.Client()
	installer.goos = "linux"
	installer.goarch = "amd64"
	installer.executablePath = func() (string, error) { return "/tmp/tlgme", nil }
	installer.evalSymlinks = func(path string) (string, error) { return path, nil }
	var replaced []byte
	var target string
	installer.replace = func(reader io.Reader, options selfupdate.Options) error {
		var err error
		replaced, err = io.ReadAll(reader)
		target = options.TargetPath
		return err
	}

	if err := installer.install(context.Background(), release); err != nil {
		t.Fatal(err)
	}
	if string(replaced) != string(binary) || target != "/tmp/tlgme" {
		t.Fatalf("replaced=%q target=%q", replaced, target)
	}
}

func TestSelectVerifyAndExtractUpdateAssets(t *testing.T) {
	tarData := makeTarGzip(t, "tlgme", []byte("unix-binary"))
	zipData := makeZip(t, "tlgme.exe", []byte("windows-binary"))
	release := testRelease("0.1.3")
	release.Assets = []releaseAsset{
		{Name: "checksums.txt", DownloadURL: "https://example.test/checksums"},
		{Name: "tlgme_0.1.3_linux_arm64.tar.gz", DownloadURL: "https://example.test/linux"},
		{Name: "tlgme_0.1.3_windows_amd64.zip", DownloadURL: "https://example.test/windows"},
	}

	linux, _, err := selectUpdateAssets(release, "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	linuxSum := sha256.Sum256(tarData)
	checksums := []byte(fmt.Sprintf("%x  %s\n", linuxSum, linux.Name))
	if err := verifyArchiveChecksum(linux.Name, tarData, checksums); err != nil {
		t.Fatal(err)
	}
	got, err := extractUpdateBinary(linux.Name, tarData, "linux")
	if err != nil || string(got) != "unix-binary" {
		t.Fatalf("binary=%q error=%v", got, err)
	}

	windows, _, err := selectUpdateAssets(release, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	got, err = extractUpdateBinary(windows.Name, zipData, "windows")
	if err != nil || string(got) != "windows-binary" {
		t.Fatalf("binary=%q error=%v", got, err)
	}
	if err := verifyArchiveChecksum(linux.Name, []byte("tampered"), checksums); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("error=%v", err)
	}
}

func testRelease(value string) releaseInfo {
	parsed, err := parseSemanticVersion(value)
	if err != nil {
		panic(err)
	}
	return releaseInfo{Version: parsed, URL: "https://example.test/releases/v" + parsed.String()}
}

func setVersionForTest(value string) func() {
	previous := version
	version = value
	return func() { version = previous }
}

func makeTarGzip(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	gzipWriter := gzip.NewWriter(&result)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func makeZip(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	zipWriter := zip.NewWriter(&result)
	file, err := zipWriter.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestUpdateCacheRefreshesAfterOneDay(t *testing.T) {
	restoreVersion := setVersionForTest("0.1.2")
	defer restoreVersion()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	start := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	app := testApplication(nil)
	app.now = func() time.Time { return start }
	app.updateCachePath = func() (string, error) { return cachePath, nil }
	fetches := 0
	app.latestRelease = func(context.Context) (releaseInfo, error) {
		fetches++
		return testRelease("0.1.3"), nil
	}
	app.notifyUpdate(context.Background())
	app.now = func() time.Time { return start.Add(23 * time.Hour) }
	app.notifyUpdate(context.Background())
	app.now = func() time.Time { return start.Add(24 * time.Hour) }
	app.notifyUpdate(context.Background())
	if fetches != 2 {
		t.Fatalf("fetches=%d", fetches)
	}
}
