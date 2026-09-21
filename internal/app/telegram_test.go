package app

import (
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func TestFreshPrivateStart(t *testing.T) {
	startedAt := time.Unix(100, 0)
	start := &models.Update{Message: &models.Message{
		Date: 100,
		Text: "/start payload",
		Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate},
	}}
	if !isFreshPrivateStart(start, startedAt) {
		t.Fatal("expected fresh private /start to match")
	}

	old := *start.Message
	old.Date = 99
	group := *start.Message
	group.Chat.Type = models.ChatTypeGroup
	other := *start.Message
	other.Text = "hello"
	for name, message := range map[string]*models.Message{"old": &old, "group": &group, "other": &other} {
		t.Run(name, func(t *testing.T) {
			if isFreshPrivateStart(&models.Update{Message: message}, startedAt) {
				t.Fatal("unexpected match")
			}
		})
	}
}

func TestInlineKeyboard(t *testing.T) {
	if inlineKeyboard(nil) != nil {
		t.Fatal("expected nil for empty buttons")
	}
	m := inlineKeyboard([]string{"Yes", "No"})
	if m == nil || len(m.InlineKeyboard) != 1 || len(m.InlineKeyboard[0]) != 2 {
		t.Fatalf("unexpected markup %#v", m)
	}
	if m.InlineKeyboard[0][0].Text != "Yes" || m.InlineKeyboard[0][0].CallbackData != "opt:0" {
		t.Fatalf("first button %#v", m.InlineKeyboard[0][0])
	}
}

func TestSendParamsNotificationDefaults(t *testing.T) {
	attachment := &attachment{file: &models.InputFileString{Data: "https://example.com/file"}}
	for name, silent := range map[string]bool{"audible": false, "silent": true} {
		t.Run(name, func(t *testing.T) {
			message := outgoing{text: "hello", attachment: attachment, silent: silent}
			textParams := newSendMessageParams(int64(42), message)
			if textParams.DisableNotification != silent {
				t.Fatalf("text DisableNotification=%v, want %v", textParams.DisableNotification, silent)
			}
			photoParams := newSendPhotoParams(int64(42), message)
			if photoParams.DisableNotification != silent {
				t.Fatalf("photo DisableNotification=%v, want %v", photoParams.DisableNotification, silent)
			}
			documentParams := newSendDocumentParams(int64(42), message)
			if documentParams.DisableNotification != silent {
				t.Fatalf("document DisableNotification=%v, want %v", documentParams.DisableNotification, silent)
			}
		})
	}
}
