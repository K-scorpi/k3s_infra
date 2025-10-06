package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"strconv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// --- Status ---
func handleStatus(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка получения списка узлов: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString("📡 Узлы:\n")
	for _, n := range nodes.Items {
		ready := "Не готов"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = "Готов"
			}
		}
		sb.WriteString(fmt.Sprintf("- %s — %s\n", n.Name, ready))
	}
	sendLong(bot, chatID, sb.String())
}

// --- Get Pods with Buttons ---
func handleGetPodsWithButtons(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns string) {
	if ns == "" {
		ns = "default"
	}
	pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка получения pod-ов: "+err.Error())
		return
	}
	if len(pods.Items) == 0 {
		sendText(bot, chatID, "В этом namespace нет pod-ов")
		return
	}
	btns := podsInlineButtons(ns, pods.Items)
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("📦 Pod-ы в namespace `%s`:", ns))
	msg.ReplyMarkup = btns
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

// --- Callback handler ---
func handleCallback(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, data string) {
	parts := strings.Split(data, "|")
	if len(parts) < 3 {
		sendText(bot, chatID, "Некорректные данные кнопки")
		return
	}
	action, ns, pod := parts[0], parts[1], parts[2]

	switch action {
	case "logs":
		handleLogs(bot, clientset, ctx, chatID, ns, pod, 200)
	case "restart":
		handleRestart(bot, clientset, ctx, chatID, ns, pod)
	case "scale":
		sendText(bot, chatID, fmt.Sprintf("Введите количество реплик для %s/%s:\n/scale %s %s <replicas>", ns, pod, ns, pod))
	}
}

// --- Logs, Restart, Scale ---
func handleLogs(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, pod string, tail int64) {
	opts := &corev1.PodLogOptions{TailLines: &tail}
	req := clientset.CoreV1().Pods(ns).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		sendText(bot, chatID, "Ошибка получения логов: "+err.Error())
		return
	}
	data, err := io.ReadAll(stream)
	stream.Close()
	if err != nil {
		sendText(bot, chatID, "Ошибка чтения логов: "+err.Error())
		return
	}
	if len(data) == 0 {
		sendText(bot, chatID, "Логи пустые")
		return
	}
	sendLong(bot, chatID, string(data))
}

func handleRestart(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, dep string) {
	now := time.Now().Format(time.RFC3339)
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now))
	_, err := clientset.AppsV1().Deployments(ns).Patch(ctx, dep, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка перезапуска deployment: "+err.Error())
		return
	}
	sendText(bot, chatID, fmt.Sprintf("✅ Deployment %s/%s перезапущен", ns, dep))
}

func handleScale(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, dep, repStr string) {
	rep, err := strconv.Atoi(repStr)
	if err != nil {
		sendText(bot, chatID, "Количество реплик должно быть числом")
		return
	}
	d, err := clientset.AppsV1().Deployments(ns).Get(ctx, dep, metav1.GetOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка получения deployment: "+err.Error())
		return
	}
	r := int32(rep)
	d.Spec.Replicas = &r
	_, err = clientset.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка масштабирования: "+err.Error())
		return
	}
	sendText(bot, chatID, fmt.Sprintf("✅ Deployment %s/%s масштабирован до %d реплик", ns, dep, rep))
}
