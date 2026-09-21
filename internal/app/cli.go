package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type stringOption struct {
	value string
	set   bool
}

func (o *stringOption) Set(value string) error {
	o.value = value
	o.set = true
	return nil
}

func (o *stringOption) String() string { return o.value }

type stringList []string

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *stringList) String() string { return strings.Join(*s, ",") }

type cliOptions struct {
	text      stringOption
	image     stringOption
	file      stringOption
	filename  stringOption
	token     stringOption
	chatID    stringOption
	setToken  stringOption
	setChatID stringOption
	buttons   stringList
	prompt    bool
	silent    bool
	learn     bool
	help      bool
	version   bool
	update    bool
}

type settings struct {
	token  string
	chatID *chatTarget
}

func parseCLI(args []string) (cliOptions, error) {
	var opts cliOptions
	flags := flag.NewFlagSet("tlgme", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Var(&opts.text, "text", "message or question text")
	flags.Var(&opts.image, "image", "image URL, path, data URI, base64 data, or - for stdin")
	flags.Var(&opts.file, "file", "file URL, path, data URI, base64 data, or - for stdin")
	flags.Var(&opts.filename, "filename", "filename for a base64 or stdin attachment")
	flags.BoolVar(&opts.prompt, "prompt", false, "wait for a Telegram reply")
	flags.BoolVar(&opts.silent, "silent", false, "send without a notification sound")
	flags.Var(&opts.buttons, "button", "prompt button label; repeatable")
	flags.Var(&opts.token, "token", "temporary bot token override")
	flags.Var(&opts.chatID, "chat-id", "temporary chat ID override")
	flags.Var(&opts.setToken, "set-token", "save a bot token without API validation")
	flags.Var(&opts.setChatID, "set-chat-id", "save a chat ID without contacting Telegram")
	flags.BoolVar(&opts.learn, "learn", false, "learn and save a private chat ID from /start")
	flags.BoolVar(&opts.help, "help", false, "show help")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.version, "version", false, "show version")
	flags.BoolVar(&opts.update, "update", false, "update tlgme to the latest release")

	if err := flags.Parse(args); err != nil {
		return cliOptions{}, fmt.Errorf("%w\n\n%s", err, usageText)
	}
	if opts.help {
		if opts.update {
			return cliOptions{}, errors.New("--update cannot be combined with other options")
		}
		if opts.silent {
			return cliOptions{}, errors.New("--silent cannot be combined with --help")
		}
		return opts, nil
	}
	if flags.NArg() != 0 {
		return cliOptions{}, fmt.Errorf("unexpected positional arguments: %s\n\n%s", strings.Join(flags.Args(), " "), usageText)
	}
	if opts.version {
		if opts.text.set || opts.image.set || opts.file.set || opts.filename.set || opts.prompt || opts.silent || len(opts.buttons) > 0 || opts.learn || opts.token.set || opts.chatID.set || opts.setToken.set || opts.setChatID.set || opts.update {
			return cliOptions{}, errors.New("--version cannot be combined with other options")
		}
		return opts, nil
	}
	if opts.update {
		if opts.text.set || opts.image.set || opts.file.set || opts.filename.set || opts.prompt || opts.silent || len(opts.buttons) > 0 || opts.learn || opts.token.set || opts.chatID.set || opts.setToken.set || opts.setChatID.set {
			return cliOptions{}, errors.New("--update cannot be combined with other options")
		}
		return opts, nil
	}

	setMode := opts.setToken.set || opts.setChatID.set
	if setMode {
		if opts.text.set || opts.image.set || opts.file.set || opts.filename.set || opts.prompt || opts.silent || len(opts.buttons) > 0 || opts.learn || opts.token.set || opts.chatID.set {
			return cliOptions{}, errors.New("--set-token and --set-chat-id cannot be combined with action or override flags")
		}
		if opts.setToken.set && strings.TrimSpace(opts.setToken.value) == "" {
			return cliOptions{}, errors.New("--set-token requires a non-empty value")
		}
		if opts.setChatID.set {
			if _, err := parseChatTarget(opts.setChatID.value, "--set-chat-id"); err != nil {
				return cliOptions{}, err
			}
		}
		return opts, nil
	}

	if opts.learn {
		if opts.text.set || opts.image.set || opts.file.set || opts.filename.set || opts.prompt || opts.silent || len(opts.buttons) > 0 {
			return cliOptions{}, errors.New("--learn cannot be combined with --text, --image, --file, --filename, --prompt, or --button")
		}
		if opts.chatID.set {
			return cliOptions{}, errors.New("--learn cannot be combined with --chat-id")
		}
	}
	if opts.text.set && strings.TrimSpace(opts.text.value) == "" {
		return cliOptions{}, errors.New("--text requires a non-empty value")
	}
	if opts.image.set && strings.TrimSpace(opts.image.value) == "" {
		return cliOptions{}, errors.New("--image requires a non-empty value")
	}
	if opts.file.set && strings.TrimSpace(opts.file.value) == "" {
		return cliOptions{}, errors.New("--file requires a non-empty value")
	}
	if opts.image.set && opts.file.set {
		return cliOptions{}, errors.New("--image and --file cannot be combined")
	}
	if opts.filename.set && !opts.image.set && !opts.file.set {
		return cliOptions{}, errors.New("--filename requires --image or --file")
	}
	if opts.prompt && !opts.text.set {
		return cliOptions{}, errors.New("--prompt requires --text")
	}
	if opts.silent && !opts.text.set && !opts.image.set && !opts.file.set {
		return cliOptions{}, errors.New("--silent requires --text, --image, or --file")
	}
	if len(opts.buttons) > 0 && !opts.prompt {
		return cliOptions{}, errors.New("--button requires --prompt")
	}
	for _, button := range opts.buttons {
		if strings.TrimSpace(button) == "" {
			return cliOptions{}, errors.New("--button requires a non-empty label")
		}
	}
	return opts, nil
}

const usageText = `Usage:
  tlgme --text "message"
  tlgme --image SOURCE [--text "caption"]
  tlgme --file SOURCE [--filename NAME] [--text "caption"]
  tlgme --text "question" --prompt [--button LABEL ...]
  tlgme --learn [--token TOKEN]
  tlgme --set-token TOKEN [--set-chat-id ID]
  tlgme --set-chat-id ID

Options:
  --text TEXT        Message or question text.
  --image SOURCE     Send an inline image from a URL, path, data URI, base64 data, or stdin.
  --file SOURCE      Send a file from a URL, path, data URI, base64 data, or stdin.
  --filename NAME    Override an attachment filename.
  --prompt           Wait for a text reply or button tap.
  --silent           Send without a notification sound.
  --button LABEL     Add a prompt button. Repeat for more buttons.
  --token TOKEN      Override the bot token for this invocation.
  --chat-id ID       Override the chat for this invocation.
  --set-token TOKEN  Save a bot token without API validation.
  --set-chat-id ID   Save a numeric chat ID or @username.
  --learn            Replace the saved chat ID after receiving /start.
  --version          Show the installed version.
  --update           Update tlgme to the latest release.
  --help, -h         Show this help.`

func (app application) printHelp() {
	fmt.Fprintln(app.stdout, usageText)
}

func (app application) printVersion() {
	fmt.Fprintf(app.stdout, "tlgme %s\n", version)
}

func (app application) resolveSettings(cfg config, opts cliOptions) (settings, error) {
	token := strings.TrimSpace(app.getenv(botTokenEnv))
	if strings.TrimSpace(cfg.BotToken) != "" {
		token = strings.TrimSpace(cfg.BotToken)
	}
	if opts.token.set {
		token = strings.TrimSpace(opts.token.value)
		if token == "" {
			return settings{}, errors.New("--token requires a non-empty value")
		}
	}

	var chatID *chatTarget
	if opts.chatID.set {
		parsed, err := parseChatTarget(opts.chatID.value, "--chat-id")
		if err != nil {
			return settings{}, err
		}
		chatID = parsed
	} else if cfg.ChatID != nil {
		chatID = cfg.ChatID
	} else if raw := strings.TrimSpace(app.getenv(chatIDEnv)); raw != "" {
		parsed, err := parseChatTarget(raw, chatIDEnv)
		if err != nil {
			return settings{}, err
		}
		chatID = parsed
	}
	return settings{token: token, chatID: chatID}, nil
}

func missingSettings(resolved settings) []string {
	var missing []string
	if resolved.token == "" {
		missing = append(missing, "bot token")
	}
	if resolved.chatID == nil {
		missing = append(missing, "chat ID")
	}
	return missing
}

// notConfigured writes a stable diagnostic to stderr and keeps it from being
// duplicated by Run.
func (app application) notConfigured(missing ...string) error {
	fmt.Fprintf(app.stderr, "tlgme: not configured: missing %s\n", strings.Join(missing, " and "))
	return quietExit(exitMissingConfig)
}
