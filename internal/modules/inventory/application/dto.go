package application

import "go.mongodb.org/mongo-driver/v2/bson"

// Actor identifies the administrator behind a manual stock change.
type Actor struct {
	ID    bson.ObjectID
	Email string
	Role  string
}

// AdjustInput is a manual stock correction.
type AdjustInput struct {
	ProductID bson.ObjectID
	// Quantity is a signed delta: positive adds stock, negative removes it.
	Quantity int
	Reason   string
}
