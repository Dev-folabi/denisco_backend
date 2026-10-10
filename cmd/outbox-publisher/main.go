// Command outbox-publisher runs the DENISCO transactional outbox publisher.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Dev-folabi/denisco_backend/internal/bootstrap"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	log := logger.New(cfg.Log.Level).With(
		slog.String("service", cfg.App.Name+"-outbox-publisher"),
		slog.String("env", cfg.App.Env),
	)
	slog.SetDefault(log)

	container, err := bootstrap.NewContainer(context.Background(), cfg, log)
	if err != nil {
		log.Error("failed to start outbox-publisher", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer container.Close()

	if err := bootstrap.RunOutboxPublisher(container); err != nil {
		log.Error("outbox-publisher terminated unexpectedly", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
