package main

import "time"

// Config содержит конфигурацию мониторинга
type Config struct {
	CheckInterval    time.Duration
	AlertThreshold   time.Duration
	EnableMonitoring bool
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() Config {
	return Config{
		CheckInterval:    1 * time.Minute,  // Проверка каждую минуту
		AlertThreshold:   10 * time.Minute, // Уведомление после 10 минут
		EnableMonitoring: true,
	}
}
