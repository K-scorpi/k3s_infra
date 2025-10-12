package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID   = os.Getenv("TELEGRAM_CHAT_ID")
)

func sendTelegram(msg string) {
	if botToken == "" || chatID == "" {
		fmt.Println("Telegram credentials not set")
		return
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	body := map[string]string{"chat_id": chatID, "text": msg}
	jsonBody, _ := json.Marshal(body)
	_, _ = http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}

func checkNodeConditions(clientset *kubernetes.Clientset) {
	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		fmt.Println("Ошибка получения списка нод:", err)
		return
	}

	for _, node := range nodes.Items {
		for _, cond := range node.Status.Conditions {
			if cond.Type == v1.NodeReady && cond.Status != v1.ConditionTrue {
				sendTelegram(fmt.Sprintf("⚠️ Узел *%s* не готов: %s (%s)", node.Name, cond.Reason, cond.Message))
			}
			if cond.Type == v1.NodeMemoryPressure && cond.Status == v1.ConditionTrue {
				sendTelegram(fmt.Sprintf("🚨 Узел *%s* испытывает MemoryPressure", node.Name))
			}
			if cond.Type == v1.NodeDiskPressure && cond.Status == v1.ConditionTrue {
				sendTelegram(fmt.Sprintf("🚨 Узел *%s* испытывает DiskPressure", node.Name))
			}
		}
	}
}

func checkNodeMetrics() {
	req, err := http.NewRequest("GET", "https://kubernetes.default.svc/apis/metrics.k8s.io/v1beta1/nodes", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+readServiceAccountToken())

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var data map[string]interface{}
	_ = json.Unmarshal(body, &data)
	items, ok := data["items"].([]interface{})
	if !ok {
		return
	}

	for _, item := range items {
		node := item.(map[string]interface{})
		meta := node["metadata"].(map[string]interface{})
		usage := node["usage"].(map[string]interface{})
		cpu := usage["cpu"].(string)
		mem := usage["memory"].(string)
		name := meta["name"].(string)
		msg := fmt.Sprintf("📊 Node %s CPU=%s, MEM=%s", name, cpu, mem)
		fmt.Println(msg)
	}
}

func readServiceAccountToken() string {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return ""
	}
	return string(data)
}

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	sendTelegram("✅ Node Health Monitor запущен в кластере 🚀")

	for {
		checkNodeConditions(clientset)
		checkNodeMetrics()
		time.Sleep(120 * time.Second)
	}
}
