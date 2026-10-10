// Package queue wraps Asynq, the Redis-backed job queue.
//
// It owns the task vocabulary shared by the outbox publisher (which enqueues)
// and the worker (which consumes), so neither has to know the other's shape.
package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	goredis "github.com/redis/go-redis/v9"
)

// Task types. One per domain event the worker acts on.
const (
	TaskSendEmail          = "email:send"
	TaskExpireReservations = "orders:expire_reservations"
	TaskPurgeSlots         = "consultations:purge_slots"
)

// Queue names, in descending priority. Email is latency-sensitive because a
// customer is waiting for it; maintenance can wait.
const (
	QueueDefault     = "default"
	QueueMaintenance = "maintenance"
)

// MaxRetries bounds delivery attempts before a task is parked for inspection.
const MaxRetries = 5

// taskTimeout bounds a single attempt.
const taskTimeout = 30 * time.Second

// EmailPayload is the task body for a transactional email.
//
// EventID is the outbox event that produced it and doubles as the task's
// identifier, which is what makes enqueueing idempotent: re-publishing the
// same event cannot queue a second copy.
type EmailPayload struct {
	EventID   string         `json:"event_id"`
	EventType string         `json:"event_type"`
	To        string         `json:"to"`
	Data      map[string]any `json:"data"`
}

// NewEmailTask builds an email task from an outbox event.
func NewEmailTask(payload EmailPayload) (*asynq.Task, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("queue: encode email payload: %w", err)
	}

	return asynq.NewTask(TaskSendEmail, body,
		// The event id is the deduplication key: Asynq rejects a second task
		// with the same id while the first is still in the queue.
		asynq.TaskID(payload.EventID),
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(MaxRetries),
		asynq.Timeout(taskTimeout),
		// Keep completed tasks briefly so a duplicate publish within the
		// window is rejected rather than re-sent.
		asynq.Retention(24*time.Hour),
	), nil
}

// NewMaintenanceTask builds a periodic housekeeping task.
func NewMaintenanceTask(taskType string) *asynq.Task {
	return asynq.NewTask(taskType, nil,
		asynq.Queue(QueueMaintenance),
		asynq.MaxRetry(2),
		asynq.Timeout(2*time.Minute),
	)
}

// RedisOptions adapts a Redis URL to the connection options Asynq expects, so
// the queue and the application share one setting.
func RedisOptions(url string) (asynq.RedisClientOpt, error) {
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return asynq.RedisClientOpt{}, fmt.Errorf("queue: parse redis url: %w", err)
	}

	return asynq.RedisClientOpt{
		Addr:     opts.Addr,
		Username: opts.Username,
		Password: opts.Password,
		DB:       opts.DB,
		// TLS is configured through the URL scheme (rediss://).
		TLSConfig: opts.TLSConfig,
	}, nil
}

// NewClient opens an enqueueing client.
func NewClient(url string) (*asynq.Client, error) {
	opts, err := RedisOptions(url)
	if err != nil {
		return nil, err
	}
	return asynq.NewClient(opts), nil
}

// QueuePriorities weights the worker's queues.
func QueuePriorities() map[string]int {
	return map[string]int{
		QueueDefault:     6,
		QueueMaintenance: 1,
	}
}
