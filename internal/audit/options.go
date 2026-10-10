package audit

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongoFindOptions builds the newest-first pagination used by the log listing.
func mongoFindOptions(page, limit int) *options.FindOptionsBuilder {
	return options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(int64((page - 1) * limit)).
		SetLimit(int64(limit))
}
