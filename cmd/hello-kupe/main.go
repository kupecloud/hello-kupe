package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kupecloud/hello-kupe/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	cfg := app.LoadConfigFromEnv()
	if err := app.Run(ctx, cfg, os.Stdout); err != nil {
		stop()
		log.Fatalf("hello-kupe failed: %v", err)
	}
	stop()
}
