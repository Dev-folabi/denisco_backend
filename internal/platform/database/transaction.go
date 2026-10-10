package database

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// TxRunner executes a unit of work inside a MongoDB transaction.
//
// Checkout and payment confirmation each touch several collections (orders,
// inventory, carts, payments, outbox) and must not leave a half-applied state
// behind, so they run through here. Atlas runs a replica set, which
// transactions require.
type TxRunner struct {
	client *mongo.Client
}

// NewTxRunner builds a runner bound to the client.
func NewTxRunner(client *mongo.Client) *TxRunner {
	return &TxRunner{client: client}
}

// Run executes fn inside a transaction, retrying on transient errors as the
// driver's convenient-transaction API does. The context passed to fn carries
// the session: every repository call made with it joins the transaction, and
// any call made with a different context does not.
func (t *TxRunner) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	session, err := t.client.StartSession()
	if err != nil {
		return fmt.Errorf("mongo: start session: %w", err)
	}
	defer session.EndSession(ctx)

	// Majority read and write concerns make the committed state immediately
	// visible to the reads that follow, which matters for stock accounting.
	txOpts := options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetWriteConcern(writeconcern.Majority())

	_, err = session.WithTransaction(ctx, func(sessCtx context.Context) (any, error) {
		return nil, fn(sessCtx)
	}, txOpts)

	return err
}
