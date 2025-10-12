package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

type NodeStatus struct {
	Hostname     string  `json:"hostname"`
	CPUUsage     float64 `json:"cpu_usage_percent"`
	MemoryUsage  float64 `json:"memory_usage_percent"`
	DiskUsage    float64 `json:"disk_usage_percent"`
	Uptime       string  `json:"uptime"`
	KubeletAlive bool    `json:"kubelet_alive"`
	Timestamp    string  `json:"timestamp"`
}

func getNodeStatus() NodeStatus {
	hostInfo, _ := host.Info()
	vmStat, _ := mem.VirtualMemory()
	cpuPercents, _ := cpu.Percent(0, false)
	diskStat, _ := disk.Usage("/")
	kubeletAlive := checkKubelet()

	hostname := hostInfo.Hostname
	cpuUsage := 0.0
	if len(cpuPercents) > 0 {
		cpuUsage = math.Round(cpuPercents[0]*100) / 100
	}

	status := NodeStatus{
		Hostname:     hostname,
		CPUUsage:     cpuUsage,
		MemoryUsage:  math.Round(vmStat.UsedPercent*100) / 100,
		DiskUsage:    math.Round(diskStat.UsedPercent*100) / 100,
		Uptime:       fmt.Sprintf("%d hours", int(hostInfo.Uptime/3600)),
		KubeletAlive: kubeletAlive,
		Timestamp:    time.Now().Format(time.RFC3339),
	}

	return status
}

func checkKubelet() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", "kubelet")
	err := cmd.Run()
	return err == nil
}

func main() {
	log.Printf("🚀 Node Health Monitor started (Go %s, %s/%s)", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := getNodeStatus()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// фоновый лог каждые 60 секунд
	go func() {
		for {
			status := getNodeStatus()
			data, _ := json.Marshal(status)
			log.Printf("%s", data)
			time.Sleep(60 * time.Second)
		}
	}()

	log.Println("✅ HTTP server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
