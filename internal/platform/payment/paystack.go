// Package payment wraps Paystack, the payment provider.
//
// The client owns the HTTP contract only: deciding what a result means for an
// order belongs to the payments module.
package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// requestTimeout bounds every call to Paystack so a slow provider cannot hold
// a checkout request open.
const requestTimeout = 20 * time.Second

// Provider-side transaction statuses Paystack reports.
const (
	StatusSuccess   = "success"
	StatusFailed    = "failed"
	StatusAbandoned = "abandoned"
	StatusOngoing   = "ongoing"
	StatusPending   = "pending"
)

// Errors surfaced to callers.
var (
	ErrNotConfigured = apperror.New(http.StatusServiceUnavailable, "PAYMENTS_UNAVAILABLE", "Online payment is not available at the moment.")
	ErrProvider      = apperror.New(http.StatusBadGateway, "PAYMENT_PROVIDER_ERROR", "The payment provider could not be reached. Please try again.")
)

// Paystack calls the provider's REST API.
type Paystack struct {
	secretKey string
	baseURL   string
	client    *http.Client
}

// NewPaystack builds the client from configuration. Missing credentials are
// not fatal at startup: only the payment endpoints report the integration as
// unavailable.
func NewPaystack(cfg config.PaystackConfig) *Paystack {
	return &Paystack{
		secretKey: cfg.SecretKey,
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		client:    &http.Client{Timeout: requestTimeout},
	}
}

// Configured reports whether payments can be initialized.
func (p *Paystack) Configured() bool {
	return p.secretKey != "" && p.baseURL != ""
}

// InitializeRequest starts a transaction.
type InitializeRequest struct {
	Email       string
	AmountKobo  int64
	Reference   string
	CallbackURL string
	Metadata    map[string]any
}

// InitializeResult is the checkout handover.
type InitializeResult struct {
	AuthorizationURL string
	AccessCode       string
	Reference        string
}

// Transaction is the provider's view of a transaction, as returned by verify
// and carried in webhooks.
type Transaction struct {
	Reference       string
	Status          string
	AmountKobo      int64
	Currency        string
	Channel         string
	PaidAt          *time.Time
	GatewayResponse string
	ProviderID      int64
}

// Initialize starts a transaction and returns the URL to send the customer to.
func (p *Paystack) Initialize(ctx context.Context, req InitializeRequest) (*InitializeResult, error) {
	if !p.Configured() {
		return nil, ErrNotConfigured
	}

	body := map[string]any{
		"email":     req.Email,
		"amount":    req.AmountKobo,
		"reference": req.Reference,
		"currency":  "NGN",
	}
	if req.CallbackURL != "" {
		body["callback_url"] = req.CallbackURL
	}
	if len(req.Metadata) > 0 {
		body["metadata"] = req.Metadata
	}

	var response struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			AuthorizationURL string `json:"authorization_url"`
			AccessCode       string `json:"access_code"`
			Reference        string `json:"reference"`
		} `json:"data"`
	}

	if err := p.call(ctx, http.MethodPost, "/transaction/initialize", body, &response); err != nil {
		return nil, err
	}

	if !response.Status || response.Data.AuthorizationURL == "" {
		return nil, ErrProvider.WithDetails(map[string]any{"provider_message": response.Message})
	}

	return &InitializeResult{
		AuthorizationURL: response.Data.AuthorizationURL,
		AccessCode:       response.Data.AccessCode,
		Reference:        response.Data.Reference,
	}, nil
}

// Verify asks Paystack for the authoritative state of a transaction.
//
// Verification is always done server-side: a customer returning to the
// callback URL proves nothing, and the webhook may not have arrived yet.
func (p *Paystack) Verify(ctx context.Context, reference string) (*Transaction, error) {
	if !p.Configured() {
		return nil, ErrNotConfigured
	}

	var response struct {
		Status  bool               `json:"status"`
		Message string             `json:"message"`
		Data    transactionPayload `json:"data"`
	}

	if err := p.call(ctx, http.MethodGet, "/transaction/verify/"+reference, nil, &response); err != nil {
		return nil, err
	}

	if !response.Status {
		return nil, ErrProvider.WithDetails(map[string]any{"provider_message": response.Message})
	}

	return response.Data.toTransaction(), nil
}

// Refund asks Paystack to refund a transaction in full.
func (p *Paystack) Refund(ctx context.Context, reference string, amountKobo int64) error {
	if !p.Configured() {
		return ErrNotConfigured
	}

	body := map[string]any{"transaction": reference}
	if amountKobo > 0 {
		body["amount"] = amountKobo
	}

	var response struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
	}

	if err := p.call(ctx, http.MethodPost, "/refund", body, &response); err != nil {
		return err
	}

	if !response.Status {
		return ErrProvider.WithDetails(map[string]any{"provider_message": response.Message})
	}

	return nil
}

// VerifySignature checks a webhook against the raw request body.
//
// Paystack signs the exact bytes it sent, so the comparison must run on the
// raw body before any JSON decoding, and in constant time.
func (p *Paystack) VerifySignature(rawBody []byte, signature string) bool {
	if p.secretKey == "" || signature == "" {
		return false
	}

	mac := hmac.New(sha512.New, []byte(p.secretKey))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}

// WebhookEvent is a decoded Paystack webhook.
type WebhookEvent struct {
	Event       string
	Transaction *Transaction
}

// ParseWebhook decodes a webhook body whose signature has already been checked.
func ParseWebhook(rawBody []byte) (*WebhookEvent, error) {
	var payload struct {
		Event string             `json:"event"`
		Data  transactionPayload `json:"data"`
	}

	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return nil, apperror.BadRequest.WithDetails(map[string]any{"body": "is not valid webhook JSON"}).Wrap(err)
	}

	return &WebhookEvent{Event: payload.Event, Transaction: payload.Data.toTransaction()}, nil
}

// transactionPayload is Paystack's transaction shape, shared by verify and
// webhook responses.
type transactionPayload struct {
	ID              int64  `json:"id"`
	Reference       string `json:"reference"`
	Status          string `json:"status"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	Channel         string `json:"channel"`
	PaidAt          string `json:"paid_at"`
	GatewayResponse string `json:"gateway_response"`
}

// toTransaction normalises the provider payload.
func (t transactionPayload) toTransaction() *Transaction {
	transaction := &Transaction{
		ProviderID:      t.ID,
		Reference:       t.Reference,
		Status:          strings.ToLower(t.Status),
		AmountKobo:      t.Amount,
		Currency:        strings.ToUpper(t.Currency),
		Channel:         t.Channel,
		GatewayResponse: t.GatewayResponse,
	}

	if t.PaidAt != "" {
		if parsed, err := time.Parse(time.RFC3339, t.PaidAt); err == nil {
			transaction.PaidAt = &parsed
		}
	}

	return transaction
}

// call performs a request against the Paystack API.
func (p *Paystack) call(ctx context.Context, method, path string, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return apperror.Internal.Wrap(err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reader)
	if err != nil {
		return apperror.Internal.Wrap(err)
	}

	req.Header.Set("Authorization", "Bearer "+p.secretKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return ErrProvider.Wrap(err)
	}
	defer res.Body.Close()

	// Cap the response so a misbehaving upstream cannot exhaust memory.
	payload, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ErrProvider.Wrap(err)
	}

	if res.StatusCode >= http.StatusInternalServerError {
		return ErrProvider.Wrap(fmt.Errorf("paystack: status %d", res.StatusCode))
	}

	if err := json.Unmarshal(payload, out); err != nil {
		return ErrProvider.Wrap(fmt.Errorf("paystack: decode response: %w", err))
	}

	return nil
}
