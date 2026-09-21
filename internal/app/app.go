package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	botTokenEnv      = "TG_BOT_TOKEN"
	chatIDEnv        = "TG_CHAT_ID"
	configDirName    = "tlgme"
	configName       = "config.json"
	requestTimeout   = 10 * time.Second
	uploadTimeout    = 60 * time.Second
	probeTimeout     = 5 * time.Second
	pollTimeout      = 30 * time.Second
	promptTimeout    = 5 * time.Minute
	checkinLeadTime  = 30 * time.Second
	checkinExtension = 5 * time.Minute
	checkinText      = "Still there? React to this message to get 5 more minutes."
	timeoutText      = "Request timed out waiting for a reply."
	callbackDataPref = "opt:"
	maxPhotoSize     = 10 << 20
	maxCaptionLength = 1024
	eyesEmoji        = "\U0001F440"
	// checkmarkEmoji acknowledges a button-tap answer. Telegram's setMessageReaction
	// only accepts a fixed emoji set for bots and rejects "\u2705" (✅) with
	// REACTION_INVALID, so thumbs-up is used instead.
	checkmarkEmoji = "\U0001F44D"
)

var (
	errPromptTimeout = errors.New("timed out waiting for a reply")
)

type application struct {
	getenv          func(string) string
	stdin           io.Reader
	stdout          io.Writer
	stderr          io.Writer
	now             func() time.Time
	configPath      func() (string, error)
	validateToken   func(context.Context, string) (string, error)
	checkAPI        func(context.Context) error
	lookPath        func(string) (string, error)
	learn           func(context.Context, string, time.Time) (int64, error)
	send            func(context.Context, string, any, outgoing) (int, error)
	awaitAnswer     func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error)
	answerCallback  func(context.Context, string, string) error
	removeKeyboard  func(context.Context, string, int64, int) error
	appendAnswer    func(context.Context, string, int64, int, string, bool) error
	react           func(context.Context, string, int64, int, string) error
	redactions      *[]string
	updateCachePath func() (string, error)
	latestRelease   func(context.Context) (releaseInfo, error)
	installRelease  func(context.Context, releaseInfo) error
}

// Run executes tlgme with the supplied arguments and process streams.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	redactions := []string{os.Getenv(botTokenEnv)}
	app := application{
		getenv:          os.Getenv,
		stdin:           stdin,
		stdout:          stdout,
		stderr:          stderr,
		now:             time.Now,
		configPath:      defaultConfigPath,
		validateToken:   validateBotToken,
		checkAPI:        checkTelegramAPI,
		lookPath:        exec.LookPath,
		learn:           learnChat,
		send:            sendOutgoing,
		awaitAnswer:     awaitAnswer,
		answerCallback:  answerCallbackQuery,
		removeKeyboard:  removeInlineKeyboard,
		appendAnswer:    appendAnswer,
		react:           react,
		redactions:      &redactions,
		updateCachePath: defaultUpdateCachePath,
		latestRelease:   fetchLatestRelease,
	}
	app.installRelease = newReleaseInstaller(stdout, stderr).install

	if err := app.run(ctx, args); err != nil {
		var silent silentExitError
		if errors.As(err, &silent) {
			return silent.code
		}
		fmt.Fprintf(stderr, "tlgme: %s\n", redactAll(err.Error(), redactions))
		return 1
	}
	return 0
}

func (app application) run(ctx context.Context, args []string) error {
	opts, err := parseCLI(args)
	if err != nil {
		return err
	}
	app.rememberSecret(opts.token.value)
	app.rememberSecret(opts.setToken.value)
	if opts.help {
		defer app.notifyUpdate(ctx)
		app.printHelp()
		return nil
	}
	if opts.update {
		return app.runUpdate(ctx)
	}
	defer app.notifyUpdate(ctx)
	if opts.version {
		app.printVersion()
		return nil
	}

	path, err := app.configPath()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	app.rememberSecret(cfg.BotToken)

	if opts.setToken.set || opts.setChatID.set {
		return app.runSet(path, cfg, opts)
	}

	resolved, err := app.resolveSettings(cfg, opts)
	if err != nil {
		return err
	}

	if opts.learn {
		if resolved.token == "" {
			return app.notConfigured("bot token")
		}
		return app.runLearn(ctx, path, cfg, resolved.token)
	}

	if opts.text.set || opts.image.set || opts.file.set {
		if missing := missingSettings(resolved); len(missing) > 0 {
			return app.notConfigured(missing...)
		}
		message, err := opts.outgoing(app.stdin)
		if err != nil {
			return err
		}
		if message.fallback {
			fmt.Fprintln(app.stderr, "tlgme: --image sent as a file because Telegram photos must be images under 10 MB")
		}
		if opts.prompt {
			return app.runPrompt(ctx, resolved.token, resolved.chatID.value, message, opts.timeout)
		}
		if _, err := app.send(ctx, resolved.token, resolved.chatID.value, message); err != nil {
			return fmt.Errorf("send message: %w", err)
		}
		return nil
	}

	if resolved.token == "" || resolved.chatID == nil {
		return app.runSetup(ctx, path, cfg, resolved)
	}
	app.printHelp()
	return nil
}

func (app application) runSetup(ctx context.Context, path string, cfg config, resolved settings) error {
	program := tea.NewProgram(
		newWizard(app, ctx, path, cfg, resolved),
		tea.WithContext(ctx),
		tea.WithInput(app.stdin),
		tea.WithOutput(app.stdout),
	)
	final, err := program.Run()
	if err != nil {
		if errors.Is(err, tea.ErrProgramKilled) || ctx.Err() != nil {
			return silentExitError{code: 130}
		}
		return fmt.Errorf("run setup: %w", err)
	}
	if w, ok := final.(wizard); ok && w.exit != 0 {
		return silentExitError{code: w.exit}
	}
	return nil
}

func (app application) runLearn(ctx context.Context, path string, cfg config, token string) error {
	if _, err := app.validateToken(ctx, token); err != nil {
		return fmt.Errorf("validate bot token: %w", err)
	}
	fmt.Fprintln(app.stdout, "Send /start to the bot in a private chat. Waiting...")

	chatID, err := app.learn(ctx, token, app.now())
	if err != nil {
		return fmt.Errorf("learn chat ID: %w", err)
	}

	cfg.ChatID = &chatTarget{value: chatID}
	if err := saveConfig(path, cfg); err != nil {
		return fmt.Errorf("save chat ID: %w", err)
	}
	return app.confirmConnection(ctx, token, cfg.ChatID)
}

func (app application) confirmConnection(ctx context.Context, token string, chatID *chatTarget) error {
	if _, err := app.send(ctx, token, chatID.value, outgoing{text: testMessageText}); err != nil {
		return fmt.Errorf("chat ID was saved, but confirmation failed: %w", err)
	}
	fmt.Fprintf(app.stdout, "Connected to chat %v.\n", chatID.value)
	return nil
}

func (app application) rememberSecret(secret string) {
	secret = strings.TrimSpace(secret)
	if secret != "" && app.redactions != nil {
		*app.redactions = append(*app.redactions, secret)
	}
}

func (app application) redact(message string) string {
	if app.redactions == nil {
		return message
	}
	return redactAll(message, *app.redactions)
}

func (app application) warn(message string, err error) {
	stderr := app.stderr
	if stderr == nil {
		stderr = io.Discard
	}
	fmt.Fprintf(stderr, "tlgme: %s: %s\n", message, app.redact(err.Error()))
}

func redact(message, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "[REDACTED]")
}

func redactAll(message string, secrets []string) string {
	for _, secret := range secrets {
		message = redact(message, secret)
	}
	return message
}
