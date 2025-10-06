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

const MaxMsgLen = 3800 // safety below Telegram limit

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is not set")
	}

	// Optional admin chat id to restrict control (set numeric chat id)
	var adminID int64 = 0
	if v := os.Getenv("TELEGRAM_CHAT_ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			adminID = id
		}
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatalf("telegram bot init: %v", err)
	}
	bot.Debug = false
	log.Printf("Bot authorized: %s", bot.Self.UserName)

	// k8s client (in-cluster)
	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("in-cluster config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("kubernetes client: %v", err)
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
			cmd = update.CallbackQuery.Data

			// v5 workaround: AnswerCallbackQuery через bot.Request
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "Processing...")
			if _, err := bot.Request(callback); err != nil {
				log.Printf("callback error: %v", err)
			}
		}

		if adminID != 0 && chatID != adminID {
			// allow only info commands for non-admins
			if cmd != "help" && cmd != "start" && cmd != "status" && cmd != "getpods" {
				sendText(bot, chatID, "❌ Access denied. Contact admin.")
				continue
			}
		}

		switch cmd {
		case "start", "help":
			sendHelpWithButtons(bot, chatID)

		case "status":
			handleStatus(bot, clientset, ctx, chatID)

		case "getpods":
			ns := strings.TrimSpace(args)
			if ns == "" {
				ns = "default"
			}
			handleGetPods(bot, clientset, ctx, chatID, ns)

		case "logs":
			// usage: /logs <namespace> <pod> [tail]
			parts := strings.Fields(args)
			if len(parts) < 2 {
				sendText(bot, chatID, "Usage: /logs <namespace> <pod> [tail-lines]")
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
			// usage: /restart <namespace> <deployment>
			parts := strings.Fields(args)
			if len(parts) != 2 {
				sendText(bot, chatID, "Usage: /restart <namespace> <deployment>")
				continue
			}
			handleRestart(bot, clientset, ctx, chatID, parts[0], parts[1])

		case "scale":
			// usage: /scale <namespace> <deployment> <replicas>
			parts := strings.Fields(args)
			if len(parts) != 3 {
				sendText(bot, chatID, "Usage: /scale <namespace> <deployment> <replicas>")
				continue
			}
			handleScale(bot, clientset, ctx, chatID, parts[0], parts[1], parts[2])

		default:
			// fallback: show help
			sendText(bot, chatID, "Unknown command. Use /help")
		}
	}
}

// --- Button helpers ---
func sendHelpWithButtons(bot *tgbotapi.BotAPI, chatID int64) {
	text := `Commands:
/help — this help
/status — list nodes
/getpods <namespace> — list pods
/logs <namespace> <pod> [tail] — get pod logs
/restart <namespace> <deployment> — restart deployment
/scale <namespace> <deployment> <replicas> — scale deployment

Quick actions via buttons:`
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Status Nodes", "status"),
			tgbotapi.NewInlineKeyboardButtonData("List Pods", "getpods default"),
		),
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

// --- Handlers ---
func handleStatus(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot, chatID, "Error listing nodes: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString("📡 Nodes:\n")
	for _, n := range nodes.Items {
		ready := "NotReady"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = "Ready"
			}
		}
		sb.WriteString(fmt.Sprintf("- %s — %s\n", n.Name, ready))
	}
	sendLong(bot, chatID, sb.String())
}

func handleGetPods(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns string) {
	pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		sendText(bot, chatID, "Error listing pods: "+err.Error())
		return
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📦 Pods in namespace `%s`:\n", ns))
	for _, p := range pods.Items {
		sb.WriteString(fmt.Sprintf("- %s (%s)\n", p.Name, p.Status.Phase))
	}
	sendLong(bot, chatID, sb.String())
}

func handleLogs(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, pod string, tail int64) {
	opts := &corev1.PodLogOptions{TailLines: &tail}
	req := clientset.CoreV1().Pods(ns).GetLogs(pod, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		sendText(bot, chatID, "Error getting logs: "+err.Error())
		return
	}
	data, err := io.ReadAll(stream)
	stream.Close()
	if err != nil {
		sendText(bot, chatID, "Error reading logs: "+err.Error())
		return
	}
	if len(data) == 0 {
		sendText(bot, chatID, "Logs empty")
		return
	}
	sendLong(bot, chatID, string(data))
}

func handleRestart(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, dep string) {
	now := time.Now().Format(time.RFC3339)
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now))
	_, err := clientset.AppsV1().Deployments(ns).Patch(ctx, dep, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		sendText(bot, chatID, "Error restarting deployment: "+err.Error())
		return
	}
	sendText(bot, chatID, fmt.Sprintf("✅ Deployment %s/%s restarted", ns, dep))
}

func handleScale(bot *tgbotapi.BotAPI, clientset *kubernetes.Clientset, ctx context.Context, chatID int64, ns, dep, repStr string) {
	rep, err := strconv.Atoi(repStr)
	if err != nil {
		sendText(bot, chatID, "Replicas must be a number")
		return
	}
	d, err := clientset.AppsV1().Deployments(ns).Get(ctx, dep, metav1.GetOptions{})
	if err != nil {
		sendText(bot, chatID, "Get deployment: "+err.Error())
		return
	}
	r := int32(rep)
	d.Spec.Replicas = &r
	_, err = clientset.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		sendText(bot, chatID, "Scale error: "+err.Error())
		return
	}
	sendText(bot, chatID, fmt.Sprintf("✅ Scaled %s/%s -> %d replicas", ns, dep, rep))
}

// --- Send helpers ---
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
	// Otherwise send as file
	tmp := "/tmp/log.txt"
	_ = os.WriteFile(tmp, []byte(txt), 0644)
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(tmp))
	doc.Caption = "Output (file)"
	bot.Send(doc)
	_ = os.Remove(tmp)
}
