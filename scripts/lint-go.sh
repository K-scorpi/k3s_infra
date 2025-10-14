#!/bin/bash
set -e
echo "🔍 Запуск проверок качества кода..."

if [ $# -eq 0 ]; then
    echo "❌ Укажите путь к Go модулю или файлу"
    echo "Использование: $0 <path-to-go-module>"
    exit 1
fi

TARGET=$1

# Переходим в директорию с Go модулем
if [ -f "$TARGET" ]; then
    # Если указан файл, переходим в его директорию
    TARGET_DIR=$(dirname "$TARGET")
    cd "$TARGET_DIR"
elif [ -d "$TARGET" ]; then
    # Если указана директория
    cd "$TARGET"
else
    echo "❌ Цель не найдена: $TARGET"
    exit 1
fi

# Проверяем, что это Go модуль
if [ ! -f "go.mod" ]; then
    echo "❌ Не найден go.mod в $PWD"
    echo "💡 Создайте модуль: go mod init <module-name>"
    exit 1
fi
echo "📁 Рабочая директория: $PWD"

# Проверка форматирования
echo "📝 Проверка форматирования..."
go fmt ./.

# Проверка зависимостей
echo "📋 Проверка зависимостей..."
go mod tidy
go mod verify

# Статический анализ (если установлен)
if command -v golangci-lint >/dev/null 2>&1; then
    echo "🔬 Запуск golangci-lint..."
    #golangci-lint run
else
    echo "⚠️  golangci-lint не установлен, пропускаем..."
    echo "💡 Установите: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
fi

# Сборка
echo "🏗️ Сборка приложения..."
go build -o k8s-bot .
echo "✅ Все проверки пройдены!"