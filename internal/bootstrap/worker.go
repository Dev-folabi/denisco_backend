package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/jobs"
	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/queue"
	"github.com/hibiken/asynq"
)

// RunWorker starts the background job consumer.
//
// The worker owns every slow or failure-prone side effect — sending email,
// expiring reservations, tidying the booking calendar — so a request handler
// never waits on one.
func RunWorker(c *Container) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisOpts, err := queue.RedisOptions(c.Config.Redis.URL)
	if err != nil {
		return err
	}

	server := asynq.NewServer(redisOpts, asynq.Config{
		Concurrency: c.Config.Worker.Concurrency,
		Queues:      queue.QueuePriorities(),
		// Exponential backoff, so a provider outage is retried with
		// increasing patience rather than hammered.
		RetryDelayFunc: func(attempt int, _ error, _ *asynq.Task) time.Duration {
			return time.Duration(1<<uint(attempt)) * 10 * time.Second
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			retried, _ := asynq.GetRetryCount(ctx)
			maxRetry, _ := asynq.GetMaxRetry(ctx)

			log := c.Logger.With(
				slog.String("job", task.Type()),
				slog.Int("attempt", retried+1),
				slog.Int("max_attempts", maxRetry+1),
				slog.String("error", err.Error()),
			)

			if retried >= maxRetry {
				// Asynq moves the task to its archive, where it stays for
				// inspection rather than disappearing.
				log.Error("job failed permanently and was archived")
				return
			}
			log.Warn("job failed; will retry")
		}),
		Logger: asynqLogger{log: c.Logger},
	})

	handlers := jobs.NewHandlers(
		c.Mail,
		c.OrderService,
		c.ConsultationService,
		c.Config.App.WebURL,
		c.Config.CORS.AdminOrigin,
		c.Config.Resend.AdminEmail,
		c.Logger,
	)

	mux := asynq.NewServeMux()
	handlers.Register(mux)

	// Periodic housekeeping. The scheduler enqueues; the same worker pool runs
	// the tasks, so there is no extra process to deploy.
	scheduler := asynq.NewScheduler(redisOpts, &asynq.SchedulerOpts{
		Logger: asynqLogger{log: c.Logger},
	})

	if _, err := scheduler.Register("@every 5m", queue.NewMaintenanceTask(queue.TaskExpireReservations)); err != nil {
		return fmt.Errorf("bootstrap: schedule reservation expiry: %w", err)
	}
	if _, err := scheduler.Register("@every 24h", queue.NewMaintenanceTask(queue.TaskPurgeSlots)); err != nil {
		return fmt.Errorf("bootstrap: schedule slot purge: %w", err)
	}

	if err := scheduler.Start(); err != nil {
		return fmt.Errorf("bootstrap: start scheduler: %w", err)
	}

	serverErr := make(chan error, 1)
	go func() {
		// Naming the unset variables is the whole point: "not configured"
		// says nothing about which line of .env to fix, and an undelivered
		// email otherwise looks identical to a delivered one.
		attrs := []any{
			slog.Int("concurrency", c.Config.Worker.Concurrency),
			slog.Bool("email_configured", c.Mail.Configured()),
		}
		if !c.Mail.Configured() {
			attrs = append(attrs,
				slog.String("email_missing", strings.Join(c.Mail.Missing(), ",")),
				slog.String("email_delivery", "disabled: messages will be logged instead of sent"),
			)
		}

		c.Logger.Info("worker started", attrs...)
		if err := server.Run(mux); err != nil {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		scheduler.Shutdown()
		return fmt.Errorf("bootstrap: worker: %w", err)
	case <-ctx.Done():
		c.Logger.Info("shutdown signal received; draining jobs")
	}

	scheduler.Shutdown()
	// Shutdown waits for in-flight jobs so a running email is not cut short.
	server.Shutdown()

	c.Logger.Info("worker stopped cleanly")
	return nil
}

// RunOutboxPublisher starts the outbox-to-queue publisher.
//
// Domain events are committed to MongoDB alongside the state change that
// produced them; this process is what moves them onto the queue, so an event
// is never lost to a Redis outage.
func RunOutboxPublisher(c *Container) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client, err := queue.NewClient(c.Config.Redis.URL)
	if err != nil {
		return err
	}
	defer client.Close()

	publisher := outbox.NewPublisher(c.OutboxReader, client, c.Logger)

	if err := publisher.Run(ctx); err != nil {
		return fmt.Errorf("bootstrap: outbox publisher: %w", err)
	}

	c.Logger.Info("outbox publisher stopped cleanly")
	return nil
}

// asynqLogger adapts the application logger to Asynq's interface so worker
// output joins the same structured stream as everything else.
type asynqLogger struct {
	log *slog.Logger
}

func (a asynqLogger) Debug(args ...any) { a.log.Debug(fmt.Sprint(args...)) }
func (a asynqLogger) Info(args ...any)  { a.log.Info(fmt.Sprint(args...)) }
func (a asynqLogger) Warn(args ...any)  { a.log.Warn(fmt.Sprint(args...)) }
func (a asynqLogger) Error(args ...any) { a.log.Error(fmt.Sprint(args...)) }
func (a asynqLogger) Fatal(args ...any) { a.log.Error(fmt.Sprint(args...)) }
