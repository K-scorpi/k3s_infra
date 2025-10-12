FROM golang:1.24 AS builder
WORKDIR /src
# Копируем модули и скачиваем зависимости
COPY go.mod go.sum main.go ./
RUN go mod download
# Сборка бинарника
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/bot .
FROM alpine:3.20
# Базовые утилиты
RUN apk add --no-cache ca-certificates tzdata
# Копируем собранный бинарник
COPY --from=builder /bin/bot /bin/bot
# Права обычного пользователя
USER 1000:1000
# Точка входа
ENTRYPOINT ["/bin/bot"]
