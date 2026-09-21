package app

import (
	"context"
	"errors"
	"net/http"
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
		bot.WithHTTPClient(timeout, &http.Client{Timeout: timeout}),
	)
}

func sendOutgoing(ctx context.Context, token string, chatID any, message outgoing) (int, error) {
	timeout := requestTimeout
	if message.attachment != nil && message.attachment.upload {
		timeout = uploadTimeout
	}
	client, err := newClient(token, timeout)
	if err != nil {
		return 0, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if message.attachment == nil {
		params := newSendMessageParams(chatID, message)
		msg, err := client.SendMessage(requestCtx, params)
		if err != nil {
			return 0, err
		}
		return msg.ID, nil
	}
	if message.asDocument {
		params := newSendDocumentParams(chatID, message)
		msg, err := client.SendDocument(requestCtx, params)
		if err != nil {
			return 0, err
		}
		return msg.ID, nil
	}
	params := newSendPhotoParams(chatID, message)
	msg, err := client.SendPhoto(requestCtx, params)
	if err != nil {
		return 0, err
	}
	return msg.ID, nil
}

func newSendMessageParams(chatID any, message outgoing) *bot.SendMessageParams {
	params := &bot.SendMessageParams{
		ChatID:              chatID,
		Text:                message.text,
		DisableNotification: message.silent,
	}
	if markup := inlineKeyboard(message.buttons); markup != nil {
		params.ReplyMarkup = markup
	}
	return params
}

func newSendPhotoParams(chatID any, message outgoing) *bot.SendPhotoParams {
	params := &bot.SendPhotoParams{
		ChatID:              chatID,
		Photo:               message.attachment.file,
		Caption:             message.text,
		DisableNotification: message.silent,
	}
	if markup := inlineKeyboard(message.buttons); markup != nil {
		params.ReplyMarkup = markup
	}
	return params
}

func newSendDocumentParams(chatID any, message outgoing) *bot.SendDocumentParams {
	params := &bot.SendDocumentParams{
		ChatID:              chatID,
		Document:            message.attachment.file,
		Caption:             message.text,
		DisableNotification: message.silent,
	}
	if markup := inlineKeyboard(message.buttons); markup != nil {
		params.ReplyMarkup = markup
	}
	return params
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
