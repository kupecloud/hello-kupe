package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kupecloud/hello-kupe/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	cfg := app.LoadConfigFromEnv()
	var err error
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "migrate" {
			err = fmt.Errorf("usage: hello-kupe [migrate]")
		} else {
			err = app.Migrate(ctx, cfg, slog.New(slog.NewJSONHandler(os.Stdout, nil)))
		}
	} else {
		err = app.Run(ctx, cfg, os.Stdout)
	}
	if err != nil {
		stop()
		slog.Error("hello-kupe failed", "error", err.Error())
		os.Exit(1)
	}
	stop()
}
