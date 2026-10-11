// Package domain holds a message sent through the contact form and the rules
// it has to satisfy before it is accepted.
package domain

import (
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
)

// Bounds on a contact message. These restate the API's own limits, which the
// web form mirrors; the server is the one that has to hold the line, because
// the form can be bypassed.
const (
	MaxNameLength    = 120
	MaxEmailLength   = 254
	MaxSubjectLength = 150
	MaxBodyLength    = 2000
	MinBodyLength    = 10
)

// Validation errors. They are 400s a visitor can act on.
var (
	ErrNameRequired    = apperror.New(http.StatusBadRequest, "CONTACT_NAME_REQUIRED", "Please tell us your name.")
	ErrEmailInvalid    = apperror.New(http.StatusBadRequest, "CONTACT_EMAIL_INVALID", "Please give a valid email address so we can reply.")
	ErrSubjectRequired = apperror.New(http.StatusBadRequest, "CONTACT_SUBJECT_REQUIRED",
		"Please give your message a subject.")
	ErrMessageTooShort = apperror.New(http.StatusBadRequest, "CONTACT_MESSAGE_TOO_SHORT",
		"Please write a little more so we can understand your enquiry.")
	ErrMessageTooLong = apperror.New(http.StatusBadRequest, "CONTACT_MESSAGE_TOO_LONG",
		"Your message is too long. Please shorten it and send again.")
)

// Message is one submission of the contact form.
type Message struct {
	Name      string
	Email     string
	Phone     string
	Subject   string
	Body      string
	Submitted time.Time
}

// Normalise trims a message's fields and folds the name's whitespace, so a
// submission of "  Ada   Obi " and one of "Ada Obi" are the same message.
func (m *Message) Normalise() {
	m.Name = strings.Join(strings.Fields(m.Name), " ")
	m.Email = strings.TrimSpace(m.Email)
	m.Phone = strings.TrimSpace(m.Phone)
	m.Subject = strings.Join(strings.Fields(m.Subject), " ")
	m.Body = strings.TrimSpace(m.Body)
	m.Submitted = m.Submitted.UTC()
}

// Validate reports whether the message can be accepted.
func (m Message) Validate() error {
	if m.Name == "" {
		return ErrNameRequired
	}
	if len(m.Name) > MaxNameLength {
		return apperror.New(http.StatusBadRequest, "CONTACT_NAME_REQUIRED", "Please give a shorter name.")
	}

	if address, err := mail.ParseAddress(m.Email); err != nil || address.Address != m.Email {
		return ErrEmailInvalid
	}
	if len(m.Email) > MaxEmailLength {
		return ErrEmailInvalid
	}

	if m.Subject == "" || len(m.Subject) > MaxSubjectLength {
		return ErrSubjectRequired
	}

	switch {
	case len(m.Body) < MinBodyLength:
		return ErrMessageTooShort
	case len(m.Body) > MaxBodyLength:
		return ErrMessageTooLong
	}

	return nil
}
