// Command seed-consultations loads the consultation services defined by
// denisco_prototype.html and opens the default booking calendar.
//
// It is idempotent: types are matched by their prototype code and refreshed,
// and availability is only opened when none exists, so re-running it never
// overwrites an administrator's own schedule.
//
// Usage:
//
//	go run ./scripts/seed-consultations
//	go run ./scripts/seed-consultations -reset-availability
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	consultdomain "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	consultinfra "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/infrastructure"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
)

func main() {
	resetAvailability := flag.Bool("reset-availability", false,
		"clear the existing calendar and reopen the default 31-day grid")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fail("failed to load configuration", err)
	}

	log := logger.New(cfg.Log.Level)
	slog.SetDefault(log)

	client, err := database.Connect(cfg.Mongo.URI, cfg.Mongo.MaxPoolSize)
	if err != nil {
		fail("failed to connect to mongodb", err)
	}
	defer func() { _ = database.Disconnect(client) }()

	db := client.Database(cfg.Mongo.Database)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := database.EnsureIndexes(ctx, db); err != nil {
		fail("failed to create indexes", err)
	}

	slotRepo := consultinfra.NewSlotRepository(db)

	service := consultapp.NewService(
		consultinfra.NewTypeRepository(db),
		slotRepo,
		consultinfra.NewBookingRepository(db),
		nil, // no events: seeding is not a business action
		nil, // no audit trail: there is no administrator behind it
	)

	seeded, err := service.SeedTypes(ctx)
	if err != nil {
		fail("failed to seed consultation types", err)
	}
	log.Info("consultation types seeded", slog.Int("count", seeded))

	if *resetAvailability {
		if _, err := slotRepo.Remove(ctx, nil, consultdomain.DefaultTimes()); err != nil {
			fail("failed to clear availability", err)
		}
	}

	opened, err := service.EnsureDefaultAvailability(ctx)
	if err != nil {
		fail("failed to open the booking calendar", err)
	}

	log.Info("booking calendar ready",
		slog.Int("slots_opened", opened),
		slog.Int("days_ahead", consultdomain.BookingDaysAhead),
	)
}

// fail reports a fatal setup error and exits non-zero.
func fail(message string, err error) {
	slog.Error(message, slog.String("error", err.Error()))
	os.Exit(1)
}
