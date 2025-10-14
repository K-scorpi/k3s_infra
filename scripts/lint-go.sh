#!/bin/bash
set -e
echo "🔍 Запуск проверок качества Go кода..."
# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color
# Функции для логирования
log_info() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

log_project() {
    echo -e "${BLUE}📁 $1${NC}"
}
if [ $# -eq 0 ]; then
    echo "❌ Укажите путь к Go модулю"
    echo "Использование: $0 <path-to-go-module>"
    exit 1
fi

TARGET=$1

# Переходим в директорию с Go модулем
if [ -f "$TARGET" ]; then
    TARGET_DIR=$(dirname "$TARGET")
    cd "$TARGET_DIR"
elif [ -d "$TARGET" ]; then
    cd "$TARGET"
else
    log_error "Цель не найдена: $TARGET"
    exit 1
fi
PROJECT_NAME=$(basename "$PWD")
log_project "Проверка проекта: $PROJECT_NAME"

# Проверяем, что это Go модуль
if [ ! -f "go.mod" ]; then
    log_error "Не найден go.mod в $PWD"
    exit 1
fi

MODULE_NAME=$(grep '^module' go.mod | awk '{print $2}')
log_info "Модуль: $MODULE_NAME"
log_info "Директория: $PWD"

# Проверка форматирования
log_info "Проверка форматирования..."
if ! go fmt ./...; then
    log_error "Ошибка форматирования кода"
    exit 1
fi

# Проверка импортов
log_info "Проверка импортов..."
if command -v goimports >/dev/null 2>&1; then
    UNFORMATTED=$(goimports -l . | grep -v "vendor" || true)
    if [ -n "$UNFORMATTED" ]; then
        log_warning "Найдены файлы с некорректными импортами:"
        echo "$UNFORMATTED"
        log_info "Автоматическое исправление..."
        goimports -w .
    else
        log_info "Импорты в порядке"
    fi
else
    log_warning "goimports не установлен, пропускаем..."
fi
# Проверка синтаксиса
log_info "Проверка синтаксиса..."
if ! go vet ./... 2>&1; then
    log_error "Найдены ошибки в синтаксисе"
    exit 1
fi

# Проверка зависимостей
log_info "Проверка зависимостей..."
if ! go mod tidy; then
    log_error "Ошибка в зависимостях"
    exit 1
fi
if ! go mod verify; then
    log_error "Ошибка верификации зависимостей"
    exit 1
fi

# Статический анализ
log_info "Статический анализ..."
if command -v golangci-lint >/dev/null 2>&1; then
    if ! golangci-lint run --timeout 2m; then
        log_error "Найдены проблемы в статическом анализе"
    fi
else
    log_warning "golangci-lint не установлен, пропускаем..."
fi

# Тестирование сборки
log_info "Тестирование сборки..."
if ! go build -o /tmp/test-build-${PROJECT_NAME} . 2>&1; then
    log_error "Ошибка сборки"
    exit 1
fi

# Запуск тестов (если есть)
log_info "Поиск тестов..."
if [ -n "$(find . -name '*_test.go' -type f | head -1)" ]; then
    log_info "Запуск тестов..."
    if ! go test -v -race ./... 2>&1; then
        log_error "Тесты не пройдены"
        exit 1
    fi
else
    log_warning "Тесты не найдены, пропускаем..."
fi

log_info "🎉 Проект $PROJECT_NAME прошел все проверки!"
echo ""