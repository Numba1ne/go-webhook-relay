package config

import (
	"log/slog"
	"os"
	"strconv"
)

// Config holds all configuration values for the service.
type Config struct {
	Port          string
	WebhookSecret string
	WorkerCount   int
	QueueCapacity int
	LogLevel      string
}

// Load loads configuration from environment variables with sensible production defaults.
func Load() *Config {
	cfg := &Config{
		Port:          getEnv("PORT", "8080"),
		WebhookSecret: getEnv("WEBHOOK_SECRET", "supersecretkey_change_me_in_production"),
		WorkerCount:   getEnvAsInt("WORKER_COUNT", 5),
		QueueCapacity: getEnvAsInt("QUEUE_CAPACITY", 100),
		LogLevel:      getEnv("LOG_LEVEL", "INFO"),
	}

	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 5
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = 100
	}

	return cfg
}

// getEnv retrieves an environment variable or returns defaultVal if empty/unset.
func getEnv(key, defaultVal string) string {
	if val, exists := os.LookupEnv(key); exists && val != "" {
		return val
	}
	return defaultVal
}

// getEnvAsInt retrieves an environment variable as an int, or returns defaultVal on parse failure.
func getEnvAsInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		slog.Warn("Invalid integer for environment variable, using default",
			"key", key,
			"value", valStr,
			"default", defaultVal,
		)
		return defaultVal
	}
	return val
}
