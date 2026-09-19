package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type config struct {
	BotToken    string      `json:"bot_token,omitempty"`
	ChatID      *chatTarget `json:"chat_id,omitempty"`
	BotUsername string      `json:"bot_username,omitempty"`
}

type chatTarget struct {
	value any
}

func (c chatTarget) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.value)
}

func (c *chatTarget) UnmarshalJSON(data []byte) error {
	var id int64
	if err := json.Unmarshal(data, &id); err == nil {
		if id == 0 {
			return errors.New("chat ID must not be zero")
		}
		c.value = id
		return nil
	}

	var username string
	if err := json.Unmarshal(data, &username); err != nil {
		return errors.New("chat ID must be an integer or an @channel username")
	}
	target, err := parseChatTarget(username, "chat ID")
	if err != nil {
		return err
	}
	c.value = target.value
	return nil
}

func parseChatTarget(value, source string) (*chatTarget, error) {
	value = strings.TrimSpace(value)
	if id, err := strconv.ParseInt(value, 10, 64); err == nil {
		if id == 0 {
			return nil, fmt.Errorf("%s must not be zero", source)
		}
		return &chatTarget{value: id}, nil
	}
	if strings.HasPrefix(value, "@") && len(value) > 1 && !strings.ContainsAny(value, " \t\r\n") {
		return &chatTarget{value: value}, nil
	}
	return nil, fmt.Errorf("%s must be an integer or an @channel username", source)
}

func (app application) runSet(path string, cfg config, opts cliOptions) error {
	if opts.setToken.set {
		cfg.BotToken = strings.TrimSpace(opts.setToken.value)
	}
	if opts.setChatID.set {
		chatID, err := parseChatTarget(opts.setChatID.value, "--set-chat-id")
		if err != nil {
			return err
		}
		cfg.ChatID = chatID
	}
	if err := saveConfig(path, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if opts.setToken.set && opts.setChatID.set {
		fmt.Fprintln(app.stdout, "Bot token and chat ID saved.")
	} else if opts.setToken.set {
		fmt.Fprintln(app.stdout, "Bot token saved.")
	} else {
		fmt.Fprintln(app.stdout, "Chat ID saved.")
	}
	return nil
}

func defaultConfigPath() (string, error) {
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("find config directory: %w", err)
		}
		return filepath.Join(base, configDirName, configName), nil
	}

	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, configDirName, configName), nil
}

func loadConfig(path string) (config, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return config{}, nil
	}
	if err != nil {
		return config{}, err
	}
	defer file.Close()

	var cfg config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return config{}, err
	}
	cfg.BotToken = strings.TrimSpace(cfg.BotToken)
	return cfg, nil
}

func saveConfig(path string, cfg config) error {
	cfg.BotToken = strings.TrimSpace(cfg.BotToken)
	if cfg.BotToken == "" && cfg.ChatID == nil {
		return errors.New("config is empty")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := json.NewEncoder(tmp).Encode(cfg); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
