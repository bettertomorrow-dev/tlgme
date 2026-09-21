package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type promptAnswer struct {
	text       string
	replyMsgID int
	callbackID string
}

type promptWaitState struct {
	mu               sync.Mutex
	checkinAttempted bool
	checkinSent      bool
	checkinMsgID     int
}

func (app application) runPrompt(ctx context.Context, token string, chatID any, message outgoing, timeout time.Duration) error {
	numericChatID, ok := chatID.(int64)
	if !ok {
		return errors.New("--prompt requires a private/group chat, not a channel username")
	}

	sentAt := app.now()
	msgID, err := app.send(ctx, token, chatID, message)
	if err != nil {
		return fmt.Errorf("send prompt: %w", err)
	}

	ans, err := app.awaitAnswer(ctx, token, numericChatID, msgID, message.buttons, sentAt, timeout)
	if err != nil {
		if len(message.buttons) > 0 {
			if rmErr := app.removeKeyboard(ctx, token, numericChatID, msgID); rmErr != nil {
				app.warn("failed to remove keyboard", rmErr)
			}
		}
		if errors.Is(err, errPromptTimeout) {
			if _, sendErr := app.send(ctx, token, chatID, outgoing{text: timeoutText}); sendErr != nil {
				app.warn("failed to send timeout notice", sendErr)
			}
		}
		return err
	}

	if ans.callbackID != "" {
		if err := app.answerCallback(ctx, token, ans.callbackID); err != nil {
			app.warn("answer callback failed", err)
		}
		answeredText := message.text + "\n\nAnswer: " + ans.text
		if err := app.appendAnswer(ctx, token, numericChatID, msgID, answeredText, message.attachment != nil); err != nil {
			app.warn("failed to record answer on message", err)
		}
		if err := app.react(ctx, token, numericChatID, msgID, checkmarkEmoji); err != nil {
			app.warn("react with checkmark failed", err)
		}
	} else {
		if len(message.buttons) > 0 {
			if err := app.removeKeyboard(ctx, token, numericChatID, msgID); err != nil {
				app.warn("failed to remove keyboard", err)
			}
		}
		if err := app.react(ctx, token, numericChatID, ans.replyMsgID, eyesEmoji); err != nil {
			app.warn("react with eyes failed", err)
		}
	}

	fmt.Fprintln(app.stdout, ans.text)
	return nil
}

func awaitAnswer(ctx context.Context, token string, chatID int64, questionMsgID int, buttons []string, after time.Time, timeout time.Duration) (promptAnswer, error) {
	found := make(chan promptAnswer, 1)
	extend := make(chan struct{}, 1)
	failures := make(chan error, 1)

	state := &promptWaitState{}

	handler := func(_ context.Context, _ *bot.Bot, update *models.Update) {
		if ans := matchingCallback(update, chatID, questionMsgID, buttons); ans != nil {
			select {
			case found <- *ans:
			default:
			}
			return
		}
		if msg := matchingReply(update, chatID, after); msg != nil {
			select {
			case found <- promptAnswer{text: msg.Text, replyMsgID: msg.ID}:
			default:
			}
			return
		}
		state.mu.Lock()
		id, sent := state.checkinMsgID, state.checkinSent
		state.mu.Unlock()
		if sent && matchesCheckinReaction(update, chatID, id) {
			select {
			case extend <- struct{}{}:
			default:
			}
		}
	}
	errorHandler := func(err error) {
		select {
		case failures <- err:
		default:
		}
	}

	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	client, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithDefaultHandler(handler),
		bot.WithErrorsHandler(errorHandler),
		bot.WithAllowedUpdates(bot.AllowedUpdates{
			models.AllowedUpdateMessage,
			models.AllowedUpdateMessageReaction,
			models.AllowedUpdateCallbackQuery,
		}),
		bot.WithHTTPClient(pollTimeout, &http.Client{Timeout: pollTimeout + 5*time.Second}),
	)
	if err != nil {
		return promptAnswer{}, err
	}
	go client.Start(waitCtx)

	return waitForPromptAnswer(ctx, found, failures, extend, after, timeout, checkinLeadTime, state,
		func(checkinCtx context.Context) (int, error) {
			return sendOutgoing(checkinCtx, token, chatID, outgoing{text: checkinText})
		})
}

func waitForPromptAnswer(
	ctx context.Context,
	found <-chan promptAnswer,
	failures <-chan error,
	extend <-chan struct{},
	after time.Time,
	timeout time.Duration,
	checkinLead time.Duration,
	state *promptWaitState,
	sendCheckin func(context.Context) (int, error),
) (promptAnswer, error) {
	deadline := after.Add(timeout)
	checkinEnabled := timeout > checkinLead
	timerDelay := time.Until(deadline)
	if checkinEnabled {
		timerDelay -= checkinLead
	}
	timer := time.NewTimer(timerDelay)
	defer timer.Stop()

	for {
		select {
		case ans := <-found:
			return ans, nil
		case err := <-failures:
			return promptAnswer{}, err
		case <-ctx.Done():
			return promptAnswer{}, ctx.Err()
		case <-extend:
			deadline = time.Now().Add(checkinExtension)
			checkinEnabled = true
			state.mu.Lock()
			state.checkinAttempted = false
			state.checkinSent = false
			state.mu.Unlock()
			resetTimer(timer, time.Until(deadline)-checkinLead)
		case <-timer.C:
			if !checkinEnabled || !time.Now().Before(deadline) {
				return promptAnswer{}, errPromptTimeout
			}
			state.mu.Lock()
			attempted := state.checkinAttempted
			state.mu.Unlock()
			if !attempted {
				state.mu.Lock()
				state.checkinAttempted = true
				state.mu.Unlock()
				checkinCtx, cancel := context.WithDeadline(ctx, deadline)
				id, sendErr := sendCheckin(checkinCtx)
				cancel()
				if sendErr == nil {
					state.mu.Lock()
					state.checkinSent, state.checkinMsgID = true, id
					state.mu.Unlock()
				}
				if !time.Now().Before(deadline) {
					return promptAnswer{}, errPromptTimeout
				}
				resetTimer(timer, time.Until(deadline))
				continue
			}
			return promptAnswer{}, errPromptTimeout
		}
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if d < 0 {
		d = 0
	}
	t.Reset(d)
}

func matchingCallback(update *models.Update, chatID int64, questionMsgID int, buttons []string) *promptAnswer {
	if update == nil || update.CallbackQuery == nil || len(buttons) == 0 {
		return nil
	}
	cq := update.CallbackQuery
	if cq.Message.Message == nil {
		return nil
	}
	msg := cq.Message.Message
	if msg.Chat.ID != chatID || msg.ID != questionMsgID {
		return nil
	}
	if !strings.HasPrefix(cq.Data, callbackDataPref) {
		return nil
	}
	idxStr := strings.TrimPrefix(cq.Data, callbackDataPref)
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx >= len(buttons) {
		return nil
	}
	return &promptAnswer{text: buttons[idx], callbackID: cq.ID}
}

func matchingReply(update *models.Update, chatID int64, after time.Time) *models.Message {
	if update == nil || update.Message == nil {
		return nil
	}
	message := update.Message
	if message.Chat.ID != chatID || int64(message.Date) < after.Unix() {
		return nil
	}
	if strings.TrimSpace(message.Text) == "" {
		return nil
	}
	return message
}

func matchesCheckinReaction(update *models.Update, chatID int64, messageID int) bool {
	if update == nil {
		return false
	}
	r := update.MessageReaction
	if r == nil {
		return false
	}
	return r.Chat.ID == chatID && r.MessageID == messageID && len(r.NewReaction) > 0
}
