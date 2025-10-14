package main

import (
	"context"
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeStatus представляет статус узла
type NodeStatus struct {
	Name     string
	Status   string
	LastSeen time.Time
	Notified bool
}

// Monitor сервис для мониторинга узлов
type Monitor struct {
	clientset *kubernetes.Clientset
	bot       *tgbotapi.BotAPI
	adminID   int64
	nodes     map[string]*NodeStatus
}

// NewMonitor создает новый монитор
func NewMonitor(clientset *kubernetes.Clientset, bot *tgbotapi.BotAPI, adminID int64) *Monitor {
	return &Monitor{
		clientset: clientset,
		bot:       bot,
		adminID:   adminID,
		nodes:     make(map[string]*NodeStatus),
	}
}

// Start запускает мониторинг
func (m *Monitor) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute) // Проверка каждую минуту
	defer ticker.Stop()

	log.Println("🚀 Запуск мониторинга узлов...")

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Остановка мониторинга...")
			return
		case <-ticker.C:
			m.checkNodes(ctx)
		}
	}
}

// checkNodes проверяет статус всех узлов
func (m *Monitor) checkNodes(ctx context.Context) {
	nodes, err := m.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("❌ Ошибка получения узлов для мониторинга: %v", err)
		return
	}

	now := time.Now()
	currentNodes := make(map[string]bool)

	// Проверяем текущие узлы
	for _, node := range nodes.Items {
		nodeName := node.Name
		currentNodes[nodeName] = true

		isReady := false
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				isReady = true
				break
			}
		}

		// Обновляем или создаем статус узла
		if status, exists := m.nodes[nodeName]; exists {
			if isReady {
				// Узел в норме
				status.Status = "Ready"
				status.LastSeen = now
				if status.Notified {
					m.sendRecoveryNotification(nodeName)
					status.Notified = false
				}
			} else {
				// Узел не готов
				status.Status = "NotReady"
				duration := now.Sub(status.LastSeen)
				if duration >= 10*time.Minute && !status.Notified {
					m.sendAlertNotification(nodeName, duration)
					status.Notified = true
				}
			}
		} else {
			// Новый узел
			m.nodes[nodeName] = &NodeStatus{
				Name:     nodeName,
				Status:   "Ready",
				LastSeen: now,
				Notified: false,
			}
			if !isReady {
				m.nodes[nodeName].Status = "NotReady"
			}
		}
	}

	// Проверяем отсутствующие узлы
	for nodeName, status := range m.nodes {
		if !currentNodes[nodeName] {
			duration := now.Sub(status.LastSeen)
			if duration >= 10*time.Minute && !status.Notified {
				m.sendNodeMissingNotification(nodeName, duration)
				status.Notified = true
			}
		}
	}
}

// sendAlertNotification отправляет уведомление о проблеме с узлом
func (m *Monitor) sendAlertNotification(nodeName string, duration time.Duration) {
	message := fmt.Sprintf("🚨 *ALERT: Node Down*\n\n"+
		"🔧 *Node:* `%s`\n"+
		"⏰ *Downtime:* %s\n"+
		"📊 *Status:* Not Ready\n\n"+
		"⚠️ Узел недоступен более 10 минут!",
		nodeName, formatDurationForAlert(duration))

	sendText(m.bot, m.adminID, message)
	log.Printf("🔔 Отправлено уведомление о проблеме с узлом: %s", nodeName)
}

// sendRecoveryNotification отправляет уведомление о восстановлении узла
func (m *Monitor) sendRecoveryNotification(nodeName string) {
	message := fmt.Sprintf("✅ *RECOVERY: Node Back Online*\n\n"+
		"🔧 *Node:* `%s`\n"+
		"📊 *Status:* Ready\n\n"+
		"🎉 Узел восстановил работу!",
		nodeName)

	sendText(m.bot, m.adminID, message)
	log.Printf("🔔 Отправлено уведомление о восстановлении узла: %s", nodeName)
}

// sendNodeMissingNotification отправляет уведомление об отсутствующем узле
func (m *Monitor) sendNodeMissingNotification(nodeName string, duration time.Duration) {
	message := fmt.Sprintf("❌ *CRITICAL: Node Missing*\n\n"+
		"🔧 *Node:* `%s`\n"+
		"⏰ *Missing for:* %s\n\n"+
		"🚨 Узел отсутствует в кластере более 10 минут!",
		nodeName, formatDurationForAlert(duration))

	sendText(m.bot, m.adminID, message)
	log.Printf("🔔 Отправлено уведомление об отсутствующем узле: %s", nodeName)
}

// formatDurationForAlert форматирует время для уведомлений
func formatDurationForAlert(d time.Duration) string {
	minutes := int(d.Minutes())
	if minutes < 60 {
		return fmt.Sprintf("%d minutes", minutes)
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes > 0 {
		return fmt.Sprintf("%d hours %d minutes", hours, remainingMinutes)
	}
	return fmt.Sprintf("%d hours", hours)
}

// GetNodeStatuses возвращает текущие статусы узлов
func (m *Monitor) GetNodeStatuses() map[string]*NodeStatus {
	return m.nodes
}
