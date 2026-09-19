# TlgMe

`tlgme` sends plain-text Telegram messages and can wait for replies.

## Install

Install the latest release with Homebrew on macOS or Linux:

```bash
brew install --cask bettertomorrow-dev/tap/tlgme
```

Prebuilt archives for macOS, Linux, and Windows are also available from
[GitHub Releases](https://github.com/bettertomorrow-dev/tlgme/releases). Verify
the installation with:

```bash
tlgme --version
```

## First run

Create a bot through [@BotFather](https://t.me/BotFather) with `/newbot`, then run:

```bash
tlgme
```

The setup wizard is a chat with the tool itself. It greets you with the current
version, asks you to paste the bot token (masked as bullets, never echoed),
and checks it against Telegram. It then shows the bot's username, asks you to
send `/start` to the bot in a private chat, and waits five minutes. When
`/start` arrives, it saves the settings, sends a test message to your chat,
and prints the commands you can use. If `/start` doesn't arrive in time, press
Enter to wait another five minutes or Ctrl+C to give up (exit code 130).

The finished transcript stays in your terminal history, and Ctrl+C works at
any step. If the Telegram API is unreachable, the wizard shows the CLI
commands for setting the token and chat ID directly instead.

On Linux, macOS, and WSL, the config lives at
`$XDG_CONFIG_HOME/tlgme/config.json`, or `~/.config/tlgme/config.json` when
`XDG_CONFIG_HOME` is unset. On native Windows, it lives at
`%AppData%\tlgme\config.json`. The config also stores the bot's username. On
Unix-like systems, the file is created with mode `0600`. Windows uses the
access controls of the user profile.

If either setting is missing, running `tlgme` resumes setup and asks
only for the missing value. A token from `TG_BOT_TOKEN` or `--token` is used
for the session but never written to the config. Once setup is complete,
running the command without arguments shows help.

If `tlgme` is not on your `PATH`, the wizard shows an export line
for zsh, bash, or fish. It never edits shell files.

The wizard needs an interactive terminal to read a missing token. For scripts,
provide `TG_BOT_TOKEN`, `--token`, or save it first with `--set-token`.

Commands that need settings (`--text`, `--prompt`, `--learn`) print
`not configured: missing <what>` to stdout and exit 1 when a required value
is unavailable from flags, environment, or saved config. Scripts and agents
can react to that line instead of parsing stderr.

## Send a message

```bash
tlgme --text "The agent finished the task"
tlgme --text "Tests passed on $HOST"
```

Text must be passed with `--text`. Positional messages are not supported.

## Send an image or file

```bash
tlgme --text "Chart regenerated" --image /tmp/chart.png
tlgme --image https://example.com/build.png
tlgme --text "Coverage report" --file ./coverage.html
tlgme --text "Report" --file - --filename report.pdf < report.pdf
```

`--image` sends an inline Telegram photo. `--file` sends a document without
recompressing it and accepts any file type. Use `--file` for PDFs, logs,
archives, source files, and screenshots whose text must remain sharp.

Both flags accept an HTTP(S) URL, a file path, a base64 `data:` URI, raw
base64, or `-` for stdin. Only one attachment can be sent at a time.
`--filename` overrides the name shown in Telegram. It is mainly useful for
stdin and base64 payloads; paths and URLs supply a name automatically.

`--text` is the attachment caption and is limited to 1024 characters. If an
`--image` upload is over 10 MB or is not an image, the command warns and sends
it as a file instead. Uploads have a 60-second timeout.

## Ask for a reply

Add `--prompt` to send a question and wait for the answer:

```bash
answer=$(tlgme --text "Which environment should I deploy to?" --prompt)
```

The command waits up to five minutes for a plain-text reply and prints the
reply on stdout. The bot reacts with 👀 to the reply.

Add preset answers with repeatable `--button` flags:

```bash
answer=$(tlgme --text "Proceed with deploy?" --prompt \
  --button Yes --button No)
```

A button tap removes the buttons, appends `Answer: <label>` to the question,
reacts with 👍, and prints the label on stdout. A text reply still works when
buttons are present.

An image or file can carry the question. Button answers update its caption:

```bash
answer=$(tlgme --text "Does this look right?" --image /tmp/preview.png \
  --prompt --button Yes --button "Needs changes")
```

About 30 seconds before the deadline, the bot sends a check-in message. React
to it with any emoji to add five minutes. This can repeat. On timeout, the bot
removes the buttons, sends `Request timed out waiting for a reply.`, writes an
error to stderr, and exits nonzero without writing to stdout.

Prompt mode requires a numeric private or group chat ID. Channel usernames are
valid send targets but cannot receive prompts.

## Override or replace settings

Environment variables are the weakest source, saved config overrides them, and
command-line overrides are strongest:

```bash
TG_BOT_TOKEN=temporary TG_CHAT_ID=42 tlgme --text "Hello"
tlgme --token temporary --chat-id 42 --text "Hello"
```

`TG_CHAT_ID` and `--chat-id` accept a nonzero numeric ID or an `@channel`
username.

Save new values without contacting Telegram:

```bash
tlgme --set-token TOKEN
tlgme --set-chat-id 42
tlgme --set-token TOKEN --set-chat-id @alerts
```

These flags update only the fields you provide. `--set-token` checks only that
the value is not empty. `--set-chat-id` checks its format. Passing a token on
the command line can leave it in shell history and the process list.

To replace the saved private chat through Telegram, run:

```bash
tlgme --learn
```

Then send `/start` to the bot. Learning and prompt modes use Telegram long
polling and cannot run while the bot has an active webhook. Press Ctrl+C to
stop waiting.

## Skill

This repository ships the [`tlg` skill](skills/tlg/SKILL.md), which teaches
agents to send a one-line completion ping and ask questions through `tlgme`.
Install it from
[skills/tlg](https://github.com/bettertomorrow-dev/tlgme/tree/main/skills/tlg).

## Releases

Every merge to `main` is released after CI passes. `VERSION` contains the
major and minor version. The release workflow calculates the patch number from
the first-parent history, creates the Git tag and GitHub Release, and updates
the Homebrew cask. Changing `VERSION` from `0.1` to `0.2` makes that merge the
`v0.2.0` release.

Before merging a release-system change, test the package locally with:

```bash
goreleaser release --snapshot --clean
```

The `bettertomorrow-dev/homebrew-tap` repository must exist, and this
repository must have an Actions secret named `TAP_GITHUB_TOKEN`. The token
needs Contents read/write access to the tap repository. Use the Release
workflow's manual trigger to retry a failed publication for a specific commit.
See [Release setup](docs/releases.md) for the one-time GitHub configuration.
