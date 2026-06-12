// Command main runs the DeepSeek-backed Telegram bot.
package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"deepseek-telegram-bot/bot"
	"deepseek-telegram-bot/deepseek"
)

func main() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer logger.Sync() //nolint:errcheck // best-effort flush on shutdown
	log := logger.Sugar()

	if err := godotenv.Load(); err != nil {
		log.Warnw("no .env file loaded, using process environment", "error", err)
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatalw("TELEGRAM_BOT_TOKEN is not set")
	}
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatalw("DEEPSEEK_API_KEY is not set")
	}
	ownerID, err := strconv.ParseInt(os.Getenv("OWNER_ID"), 10, 64)
	if err != nil {
		log.Fatalw("OWNER_ID is not set or not a valid Telegram user ID", "error", err)
	}
	baseURL := os.Getenv("DEEPSEEK_BASE_URL") // optional, defaults inside the client
	model := os.Getenv("DEEPSEEK_MODEL")      // optional, defaults inside the client

	ai := deepseek.New(apiKey, baseURL, model)

	b, err := bot.New(botToken, ownerID, ai, log)
	if err != nil {
		log.Fatalw("failed to create bot", "error", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	b.Start(ctx)
}
