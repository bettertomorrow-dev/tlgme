package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultConfigPathUsesXDGConfigHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses the native user config directory")
	}

	base := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", "  "+base+"  ")

	got, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, configDirName, configName)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDefaultConfigPathFallsBackToDotConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses the native user config directory")
	}

	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", configDirName, configName)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDefaultConfigPathUsesWindowsConfigDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific path")
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "ignored"))
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, configDirName, configName)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSetFlagsUpdateConfigWithoutNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, config{BotToken: "old-token", ChatID: &chatTarget{value: int64(1)}}); err != nil {
		t.Fatal(err)
	}
	app := testApplication(nil)
	app.configPath = func() (string, error) { return path, nil }

	if err := app.run(context.Background(), []string{"--set-token", "new-token"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "new-token" || cfg.ChatID == nil || cfg.ChatID.value != int64(1) {
		t.Fatalf("config after token update %#v", cfg)
	}

	if err := app.run(context.Background(), []string{"--set-token", "final-token", "--set-chat-id", "@alerts"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "final-token" || cfg.ChatID == nil || cfg.ChatID.value != "@alerts" {
		t.Fatalf("config after combined update %#v", cfg)
	}
}

func TestInvalidSetDoesNotChangeConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, config{BotToken: "old-token", ChatID: &chatTarget{value: int64(1)}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := testApplication(nil)
	app.configPath = func() (string, error) { return path, nil }
	if err := app.run(context.Background(), []string{"--set-token", "new-token", "--set-chat-id", "bad"}); err == nil {
		t.Fatal("expected invalid chat ID")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("config changed: before=%q after=%q", before, after)
	}
}

func TestLoadLegacyAndUsernameConfig(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacyPath, []byte("{\"chat_id\":123}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := loadConfig(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.BotToken != "" || legacy.ChatID == nil || legacy.ChatID.value != int64(123) {
		t.Fatalf("legacy config %#v", legacy)
	}

	usernamePath := filepath.Join(t.TempDir(), "username.json")
	if err := saveConfig(usernamePath, config{ChatID: &chatTarget{value: "@alerts"}}); err != nil {
		t.Fatal(err)
	}
	username, err := loadConfig(usernamePath)
	if err != nil {
		t.Fatal(err)
	}
	if username.ChatID == nil || username.ChatID.value != "@alerts" {
		t.Fatalf("username config %#v", username)
	}
}
