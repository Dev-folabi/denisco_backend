// Command migrate-categories applies the 2026-10-30 category restructure to an
// existing database without touching anything else:
//
//   - "Pig & Micro-Livestock" (piggery) becomes "Micro-Livestock"
//     (micro-livestock); the Pig product moves to Livestock.
//   - "Spices & Nutrition" becomes "Spices".
//   - Snail Farming is removed. The Giant Ghana Tiger Snail moves to
//     Micro-Livestock; the East African Snail is deleted, or archived when an
//     order refers to it (the same rule as DELETE /admin/products/{id}).
//   - "Animal Nutrition" is created (its eight products come from
//     seed-catalogue -create-only, run afterwards).
//
// It never changes prices, stock or any other product. It is a dry run unless
// -apply is passed, and with -apply it first writes every document it will
// touch to a JSON backup file.
//
// Usage:
//
//	go run ./scripts/migrate-categories                  # dry run
//	go run ./scripts/migrate-categories -apply -backup backup.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func main() {
	apply := flag.Bool("apply", false, "write the changes (default is a dry run)")
	backup := flag.String("backup", "category-migration-backup.json", "file the affected documents are saved to before -apply writes")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fail("failed to load configuration", err)
	}
	slog.SetDefault(logger.New(cfg.Log.Level))

	client, err := database.Connect(cfg.Mongo.URI, cfg.Mongo.MaxPoolSize)
	if err != nil {
		fail("failed to connect to mongodb", err)
	}
	defer func() { _ = database.Disconnect(client) }()

	db := client.Database(cfg.Mongo.Database)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	m := &migration{ctx: ctx, db: db, apply: *apply, saved: map[string][]bson.M{}}
	fmt.Printf("database: %s   mode: %s\n\n", cfg.Mongo.Database, map[bool]string{true: "APPLY", false: "DRY RUN"}[*apply])

	m.plan()

	if !*apply {
		fmt.Println("\nDry run only — nothing was written. Re-run with -apply to make these changes.")
		return
	}

	if err := m.writeBackup(*backup); err != nil {
		fail("could not write the backup; nothing was changed", err)
	}
	fmt.Printf("\nbackup written to %s\n", *backup)

	m.execute()
}

type migration struct {
	ctx   context.Context
	db    *mongo.Database
	apply bool
	saved map[string][]bson.M
	steps []func()
}

func (m *migration) coll(name string) *mongo.Collection { return m.db.Collection(name) }

func (m *migration) one(coll string, filter bson.M) bson.M {
	var doc bson.M
	err := m.coll(coll).FindOne(m.ctx, filter).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil
	}
	if err != nil {
		fail("lookup failed", err)
	}
	return doc
}

func (m *migration) keep(coll string, doc bson.M) {
	if doc != nil {
		m.saved[coll] = append(m.saved[coll], doc)
	}
}

func (m *migration) do(desc string, fn func()) {
	fmt.Println(" •", desc)
	m.steps = append(m.steps, fn)
}

// plan inspects current state and queues the idempotent steps. Each step
// re-checks its own precondition, so re-running after a partial run is safe.
func (m *migration) plan() {
	now := time.Now().UTC()

	piggery := m.one(database.CollCategories, bson.M{"slug": "piggery"})
	micro := m.one(database.CollCategories, bson.M{"slug": "micro-livestock"})
	livestock := m.one(database.CollCategories, bson.M{"slug": "livestock"})
	snailCat := m.one(database.CollCategories, bson.M{"slug": "snail"})
	spices := m.one(database.CollCategories, bson.M{"slug": "spices"})
	nutrition := m.one(database.CollCategories, bson.M{"slug": "animal-nutrition"})

	if livestock == nil {
		fail("the livestock category is missing; refusing to continue", nil)
	}

	// 1. piggery -> micro-livestock
	switch {
	case micro != nil:
		fmt.Println(" • micro-livestock category already exists — nothing to rename")
	case piggery != nil:
		m.keep(database.CollCategories, piggery)
		m.do(`rename category "Pig & Micro-Livestock" (piggery) -> "Micro-Livestock" (micro-livestock)`, func() {
			m.set(database.CollCategories, bson.M{"_id": piggery["_id"]}, bson.M{"name": "Micro-Livestock", "slug": "micro-livestock", "position": 3, "updated_at": now})
		})
		micro = piggery
	default:
		fail("neither piggery nor micro-livestock category exists", nil)
	}
	microID := micro["_id"]

	// 2. Pig -> Livestock
	if pig := m.one(database.CollProducts, bson.M{"slug": "pig"}); pig != nil {
		if pig["category_id"] != livestock["_id"] {
			m.keep(database.CollProducts, pig)
			m.do(`move product "Pig" to Livestock`, func() {
				m.set(database.CollProducts, bson.M{"_id": pig["_id"]}, bson.M{"category_id": livestock["_id"], "updated_at": now})
			})
		}
	} else {
		fmt.Println(" ! no product with slug 'pig' found — skipped")
	}

	// 3. snails
	if g := m.one(database.CollProducts, bson.M{"slug": "giant-ghana-tiger-snail"}); g != nil {
		if g["category_id"] != microID {
			m.keep(database.CollProducts, g)
			m.do(`move "Giant Ghana Tiger Snail" to Micro-Livestock`, func() {
				m.set(database.CollProducts, bson.M{"_id": g["_id"]}, bson.M{"category_id": microID, "updated_at": now})
			})
		}
	} else {
		fmt.Println(" ! 'giant-ghana-tiger-snail' not found — skipped")
	}

	if e := m.one(database.CollProducts, bson.M{"slug": "east-african-snail"}); e != nil {
		id := e["_id"]
		ordered, err := m.coll(database.CollOrders).CountDocuments(m.ctx, bson.M{"items.product_id": id})
		if err != nil {
			fail("order lookup failed", err)
		}
		m.keep(database.CollProducts, e)
		for _, coll := range []string{database.CollInventory, database.CollInventoryMovements} {
			m.keepAll(coll, bson.M{"product_id": id})
		}
		if ordered > 0 {
			m.do(fmt.Sprintf(`archive "East African Snail" (%d order(s) refer to it, so it cannot be deleted)`, ordered), func() {
				m.set(database.CollProducts, bson.M{"_id": id}, bson.M{"status": "archived", "updated_at": now})
			})
		} else {
			m.do(`delete "East African Snail" with its stock record and movement log (no order refers to it)`, func() {
				m.del(database.CollProducts, bson.M{"_id": id})
				m.del(database.CollInventory, bson.M{"product_id": id})
				m.del(database.CollInventoryMovements, bson.M{"product_id": id})
			})
		}
		if carts, _ := m.coll(database.CollCarts).CountDocuments(m.ctx, bson.M{"items.product_id": id}); carts > 0 {
			fmt.Printf(" ! %d cart(s) still hold it; carts drop unavailable products when read\n", carts)
		}
	}

	// 4. remove the snail category once nothing points at it. Archived
	// leftovers (hidden from the shop, kept for order history) move to
	// Micro-Livestock so they keep a valid category; an active one stops the
	// removal, since that would be a product the client did not mention.
	if snailCat != nil {
		cur, err := m.coll(database.CollProducts).Find(m.ctx, bson.M{"category_id": snailCat["_id"], "slug": bson.M{"$nin": bson.A{"east-african-snail", "giant-ghana-tiger-snail"}}})
		if err != nil {
			fail("lookup failed", err)
		}
		var others []bson.M
		if err := cur.All(m.ctx, &others); err != nil {
			fail("lookup failed", err)
		}
		blocked := false
		for _, o := range others {
			o := o
			if o["status"] != "archived" {
				fmt.Printf(" ! active product %v still uses the snail category — the category will be kept\n", o["slug"])
				blocked = true
				continue
			}
			m.keep(database.CollProducts, o)
			m.do(fmt.Sprintf("move archived product %q to Micro-Livestock (stays archived)", o["slug"]), func() {
				m.set(database.CollProducts, bson.M{"_id": o["_id"]}, bson.M{"category_id": microID})
			})
		}
		if !blocked {
			m.keep(database.CollCategories, snailCat)
			m.do("delete the Snail Farming category", func() {
				m.del(database.CollCategories, bson.M{"_id": snailCat["_id"]})
			})
		}
	}

	// 5. Spices & Nutrition -> Spices
	if spices != nil && spices["name"] != "Spices" {
		m.keep(database.CollCategories, spices)
		m.do(`rename category "Spices & Nutrition" -> "Spices"`, func() {
			m.set(database.CollCategories, bson.M{"_id": spices["_id"]}, bson.M{"name": "Spices", "position": 6, "updated_at": now})
		})
	}

	// 6. Animal Nutrition + positions
	if nutrition == nil {
		m.do(`create category "Animal Nutrition"`, func() {
			_, err := m.coll(database.CollCategories).InsertOne(m.ctx, bson.M{
				"name": "Animal Nutrition", "slug": "animal-nutrition", "position": 5,
				"active": true, "created_at": now, "updated_at": now,
			})
			if err != nil {
				fail("insert failed", err)
			}
		})
	}
	m.do("set display order: Poultry, Livestock, Micro-Livestock, Crop Farming, Animal Nutrition, Spices", func() {
		for slug, pos := range map[string]int{"poultry": 1, "livestock": 2, "micro-livestock": 3, "crops": 4, "animal-nutrition": 5, "spices": 6} {
			m.set(database.CollCategories, bson.M{"slug": slug}, bson.M{"position": pos})
		}
	})
}

func (m *migration) keepAll(coll string, filter bson.M) {
	cur, err := m.coll(coll).Find(m.ctx, filter)
	if err != nil {
		fail("backup read failed", err)
	}
	var docs []bson.M
	if err := cur.All(m.ctx, &docs); err != nil {
		fail("backup read failed", err)
	}
	m.saved[coll] = append(m.saved[coll], docs...)
}

func (m *migration) set(coll string, filter, fields bson.M) {
	if _, err := m.coll(coll).UpdateOne(m.ctx, filter, bson.M{"$set": fields}); err != nil {
		fail("update failed", err)
	}
}

func (m *migration) del(coll string, filter bson.M) {
	if _, err := m.coll(coll).DeleteMany(m.ctx, filter); err != nil {
		fail("delete failed", err)
	}
}

func (m *migration) writeBackup(path string) error {
	out, err := bson.MarshalExtJSON(bson.M{"taken_at": time.Now().UTC(), "documents": m.saved}, true, false)
	if err != nil {
		return err
	}
	var pretty any
	if err := json.Unmarshal(out, &pretty); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(pretty, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

func (m *migration) execute() {
	for _, step := range m.steps {
		step()
	}
	fmt.Printf("\napplied %d step(s)\n", len(m.steps))
}

func fail(message string, err error) {
	if err != nil {
		slog.Error(message, slog.String("error", err.Error()))
	} else {
		slog.Error(message)
	}
	os.Exit(1)
}
