package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func validateBotToken(ctx context.Context, token string) (string, error) {
	client, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(requestTimeout, &http.Client{Timeout: requestTimeout}),
	)
	if err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	user, err := client.GetMe(requestCtx)
	if err != nil {
		return "", err
	}
	return user.Username, nil
}

// checkTelegramAPI reports whether the Telegram API host is reachable. Any
// HTTP response counts as reachable; only transport errors mean offline.
func checkTelegramAPI(ctx context.Context) error {
	requestCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://api.telegram.org", nil)
	if err != nil {
		return err
	}
	_, err = http.DefaultClient.Do(req)
	return err
}

func isInvalidBotToken(err error) bool {
	return errors.Is(err, bot.ErrorUnauthorized) || errors.Is(err, bot.ErrorNotFound)
}

func inlineKeyboard(buttons []string) *models.InlineKeyboardMarkup {
	if len(buttons) == 0 {
		return nil
	}
	row := make([]models.InlineKeyboardButton, len(buttons))
	for i, label := range buttons {
		row[i] = models.InlineKeyboardButton{
			Text:         label,
			CallbackData: callbackDataPref + strconv.Itoa(i),
		}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
}

func newClient(token string, timeout time.Duration) (*bot.Bot, error) {
	return bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(timeout, telegramHTTPClient(timeout)),
	)
}

type telegramHTTPStatusError struct {
	statusCode int
}

func (e *telegramHTTPStatusError) Error() string {
	return fmt.Sprintf("Telegram returned HTTP %d", e.statusCode)
}

type telegramStatusTransport struct {
	base http.RoundTripper
}

func (t telegramStatusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.StatusCode < http.StatusInternalServerError {
		return resp, err
	}
	resp.Body.Close()
	return nil, &telegramHTTPStatusError{statusCode: resp.StatusCode}
}

func telegramHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: telegramStatusTransport{base: http.DefaultTransport}}
}

func sendOutgoing(ctx context.Context, token string, chatID any, message outgoing) (int, error) {
	timeout := requestTimeout
	if message.attachment != nil && message.attachment.upload {
		timeout = uploadTimeout
	}
	return retryTelegram(ctx, message.retries, timeout, func(requestCtx context.Context) (int, error) {
		client, err := newClient(token, timeout)
		if err != nil {
			return 0, err
		}
		return sendOutgoingAttempt(requestCtx, client, chatID, message)
	}, waitForRetry)
}

func sendOutgoingAttempt(ctx context.Context, client *bot.Bot, chatID any, message outgoing) (int, error) {
	if message.attachment == nil {
		params := &bot.SendMessageParams{ChatID: chatID, Text: message.text}
		if markup := inlineKeyboard(message.buttons); markup != nil {
			params.ReplyMarkup = markup
		}
		msg, err := client.SendMessage(ctx, params)
		if err != nil {
			return 0, err
		}
		return msg.ID, nil
	}
	file := message.attachment.inputFile()
	if message.asDocument {
		params := &bot.SendDocumentParams{ChatID: chatID, Document: file, Caption: message.text}
		if markup := inlineKeyboard(message.buttons); markup != nil {
			params.ReplyMarkup = markup
		}
		msg, err := client.SendDocument(ctx, params)
		if err != nil {
			return 0, err
		}
		return msg.ID, nil
	}
	params := &bot.SendPhotoParams{ChatID: chatID, Photo: file, Caption: message.text}
	if markup := inlineKeyboard(message.buttons); markup != nil {
		params.ReplyMarkup = markup
	}
	msg, err := client.SendPhoto(ctx, params)
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

type retryWait func(context.Context, time.Duration) error

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryTelegram(ctx context.Context, retries int, timeout time.Duration, attempt func(context.Context) (int, error), wait retryWait) (int, error) {
	for attemptNumber := 0; ; attemptNumber++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		result, err := attempt(attemptCtx)
		cancel()
		if err == nil {
			return result, nil
		}
		if attemptNumber >= retries || !isRetryableTelegramError(ctx, err) {
			return 0, err
		}
		if err := wait(ctx, retryDelay(attemptNumber, err)); err != nil {
			return 0, err
		}
	}
}

func isRetryableTelegramError(parent context.Context, err error) bool {
	if parent.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var tooMany *bot.TooManyRequestsError
	if errors.As(err, &tooMany) {
		return true
	}
	var statusErr *telegramHTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.statusCode >= http.StatusInternalServerError && statusErr.statusCode < 600
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}

func retryDelay(attemptNumber int, err error) time.Duration {
	var tooMany *bot.TooManyRequestsError
	if errors.As(err, &tooMany) && tooMany.RetryAfter > 0 {
		return time.Duration(tooMany.RetryAfter) * time.Second
	}
	if attemptNumber >= 5 {
		return 30 * time.Second
	}
	return time.Second << attemptNumber
}

func answerCallbackQuery(ctx context.Context, token, callbackQueryID string) error {
	client, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(requestTimeout, &http.Client{Timeout: requestTimeout}),
	)
	if err != nil {
		return err
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	_, err = client.AnswerCallbackQuery(requestCtx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
	})
	return err
}

func removeInlineKeyboard(ctx context.Context, token string, chatID int64, messageID int) error {
	client, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(requestTimeout, &http.Client{Timeout: requestTimeout}),
	)
	if err != nil {
		return err
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	_, err = client.EditMessageReplyMarkup(requestCtx, &bot.EditMessageReplyMarkupParams{
		ChatID:    chatID,
		MessageID: messageID,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{},
		},
	})
	return err
}

func appendAnswer(ctx context.Context, token string, chatID int64, messageID int, text string, hasAttachment bool) error {
	client, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(requestTimeout, &http.Client{Timeout: requestTimeout}),
	)
	if err != nil {
		return err
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if hasAttachment {
		_, err = client.EditMessageCaption(requestCtx, &bot.EditMessageCaptionParams{
			ChatID: chatID, MessageID: messageID, Caption: text,
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
		})
		return err
	}
	_, err = client.EditMessageText(requestCtx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
	})
	return err
}

func react(ctx context.Context, token string, chatID int64, messageID int, emoji string) error {
	client, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithHTTPClient(requestTimeout, &http.Client{Timeout: requestTimeout}),
	)
	if err != nil {
		return err
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	_, err = client.SetMessageReaction(requestCtx, &bot.SetMessageReactionParams{
		ChatID:    chatID,
		MessageID: messageID,
		Reaction: []models.ReactionType{{
			Type:              models.ReactionTypeTypeEmoji,
			ReactionTypeEmoji: &models.ReactionTypeEmoji{Emoji: emoji},
		}},
	})
	return err
}

func learnChat(ctx context.Context, token string, startedAt time.Time) (int64, error) {
	learnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	found := make(chan int64, 1)
	failures := make(chan error, 1)
	handler := func(_ context.Context, _ *bot.Bot, update *models.Update) {
		if !isFreshPrivateStart(update, startedAt) {
			return
		}
		select {
		case found <- update.Message.Chat.ID:
			cancel()
		default:
		}
	}
	errorHandler := func(err error) {
		select {
		case failures <- err:
			cancel()
		default:
		}
	}

	client, err := bot.New(
		token,
		bot.WithSkipGetMe(),
		bot.WithDefaultHandler(handler),
		bot.WithErrorsHandler(errorHandler),
		bot.WithAllowedUpdates(bot.AllowedUpdates{models.AllowedUpdateMessage}),
		bot.WithHTTPClient(pollTimeout, &http.Client{Timeout: pollTimeout + 5*time.Second}),
	)
	if err != nil {
		return 0, err
	}

	client.Start(learnCtx)

	select {
	case id := <-found:
		return id, nil
	default:
	}
	select {
	case err := <-failures:
		return 0, err
	default:
	}
	return 0, ctx.Err()
}

func isFreshPrivateStart(update *models.Update, startedAt time.Time) bool {
	if update == nil || update.Message == nil {
		return false
	}
	message := update.Message
	if message.Chat.Type != models.ChatTypePrivate || int64(message.Date) < startedAt.Unix() {
		return false
	}
	fields := strings.Fields(message.Text)
	if len(fields) == 0 {
		return false
	}
	command := fields[0]
	return command == "/start"
}
