// Command resolve-slot-collisions reports, and optionally clears, consultation
// times held by more than one live booking.
//
// Such a pair can only exist in a database written before a consultation slot
// became exclusive across services, when two services could share an hour.
// The API refuses to start while one exists, because the unique index that
// enforces the rule cannot be created over it.
//
// Resolving a collision means one of the customers loses their time, which is
// a decision for the farm rather than for a migration: the default is a dry
// run that prints who is affected, with their contact details, so staff can
// call them before anything changes. Passing -apply keeps the earliest
// booking at each time and cancels the rest, which releases their slot and
// leaves the record in place.
//
// Usage:
//
//	go run ./scripts/resolve-slot-collisions
//	go run ./scripts/resolve-slot-collisions -apply
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	consultdomain "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	apply := flag.Bool("apply", false, "cancel the later bookings instead of only reporting them")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fail("could not load configuration", err)
	}

	log := logger.New(cfg.Log.Level)
	slog.SetDefault(log)

	client, err := database.Connect(cfg.Mongo.URI, cfg.Mongo.MaxPoolSize)
	if err != nil {
		fail("could not connect to mongodb", err)
	}
	defer func() { _ = database.Disconnect(client) }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Deliberately no EnsureIndexes call: this script exists to be runnable
	// when that is exactly what fails.
	bookings := client.Database(cfg.Mongo.Database).Collection(database.CollConsultationBookings)

	// Oldest first, so "the booking that got there first" is the one kept.
	cursor, err := bookings.Find(ctx, bson.M{"active": true},
		options.Find().SetSort(bson.D{
			{Key: "date", Value: 1},
			{Key: "time", Value: 1},
			{Key: "created_at", Value: 1},
		}))
	if err != nil {
		fail("could not read bookings", err)
	}

	var live []consultdomain.Booking
	if err := cursor.All(ctx, &live); err != nil {
		fail("could not decode bookings", err)
	}

	// Group by the slot they hold.
	held := map[string][]consultdomain.Booking{}
	order := []string{}
	for _, booking := range live {
		key := booking.Date + " " + booking.Time
		if _, seen := held[key]; !seen {
			order = append(order, key)
		}
		held[key] = append(held[key], booking)
	}

	collisions := 0
	cancelled := 0

	for _, key := range order {
		group := held[key]
		if len(group) < 2 {
			continue
		}
		collisions++

		fmt.Printf("\n%s is held by %d bookings:\n", key, len(group))
		for i, booking := range group {
			verdict := "cancel"
			if i == 0 {
				verdict = "keep (booked first)"
			}
			fmt.Printf("  %-9s %-44s %-28s %-16s  %s\n",
				booking.Reference, booking.TypeName, booking.Email, booking.Phone, verdict)
		}

		if !*apply {
			continue
		}

		for _, booking := range group[1:] {
			now := time.Now().UTC()
			result, err := bookings.UpdateOne(ctx,
				bson.M{"_id": booking.ID, "active": true},
				bson.M{
					"$set":   bson.M{"status": consultdomain.StatusCancelled, "cancelled_at": now, "updated_at": now},
					"$unset": bson.M{"active": ""},
				})
			if err != nil {
				fail("could not cancel booking "+booking.Reference, err)
			}
			if result.ModifiedCount == 1 {
				cancelled++
				log.Info("booking cancelled to resolve a slot collision",
					slog.String("reference", booking.Reference),
					slog.String("date", booking.Date),
					slog.String("time", booking.Time),
					slog.String("email", booking.Email),
				)
			}
		}
	}

	fmt.Println()
	switch {
	case collisions == 0:
		fmt.Println("No consultation time is held by more than one booking. Nothing to do.")
	case *apply:
		fmt.Printf("Resolved %d collision(s) by cancelling %d booking(s). Contact those customers to rebook.\n",
			collisions, cancelled)
	default:
		fmt.Printf("Found %d collision(s). This was a dry run — nothing changed.\n", collisions)
		fmt.Println("Re-run with -apply to cancel the later bookings at each time.")
		os.Exit(1)
	}
}

// fail prints a reason and exits non-zero.
func fail(message string, err error) {
	fmt.Fprintf(os.Stderr, "resolve-slot-collisions: %s: %v\n", message, err)
	os.Exit(1)
}
