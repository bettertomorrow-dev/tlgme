package app

import (
	"errors"
	"fmt"
)

// runDryRun reports the fully resolved delivery without creating a Telegram
// client or scheduling the normal background update check.
func (app application) runDryRun(chatID *chatTarget, prompt bool, message outgoing) error {
	if prompt {
		if _, ok := chatID.value.(int64); !ok {
			return inputError(errors.New("--prompt requires a private/group chat, not a channel username"))
		}
	}

	fmt.Fprintln(app.stdout, "dry-run: no network requests made")
	fmt.Fprintf(app.stdout, "target: %v\n", chatID.value)
	if message.attachment == nil {
		fmt.Fprintf(app.stdout, "message-length: %d\n", len(message.text))
		fmt.Fprintln(app.stdout, "attachment-kind: none")
	} else {
		fmt.Fprintf(app.stdout, "caption-length: %d\n", len(message.text))
		kind := "photo"
		if message.asDocument {
			kind = "document"
		}
		fmt.Fprintf(app.stdout, "attachment-kind: %s\n", kind)
		fmt.Fprintf(app.stdout, "filename: %s\n", message.attachment.filename)
		if message.attachment.upload {
			fmt.Fprintf(app.stdout, "byte-size: %d\n", message.attachment.size)
		} else {
			fmt.Fprintln(app.stdout, "byte-size: unknown")
		}
	}
	fmt.Fprintf(app.stdout, "prompt: %t\n", prompt)
	fmt.Fprintf(app.stdout, "button-count: %d\n", len(message.buttons))
	return nil
}
