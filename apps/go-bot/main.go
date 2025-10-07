package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const MaxMsgLen = 3800

func main() {
	// Лог только в stdout (корректно для Kubernetes/Docker)
	log.SetOutput(os.Stdout)

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
		log.Fatalf("Ошибка клиента Kubernetes: %v", err)
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
			log.Printf("[MSG] %s: %s %s", update.Message.From.UserName, cmd, args)
		} else if update.CallbackQuery != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
			parts := strings.Fields(update.CallbackQuery.Data)
			if len(parts) > 0 {
				cmd = parts[0]
				if len(parts) > 1 {
					args = strings.Join(parts[1:], " ")
				}
			}
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "✅")
			bot.Request(callback)
			log.Printf("[BTN] %s", update.CallbackQuery.Data)
		}

		// Проверка доступа
		if adminID != 0 && chatID != adminID {
			if cmd != "help" && cmd != "start" && cmd != "status" && cmd != "getpods" {
				sendText(bot, chatID, "❌ Доступ запрещён.")
				continue
			}
		}

		switch cmd {
		case "start", "help":
			sendHelpWithButtons(bot, chatID, clientset, ctx)

		case "status":
			handleStatus(bot, clientset, ctx, chatID)

		case "getpods":
			ns := strings.TrimSpace(args)
			if ns == "" || ns == "all" {
				handleGetAllPods(bot, clientset, ctx, chatID)
			} else {
				handleGetPods(bot, clientset, ctx, chatID, ns)
			}

		case "logs":
			parts := strings.Fields(args)
			if len(parts) < 2 {
				sendText(bot, chatID, "Использование: /logs <namespace> <pod> [кол-во строк]")
				continue
			}
			ns, pod := parts[0], parts[1]
			var tail int64 = 200
			if len(parts) >= 3 {
				if t, err := strconv.Atoi(parts[2]); err == nil {
					tail = int64(t)
				}
			}
			handleLogs(bot, clientset, ctx, chatID, ns, pod, tail)

		case "restart":
			parts := strings.Fields(args)
			if len(parts) != 2 {
				sendText(bot, chatID, "Использование: /restart <namespace> <deployment>")
				continue
			}
			handleRestart(bot, clientset, ctx, chatID, parts[0], parts[1])

		case "scale":
			parts := strings.Fields(args)
			if len(parts) != 3 {
				sendText(bot, chatID, "Использование: /scale <namespace> <deployment> <реплики>")
				continue
			}
			handleScale(bot, clientset, ctx, chatID, parts[0], parts[1], parts[2])

		default:
			sendText(bot, chatID, "Неизвестная команда. /help")
		}
	}
}

// --- Help + кнопки ---
func sendHelpWithButtons(bot *tgbotapi.BotAPI, chatID int64, clientset *kubernetes.Clientset, ctx context.Context) {
	text := `Команды:
/status — список узлов
/getpods [ns|all] — pod-ы
/logs <ns> <pod> [tail] — логи pod-а
/restart <ns> <dep> — перезапуск
/scale <ns> <dep> <replicas> — масштабирование`

	// Соберем список ns для кнопок
	nss, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("Ошибка получения ns: %v", err)
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Статус узлов", "status"),
		tgbotapi.NewInlineKeyboardButtonData("Pod-ы (все)", "getpods all"),
	))
	for _, ns := range nss.Items {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("Pod-ы (%s)", ns.Name), "getpods "+ns.Name),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// --- Handlers ---
func handleStatus(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString("📡 Nodes:\n")
	for _, n := range nodes.Items {
		ready := "❌ NotReady"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = "✅ Ready"
			}
		}
		sb.WriteString(fmt.Sprintf("- %s — %s\n", n.Name, ready))
	}
	sendLong(bot, chatID, sb.String())
}

func handleGetPods(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns string) {
	pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📦 Pod-ы `%s`:\n", ns))
	for _, p := range pods.Items {
		sb.WriteString(fmt.Sprintf("- %s (%s)\n", p.Name, p.Status.Phase))
	}
	sendLong(bot, chatID, sb.String())
}

func handleGetAllPods(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64) {
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString("📦 Pod-ы во всех namespace:\n")
	for _, p := range pods.Items {
		sb.WriteString(fmt.Sprintf("[%s] %s (%s)\n", p.Namespace, p.Name, p.Status.Phase))
	}
	sendLong(bot, chatID, sb.String())
}

func handleLogs(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, pod string, tail int64) {
	opts := &corev1.PodLogOptions{TailLines: &tail}
	req := clientset.CoreV1().Pods(ns).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		sendText(bot, chatID, "Ошибка логов: "+err.Error())
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
		sendText(bot, chatID, "Ошибка: "+err.Error())
		return
	}
	sendText(bot, chatID, fmt.Sprintf("✅ Deployment %s/%s перезапущен", ns, dep))
}

func handleScale(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, dep, repStr string) {
	rep, err := strconv.Atoi(repStr)
	if err != nil {
		sendText(bot, chatID, "Реплики должны быть числом")
		return
	}
	d, err := clientset.AppsV1().Deployments(ns).Get(ctx, dep, metav1.GetOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка: "+err.Error())
		return
	}
	r := int32(rep)
	d.Spec.Replicas = &r
	_, err = clientset.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		sendText(bot, chatID, "Ошибка: "+err.Error())
		return
	}
	sendText(bot, chatID, fmt.Sprintf("✅ Deployment %s/%s → %d реплик", ns, dep, rep))
}

// --- Отправка сообщений ---
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
	tmp := "/tmp/out.txt"
	_ = os.WriteFile(tmp, []byte(txt), 0644)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(tmp))
	doc.Caption = "Результат в файле"
	bot.Send(doc)
	_ = os.Remove(tmp)
}
