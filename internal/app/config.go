package app

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config defines the hello-kupe runtime configuration.
type Config struct {
	ServiceName  string
	Tenant       string
	PublicURL    string
	PodName      string
	PodNamespace string
	Port         int
	LogInterval  time.Duration
}

// LoadConfigFromEnv reads runtime configuration from environment variables.
func LoadConfigFromEnv() Config {
	return Config{
		ServiceName:  valueOrDefault(os.Getenv("SERVICE_NAME"), "hello-kupe"),
		Tenant:       valueOrDefault(os.Getenv("TENANT"), "unknown"),
		PublicURL:    valueOrDefault(os.Getenv("PUBLIC_URL"), "http://localhost:8080"),
		PodName:      valueOrDefault(os.Getenv("POD_NAME"), "local"),
		PodNamespace: valueOrDefault(os.Getenv("POD_NAMESPACE"), "default"),
		Port:         parsePositiveIntEnv("PORT", 8080),
		LogInterval:  time.Duration(parsePositiveIntEnv("LOG_INTERVAL_SECONDS", 5)) * time.Second,
	}
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parsePositiveIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}
