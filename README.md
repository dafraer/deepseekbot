# DeepSeek Telegram Bot

A Telegram chat bot backed by the [DeepSeek API](https://api-docs.deepseek.com/).
Every text message from an allowed user is forwarded to DeepSeek and the reply
is sent back to the chat. All other users are rejected.

## Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy the token.
2. Get an API key at <https://platform.deepseek.com/api_keys>.
3. Copy the env template and fill in your values:

   ```sh
   cp .env.example .env
   ```

4. Put your Telegram user ID into the `allowedUserIDs` whitelist in
   `bot/bot.go` (message [@userinfobot](https://t.me/userinfobot) to find it).

## Run

```sh
go run ./cmd
```

## Build

```sh
go build -o bin/deepseek-telegram-bot ./cmd
```

## Run with Docker Compose

You only need the compose file — the image is pulled from Docker Hub
(`dafraer/deepseekbot`):

```sh
wget https://raw.githubusercontent.com/dafraer/deepseekbot/refs/heads/main/docker-compose.yml
```

Create a `.env` file next to it with your values (see the
[Configuration](#configuration-env) table):

```sh
TELEGRAM_BOT_TOKEN=...
DEEPSEEK_API_KEY=...
OWNER_ID=...
```

Then start the bot:

```sh
docker compose up -d
```

The image tag defaults to `latest`; pin a version with `TAG=v1 docker compose up -d`.
The whitelist is stored in the `bot-data` volume, so it survives container
restarts and upgrades.

## Commands

| Command        | Who           | Description                                            |
|----------------|---------------|--------------------------------------------------------|
| `/model`       | allowed users | Pick `deepseek-v4-flash` or `deepseek-v4-pro` via inline keyboard (per user) |
| `/add <id>`    | owner only    | Add a Telegram user ID to the allowed users            |
| `/remove <id>` | owner only    | Remove a Telegram user ID from the allowed users       |

The whitelist is persisted to `allowed_users.txt` (comma-separated user IDs)
next to the binary. On first start, when the file does not exist yet, it is
seeded from the hardcoded list in `bot/bot.go` plus the owner; after that the
file is the source of truth and survives restarts.

The bot only responds in private chats — messages in groups and channels are
ignored.

## Configuration (.env)

| Variable             | Required | Default                    | Description                       |
|----------------------|----------|----------------------------|-----------------------------------|
| `TELEGRAM_BOT_TOKEN` | yes      | —                          | Bot token from @BotFather         |
| `DEEPSEEK_API_KEY`   | yes      | —                          | DeepSeek API key                  |
| `OWNER_ID`           | yes      | —                          | Owner's Telegram user ID          |
| `DEEPSEEK_BASE_URL`  | no       | `https://api.deepseek.com` | API base URL                      |
| `DEEPSEEK_MODEL`     | no       | `deepseek-v4-flash`        | Default model for new users       |
