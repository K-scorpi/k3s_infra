package main

import (
	"context"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"k8s.io/client-go/kubernetes"
)

type Bot struct {
	API *tgbotapi.BotAPI
}

func handleUpdate(botAPI *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, update tgbotapi.Update, adminID int64) {
	bot := &Bot{API: botAPI}

	if update.Message != nil {
		chatID := update.Message.Chat.ID
		cmd := update.Message.Command()
		args := update.Message.CommandArguments()

		if adminID != 0 && chatID != adminID && cmd != "help" && cmd != "start" && cmd != "status" && cmd != "getpods" {
			sendText(bot.API, chatID, "❌ Доступ запрещён. Свяжитесь с администратором.")
			return
		}

		switch cmd {
		case "start", "help":
			sendHelpWithButtons(bot.API, chatID)
		case "status":
			handleStatus(bot, clientset, ctx, chatID)
		case "getpods":
			ns := strings.TrimSpace(args)
			if ns == "" {
				ns = "default"
			}
			handleGetPodsWithButtons(bot, clientset, ctx, chatID, ns)
		default:
			sendText(bot.API, chatID, "Unknown command. Use /help")
		}

	} else if update.CallbackQuery != nil {
		data := update.CallbackQuery.Data
		chatID := update.CallbackQuery.Message.Chat.ID

		parts := strings.Split(data, "|")
		switch parts[0] {
		case "logs":
			if len(parts) == 3 {
				handleLogs(bot, clientset, ctx, chatID, parts[1], parts[2], 200)
			}
		case "restart":
			if len(parts) == 3 {
				handleRestart(bot, clientset, ctx, chatID, parts[1], parts[2])
			}
		case "scale":
			if len(parts) == 3 {
				replicas, _ := strconv.Atoi(parts[2])
				handleScale(bot, clientset, ctx, chatID, parts[1], parts[2], int32(replicas))
			}
		default:
			sendText(bot.API, chatID, "Некорректные данные кнопки")
		}

		callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Обработка...")
		if _, err := bot.API.Request(callback); err != nil {
			log.Println("callback error:", err)
		}
	}
}
