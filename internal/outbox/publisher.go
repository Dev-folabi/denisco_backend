package outbox

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/queue"
	"github.com/hibiken/asynq"
)

// Publisher tuning.
const (
	// pollInterval is how often the publisher looks for new events. Events are
	// emitted inside request handling, so a short interval keeps email prompt
	// without hammering MongoDB.
	pollInterval = 3 * time.Second
	// batchSize bounds one sweep.
	batchSize = 50
	// claimTimeout is how long a claimed event may sit before another
	// publisher may reclaim it. It only matters after a crash.
	claimTimeout = 2 * time.Minute
)

// Publisher moves committed domain events onto the job queue.
//
// It is a separate process from the API so that publishing cannot slow a
// request, and separate from the worker so that a slow email provider cannot
// stall publication.
type Publisher struct {
	events Reader
	client *asynq.Client
	log    *slog.Logger
}

// NewPublisher wires the publisher to the outbox and the queue.
func NewPublisher(events Reader, client *asynq.Client, log *slog.Logger) *Publisher {
	return &Publisher{events: events, client: client, log: log}
}

// Run polls until the context is cancelled.
func (p *Publisher) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	p.log.Info("outbox publisher started",
		slog.Duration("poll_interval", pollInterval),
		slog.Int("batch_size", batchSize),
	)

	for {
		select {
		case <-ctx.Done():
			p.log.Info("outbox publisher stopping")
			return nil
		case <-ticker.C:
			if published := p.sweep(ctx); published > 0 {
				p.log.Debug("outbox events published", slog.Int("count", published))
			}
		}
	}
}

// sweep claims and publishes one batch, returning how many went out.
func (p *Publisher) sweep(ctx context.Context) int {
	claimed, err := p.events.Claim(ctx, batchSize, claimTimeout)
	if err != nil {
		p.log.Error("could not claim outbox events", slog.String("error", err.Error()))
		return 0
	}

	published := 0
	for _, event := range claimed {
		if err := p.publish(ctx, event); err != nil {
			p.log.Error("could not publish outbox event",
				slog.String("event_id", event.ID.Hex()),
				slog.String("event_type", event.Type),
				slog.Int("attempts", event.Attempts),
				slog.String("error", err.Error()),
			)

			if markErr := p.events.MarkFailed(ctx, event.ID, err.Error()); markErr != nil {
				p.log.Error("could not record the publication failure",
					slog.String("event_id", event.ID.Hex()),
					slog.String("error", markErr.Error()),
				)
			}
			continue
		}

		if err := p.events.MarkPublished(ctx, event.ID); err != nil {
			// The task is already queued. Leaving the event claimed would have
			// it republished later, which the queue's deduplication absorbs.
			p.log.Error("could not mark an event as published",
				slog.String("event_id", event.ID.Hex()),
				slog.String("error", err.Error()),
			)
		}

		published++
	}

	return published
}

// publish turns one event into a queued task.
func (p *Publisher) publish(ctx context.Context, event Event) error {
	task, err := queue.NewEmailTask(queue.EmailPayload{
		EventID:   event.ID.Hex(),
		EventType: event.Type,
		To:        recipient(event.Payload),
		Data:      event.Payload,
	})
	if err != nil {
		return err
	}

	if _, err := p.client.EnqueueContext(ctx, task); err != nil {
		// The event id is the task id, so a republication of an event whose
		// task is still queued is not an error — it is the deduplication
		// working.
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			p.log.Debug("event already queued; skipping",
				slog.String("event_id", event.ID.Hex()))
			return nil
		}
		return err
	}

	return nil
}

// recipient reads the address an event's email should go to.
func recipient(payload map[string]any) string {
	if email, ok := payload["email"].(string); ok {
		return email
	}
	return ""
}
