package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
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
	// Получаем метрики узлов (если установлен metrics-server)
	nodeMetrics, err := getNodeMetrics(ctx, clientset)
	if err != nil {
		log.Printf("⚠️ Metrics server не доступен: %v", err)
	}
	// Получаем все поды для подсчета
	pods, _ := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	var sb strings.Builder
	sb.WriteString("🖥️ *СТАТУС КЛАСТЕРА*\n\n")
	totalCPU, totalMemory := int64(0), int64(0)
	usedCPU, usedMemory := int64(0), int64(0)
	readyNodes := 0

	for _, node := range nodes.Items {
		nodeReady, nodeStatus := getNodeStatus(node)
		if nodeReady {
			readyNodes++
		}
		// Ресурсы узла
		capacity := node.Status.Capacity
		nodeCPU := capacity.Cpu().MilliValue()
		nodeMemory := capacity.Memory().Value()
		totalCPU += nodeCPU
		totalMemory += nodeMemory

		// Использование ресурсов
		cpuUsage, memoryUsage := getNodeUsage(node.Name, nodeMetrics, node, pods.Items)
		usedCPU += cpuUsage
		usedMemory += memoryUsage
		// Подсчет подов на узле
		nodePods := countPodsOnNode(pods.Items, node.Name)
		runningPods := countRunningPodsOnNode(pods.Items, node.Name)

		// Вывод информации об узле
		sb.WriteString(fmt.Sprintf("%s *%s*\n", getStatusEmoji(nodeReady), node.Name))
		sb.WriteString(fmt.Sprintf("   📊 Статус: %s\n", nodeStatus))
		sb.WriteString(fmt.Sprintf("   🏷️  OS: %s | Arch: %s\n",
			node.Status.NodeInfo.OperatingSystem,
			node.Status.NodeInfo.Architecture))

		// Использование CPU
		cpuPercent := calculatePercent(cpuUsage, nodeCPU)
		sb.WriteString(fmt.Sprintf("   🔵 CPU: %s/%s (%d%%) %s\n",
			formatCPU(cpuUsage),
			formatCPU(nodeCPU),
			int(cpuPercent),
			getProgressBar(cpuPercent, 8)))

		// Использование Memory
		memoryPercent := calculatePercent(memoryUsage, nodeMemory)
		sb.WriteString(fmt.Sprintf("   🟠 Memory: %s/%s (%d%%) %s\n",
			formatMemory(memoryUsage),
			formatMemory(nodeMemory),
			int(memoryPercent),
			getProgressBar(memoryPercent, 8)))

		// Pods
		sb.WriteString(fmt.Sprintf("   📦 Pods: %d/%d запущено\n", runningPods, nodePods))

		// Внешний IP
		externalIP := getNodeExternalIP(node)
		if externalIP != "" {
			sb.WriteString(fmt.Sprintf("   🌐 IP: %s\n", externalIP))
		}

		// Возраст узла
		age := time.Since(node.CreationTimestamp.Time).Round(time.Hour)
		sb.WriteString(fmt.Sprintf("   ⏰ Возраст: %s\n", formatDuration(age)))

		sb.WriteString("\n")
	}

	// Добавим общую статистику кластера
	sb.WriteString("📈 *ОБЩАЯ СТАТИСТИКА*\n")
	sb.WriteString(fmt.Sprintf("   🖥️  Всего узлов: %d\n", len(nodes.Items)))
	sb.WriteString(fmt.Sprintf("   🟢 Готовых: %d\n", readyNodes))
	sb.WriteString(fmt.Sprintf("   🔴 Не готовых: %d\n", len(nodes.Items)-readyNodes))

	// Общее использование ресурсов
	totalPods := len(pods.Items)
	runningPods := countRunningPods(pods.Items)
	sb.WriteString(fmt.Sprintf("   📦 Pods: %d/%d запущено\n", runningPods, totalPods))

	if totalCPU > 0 && totalMemory > 0 {
		totalCPUPercent := calculatePercent(usedCPU, totalCPU)
		totalMemoryPercent := calculatePercent(usedMemory, totalMemory)

		sb.WriteString(fmt.Sprintf("\n💾 *Использование ресурсов:*\n"))
		sb.WriteString(fmt.Sprintf("   🔵 CPU: %s/%s (%d%%) %s\n",
			formatCPU(usedCPU),
			formatCPU(totalCPU),
			int(totalCPUPercent),
			getProgressBar(totalCPUPercent, 12)))

		sb.WriteString(fmt.Sprintf("   🟠 Memory: %s/%s (%d%%) %s\n",
			formatMemory(usedMemory),
			formatMemory(totalMemory),
			int(totalMemoryPercent),
			getProgressBar(totalMemoryPercent, 12)))
	}

	sendLong(bot, chatID, sb.String())
}

// Вспомогательные функции
func getNodeStatus(node corev1.Node) (bool, string) {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			if cond.Status == corev1.ConditionTrue {
				return true, "Ready"
			}
			return false, "Not Ready"
		}
	}
	return false, "Unknown"
}

func getStatusEmoji(ready bool) string {
	if ready {
		return "🟢"
	}
	return "🔴"
}

func getNodeMetrics(ctx context.Context, clientset *kubernetes.Clientset) (map[string]struct{ CPU, Memory int64 }, error) {
	metrics := make(map[string]struct{ CPU, Memory int64 })

	// Пробуем получить конфигурацию
	config, err := getK8sConfig()
	if err != nil {
		return metrics, err
	}

	// Создаем клиент для метрик
	metricsClient, err := metricsv.NewForConfig(config)
	if err != nil {
		return metrics, err
	}

	nodeMetricsList, err := metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return metrics, err
	}

	for _, metric := range nodeMetricsList.Items {
		metrics[metric.Name] = struct{ CPU, Memory int64 }{
			CPU:    metric.Usage.Cpu().MilliValue(),
			Memory: metric.Usage.Memory().Value(),
		}
	}

	return metrics, nil
}

func getK8sConfig() (*rest.Config, error) {
	// Сначала пробуем in-cluster config (если запущено в поде)
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Если не в кластере, пробуем kubeconfig из файла
	home, _ := os.UserHomeDir()
	kubeconfig := filepath.Join(home, ".kube", "config")
	if _, err := os.Stat(kubeconfig); err == nil {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	return nil, fmt.Errorf("не удалось найти конфигурацию Kubernetes")
}

func getNodeUsage(nodeName string, metrics map[string]struct{ CPU, Memory int64 }, node corev1.Node, pods []corev1.Pod) (int64, int64) {
	// Если есть метрики - используем их
	if metric, exists := metrics[nodeName]; exists {
		return metric.CPU, metric.Memory
	}

	// Если метрик нет, используем приблизительный расчет на основе requests подов
	return calculateUsageFromPods(nodeName, pods)
}

func calculateUsageFromPods(nodeName string, pods []corev1.Pod) (int64, int64) {
	cpuUsage, memoryUsage := int64(0), int64(0)

	for _, pod := range pods {
		if pod.Spec.NodeName == nodeName && pod.Status.Phase == corev1.PodRunning {
			for _, container := range pod.Spec.Containers {
				if container.Resources.Requests != nil {
					cpuUsage += container.Resources.Requests.Cpu().MilliValue()
					memoryUsage += container.Resources.Requests.Memory().Value()
				}
			}
		}
	}

	return cpuUsage, memoryUsage
}

func countPodsOnNode(pods []corev1.Pod, nodeName string) int {
	count := 0
	for _, pod := range pods {
		if pod.Spec.NodeName == nodeName {
			count++
		}
	}
	return count
}

func countRunningPodsOnNode(pods []corev1.Pod, nodeName string) int {
	count := 0
	for _, pod := range pods {
		if pod.Spec.NodeName == nodeName && pod.Status.Phase == corev1.PodRunning {
			count++
		}
	}
	return count
}

func countRunningPods(pods []corev1.Pod) int {
	count := 0
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodRunning {
			count++
		}
	}
	return count
}

func getNodeExternalIP(node corev1.Node) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeExternalIP {
			return addr.Address
		}
	}
	// Если нет внешнего IP, используем внутренний
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	return ""
}

func calculatePercent(used, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

func formatCPU(milliCPU int64) string {
	if milliCPU >= 1000 {
		return fmt.Sprintf("%.1f core", float64(milliCPU)/1000)
	}
	return fmt.Sprintf("%d m", milliCPU)
}

func formatMemory(bytes int64) string {
	const GB = 1024 * 1024 * 1024
	const MB = 1024 * 1024

	if bytes >= GB {
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(GB))
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/float64(MB))
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days > 0 {
		return fmt.Sprintf("%dд", days)
	}
	hours := int(d.Hours())
	if hours > 0 {
		return fmt.Sprintf("%dч", hours)
	}
	return fmt.Sprintf("%dм", int(d.Minutes()))
}

func getProgressBar(percent float64, length int) string {
	filled := int(percent / 100 * float64(length))
	if filled > length {
		filled = length
	}
	empty := length - filled

	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█"
	}
	for i := 0; i < empty; i++ {
		bar += "░"
	}
	return bar
}

func countReadyNodes(nodes []corev1.Node) int {
	count := 0
	for _, node := range nodes {
		for _, c := range node.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				count++
				break
			}
		}
	}
	return count
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
