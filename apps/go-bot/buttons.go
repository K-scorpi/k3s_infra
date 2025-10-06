package main

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	corev1 "k8s.io/api/core/v1"
)

func podsInlineButtons(ns string, pods []corev1.Pod) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, p := range pods {
		row := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Logs", fmt.Sprintf("logs|%s|%s", ns, p.Name)),
			tgbotapi.NewInlineKeyboardButtonData("Restart", fmt.Sprintf("restart|%s|%s", ns, p.Name)),
			tgbotapi.NewInlineKeyboardButtonData("Scale", fmt.Sprintf("scale|%s|%s", ns, p.Name)),
		)
		rows = append(rows, row)
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
