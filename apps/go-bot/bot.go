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
	if v := os.Getenv("TELEGRAM_ADMIN_ID"); v != "" {
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

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)
	ctx := context.Background()

	for update := range updates {
		if update.Message == nil {
			continue
		}
		chatID := update.Message.Chat.ID
		//text := update.Message.Text
		cmd := update.Message.Command()
		args := update.Message.CommandArguments()

		// Allow everyone /help and /start; restrict powerful commands if adminID is set
		if adminID != 0 && chatID != adminID {
			// allow only info commands for non-admins
			if cmd != "help" && cmd != "start" && cmd != "status" && cmd != "getpods" {
				bot.Send(tgbotapi.NewMessage(chatID, "Access denied. Contact admin."))
				continue
			}
		}

		switch cmd {
		case "start", "help":
			sendText(bot, chatID, helpText())

		case "status":
			nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				sendText(bot, chatID, "Error listing nodes: "+err.Error())
				continue
			}
			var sb strings.Builder
			sb.WriteString("📡 Nodes:\n")
			for _, n := range nodes.Items {
				ready := "NotReady"
				for _, c := range n.Status.Conditions {
					if c.Type == corev1.NodeReady {
						if c.Status == corev1.ConditionTrue {
							ready = "Ready"
						}
					}
				}
				sb.WriteString(fmt.Sprintf("- %s — %s\n", n.Name, ready))
			}
			sendText(bot, chatID, sb.String())

		case "getpods":
			ns := strings.TrimSpace(args)
			if ns == "" {
				ns = "default"
			}
			pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				sendText(bot, chatID, "Error listing pods: "+err.Error())
				continue
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Pods in namespace %s:\n", ns))
			for _, p := range pods.Items {
				sb.WriteString(fmt.Sprintf("- %s  (%s)\n", p.Name, p.Status.Phase))
			}
			sendLong(bot, chatID, sb.String())

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
			opts := &corev1.PodLogOptions{TailLines: &tail}
			req := clientset.CoreV1().Pods(ns).GetLogs(pod, opts)
			stream, err := req.Stream(ctx)
			if err != nil {
				sendText(bot, chatID, "Error getting logs: "+err.Error())
				continue
			}
			data, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				sendText(bot, chatID, "Error reading logs: "+err.Error())
				continue
			}
			if len(data) == 0 {
				sendText(bot, chatID, "Logs empty")
				continue
			}
			sendLong(bot, chatID, string(data))

		case "restart":
			// usage: /restart <namespace> <deployment>
			parts := strings.Fields(args)
			if len(parts) != 2 {
				sendText(bot, chatID, "Usage: /restart <namespace> <deployment>")
				continue
			}
			ns, dep := parts[0], parts[1]
			now := time.Now().Format(time.RFC3339)
			patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, now))
			_, err := clientset.AppsV1().Deployments(ns).Patch(ctx, dep, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			if err != nil {
				sendText(bot, chatID, "Error restarting deployment: "+err.Error())
				continue
			}
			sendText(bot, chatID, fmt.Sprintf("Deployment %s/%s restarted (annotation restartedAt=%s)", ns, dep, now))

		case "scale":
			// usage: /scale <namespace> <deployment> <replicas>
			parts := strings.Fields(args)
			if len(parts) != 3 {
				sendText(bot, chatID, "Usage: /scale <namespace> <deployment> <replicas>")
				continue
			}
			ns, dep := parts[0], parts[1]
			rep, err := strconv.Atoi(parts[2])
			if err != nil {
				sendText(bot, chatID, "replicas must be a number")
				continue
			}
			d, err := clientset.AppsV1().Deployments(ns).Get(ctx, dep, metav1.GetOptions{})
			if err != nil {
				sendText(bot, chatID, "Get deployment: "+err.Error())
				continue
			}
			r := int32(rep)
			d.Spec.Replicas = &r
			_, err = clientset.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{})
			if err != nil {
				sendText(bot, chatID, "Scale error: "+err.Error())
				continue
			}
			sendText(bot, chatID, fmt.Sprintf("Scaled %s/%s -> %d replicas", ns, dep, rep))

		default:
			// fallback: show help
			sendText(bot, chatID, "Unknown command. Use /help")
		}
	}
}

func helpText() string {
	return `Commands:
/help — this help
/status — list nodes
/getpods <namespace> — list pods in namespace (default: default)
/logs <namespace> <pod> [tail] — get pod logs
/restart <namespace> <deployment> — restart deployment (rollout restart)
/scale <namespace> <deployment> <replicas> — scale deployment`
}

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
