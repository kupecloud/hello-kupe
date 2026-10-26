package app

import (
	"os"
	"strconv"
	"strings"
)

// Config defines the hello-kupe runtime configuration.
type Config struct {
	DevelopmentOrigin string
	ServiceName       string
	Tenant            string
	PublicURL         string
	PodName           string
	PodNamespace      string
	Port              int
	WebRoot           string
	DatabaseBinding   string
	BindingRoot       string
	DatabaseURL       string
}

// LoadConfigFromEnv reads runtime configuration from environment variables.
func LoadConfigFromEnv() Config {
	return Config{
		DevelopmentOrigin: os.Getenv("DEVELOPMENT_ORIGIN"),
		ServiceName:       valueOrDefault(os.Getenv("SERVICE_NAME"), "hello-kupe"),
		Tenant:            valueOrDefault(os.Getenv("TENANT"), "unknown"),
		PublicURL:         valueOrDefault(os.Getenv("PUBLIC_URL"), "http://localhost:8080"),
		PodName:           valueOrDefault(os.Getenv("POD_NAME"), "local"),
		PodNamespace:      valueOrDefault(os.Getenv("POD_NAMESPACE"), "default"),
		Port:              parsePortEnv(),
		WebRoot:           valueOrDefault(os.Getenv("WEB_ROOT"), "web/out"),
		DatabaseBinding:   strings.TrimSpace(os.Getenv("DATABASE_BINDING")),
		BindingRoot:       valueOrDefault(os.Getenv("SERVICE_BINDING_ROOT"), "/bindings"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
	}
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parsePortEnv() int {
	const fallback = 8080
	raw := strings.TrimSpace(os.Getenv("PORT"))
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 || parsed > 65535 {
		return fallback
	}

	return parsed
}
