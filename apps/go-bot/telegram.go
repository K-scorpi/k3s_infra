package main

import (
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const MaxMsgLen = 3800

func sendText(bot *tgbotapi.BotAPI, chatID int64, txt string) {
	msg := tgbotapi.NewMessage(chatID, txt)
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func sendLong(bot *tgbotapi.BotAPI, chatID int64, txt string) {
	if len(txt) < MaxMsgLen {
		sendText(bot, chatID, "```\n"+txt+"\n```")
		return
	}
	tmp := "/tmp/log.txt"
	_ = os.WriteFile(tmp, []byte(txt), 0644)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(tmp))
	doc.Caption = "Результат (файл)"
	bot.Send(doc)
	_ = os.Remove(tmp)
}

func sendHelpWithButtons(bot *tgbotapi.BotAPI, chatID int64) {
	text := `Команды:
/help — помощь
/status — список узлов
/getpods <namespace> — список pod-ов

Для каждого pod можно выбрать через кнопки: Logs / Restart / Scale`
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Статус узлов", "status"),
			tgbotapi.NewInlineKeyboardButtonData("Список pod-ов", "getpods default"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}
