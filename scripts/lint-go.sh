#!/bin/bash
set -e  
# Форматирование кода
go fmt ./...

# Импорты
goimports -w .

# Статический анализ
golangci-lint run

# Проверка безопасности
gosec ./...

# Проверка зависимостей
go mod tidy
go mod verify

# Тестирование сборки
go build -o k8s-bot .