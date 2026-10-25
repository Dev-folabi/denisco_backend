// Command seed-delivery loads Nigeria's 36 states (the FCT/Abuja excluded,
// since it is the farm's own base) as home-delivery areas, each with a
// starting rate, and switches the home-delivery offering on.
//
// Rates are a starting point grouped loosely by distance from Abuja; the farm
// edits any of them in the admin settings panel exactly as it manages areas
// added by hand. The command is idempotent: it matches an existing area by
// its case-insensitive name and updates the fee rather than duplicating it,
// and it never deactivates or deletes an area, so manual changes survive a
// re-run.
//
// Usage:
//
//	go run ./scripts/seed-delivery
//	go run ./scripts/seed-delivery -reset-fees   # overwrite fees back to these defaults
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	deliverydomain "github.com/Dev-folabi/denisco_backend/internal/modules/delivery/domain"
	deliveryinfra "github.com/Dev-folabi/denisco_backend/internal/modules/delivery/infrastructure"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// koboPerNaira converts the naira rates below into the minor units stored.
const koboPerNaira = 100

// state is one delivery area: the state name and its starting rate in naira.
type state struct {
	Name     string
	FeeNaira int64
}

// states lists the 36 Nigerian states with starting delivery rates, grouped
// by geopolitical zone. The FCT (Abuja) is deliberately absent.
func states() []state {
	return []state{
		// North-Central (nearest Abuja)
		{"Nasarawa", 3_000},
		{"Niger", 3_500},
		{"Kogi", 3_500},
		{"Kwara", 4_000},
		{"Plateau", 4_000},
		{"Benue", 4_000},
		// North-West
		{"Kaduna", 4_500},
		{"Kano", 6_500},
		{"Katsina", 6_500},
		{"Zamfara", 6_500},
		{"Jigawa", 7_000},
		{"Kebbi", 7_000},
		{"Sokoto", 7_500},
		// North-East
		{"Bauchi", 6_000},
		{"Gombe", 6_500},
		{"Taraba", 6_500},
		{"Adamawa", 7_000},
		{"Yobe", 7_500},
		{"Borno", 8_000},
		// South-West
		{"Lagos", 5_000},
		{"Oyo", 5_000},
		{"Ogun", 5_000},
		{"Ondo", 5_000},
		{"Osun", 5_000},
		{"Ekiti", 5_000},
		// South-South
		{"Edo", 5_500},
		{"Delta", 6_000},
		{"Rivers", 6_000},
		{"Bayelsa", 6_500},
		{"Akwa Ibom", 6_500},
		{"Cross River", 6_500},
		// South-East
		{"Enugu", 5_500},
		{"Anambra", 5_500},
		{"Ebonyi", 5_500},
		{"Imo", 6_000},
		{"Abia", 6_000},
	}
}

func main() {
	resetFees := flag.Bool("reset-fees", false, "overwrite existing areas' fees back to the seed defaults")
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

	locationRepo := deliveryinfra.NewMongoLocationRepository(db)
	settingsRepo := deliveryinfra.NewMongoSettingsRepository(db)

	existing, err := locationRepo.List(ctx, false)
	if err != nil {
		fail("failed to list delivery locations", err)
	}
	byKey := make(map[string]deliverydomain.Location, len(existing))
	for _, loc := range existing {
		byKey[loc.NameKey] = loc
	}

	var created, updated int
	for _, s := range states() {
		name := strings.TrimSpace(s.Name)
		if name == "" || s.FeeNaira == 0 {
			continue // guard against the readability placeholder above
		}
		key := strings.ToLower(name)
		fee := s.FeeNaira * koboPerNaira
		now := time.Now().UTC()

		if current, ok := byKey[key]; ok {
			if !*resetFees {
				continue
			}
			if err := locationRepo.Update(ctx, current.ID, bson.M{"fee": fee, "updated_at": now}); err != nil {
				fail("failed to update delivery location", err)
			}
			updated++
			continue
		}

		location := &deliverydomain.Location{
			Name:      name,
			NameKey:   key,
			Fee:       fee,
			Active:    true,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := locationRepo.Create(ctx, location); err != nil {
			fail("failed to create delivery location", err)
		}
		created++
	}

	if err := settingsRepo.SetEnabled(ctx, true); err != nil {
		fail("failed to enable home delivery", err)
	}

	log.Info("delivery areas seeded",
		slog.Int("created", created),
		slog.Int("updated", updated),
		slog.Bool("reset_fees", *resetFees),
		slog.Bool("home_delivery_enabled", true),
	)
}

// fail reports a fatal setup error and exits non-zero.
func fail(message string, err error) {
	slog.Error(message, slog.String("error", err.Error()))
	os.Exit(1)
}
