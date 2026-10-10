// Package application coordinates the payment workflows: starting an attempt,
// applying the provider's verdict and refunding.
package application

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	"github.com/Dev-folabi/denisco_backend/internal/modules/payments/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/platform/payment"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// referenceAttempts bounds the retry loop for a unique payment reference.
const referenceAttempts = 10

// OrderPort is the payments module's view of the order lifecycle.
type OrderPort interface {
	// ForPayment loads an order that is still awaiting payment, checking that
	// the caller owns it.
	ForPayment(ctx context.Context, orderID string, userID bson.ObjectID, isAdmin bool) (*ordersdomain.Order, error)
	// Load returns an order regardless of its payment state.
	Load(ctx context.Context, orderID bson.ObjectID) (*ordersdomain.Order, error)
	// MarkPaid records a confirmed payment and deducts the reserved stock.
	MarkPaid(ctx context.Context, orderID bson.ObjectID, reference, method string, paidAt time.Time) error
	// MarkPaymentFailed records a failed attempt and releases the stock.
	MarkPaymentFailed(ctx context.Context, orderID bson.ObjectID, status string) error
	// MarkRefunded records a refund against the order.
	MarkRefunded(ctx context.Context, orderID bson.ObjectID) error
}

// BookingTarget is the consultation booking an attempt is paying for,
// expressed in the payments module's own terms so it does not depend on the
// consultation module's types.
type BookingTarget struct {
	ID        bson.ObjectID
	Reference string
	TypeName  string
	Amount    int64
	Currency  string
	Name      string
	Email     string
	// UserID is zero for a guest booking.
	UserID bson.ObjectID
}

// BookingPort is the payments module's view of the consultation lifecycle.
type BookingPort interface {
	// ForPayment loads a booking whose fee is still outstanding, checking
	// that the caller may pay it.
	ForPayment(ctx context.Context, bookingID, bookingReference string, userID bson.ObjectID, isAdmin bool) (*BookingTarget, error)
	// MarkPaid records a settled fee and confirms the booking.
	MarkPaid(ctx context.Context, bookingID bson.ObjectID, reference string, paidAt time.Time) error
	// MarkPaymentFailed records a declined or abandoned attempt.
	MarkPaymentFailed(ctx context.Context, bookingID bson.ObjectID, status string) error
	// MarkRefunded records a refunded fee.
	MarkRefunded(ctx context.Context, bookingID bson.ObjectID) error
}

// Service implements the payment workflows.
type Service struct {
	payments domain.Repository
	orders   OrderPort
	bookings BookingPort
	provider *payment.Paystack
	auditor  audit.Recorder
	// callbackURL is where Paystack returns the customer after checkout.
	callbackURL string
}

// NewService wires the payments module to its repository, the modules whose
// records it settles and the provider.
func NewService(
	payments domain.Repository,
	orders OrderPort,
	bookings BookingPort,
	provider *payment.Paystack,
	auditor audit.Recorder,
	callbackURL string,
) *Service {
	return &Service{
		payments:    payments,
		orders:      orders,
		bookings:    bookings,
		provider:    provider,
		auditor:     auditor,
		callbackURL: callbackURL,
	}
}

// Initialize starts a payment attempt for an order and returns the provider's
// checkout URL.
//
// A fresh reference is minted for every attempt, so a customer retrying after
// a failure never reuses the reference of the failed one.
func (s *Service) Initialize(ctx context.Context, orderID string, userID bson.ObjectID, isAdmin bool) (*InitializeView, error) {
	if !s.provider.Configured() {
		return nil, payment.ErrNotConfigured
	}

	order, err := s.orders.ForPayment(ctx, orderID, userID, isAdmin)
	if err != nil {
		return nil, apperror.From(err)
	}

	return s.initialize(ctx, attemptTarget{
		Purpose:      domain.PurposeOrder,
		OrderID:      order.ID,
		OrderNumber:  order.OrderNumber,
		UserID:       order.UserID,
		CustomerName: order.Customer.Name,
		Email:        order.Customer.Email,
		Amount:       order.Total,
		Currency:     order.Currency,
		Metadata: map[string]any{
			"purpose":      domain.PurposeOrder,
			"order_id":     order.ID.Hex(),
			"order_number": order.OrderNumber,
		},
	})
}

// InitializeBooking starts a payment attempt for a consultation booking.
//
// It mirrors Initialize rather than sharing its entry point because the two
// have different callers and different authorization: a booking may belong to
// no account at all, since the consultation page is public.
func (s *Service) InitializeBooking(ctx context.Context, bookingID, bookingReference string, userID bson.ObjectID, isAdmin bool) (*InitializeView, error) {
	if !s.provider.Configured() {
		return nil, payment.ErrNotConfigured
	}

	booking, err := s.bookings.ForPayment(ctx, bookingID, bookingReference, userID, isAdmin)
	if err != nil {
		return nil, apperror.From(err)
	}

	return s.initialize(ctx, attemptTarget{
		Purpose:          domain.PurposeConsultation,
		BookingID:        booking.ID,
		BookingReference: booking.Reference,
		UserID:           booking.UserID,
		CustomerName:     booking.Name,
		Email:            booking.Email,
		Amount:           booking.Amount,
		Currency:         booking.Currency,
		Metadata: map[string]any{
			"purpose":           domain.PurposeConsultation,
			"booking_id":        booking.ID.Hex(),
			"booking_reference": booking.Reference,
			"consultation":      booking.TypeName,
		},
	})
}

// attemptTarget is what a new attempt is being opened against, assembled by
// the caller that knows the record.
type attemptTarget struct {
	Purpose          string
	OrderID          bson.ObjectID
	OrderNumber      string
	BookingID        bson.ObjectID
	BookingReference string
	UserID           bson.ObjectID
	CustomerName     string
	Email            string
	Amount           int64
	Currency         string
	Metadata         map[string]any
}

// initialize records an attempt and opens it with the provider.
//
// The record is written before the provider is called so that an
// initialization that fails part way through is still visible, rather than
// being a charge nobody has a row for.
func (s *Service) initialize(ctx context.Context, target attemptTarget) (*InitializeView, error) {
	reference, err := s.newReference(ctx)
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	now := time.Now().UTC()
	record := &domain.Payment{
		ID:               bson.NewObjectID(),
		Reference:        reference,
		Purpose:          target.Purpose,
		OrderID:          target.OrderID,
		OrderNumber:      target.OrderNumber,
		BookingID:        target.BookingID,
		BookingReference: target.BookingReference,
		UserID:           target.UserID,
		CustomerName:     target.CustomerName,
		Email:            target.Email,
		Amount:           target.Amount,
		Currency:         target.Currency,
		Status:           domain.StatusInitialized,
		Provider:         domain.ProviderPaystack,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.payments.Create(ctx, record); err != nil {
		return nil, apperror.From(err)
	}

	result, err := s.provider.Initialize(ctx, payment.InitializeRequest{
		Email:       target.Email,
		AmountKobo:  target.Amount,
		Reference:   reference,
		CallbackURL: s.callbackURL,
		Metadata:    target.Metadata,
	})
	if err != nil {
		// The attempt stays on record as failed, so an abandoned
		// initialization is visible rather than silently lost.
		if _, settleErr := s.payments.Settle(ctx, reference, domain.SettlementUpdate{
			Status:          domain.StatusFailed,
			GatewayResponse: "initialization failed",
		}); settleErr != nil {
			logger.FromContext(ctx).Error("could not record failed initialization",
				slog.String("reference", reference),
			)
		}
		return nil, apperror.From(err)
	}

	if _, err := s.payments.Settle(ctx, reference, domain.SettlementUpdate{Status: domain.StatusPending}); err != nil {
		return nil, apperror.From(err)
	}

	logger.FromContext(ctx).Info("payment initialized",
		slog.String("module", "payments"),
		slog.String("purpose", record.Kind()),
		slog.String("subject_id", subjectID(record)),
		slog.String("payment_reference", reference),
		slog.Int64("amount", target.Amount),
	)

	return &InitializeView{
		Reference:        reference,
		AuthorizationURL: result.AuthorizationURL,
		AccessCode:       result.AccessCode,
		Amount:           target.Amount,
		Currency:         target.Currency,
		Purpose:          record.Kind(),
		OrderID:          hexOrEmpty(target.OrderID),
		OrderNumber:      target.OrderNumber,
		BookingID:        hexOrEmpty(target.BookingID),
		BookingReference: target.BookingReference,
	}, nil
}

// subjectID names the record an attempt belongs to, for logging.
func subjectID(record *domain.Payment) string {
	if record.Kind() == domain.PurposeConsultation {
		return record.BookingID.Hex()
	}
	return record.OrderID.Hex()
}

// hexOrEmpty renders an identifier, or an empty string when it is unset, so
// an order payment does not report a booking of all zeroes.
func hexOrEmpty(id bson.ObjectID) string {
	if id.IsZero() {
		return ""
	}
	return id.Hex()
}

// Verify asks the provider for the authoritative state of an attempt and
// applies it. The callback page calls this, because a customer arriving back
// from Paystack proves nothing on its own.
func (s *Service) Verify(ctx context.Context, reference string, userID bson.ObjectID, isAdmin bool) (*View, error) {
	record, err := s.payments.FindByReference(ctx, strings.TrimSpace(reference))
	if err != nil {
		return nil, apperror.From(err)
	}

	if err := authorizeView(record, userID, isAdmin); err != nil {
		return nil, err
	}

	if record.IsSettled() {
		view := newView(record)
		return &view, nil
	}

	transaction, err := s.provider.Verify(ctx, record.Reference)
	if err != nil {
		return nil, apperror.From(err)
	}

	if err := s.apply(ctx, record, transaction); err != nil {
		return nil, err
	}

	return s.Get(ctx, record.Reference, userID, isAdmin)
}

// HandleWebhook applies a provider callback whose signature has already been
// checked.
func (s *Service) HandleWebhook(ctx context.Context, event *payment.WebhookEvent) error {
	if event.Transaction == nil || event.Transaction.Reference == "" {
		return nil
	}

	record, err := s.payments.FindByReference(ctx, event.Transaction.Reference)
	if err != nil {
		// An unknown reference is not an error worth retrying: it belongs to
		// another integration or a test call.
		logger.FromContext(ctx).Warn("webhook referenced an unknown payment",
			slog.String("module", "payments"),
			slog.String("event", event.Event),
		)
		return nil
	}

	return s.apply(ctx, record, event.Transaction)
}

// apply records the provider's verdict against an attempt and whatever it was
// paying for.
//
// The amount is re-checked against the recorded amount: a transaction that
// paid a different sum is never treated as settling the order or the booking,
// however the request reached us.
func (s *Service) apply(ctx context.Context, record *domain.Payment, transaction *payment.Transaction) error {
	log := logger.FromContext(ctx)

	switch transaction.Status {
	case payment.StatusSuccess:
		if transaction.AmountKobo != record.Amount || (transaction.Currency != "" && transaction.Currency != record.Currency) {
			log.Error("payment amount does not match the amount due",
				slog.String("module", "payments"),
				slog.String("payment_reference", record.Reference),
				slog.Int64("expected", record.Amount),
				slog.Int64("received", transaction.AmountKobo),
			)

			if _, err := s.payments.Settle(ctx, record.Reference, domain.SettlementUpdate{
				Status:          domain.StatusFailed,
				GatewayResponse: "amount mismatch",
			}); err != nil {
				return apperror.From(err)
			}
			return domain.ErrAmountMismatch
		}

		paidAt := time.Now().UTC()
		if transaction.PaidAt != nil {
			paidAt = *transaction.PaidAt
		}

		applied, err := s.payments.Settle(ctx, record.Reference, domain.SettlementUpdate{
			Status:            domain.StatusSuccessful,
			ProviderReference: fmt.Sprint(transaction.ProviderID),
			Channel:           transaction.Channel,
			GatewayResponse:   transaction.GatewayResponse,
			PaidAt:            &paidAt,
		})
		if err != nil {
			return apperror.From(err)
		}
		if !applied {
			// Already settled by a parallel webhook or verify call.
			return nil
		}

		if record.Kind() == domain.PurposeConsultation {
			return s.bookings.MarkPaid(ctx, record.BookingID, record.Reference, paidAt)
		}

		return s.orders.MarkPaid(ctx, record.OrderID, record.Reference, transaction.Channel, paidAt)

	case payment.StatusFailed, payment.StatusAbandoned:
		status := domain.StatusFailed
		if transaction.Status == payment.StatusAbandoned {
			status = domain.StatusAbandoned
		}

		applied, err := s.payments.Settle(ctx, record.Reference, domain.SettlementUpdate{
			Status:          status,
			GatewayResponse: transaction.GatewayResponse,
		})
		if err != nil {
			return apperror.From(err)
		}
		if !applied {
			return nil
		}

		// A consultation slot is not consumed by the attempt, so unlike stock
		// it is not returned here: the booking keeps its time and the
		// customer can retry.
		if record.Kind() == domain.PurposeConsultation {
			return s.bookings.MarkPaymentFailed(ctx, record.BookingID, status)
		}

		// Returning the stock here is what keeps an abandoned checkout from
		// holding inventory indefinitely.
		return s.orders.MarkPaymentFailed(ctx, record.OrderID, status)

	default:
		// Still in flight; nothing to apply yet.
		return nil
	}
}

// Get returns one attempt, enforcing ownership.
func (s *Service) Get(ctx context.Context, reference string, userID bson.ObjectID, isAdmin bool) (*View, error) {
	record, err := s.payments.FindByReference(ctx, strings.TrimSpace(reference))
	if err != nil {
		return nil, apperror.From(err)
	}

	if err := authorizeView(record, userID, isAdmin); err != nil {
		return nil, err
	}

	view := newView(record)
	return &view, nil
}

// authorizeView decides who may read an attempt.
//
// An administrator may read any. A customer may read their own. A guest
// consultation payment belongs to no account, and the only way to ask for it
// is to hold its reference — 80 bits of randomness handed to the browser that
// opened the attempt — so possession of the reference is what stands in for a
// session there. A customer asking for someone else's gets "not found", so
// references cannot be probed.
func authorizeView(record *domain.Payment, userID bson.ObjectID, isAdmin bool) error {
	switch {
	case isAdmin:
		return nil
	case record.IsGuest():
		return nil
	case record.UserID == userID:
		return nil
	default:
		return domain.ErrPaymentNotFound
	}
}

// ListForCustomer returns a customer's transaction history.
func (s *Service) ListForCustomer(ctx context.Context, userID bson.ObjectID, page, limit int) ([]View, int64, error) {
	payments, total, err := s.payments.ListForUser(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}
	return toViews(payments), total, nil
}

// List returns a filtered page for the admin dashboard.
func (s *Service) List(ctx context.Context, filter domain.Filter, page, limit int) ([]View, int64, error) {
	payments, total, err := s.payments.List(ctx, filter, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}
	return toViews(payments), total, nil
}

// GetByID returns one attempt for the admin dashboard.
func (s *Service) GetByID(ctx context.Context, id string) (*View, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrPaymentNotFound
	}

	record, err := s.payments.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	view := newView(record)
	return &view, nil
}

// Refund returns a successful payment through the provider and marks the
// order refunded.
func (s *Service) Refund(ctx context.Context, id string, actor Actor) (*View, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrPaymentNotFound
	}

	record, err := s.payments.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	if !record.IsRefundable() {
		return nil, domain.ErrNotRefundable
	}

	if err := s.provider.Refund(ctx, record.Reference, record.Amount); err != nil {
		return nil, apperror.From(err)
	}

	applied, err := s.payments.MarkRefunded(ctx, record.ID)
	if err != nil {
		return nil, apperror.From(err)
	}
	if applied {
		if record.Kind() == domain.PurposeConsultation {
			if err := s.bookings.MarkRefunded(ctx, record.BookingID); err != nil {
				return nil, apperror.From(err)
			}
		} else if err := s.orders.MarkRefunded(ctx, record.OrderID); err != nil {
			return nil, apperror.From(err)
		}
	}

	if s.auditor != nil && !actor.ID.IsZero() {
		actorID := actor.ID
		s.auditor.Record(ctx, audit.Entry{
			ActorID:    &actorID,
			ActorEmail: actor.Email,
			ActorRole:  actor.Role,
			Action:     audit.ActionPaymentRefunded,
			Resource:   "payment",
			ResourceID: record.ID.Hex(),
			Metadata:   map[string]any{"reference": record.Reference, "amount": record.Amount},
		})
	}

	logger.FromContext(ctx).Info("payment refunded",
		slog.String("module", "payments"),
		slog.String("payment_reference", record.Reference),
		slog.Int64("amount", record.Amount),
	)

	return s.GetByID(ctx, id)
}

// VerifySignature exposes the provider's webhook check to the transport layer.
func (s *Service) VerifySignature(rawBody []byte, signature string) bool {
	return s.provider.VerifySignature(rawBody, signature)
}

// newReference mints a unique payment reference in the prototype's format:
// "PSK-" followed by random base32 characters.
func (s *Service) newReference(ctx context.Context) (string, error) {
	for attempt := 0; attempt < referenceAttempts; attempt++ {
		buf := make([]byte, 10)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("payments: generate reference: %w", err)
		}

		candidate := "PSK-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)

		taken, err := s.payments.ReferenceExists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("payments: could not allocate a unique reference")
}
