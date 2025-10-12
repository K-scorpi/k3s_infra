FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy && go build -o monitor main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/monitor .
CMD ["./monitor"]
