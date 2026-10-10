package payment

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"testing"

	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
)

// testSecret stands in for the Paystack secret key.
const testSecret = "sk_test_signature_verification"

// sign produces the signature Paystack would send for a body.
func sign(body []byte) string {
	mac := hmac.New(sha512.New, []byte(testSecret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	client := NewPaystack(config.PaystackConfig{SecretKey: testSecret, BaseURL: "https://api.paystack.co"})

	body := []byte(`{"event":"charge.success","data":{"reference":"PSK-ABC","amount":650000}}`)

	if !client.VerifySignature(body, sign(body)) {
		t.Error("VerifySignature() rejected a correctly signed body")
	}

	if client.VerifySignature(body, "deadbeef") {
		t.Error("VerifySignature() accepted a forged signature")
	}

	if client.VerifySignature(body, "") {
		t.Error("VerifySignature() accepted a missing signature")
	}

	// The signature covers the exact bytes, so any change invalidates it.
	tampered := []byte(`{"event":"charge.success","data":{"reference":"PSK-ABC","amount":1}}`)
	if client.VerifySignature(tampered, sign(body)) {
		t.Error("VerifySignature() accepted a body that had been altered after signing")
	}
}

func TestVerifySignatureWithoutKey(t *testing.T) {
	client := NewPaystack(config.PaystackConfig{})

	body := []byte(`{}`)
	if client.VerifySignature(body, sign(body)) {
		t.Error("VerifySignature() accepted a webhook while no secret key is configured")
	}
}

func TestConfigured(t *testing.T) {
	if NewPaystack(config.PaystackConfig{}).Configured() {
		t.Error("Configured() reported true without credentials")
	}

	client := NewPaystack(config.PaystackConfig{SecretKey: testSecret, BaseURL: "https://api.paystack.co"})
	if !client.Configured() {
		t.Error("Configured() reported false with credentials")
	}
}

func TestParseWebhook(t *testing.T) {
	body := []byte(`{
		"event": "charge.success",
		"data": {
			"id": 2009,
			"reference": "PSK-ABC",
			"status": "SUCCESS",
			"amount": 650000,
			"currency": "ngn",
			"channel": "card",
			"paid_at": "2026-10-05T12:00:00Z",
			"gateway_response": "Successful"
		}
	}`)

	event, err := ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook() returned error: %v", err)
	}

	if event.Event != "charge.success" {
		t.Errorf("event = %q, want charge.success", event.Event)
	}
	// Status and currency are normalised so comparisons elsewhere are simple.
	if event.Transaction.Status != StatusSuccess {
		t.Errorf("status = %q, want %q", event.Transaction.Status, StatusSuccess)
	}
	if event.Transaction.Currency != "NGN" {
		t.Errorf("currency = %q, want NGN", event.Transaction.Currency)
	}
	if event.Transaction.AmountKobo != 650_000 {
		t.Errorf("amount = %d, want 650000", event.Transaction.AmountKobo)
	}
	if event.Transaction.PaidAt == nil {
		t.Error("paid_at was not parsed")
	}
}

func TestParseWebhookRejectsGarbage(t *testing.T) {
	if _, err := ParseWebhook([]byte("not json")); err == nil {
		t.Error("ParseWebhook() accepted a body that is not JSON")
	}
}
