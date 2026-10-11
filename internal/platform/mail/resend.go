// Package mail wraps Resend, the transactional email provider.
//
// Delivery always happens in the worker, never in a request handler: a slow or
// failing provider must not hold up a checkout or a registration.
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
)

// requestTimeout bounds a send so one stuck call cannot occupy a worker slot.
const requestTimeout = 15 * time.Second

// endpoint is Resend's send API.
const endpoint = "https://api.resend.com/emails"

// ErrNotConfigured is returned when no API key is present.
var ErrNotConfigured = fmt.Errorf("mail: Resend is not configured")

// Message is one email to send.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Sender delivers transactional email.
type Sender interface {
	Send(ctx context.Context, message Message) error
	Configured() bool
	// Missing names the configuration that still has to be set. It is empty
	// exactly when Configured reports true, and it exists so a process can
	// say *what* is missing rather than only that something is.
	Missing() []string
}

// Resend is the Resend-backed Sender.
type Resend struct {
	apiKey string
	from   string
	client *http.Client
}

// NewResend builds the client from configuration. A missing key is not fatal:
// the worker logs the email it would have sent and carries on, so local
// development needs no provider account.
func NewResend(cfg config.ResendConfig) *Resend {
	return &Resend{
		apiKey: strings.TrimSpace(cfg.APIKey),
		from:   strings.TrimSpace(cfg.EmailFrom),
		client: &http.Client{Timeout: requestTimeout},
	}
}

// Configured reports whether email can actually be delivered.
func (r *Resend) Configured() bool {
	return r.apiKey != "" && r.from != ""
}

// Missing names the environment variables that still have to be set, so a
// startup log can point straight at them. A blank sender is the common case
// during local development and the reason `Send` falls back to logging, which
// is easy to mistake for a delivery failure.
func (r *Resend) Missing() []string {
	var missing []string
	if r.apiKey == "" {
		missing = append(missing, "RESEND_API_KEY")
	}
	if r.from == "" {
		missing = append(missing, "EMAIL_FROM")
	}
	return missing
}

// Send delivers one message.
func (r *Resend) Send(ctx context.Context, message Message) error {
	if !r.Configured() {
		return ErrNotConfigured
	}

	payload := map[string]any{
		"from":    r.from,
		"to":      []string{message.To},
		"subject": message.Subject,
	}
	if message.HTML != "" {
		payload["html"] = message.HTML
	}
	if message.Text != "" {
		payload["text"] = message.Text
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mail: encode message: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mail: build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: send: %w", err)
	}
	defer res.Body.Close()

	// Cap the response so a misbehaving upstream cannot exhaust memory.
	response, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))

	if res.StatusCode >= http.StatusBadRequest {
		// 4xx is a permanent failure (bad address, rejected domain); 5xx is
		// worth retrying. The caller decides based on this distinction.
		return &DeliveryError{
			StatusCode: res.StatusCode,
			Body:       string(response),
		}
	}

	return nil
}

// DeliveryError is a provider rejection, carrying enough to decide whether a
// retry is worthwhile.
type DeliveryError struct {
	StatusCode int
	Body       string
}

func (e *DeliveryError) Error() string {
	return fmt.Sprintf("mail: provider returned %d: %s", e.StatusCode, e.Body)
}

// Permanent reports whether retrying would be pointless.
func (e *DeliveryError) Permanent() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != http.StatusTooManyRequests
}
