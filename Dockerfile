# --- Build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache dependencies separately from source changes
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/deepseek-telegram-bot ./cmd

# --- Runtime stage ---
FROM scratch

# TLS roots for the Telegram and DeepSeek HTTPS APIs
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/deepseek-telegram-bot /usr/local/bin/deepseek-telegram-bot

# allowed_users.txt is read and written in the working directory;
# mount a volume here to persist the whitelist across containers
WORKDIR /data

ENTRYPOINT ["/usr/local/bin/deepseek-telegram-bot"]
