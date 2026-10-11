package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestContactFormReachesTheFarm covers the visitor with no account: the
// contact page is the only way such a person reaches the farm, so the
// submission has to land in an outbox event addressed to staff and the
// response has to be the same whether or not mail is configured.
func TestContactFormReachesTheFarm(t *testing.T) {
	e := require(t)

	body, err := json.Marshal(map[string]string{
		"name":    "  Ayo   Bankole ",
		"email":   "ayo@example.com",
		"phone":   "0803 000 0000",
		"subject": "Snail farming startup",
		"message": "I want to start a snail farm on two acres in Ogun State. What breed do you recommend?",
	})
	if err != nil {
		t.Fatalf("could not encode request: %v", err)
	}

	recorder := e.post(t, "/api/v1/contact", body, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/contact status = %d, want 200; body = %s", recorder.Code, recorder.Body.String())
	}

	if count := e.outboxCount(t, "contact.received"); count != 1 {
		t.Fatalf("contact.received events = %d, want 1", count)
	}

	var event struct {
		Type        string         `bson:"type"`
		AggregateID string         `bson:"aggregate_id"`
		Payload     map[string]any `bson:"payload"`
	}
	if err := e.db.Collection("outbox_events").FindOne(ctx(t), bson.M{"type": "contact.received"}).Decode(&event); err != nil {
		t.Fatalf("could not read the contact event: %v", err)
	}

	// The name is folded the way the domain folds it, so "  Ayo   Bankole "
	// and "Ayo Bankole" are the same message in the farm's inbox.
	if event.Payload["name"] != "Ayo Bankole" {
		t.Errorf("payload name = %v, want %q", event.Payload["name"], "Ayo Bankole")
	}
	if event.Payload["email"] != "ayo@example.com" {
		t.Errorf("payload email = %v, want %q", event.Payload["email"], "ayo@example.com")
	}
	if event.Payload["subject"] != "Snail farming startup" {
		t.Errorf("payload subject = %v, want %q", event.Payload["subject"], "Snail farming startup")
	}
	// It is addressed to staff: a visitor has written in, and there is
	// nobody else it concerns.
	if _, ok := event.Payload["to"]; ok {
		t.Errorf("payload to = %v, want the field absent so the worker addresses staff", event.Payload["to"])
	}
}

// TestContactFormRejectsWhatItCannotReplyTo covers the fields the server has
// to hold the line on, because the web form can be bypassed.
func TestContactFormRejectsWhatItCannotReplyTo(t *testing.T) {
	e := require(t)

	cases := []struct {
		name string
		body map[string]string
	}{
		{"no name", map[string]string{"email": "a@b.com", "subject": "Hi", "message": "long enough message"}},
		{"bad email", map[string]string{"name": "A", "email": "not-an-address", "subject": "Hi", "message": "long enough message"}},
		{"no subject", map[string]string{"name": "A", "email": "a@b.com", "message": "long enough message"}},
		{"message too short", map[string]string{"name": "A", "email": "a@b.com", "subject": "Hi", "message": "short"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatalf("could not encode request: %v", err)
			}

			recorder := e.post(t, "/api/v1/contact", body, "")
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	// Nothing rejected was recorded.
	if count := e.outboxCount(t, "contact.received"); count != 0 {
		t.Errorf("contact.received events = %d, want 0 after rejections", count)
	}
}
