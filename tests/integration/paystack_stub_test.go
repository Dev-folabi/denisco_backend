package integration

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// stubSecretKey is the key the stand-in and the application share, so webhook
// signatures can be produced in the tests exactly as Paystack produces them.
const stubSecretKey = "sk_test_denisco_integration"

// paystackStub stands in for Paystack.
//
// It is a real HTTP server speaking the provider's JSON contract rather than a
// mocked Go interface, so the client's request building, response decoding and
// signature verification are all under test too.
type paystackStub struct {
	server *httptest.Server

	mu sync.Mutex
	// attempts records what each reference was initialized with, so verify
	// can answer with a consistent amount the way the provider does.
	attempts map[string]*stubAttempt
	refunds  []string
	// failInitialize makes the next initialize call fail, for the branch
	// where the provider is unreachable at checkout.
	failInitialize bool
}

// stubAttempt is one transaction the stand-in knows about.
type stubAttempt struct {
	reference string
	amount    int64
	status    string
	channel   string
	providerI int64
}

// newPaystackStub starts the stand-in.
func newPaystackStub() *paystackStub {
	stub := &paystackStub{attempts: map[string]*stubAttempt{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/transaction/initialize", stub.initialize)
	mux.HandleFunc("/transaction/verify/", stub.verify)
	mux.HandleFunc("/refund", stub.refund)

	stub.server = httptest.NewServer(mux)
	return stub
}

// URL is the base URL the application is configured with.
func (s *paystackStub) URL() string { return s.server.URL }

// Close shuts the stand-in down.
func (s *paystackStub) Close() { s.server.Close() }

// reset forgets every transaction between tests.
func (s *paystackStub) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.attempts = map[string]*stubAttempt{}
	s.refunds = nil
	s.failInitialize = false
}

// settle sets what verify and webhooks will report for a reference.
func (s *paystackStub) settle(reference, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if attempt, ok := s.attempts[reference]; ok {
		attempt.status = status
	}
}

// tamperAmount makes the stand-in report a different amount than the order
// total, which is how a tampered-with callback is simulated.
func (s *paystackStub) tamperAmount(reference string, amount int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if attempt, ok := s.attempts[reference]; ok {
		attempt.amount = amount
	}
}

// amountFor returns the amount a reference was initialized with.
func (s *paystackStub) amountFor(reference string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if attempt, ok := s.attempts[reference]; ok {
		return attempt.amount
	}
	return 0
}

// refunded returns the references refunded through the stand-in.
func (s *paystackStub) refunded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.refunds...)
}

// initialize records an attempt and hands back a checkout URL.
func (s *paystackStub) initialize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reference string `json:"reference"`
		Amount    int64  `json:"amount"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": false, "message": "bad request"})
		return
	}

	s.mu.Lock()
	if s.failInitialize {
		s.mu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": false, "message": "declined by the provider"})
		return
	}

	s.attempts[body.Reference] = &stubAttempt{
		reference: body.Reference,
		amount:    body.Amount,
		// Paystack reports a transaction as pending until the customer acts.
		status:    "pending",
		channel:   "card",
		providerI: int64(len(s.attempts) + 1_000_000),
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  true,
		"message": "Authorization URL created",
		"data": map[string]any{
			"authorization_url": "https://checkout.paystack.test/" + body.Reference,
			"access_code":       "acc_" + body.Reference,
			"reference":         body.Reference,
		},
	})
}

// verify answers with the transaction's current state.
func (s *paystackStub) verify(w http.ResponseWriter, r *http.Request) {
	reference := strings.TrimPrefix(r.URL.Path, "/transaction/verify/")

	s.mu.Lock()
	attempt, ok := s.attempts[reference]
	var payload map[string]any
	if ok {
		payload = attempt.payload()
	}
	s.mu.Unlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"status": false, "message": "transaction not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  true,
		"message": "Verification successful",
		"data":    payload,
	})
}

// refund records a refund request.
func (s *paystackStub) refund(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Transaction string `json:"transaction"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	s.mu.Lock()
	s.refunds = append(s.refunds, body.Transaction)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"status": true, "message": "Refund queued"})
}

// payload is the transaction shape both verify and webhooks carry.
func (a *stubAttempt) payload() map[string]any {
	payload := map[string]any{
		"id":               a.providerI,
		"reference":        a.reference,
		"status":           a.status,
		"amount":           a.amount,
		"currency":         "NGN",
		"channel":          a.channel,
		"gateway_response": "Successful",
	}

	if a.status == "success" {
		payload["paid_at"] = time.Now().UTC().Format(time.RFC3339)
	}

	return payload
}

// webhookBody builds the body Paystack would POST for a reference, together
// with the signature it would send alongside it.
func (s *paystackStub) webhookBody(event, reference string) (body []byte, signature string, err error) {
	s.mu.Lock()
	attempt, ok := s.attempts[reference]
	var data map[string]any
	if ok {
		data = attempt.payload()
	}
	s.mu.Unlock()

	if !ok {
		return nil, "", fmt.Errorf("paystack stub: unknown reference %q", reference)
	}

	body, err = json.Marshal(map[string]any{"event": event, "data": data})
	if err != nil {
		return nil, "", err
	}

	mac := hmac.New(sha512.New, []byte(stubSecretKey))
	mac.Write(body)

	return body, hex.EncodeToString(mac.Sum(nil)), nil
}

// writeJSON is the stand-in's response helper.
func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
