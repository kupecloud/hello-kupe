// Package config reads the process configuration from the environment.
package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kupecloud/hello-kupe/samples/managed-database/internal/binding"
)

// Bindings follow the servicebinding.io convention: each binding Secret is
// mounted at $SERVICE_BINDING_ROOT/<secret name>, the path the Secret's own
// `uri` key refers to for ca.crt.
const (
	DefaultPort        = 8080
	DefaultBindingRoot = "/bindings"
)

// Config is the runtime configuration shared by the API and the migration.
type Config struct {
	Port int
	// BindingDir is where the database binding Secret is mounted:
	// $SERVICE_BINDING_ROOT/$DATABASE_BINDING.
	BindingDir string
}

// FromEnv reads PORT, SERVICE_BINDING_ROOT and DATABASE_BINDING through
// getenv (os.Getenv in production). DATABASE_BINDING is the name of the
// binding Secret, for example todo-db-app, and is required: the API and the
// migration mount different Secrets, and guessing would pick the wrong role.
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{Port: DefaultPort}

	if raw := strings.TrimSpace(getenv("PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("PORT %q is not a TCP port", raw)
		}
		cfg.Port = port
	}

	root := valueOr(getenv("SERVICE_BINDING_ROOT"), DefaultBindingRoot)
	name := strings.TrimSpace(getenv("DATABASE_BINDING"))
	if name == "" {
		return Config{}, fmt.Errorf("DATABASE_BINDING is required: set it to the binding Secret name")
	}
	dir, err := binding.Dir(root, name)
	if err != nil {
		return Config{}, fmt.Errorf("SERVICE_BINDING_ROOT/DATABASE_BINDING: %w", err)
	}
	cfg.BindingDir = dir
	return cfg, nil
}

func valueOr(value, fallback string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return fallback
}
