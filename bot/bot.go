// Package bot wires the Telegram bot to the DeepSeek client: every text
// message from an allowed user is forwarded to DeepSeek and the reply is
// sent back to the chat.
package bot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.uber.org/zap"

	"deepseek-telegram-bot/deepseek"
)

// allowedUserIDs seeds the whitelist on the very first start, before
// allowed_users.txt exists. After that the file is the source of truth and
// the owner manages it with /add and /remove. Find your ID by messaging
// @userinfobot on Telegram.
var allowedUserIDs = map[int64]struct{}{
	2066065712: {},
	977211402:  {},
}

// availableModels are the DeepSeek models selectable via /model.
var availableModels = []string{
	"deepseek-v4-flash",
	"deepseek-v4-pro",
}

// modelCallbackPrefix namespaces inline-keyboard callback data for /model.
const modelCallbackPrefix = "model:"

const notAuthorizedMessage = "Sorry, you are not authorized to use this bot. Contact @dafraer to get access"

// telegramMessageLimit is Telegram's maximum message length, measured in
// UTF-16 code units (astral-plane characters such as emoji count as two).
const telegramMessageLimit = 4096

// deepseekTimeout bounds a single DeepSeek API call.
const deepseekTimeout = 2 * time.Minute

// typingRefreshInterval is how often the "typing..." chat action is resent;
// Telegram clients drop it about five seconds after each send.
const typingRefreshInterval = 4 * time.Second

// Bot is the Telegram bot backed by DeepSeek.
type Bot struct {
	tg      *tgbot.Bot
	ai      *deepseek.Client
	log     *zap.SugaredLogger
	ownerID int64

	mu         sync.RWMutex
	allowed    map[int64]struct{}
	userModels map[int64]string // per-user model override, keyed by user ID
}

// New builds the Telegram bot with the DeepSeek client as its backend.
// ownerID is always allowed and is the only user who may run /add and /remove.
// The whitelist is loaded from allowed_users.txt when it exists, otherwise
// seeded from allowedUserIDs, and is written back so the file always reflects
// the current state.
func New(token string, ownerID int64, ai *deepseek.Client, log *zap.SugaredLogger) (*Bot, error) {
	allowed, fromFile, err := loadAllowedUsers(allowedUsersFile)
	if err != nil {
		return nil, fmt.Errorf("load allowed users: %w", err)
	}
	if !fromFile {
		allowed = make(map[int64]struct{}, len(allowedUserIDs)+1)
		for id := range allowedUserIDs {
			allowed[id] = struct{}{}
		}
	}
	allowed[ownerID] = struct{}{}
	if err := saveAllowedUsers(allowedUsersFile, allowed); err != nil {
		return nil, fmt.Errorf("persist allowed users: %w", err)
	}
	log.Infow("allowed users loaded", "count", len(allowed), "from_file", fromFile)

	b := &Bot{
		ai:         ai,
		log:        log,
		ownerID:    ownerID,
		allowed:    allowed,
		userModels: make(map[int64]string),
	}

	tg, err := tgbot.New(token, tgbot.WithDefaultHandler(b.handleUpdate))
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	b.tg = tg
	return b, nil
}

// Start begins long polling for updates and blocks until ctx is cancelled.
func (b *Bot) Start(ctx context.Context) {
	b.log.Infow("bot started, polling for updates", "owner_id", b.ownerID)
	b.tg.Start(ctx)
	b.log.Infow("bot stopped")
}

func (b *Bot) handleUpdate(ctx context.Context, _ *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery != nil {
		b.handleCallback(ctx, update.CallbackQuery)
		return
	}

	msg := update.Message
	if msg == nil || msg.From == nil || msg.Text == "" {
		return
	}
	// Private chats only: in a group the bot would otherwise answer (and pay
	// DeepSeek for) every message it sees.
	if msg.Chat.Type != models.ChatTypePrivate {
		return
	}

	cmd, args := parseCommand(msg.Text)
	if cmd == "/start" {
		b.send(ctx, msg.Chat.ID, "Hello, Im free deepseek bot for cheating")
		return
	}

	userID := msg.From.ID
	chatID := msg.Chat.ID
	log := b.log.With("user_id", userID, "chat_id", chatID, "username", msg.From.Username)

	if !b.isAllowed(userID) {
		log.Warnw("rejected message from unauthorized user")
		b.send(ctx, chatID, notAuthorizedMessage)
		return
	}

	switch cmd {
	case "":
		b.handleChat(ctx, log, chatID, userID, msg.Text)
	case "/add", "/remove":
		b.handleUserManagement(ctx, log, chatID, userID, cmd, args)
	case "/model":
		b.sendModelKeyboard(ctx, chatID, userID)
	default:
		b.send(ctx, chatID, "Unknown command. Use /model to choose a model, or just send a message to chat.")
	}
}

// handleChat forwards the user's message to DeepSeek and delivers the reply.
func (b *Bot) handleChat(ctx context.Context, log *zap.SugaredLogger, chatID, userID int64, text string) {
	log.Infow("received message", "length", len(text))

	stopTyping := b.keepTyping(ctx, chatID)
	defer stopTyping()

	aiCtx, cancel := context.WithTimeout(ctx, deepseekTimeout)
	defer cancel()

	model := b.modelFor(userID)
	reply, err := b.ai.Chat(aiCtx, model, text)
	stopTyping()
	if err != nil {
		log.Errorw("deepseek request failed", "model", model, "error", err)
		b.send(ctx, chatID, "Sorry, I could not get a reply from DeepSeek. Please try again.")
		return
	}

	chunks := splitMessage(reply, telegramMessageLimit)
	log.Infow("sending reply", "model", model, "length", len(reply), "chunks", len(chunks))
	for i, chunk := range chunks {
		if err := b.send(ctx, chatID, chunk); err != nil {
			log.Errorw("reply delivery aborted", "failed_chunk", i+1, "chunks", len(chunks), "error", err)
			b.send(ctx, chatID, "Sorry, part of the reply could not be delivered. Please try again.")
			return
		}
	}
}

// keepTyping shows the "typing..." chat action and keeps refreshing it until
// the returned stop function is called (safe to call more than once).
func (b *Bot) keepTyping(ctx context.Context, chatID int64) context.CancelFunc {
	typingCtx, stop := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(typingRefreshInterval)
		defer ticker.Stop()
		for {
			if _, err := b.tg.SendChatAction(typingCtx, &tgbot.SendChatActionParams{
				ChatID: chatID,
				Action: models.ChatActionTyping,
			}); err != nil && typingCtx.Err() == nil {
				b.log.Debugw("failed to send typing action", "chat_id", chatID, "error", err)
			}
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return stop
}

// handleUserManagement processes the owner-only /add and /remove commands
// that edit the whitelist and persist it to allowed_users.txt.
func (b *Bot) handleUserManagement(ctx context.Context, log *zap.SugaredLogger, chatID, userID int64, cmd string, args []string) {
	if userID != b.ownerID {
		log.Warnw("non-owner tried to manage users", "command", cmd)
		b.send(ctx, chatID, "Only the bot owner can manage allowed users.")
		return
	}
	if len(args) != 1 {
		b.send(ctx, chatID, fmt.Sprintf("Usage: %s <telegram_user_id>", cmd))
		return
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		b.send(ctx, chatID, fmt.Sprintf("%q is not a valid Telegram user ID (must be a positive number).", args[0]))
		return
	}

	if cmd == "/remove" && id == b.ownerID {
		b.send(ctx, chatID, "The owner cannot be removed.")
		return
	}

	b.mu.Lock()
	_, exists := b.allowed[id]
	changed := false
	if cmd == "/add" && !exists {
		b.allowed[id] = struct{}{}
		changed = true
	}
	if cmd == "/remove" && exists {
		delete(b.allowed, id)
		delete(b.userModels, id)
		changed = true
	}
	var saveErr error
	if changed {
		saveErr = saveAllowedUsers(allowedUsersFile, b.allowed)
	}
	b.mu.Unlock()

	if saveErr != nil {
		log.Errorw("failed to persist allowed users", "error", saveErr)
		b.send(ctx, chatID, "Warning: the change is active but could not be saved to disk, so it will be lost on restart.")
	}

	switch {
	case cmd == "/add" && exists:
		b.send(ctx, chatID, fmt.Sprintf("User %d is already allowed.", id))
	case cmd == "/add":
		log.Infow("user added to whitelist", "added_id", id)
		b.send(ctx, chatID, fmt.Sprintf("User %d added to allowed users.", id))
	case cmd == "/remove" && !exists:
		b.send(ctx, chatID, fmt.Sprintf("User %d is not in the allowed users.", id))
	case cmd == "/remove":
		log.Infow("user removed from whitelist", "removed_id", id)
		b.send(ctx, chatID, fmt.Sprintf("User %d removed from allowed users.", id))
	}
}

// sendModelKeyboard shows an inline keyboard with the selectable models,
// marking the user's current one. If the current model (e.g. a custom
// DEEPSEEK_MODEL default) is not in availableModels it gets its own row, so
// the user can always switch back to it.
func (b *Bot) sendModelKeyboard(ctx context.Context, chatID, userID int64) {
	current := b.modelFor(userID)

	selectable := availableModels
	if !slices.Contains(selectable, current) {
		selectable = append([]string{current}, selectable...)
	}

	rows := make([][]models.InlineKeyboardButton, 0, len(selectable))
	for _, m := range selectable {
		label := m
		if m == current {
			label = "✅ " + m
		}
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: label, CallbackData: modelCallbackPrefix + m},
		})
	}

	if _, err := b.tg.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:      chatID,
		Text:        fmt.Sprintf("Current model: %s\nChoose a model:", current),
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: rows},
	}); err != nil {
		b.log.Errorw("failed to send model keyboard", "chat_id", chatID, "error", err)
	}
}

// handleCallback processes inline-keyboard presses (currently only model
// selection). Unauthorized users get the same rejection as in chat.
func (b *Bot) handleCallback(ctx context.Context, cb *models.CallbackQuery) {
	userID := cb.From.ID
	log := b.log.With("user_id", userID, "callback_data", cb.Data)

	// Always answer the callback so the client stops showing a spinner.
	answer := func(text string) {
		if _, err := b.tg.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: cb.ID,
			Text:            text,
		}); err != nil {
			log.Errorw("failed to answer callback query", "error", err)
		}
	}

	if !b.isAllowed(userID) {
		log.Warnw("rejected callback from unauthorized user")
		answer(notAuthorizedMessage)
		return
	}

	model, ok := strings.CutPrefix(cb.Data, modelCallbackPrefix)
	if !ok || !b.isSelectableModel(model) {
		log.Warnw("ignoring unknown callback data")
		answer("Unknown action.")
		return
	}

	b.mu.Lock()
	b.userModels[userID] = model
	b.mu.Unlock()

	log.Infow("model selected", "model", model)
	answer("Model set to " + model)

	// Replace the keyboard message with a confirmation, if it is accessible.
	if m := cb.Message.Message; m != nil {
		if _, err := b.tg.EditMessageText(ctx, &tgbot.EditMessageTextParams{
			ChatID:    m.Chat.ID,
			MessageID: m.ID,
			Text:      "Model set to " + model,
		}); err != nil {
			log.Errorw("failed to edit model keyboard message", "error", err)
		}
	}
}

// isAllowed reports whether the user is on the whitelist.
func (b *Bot) isAllowed(userID int64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.allowed[userID]
	return ok
}

// modelFor returns the user's selected model, falling back to the client's
// default.
func (b *Bot) modelFor(userID int64) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if m, ok := b.userModels[userID]; ok {
		return m
	}
	return b.ai.Model()
}

// isSelectableModel reports whether the model may be chosen via the keyboard:
// one of availableModels or the configured default.
func (b *Bot) isSelectableModel(model string) bool {
	return slices.Contains(availableModels, model) || model == b.ai.Model()
}

// parseCommand splits a message into a bot command and its arguments. It
// returns an empty command when the message is not a command. A "@botname"
// suffix on the command is stripped.
func parseCommand(text string) (string, []string) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", nil
	}
	cmd, _, _ := strings.Cut(fields[0], "@")
	return cmd, fields[1:]
}

// send delivers a single message to the chat. When Telegram rate-limits the
// request it waits the advised period and retries once. The error is logged
// and returned so callers may react or ignore it.
func (b *Bot) send(ctx context.Context, chatID int64, text string) error {
	params := &tgbot.SendMessageParams{ChatID: chatID, Text: text}
	_, err := b.tg.SendMessage(ctx, params)

	var tooMany *tgbot.TooManyRequestsError
	if errors.As(err, &tooMany) {
		wait := min(max(time.Duration(tooMany.RetryAfter)*time.Second, time.Second), 30*time.Second)
		b.log.Warnw("telegram rate limit hit, retrying", "chat_id", chatID, "wait", wait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		_, err = b.tg.SendMessage(ctx, params)
	}

	if err != nil {
		b.log.Errorw("failed to send telegram message", "chat_id", chatID, "error", err)
	}
	return err
}

// splitMessage breaks text into chunks of at most limit UTF-16 code units —
// the unit Telegram's length cap is measured in — preferring to split on
// newlines, then spaces, so chunks stay readable. Whitespace-only chunks are
// dropped because Telegram rejects empty message text.
func splitMessage(text string, limit int) []string {
	runes := []rune(text)
	var chunks []string

	appendChunk := func(rs []rune) {
		if chunk := string(rs); strings.TrimSpace(chunk) != "" {
			chunks = append(chunks, chunk)
		}
	}

	for len(runes) > 0 {
		units := 0
		fit := 0 // number of leading runes that fit within limit
		lastNewline, lastSpace := 0, 0
		for i, r := range runes {
			u := 1
			if r > 0xFFFF {
				u = 2 // astral-plane runes encode as a UTF-16 surrogate pair
			}
			if units+u > limit {
				break
			}
			units += u
			fit = i + 1
			if units > limit/2 { // only consider split points past halfway
				switch r {
				case '\n':
					lastNewline = fit
				case ' ':
					lastSpace = fit
				}
			}
		}
		if fit == len(runes) {
			appendChunk(runes)
			break
		}
		cut := fit
		if lastNewline > 0 {
			cut = lastNewline
		} else if lastSpace > 0 {
			cut = lastSpace
		}
		appendChunk(runes[:cut])
		runes = runes[cut:]
	}
	return chunks
}
