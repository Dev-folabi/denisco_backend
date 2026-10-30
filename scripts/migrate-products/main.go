// Command migrate-products applies the 2026-10-30 (second) catalogue pass to an
// existing database, touching only the products it names:
//
//   - the eight Animal Nutrition products are matched to the client's new
//     images: five are renamed or re-described to match their bag, and the two
//     products whose image duplicates another are archived;
//   - the Giant Ghana Tiger Snail becomes "Snail";
//   - every Spices product gets its own generated image in place of the shared
//     field photograph.
//
// Prices and stock are never modified. Dry run unless -apply is passed; with
// -apply every product it will change is first saved to a JSON backup.
//
// Usage:
//
//	go run ./scripts/migrate-products
//	go run ./scripts/migrate-products -apply -backup backup.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	productsdomain "github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/slug"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// nutrition maps each product's current image to what it should be now.
// Archive means the new image duplicates another product's, so the listing is
// hidden rather than shown twice.
var nutrition = []struct {
	Image       string
	Name        string
	Description string
	Archive     bool
}{
	{"nutrition-1.jpeg", "Broiler Growth Booster (1kg)", "Mixed feed additive (Bacillus subtilis + Bacillus licheniformis) that supports gut health, faster growth and lower feed cost in broilers.", false},
	{"nutrition-2.jpeg", "Pig Growth Booster (1kg)", "Bacillus subtilis and Saccharomyces cerevisiae feed additive that supports healthy digestion, steady weight gain and better feed conversion in pigs.", false},
	{"nutrition-3.jpeg", "BactoClear (100g)", "Lysozyme and glycine feed additive that supports gut health and immunity, helping control diarrhoea and enteritis in livestock and poultry.", false},
	{"nutrition-4.jpeg", "", "", true},
	{"nutrition-5.jpeg", "Turkey Weight Gain Booster (1kg)", "Feed additive that supports faster, healthier weight gain in turkeys.", false},
	{"nutrition-6.jpeg", "Vibrio Clear (100g)", "Lysozyme and glycine feed additive for aquaculture that helps eliminate pathogenic Vibrio and supports healthy, thriving fish and shrimp.", false},
	{"nutrition-7.jpeg", "Ruminant Growth Booster (1kg)", "Bacillus subtilis and Saccharomyces cerevisiae feed additive for cattle and sheep, improving roughage digestibility, weight gain and feed efficiency.", false},
	{"nutrition-8.jpeg", "Multivitamin Complex (1kg)", "Taurine and L-ascorbic acid (vitamin C) multivitamin feed additive that boosts immunity, stress resistance and daily weight gain across livestock and poultry.", false},
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// spiceImage is the generated image's file name for a spice product name.
func spiceImage(name string) string {
	n := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "(100g)", ""), "&", " "))
	return "spice-" + strings.Trim(nonAlnum.ReplaceAllString(n, "-"), "-") + ".jpeg"
}

type change struct {
	id     bson.ObjectID
	label  string
	fields bson.M
}

func main() {
	apply := flag.Bool("apply", false, "write the changes (default is a dry run)")
	backup := flag.String("backup", "product-migration-backup.json", "file the affected products are saved to before -apply writes")
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
	products := db.Collection(database.CollProducts)

	fmt.Printf("database: %s   mode: %s\n\n", cfg.Mongo.Database, map[bool]string{true: "APPLY", false: "DRY RUN"}[*apply])

	now := time.Now().UTC()
	var changes []change
	var saved []bson.M

	find := func(filter bson.M) bson.M {
		var doc bson.M
		if err := products.FindOne(ctx, filter).Decode(&doc); err != nil {
			return nil
		}
		return doc
	}
	imageURL := func(file string) []productsdomain.Image { return []productsdomain.Image{{URL: "/images/" + file}} }

	// Animal nutrition, matched by the image each product currently carries.
	for _, n := range nutrition {
		doc := find(bson.M{"images.url": "/images/" + n.Image})
		if doc == nil {
			fmt.Printf(" ! no product with image %s — skipped\n", n.Image)
			continue
		}
		saved = append(saved, doc)
		id := doc["_id"].(bson.ObjectID)
		oldSlug, _ := doc["slug"].(string)
		oldName, _ := doc["name"].(string)

		if n.Archive {
			if doc["status"] == "archived" {
				continue
			}
			changes = append(changes, change{id, fmt.Sprintf("archive %q (its image duplicates another product's)", oldName),
				bson.M{"status": "archived", "slug": oldSlug + "-duplicate", "updated_at": now}})
			continue
		}
		fields := bson.M{"name": n.Name, "slug": slug.Make(n.Name), "description": n.Description, "status": "active", "updated_at": now}
		changes = append(changes, change{id, fmt.Sprintf("%q -> %q", oldName, n.Name), fields})
	}

	// Snail.
	if doc := find(bson.M{"slug": "giant-ghana-tiger-snail"}); doc != nil {
		saved = append(saved, doc)
		changes = append(changes, change{doc["_id"].(bson.ObjectID), `"Giant Ghana Tiger Snail" -> "Snail"`,
			bson.M{"name": "Snail", "slug": "snail", "description": "Fresh, plump snails, sustainably reared in our snailry and sold by the bag.", "updated_at": now}})
	} else {
		fmt.Println(" • snail already renamed (or not found)")
	}

	// Spices: one image per product.
	var spiceCat bson.M
	_ = db.Collection(database.CollCategories).FindOne(ctx, bson.M{"slug": "spices"}).Decode(&spiceCat)
	if spiceCat != nil {
		cur, err := products.Find(ctx, bson.M{"category_id": spiceCat["_id"]})
		if err != nil {
			fail("spice lookup failed", err)
		}
		var spices []bson.M
		if err := cur.All(ctx, &spices); err != nil {
			fail("spice lookup failed", err)
		}
		for _, doc := range spices {
			name, _ := doc["name"].(string)
			file := spiceImage(name)
			if _, err := os.Stat("../assets/" + file); err != nil {
				fmt.Printf(" ! no generated image %s for %q — skipped\n", file, name)
				continue
			}
			saved = append(saved, doc)
			changes = append(changes, change{doc["_id"].(bson.ObjectID), fmt.Sprintf("%q image -> /images/%s", name, file),
				bson.M{"images": imageURL(file), "updated_at": now}})
		}
	}

	for _, c := range changes {
		fmt.Println(" •", c.label)
	}

	if !*apply {
		fmt.Printf("\nDry run only — %d change(s), nothing written. Re-run with -apply.\n", len(changes))
		return
	}

	out, err := bson.MarshalExtJSON(bson.M{"taken_at": now, "products": saved}, true, false)
	if err != nil {
		fail("backup failed; nothing changed", err)
	}
	var pretty any
	_ = json.Unmarshal(out, &pretty)
	b, _ := json.MarshalIndent(pretty, "", "  ")
	if err := os.WriteFile(*backup, b, 0o600); err != nil {
		fail("backup failed; nothing changed", err)
	}
	fmt.Printf("\nbackup written to %s\n", *backup)

	// Two phases so renames that swap slugs never collide on the unique index:
	// first park every slug being changed, then write the final values.
	for _, c := range changes {
		if _, ok := c.fields["slug"]; ok {
			if _, err := products.UpdateOne(ctx, bson.M{"_id": c.id}, bson.M{"$set": bson.M{"slug": "migrating-" + c.id.Hex()}}); err != nil {
				fail("parking slug failed", err)
			}
		}
	}
	for _, c := range changes {
		if _, err := products.UpdateOne(ctx, bson.M{"_id": c.id}, bson.M{"$set": c.fields}); err != nil {
			fail("update failed", err)
		}
	}
	fmt.Printf("applied %d change(s)\n", len(changes))
}

func fail(message string, err error) {
	if err != nil {
		slog.Error(message, slog.String("error", err.Error()))
	} else {
		slog.Error(message)
	}
	os.Exit(1)
}
