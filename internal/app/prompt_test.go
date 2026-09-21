package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func TestRunPromptSendsQuestionAndReturnsReply(t *testing.T) {
	var promptQuestion string
	var promptButtons []string
	var reactedChat int64
	var reactedMsg int
	var stdout strings.Builder
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.stdout = &stdout
	app.send = func(_ context.Context, _ string, chatID any, message outgoing) (int, error) {
		if chatID != int64(42) {
			t.Fatalf("unexpected chat %v", chatID)
		}
		promptQuestion, promptButtons = message.text, message.buttons
		return 10, nil
	}
	app.awaitAnswer = func(_ context.Context, _ string, chatID int64, questionMsgID int, buttons []string, _ time.Time, timeout time.Duration) (promptAnswer, error) {
		if chatID != 42 || questionMsgID != 10 || len(buttons) != 0 {
			t.Fatalf("unexpected await chat=%d msg=%d buttons=%v", chatID, questionMsgID, buttons)
		}
		if timeout != promptTimeout {
			t.Fatalf("unexpected timeout %s", timeout)
		}
		return promptAnswer{text: "user answer", replyMsgID: 99}, nil
	}
	var reactedEmoji string
	app.react = func(_ context.Context, _ string, chatID int64, messageID int, emoji string) error {
		reactedChat, reactedMsg, reactedEmoji = chatID, messageID, emoji
		return nil
	}

	if err := app.run(context.Background(), []string{"--text", "pick one", "--prompt"}); err != nil {
		t.Fatal(err)
	}
	if promptQuestion != "pick one" || len(promptButtons) != 0 {
		t.Fatalf("unexpected prompt q=%q buttons=%v", promptQuestion, promptButtons)
	}
	if reactedChat != 42 || reactedMsg != 99 || reactedEmoji != eyesEmoji {
		t.Fatalf("unexpected react chat=%d msg=%d emoji=%q", reactedChat, reactedMsg, reactedEmoji)
	}
	if stdout.String() != "user answer\n" {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestRunPromptWithButtonTap(t *testing.T) {
	var gotButtons []string
	var removeCalled bool
	var callbackID string
	var appendedText string
	var reactedMsg int
	var reactedEmoji string
	var stdout strings.Builder
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.stdout = &stdout
	app.send = func(_ context.Context, _ string, _ any, message outgoing) (int, error) {
		gotButtons = message.buttons
		return 11, nil
	}
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error) {
		return promptAnswer{text: "Yes", callbackID: "cb-1"}, nil
	}
	app.removeKeyboard = func(context.Context, string, int64, int) error {
		removeCalled = true
		return nil
	}
	app.answerCallback = func(_ context.Context, _ string, id string) error {
		callbackID = id
		return nil
	}
	app.appendAnswer = func(_ context.Context, _ string, chatID int64, messageID int, text string, _ bool) error {
		if chatID != 42 || messageID != 11 {
			t.Fatalf("unexpected appendAnswer chat=%d msg=%d", chatID, messageID)
		}
		appendedText = text
		return nil
	}
	app.react = func(_ context.Context, _ string, _ int64, messageID int, emoji string) error {
		reactedMsg, reactedEmoji = messageID, emoji
		return nil
	}

	err := app.run(context.Background(), []string{"--text", "Proceed?", "--prompt", "--button", "Yes", "--button", "No"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotButtons, []string{"Yes", "No"}) {
		t.Fatalf("buttons %v", gotButtons)
	}
	// removeKeyboard is not called on a button tap: appendAnswer already clears the markup.
	if removeCalled {
		t.Fatal("removeKeyboard should not be called on a button tap")
	}
	if callbackID != "cb-1" {
		t.Fatalf("callback %q", callbackID)
	}
	if appendedText != "Proceed?\n\nAnswer: Yes" {
		t.Fatalf("appended text %q", appendedText)
	}
	if reactedMsg != 11 || reactedEmoji != checkmarkEmoji {
		t.Fatalf("reacted msg=%d emoji=%q", reactedMsg, reactedEmoji)
	}
	if stdout.String() != "Yes\n" {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestRunPromptWithAttachmentEditsCaption(t *testing.T) {
	var got outgoing
	var hasAttachment bool
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.send = func(_ context.Context, _ string, _ any, message outgoing) (int, error) {
		got = message
		return 11, nil
	}
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error) {
		return promptAnswer{text: "Yes", callbackID: "cb-1"}, nil
	}
	app.answerCallback = func(context.Context, string, string) error { return nil }
	app.appendAnswer = func(_ context.Context, _ string, _ int64, _ int, _ string, attachment bool) error {
		hasAttachment = attachment
		return nil
	}
	app.react = func(context.Context, string, int64, int, string) error { return nil }

	if err := app.run(context.Background(), []string{
		"--text", "Looks good?", "--image", "data:image/png;base64,iVBORw0KGgo=", "--prompt", "--button", "Yes",
	}); err != nil {
		t.Fatal(err)
	}
	if got.attachment == nil || !hasAttachment {
		t.Fatalf("attachment=%#v hasAttachment=%v", got.attachment, hasAttachment)
	}
}

func TestRunPromptTextReplyWithButtonsRemovesKeyboard(t *testing.T) {
	var removeCalled bool
	var reactedMsg int
	var reactedEmoji string
	var callbackCalled bool
	var appendCalled bool
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.send = func(context.Context, string, any, outgoing) (int, error) { return 5, nil }
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error) {
		return promptAnswer{text: "custom", replyMsgID: 7}, nil
	}
	app.removeKeyboard = func(context.Context, string, int64, int) error {
		removeCalled = true
		return nil
	}
	app.react = func(_ context.Context, _ string, _ int64, messageID int, emoji string) error {
		reactedMsg, reactedEmoji = messageID, emoji
		return nil
	}
	app.answerCallback = func(context.Context, string, string) error {
		callbackCalled = true
		return nil
	}
	app.appendAnswer = func(context.Context, string, int64, int, string, bool) error {
		appendCalled = true
		return nil
	}

	if err := app.run(context.Background(), []string{"--text", "q", "--prompt", "--button", "A"}); err != nil {
		t.Fatal(err)
	}
	if !removeCalled || callbackCalled || appendCalled {
		t.Fatalf("remove=%v callback=%v append=%v", removeCalled, callbackCalled, appendCalled)
	}
	if reactedMsg != 7 || reactedEmoji != eyesEmoji {
		t.Fatalf("reacted msg=%d emoji=%q", reactedMsg, reactedEmoji)
	}
}

func TestRunPromptTimesOutAndNotifiesChat(t *testing.T) {
	var sends []string
	var removeCalled bool
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.send = func(_ context.Context, _ string, _ any, message outgoing) (int, error) {
		if message.buttons != nil {
			return 1, nil
		}
		sends = append(sends, message.text)
		return 1, nil
	}
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error) {
		return promptAnswer{}, errPromptTimeout
	}
	app.removeKeyboard = func(context.Context, string, int64, int) error {
		removeCalled = true
		return nil
	}

	err := app.run(context.Background(), []string{"--text", "hello?", "--prompt", "--button", "Yes"})
	if !errors.Is(err, errPromptTimeout) {
		t.Fatalf("got %v", err)
	}
	if !removeCalled || len(sends) != 1 || sends[0] != timeoutText {
		t.Fatalf("remove=%v sends=%#v", removeCalled, sends)
	}
}

func TestRunPromptTimesOutWithoutButtonsSkipsRemove(t *testing.T) {
	var removeCalled bool
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.send = func(context.Context, string, any, outgoing) (int, error) { return 1, nil }
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error) {
		return promptAnswer{}, errPromptTimeout
	}
	app.removeKeyboard = func(context.Context, string, int64, int) error {
		removeCalled = true
		return nil
	}

	err := app.run(context.Background(), []string{"--text", "hello?", "--prompt"})
	if !errors.Is(err, errPromptTimeout) {
		t.Fatalf("got %v", err)
	}
	if removeCalled {
		t.Fatal("removeKeyboard should not run without buttons")
	}
}

func TestRunPromptRequiresChat(t *testing.T) {
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "@alerts"})
	err := app.run(context.Background(), []string{"--text", "hello?", "--prompt"})
	if err == nil || !strings.Contains(err.Error(), "private/group chat") {
		t.Fatalf("got %v", err)
	}
}

func TestRunPromptRequiresQuestion(t *testing.T) {
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	err := app.run(context.Background(), []string{"--prompt"})
	if err == nil || !strings.Contains(err.Error(), "requires --text") {
		t.Fatalf("got %v", err)
	}
}

func TestRunPromptReactFailureStillReturnsReply(t *testing.T) {
	var stdout strings.Builder
	app := testApplication(map[string]string{botTokenEnv: "secret", chatIDEnv: "42"})
	app.stdout = &stdout
	app.send = func(context.Context, string, any, outgoing) (int, error) { return 1, nil }
	app.awaitAnswer = func(context.Context, string, int64, int, []string, time.Time, time.Duration) (promptAnswer, error) {
		return promptAnswer{text: "ok", replyMsgID: 1}, nil
	}
	app.react = func(context.Context, string, int64, int, string) error {
		return errors.New("reaction failed")
	}

	if err := app.run(context.Background(), []string{"--text", "q", "--prompt"}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "ok\n" {
		t.Fatalf("stdout %q", stdout.String())
	}
}

func TestMatchingReply(t *testing.T) {
	after := time.Unix(100, 0)
	valid := &models.Update{Message: &models.Message{
		Date: 100,
		Text: "answer",
		Chat: models.Chat{ID: 42},
	}}
	if msg := matchingReply(valid, 42, after); msg == nil || msg.Text != "answer" {
		t.Fatal("expected valid reply")
	}

	wrongChat := *valid.Message
	wrongChat.Chat.ID = 1
	stale := *valid.Message
	stale.Date = 99
	empty := *valid.Message
	empty.Text = "   "
	for name, update := range map[string]*models.Update{
		"wrong chat": {Message: &wrongChat},
		"stale":      {Message: &stale},
		"empty":      {Message: &empty},
		"nil":        nil,
	} {
		t.Run(name, func(t *testing.T) {
			if matchingReply(update, 42, after) != nil {
				t.Fatal("unexpected match")
			}
		})
	}
}

func TestMatchingCallback(t *testing.T) {
	buttons := []string{"A", "B", "C"}
	valid := &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   "cb1",
		Data: "opt:1",
		Message: models.MaybeInaccessibleMessage{
			Type: models.MaybeInaccessibleMessageTypeMessage,
			Message: &models.Message{
				ID:   7,
				Chat: models.Chat{ID: 42},
			},
		},
	}}
	if ans := matchingCallback(valid, 42, 7, buttons); ans == nil || ans.text != "B" || ans.callbackID != "cb1" {
		t.Fatalf("got %#v", ans)
	}

	wrongChat := *valid.CallbackQuery
	wrongChat.Message.Message.Chat.ID = 1
	wrongMsg := *valid.CallbackQuery
	wrongMsg.Message.Message.ID = 8
	badData := *valid.CallbackQuery
	badData.Data = "opt:9"
	noButtons := *valid.CallbackQuery
	for name, cq := range map[string]*models.CallbackQuery{
		"nil":        nil,
		"wrong chat": &wrongChat,
		"wrong msg":  &wrongMsg,
		"bad index":  &badData,
		"no buttons": &noButtons,
	} {
		t.Run(name, func(t *testing.T) {
			var update *models.Update
			if cq != nil {
				update = &models.Update{CallbackQuery: cq}
			}
			btn := buttons
			if name == "no buttons" {
				btn = nil
			}
			if matchingCallback(update, 42, 7, btn) != nil {
				t.Fatal("unexpected match")
			}
		})
	}
}

func TestMatchesCheckinReaction(t *testing.T) {
	valid := &models.Update{MessageReaction: &models.MessageReactionUpdated{
		Chat:        models.Chat{ID: 42},
		MessageID:   7,
		NewReaction: []models.ReactionType{{Type: models.ReactionTypeTypeEmoji}},
	}}
	if !matchesCheckinReaction(valid, 42, 7) {
		t.Fatal("expected match")
	}

	wrongChat := *valid.MessageReaction
	wrongChat.Chat.ID = 1
	wrongMsg := *valid.MessageReaction
	wrongMsg.MessageID = 8
	empty := *valid.MessageReaction
	empty.NewReaction = nil
	for name, update := range map[string]*models.Update{
		"nil update":  nil,
		"no reaction": {Message: &models.Message{}},
		"wrong chat":  {MessageReaction: &wrongChat},
		"wrong msg":   {MessageReaction: &wrongMsg},
		"empty":       {MessageReaction: &empty},
	} {
		t.Run(name, func(t *testing.T) {
			if matchesCheckinReaction(update, 42, 7) {
				t.Fatal("unexpected match")
			}
		})
	}
}
