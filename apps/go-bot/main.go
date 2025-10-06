package main

import (
	"context"
	"log"
	"os"
	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN не установлен")
	}

	var adminID int64 = 0
	if v := os.Getenv("TELEGRAM_CHAT_ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			adminID = id
		}
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatalf("Ошибка инициализации бота: %v", err)
	}
	log.Printf("Бот авторизован: %s", bot.Self.UserName)

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Ошибка конфигурации Kubernetes: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Ошибка создания клиента Kubernetes: %v", err)
	}

	ctx := context.Background()
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil {
			continue
		}

		var chatID int64
		var cmd, args string

		if update.Message != nil {
			chatID = update.Message.Chat.ID
			cmd = update.Message.Command()
			args = update.Message.CommandArguments()
		} else if update.CallbackQuery != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
			cmd = "callback"
			args = update.CallbackQuery.Data

			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Обработка...")
			if _, err := bot.Request(callback); err != nil {
				log.Printf("Ошибка callback: %v", err)
			}
		}

		if adminID != 0 && chatID != adminID {
			if cmd != "help" && cmd != "start" && cmd != "status" && cmd != "getpods" && cmd != "callback" {
				sendText(bot, chatID, "❌ Доступ запрещён. Свяжитесь с администратором.")
				continue
			}
		}

		switch cmd {
		case "start", "help":
			sendHelpWithButtons(bot, chatID)

		case "status":
			handleStatus(bot, clientset, ctx, chatID)

		case "getpods":
			handleGetPodsWithButtons(bot, clientset, ctx, chatID, args)

		case "callback":
			handleCallback(bot, clientset, ctx, chatID, args)

		default:
			sendText(bot, chatID, "Неизвестная команда. Используйте /help")
		}
	}
}
