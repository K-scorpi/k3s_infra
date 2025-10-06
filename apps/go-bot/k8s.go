package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

func handleStatus(bot *Bot, clientset *kubernetes.Clientset, ctx context.Context, chatID int64) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot.API, chatID, "Ошибка получения списка узлов: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString("📡 Узлы:\n")
	for _, n := range nodes.Items {
		status := "NotReady"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				status = "Ready"
			}
		}
		sb.WriteString(fmt.Sprintf("- %s — %s\n", n.Name, status))
	}
	sendLong(bot.API, chatID, sb.String())
}

func handleGetPodsWithButtons(bot *Bot, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns string) {
	pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil || len(pods.Items) == 0 {
		sendText(bot.API, chatID, "В этом namespace нет pod-ов")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📦 Pod-ы в namespace `%s`:\n", ns))
	for _, p := range pods.Items {
		sb.WriteString(fmt.Sprintf("- %s (%s)\n", p.Name, p.Status.Phase))
		// Кнопки для каждого pod
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Logs", "logs|"+ns+"|"+p.Name),
				tgbotapi.NewInlineKeyboardButtonData("Restart", "restart|"+ns+"|"+p.Name),
				tgbotapi.NewInlineKeyboardButtonData("Scale", "scale|"+ns+"|"+p.Name),
			),
		)
		msg := tgbotapi.NewMessage(chatID, "Действия для pod "+p.Name)
		msg.ReplyMarkup = keyboard
		bot.API.Send(msg)
	}
	sendLong(bot.API, chatID, sb.String())
}

func handleLogs(bot *Bot, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, pod string, tail int64) {
	opts := &corev1.PodLogOptions{TailLines: &tail}
	req := clientset.CoreV1().Pods(ns).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		sendText(bot.API, chatID, "Ошибка получения логов: "+err.Error())
		return
	}
	defer stream.Close()
	data, _ := io.ReadAll(stream)
	if len(data) == 0 {
		sendText(bot.API, chatID, "Логи пустые")
		return
	}
	sendLong(bot.API, chatID, string(data))
}

func handleRestart(bot *Bot, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, dep string) {
	now := time.Now().Format(time.RFC3339)
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now))
	_, err := clientset.AppsV1().Deployments(ns).Patch(ctx, dep, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		sendText(bot.API, chatID, "Ошибка перезапуска deployment: "+err.Error())
		return
	}
	sendText(bot.API, chatID, fmt.Sprintf("✅ Deployment %s/%s перезапущен", ns, dep))
}

func handleScale(bot *Bot, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, dep string, replicas int32) {
	d, err := clientset.AppsV1().Deployments(ns).Get(ctx, dep, metav1.GetOptions{})
	if err != nil {
		sendText(bot.API, chatID, "Ошибка получения deployment: "+err.Error())
		return
	}
	d.Spec.Replicas = &replicas
	_, err = clientset.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		sendText(bot.API, chatID, "Ошибка масштабирования: "+err.Error())
		return
	}
	sendText(bot.API, chatID, fmt.Sprintf("✅ Deployment %s/%s масштабирован до %d реплик", ns, dep, replicas))
}
