package integration

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	consultdomain "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	paymentsapp "github.com/Dev-folabi/denisco_backend/internal/modules/payments/application"
	paymentsdomain "github.com/Dev-folabi/denisco_backend/internal/modules/payments/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// The payment tests run against the Paystack stand-in in paystack_stub_test.go:
// a real HTTP server speaking the provider's contract, so the client's
// requests, its response decoding and the webhook signature check are all
// under test alongside the business rules.

func TestPaymentInitializeThenVerify(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("verify"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 2)

	order := e.mustCheckout(t, buyer)

	started, err := e.container.PaymentService.Initialize(ctx(t), order.ID, buyer.ID, false)
	if err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}
	if started.AuthorizationURL == "" {
		t.Error("Initialize() returned no checkout URL")
	}
	if started.Amount != order.Total {
		t.Errorf("attempt amount = %d, want the order total %d", started.Amount, order.Total)
	}
	if amount := e.provider.amountFor(started.Reference); amount != order.Total {
		t.Errorf("the provider was asked for %d, want %d", amount, order.Total)
	}

	// The customer has not paid yet, so verifying changes nothing.
	pending, err := e.container.PaymentService.Verify(ctx(t), started.Reference, buyer.ID, false)
	if err != nil {
		t.Fatalf("Verify() before payment returned error: %v", err)
	}
	if pending.Status != paymentsdomain.StatusPending {
		t.Errorf("status = %q, want %q before the customer pays", pending.Status, paymentsdomain.StatusPending)
	}
	if e.order(t, order.ID).PaymentStatus != ordersdomain.PaymentPending {
		t.Error("the order is marked paid before the provider confirmed anything")
	}

	// The customer pays; the callback page verifies rather than trusting the
	// redirect it arrived on.
	e.provider.settle(started.Reference, "success")

	settled, err := e.container.PaymentService.Verify(ctx(t), started.Reference, buyer.ID, false)
	if err != nil {
		t.Fatalf("Verify() returned error: %v", err)
	}
	if settled.Status != paymentsdomain.StatusSuccessful {
		t.Errorf("status = %q, want %q", settled.Status, paymentsdomain.StatusSuccessful)
	}

	paid := e.order(t, order.ID)
	if paid.PaymentStatus != ordersdomain.PaymentPaid {
		t.Errorf("order payment_status = %q, want %q", paid.PaymentStatus, ordersdomain.PaymentPaid)
	}

	// Another customer must not be able to read, or settle, this attempt.
	stranger := e.newCustomer(t, uniqueEmail("stranger"))
	if _, err := e.container.PaymentService.Get(ctx(t), started.Reference, stranger.ID, false); !errors.Is(err, paymentsdomain.ErrPaymentNotFound) {
		t.Errorf("Get() as another customer error = %v, want ErrPaymentNotFound", err)
	}
}

func TestDuplicateWebhookIsAppliedOnce(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("duplicate"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 3)

	order := e.mustCheckout(t, buyer)
	reference := e.initializePayment(t, order.ID, buyer)
	e.provider.settle(reference, "success")

	// Paystack retries deliveries it is unsure about, and a verify call may
	// race the webhook, so the same success arrives three times.
	e.deliverWebhook(t, "charge.success", reference)
	e.deliverWebhook(t, "charge.success", reference)
	if _, err := e.container.PaymentService.Verify(ctx(t), reference, buyer.ID, false); err != nil {
		t.Fatalf("Verify() returned error: %v", err)
	}

	available, reserved := e.stockSnapshot(t, broiler)
	if available != 347 || reserved != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (347, 0): the units may only be deducted once",
			available, reserved)
	}

	if count := e.outboxCount(t, "order.paid"); count != 1 {
		t.Errorf("order.paid events = %d, want 1: the customer must be told once", count)
	}

	movements, _, err := e.container.InventoryService.Movements(ctx(t), &broiler.ID, 1, 50)
	if err != nil {
		t.Fatalf("Movements() returned error: %v", err)
	}

	var deductions int
	for _, movement := range movements {
		if movement.Reference == order.OrderNumber && movement.Quantity == -3 {
			deductions++
		}
	}
	if deductions != 1 {
		t.Errorf("deduction movements = %d, want 1", deductions)
	}
}

func TestWebhookWithAnInvalidSignatureChangesNothing(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("forged"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 1)

	order := e.mustCheckout(t, buyer)
	reference := e.initializePayment(t, order.ID, buyer)
	e.provider.settle(reference, "success")

	body, _, err := e.provider.webhookBody("charge.success", reference)
	if err != nil {
		t.Fatalf("could not build the webhook body: %v", err)
	}

	// Right body, wrong signature: the only thing standing between a forged
	// callback and a free order.
	forged := e.post(t, "/api/v1/webhooks/paystack", body, "00000000deadbeef")
	if forged.Code != http.StatusUnauthorized {
		t.Errorf("a forged webhook returned %d, want %d", forged.Code, http.StatusUnauthorized)
	}

	// And no signature at all.
	unsigned := e.post(t, "/api/v1/webhooks/paystack", body, "")
	if unsigned.Code != http.StatusUnauthorized {
		t.Errorf("an unsigned webhook returned %d, want %d", unsigned.Code, http.StatusUnauthorized)
	}

	if e.order(t, order.ID).PaymentStatus != ordersdomain.PaymentPending {
		t.Error("the order was marked paid by a webhook that failed its signature check")
	}
	if _, reserved := e.stockSnapshot(t, broiler); reserved != 1 {
		t.Errorf("reserved = %d, want the reservation untouched at 1", reserved)
	}
}

func TestWebhookPayingTheWrongAmountIsRefused(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("mismatch"))
	ram := e.newProduct(t, "Ram", "livestock", "head", 12_000_000, 2)
	e.fillCart(t, buyer, ram, 1)

	order := e.mustCheckout(t, buyer)
	reference := e.initializePayment(t, order.ID, buyer)

	// A correctly signed callback that reports ₦100 against a ₦120,000 order.
	e.provider.settle(reference, "success")
	e.provider.tamperAmount(reference, 10_000)

	response := e.postWebhook(t, "charge.success", reference)
	if response.Code != http.StatusConflict {
		t.Errorf("a short payment returned %d, want %d", response.Code, http.StatusConflict)
	}

	attempt, err := e.container.PaymentService.Get(ctx(t), reference, buyer.ID, false)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if attempt.Status != paymentsdomain.StatusFailed {
		t.Errorf("attempt status = %q, want %q", attempt.Status, paymentsdomain.StatusFailed)
	}

	if e.order(t, order.ID).PaymentStatus != ordersdomain.PaymentPending {
		t.Error("the order was settled by a payment for the wrong amount")
	}
}

func TestRetriedPaymentKeepsEachAttemptOnRecord(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("retry"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 2)

	order := e.mustCheckout(t, buyer)

	// The first card is declined.
	first := e.initializePayment(t, order.ID, buyer)
	e.provider.settle(first, "failed")
	e.deliverWebhook(t, "charge.failed", first)

	failedOrder := e.order(t, order.ID)
	if failedOrder.PaymentStatus != ordersdomain.PaymentFailed {
		t.Errorf("payment_status = %q, want %q after a declined card", failedOrder.PaymentStatus, ordersdomain.PaymentFailed)
	}
	// The hold is returned, so an abandoned checkout does not keep the stock.
	if _, reserved := e.stockSnapshot(t, broiler); reserved != 0 {
		t.Errorf("reserved = %d after a failed payment, want 0", reserved)
	}

	// The decline is announced. The cart was emptied at checkout, so without
	// this notice a declined card leaves the customer with no way back to the
	// order and no idea anything is wrong.
	if count := e.outboxCount(t, "order.payment_failed"); count != 1 {
		t.Errorf("order.payment_failed events = %d, want 1", count)
	}

	// The customer tries again. The failed attempt is kept rather than
	// overwritten, and the new attempt gets its own reference.
	second := e.initializePayment(t, order.ID, buyer)
	if second == first {
		t.Fatal("the retry reused the first attempt's reference")
	}

	e.provider.settle(second, "success")
	e.deliverWebhook(t, "charge.success", second)

	// The retry settles the order, so the decline is not re-announced.
	if count := e.outboxCount(t, "order.payment_failed"); count != 1 {
		t.Errorf("order.payment_failed events = %d after a successful retry, want still 1", count)
	}

	attempts, total, err := e.container.PaymentService.ListForCustomer(ctx(t), buyer.ID, 1, 20)
	if err != nil {
		t.Fatalf("ListForCustomer() returned error: %v", err)
	}
	if total != 2 || len(attempts) != 2 {
		t.Fatalf("the customer has %d attempts on record, want 2", len(attempts))
	}

	var successful, failed int
	for _, attempt := range attempts {
		switch attempt.Status {
		case paymentsdomain.StatusSuccessful:
			successful++
		case paymentsdomain.StatusFailed:
			failed++
		}
	}
	if successful != 1 || failed != 1 {
		t.Errorf("attempts = %d successful and %d failed, want 1 and 1", successful, failed)
	}

	// The retry re-secures the stock through the late-payment path, because
	// the failed attempt had already released it.
	paid := e.order(t, order.ID)
	if paid.PaymentStatus != ordersdomain.PaymentPaid {
		t.Errorf("payment_status = %q, want %q", paid.PaymentStatus, ordersdomain.PaymentPaid)
	}
	if paid.NeedsReconciliation {
		t.Error("needs_reconciliation = true, want false: the broilers were still available")
	}

	available, reserved := e.stockSnapshot(t, broiler)
	if available != 348 || reserved != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (348, 0)", available, reserved)
	}
}

func TestLatePaymentSecuresStockWhenItIsStillThere(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("late-ok"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 4)

	order := e.mustCheckout(t, buyer)
	reference := e.initializePayment(t, order.ID, buyer)

	e.expireReservations(t, order.ID)

	// The customer finishes paying on a checkout page they left open.
	e.provider.settle(reference, "success")
	e.deliverWebhook(t, "charge.success", reference)

	reconciled := e.order(t, order.ID)
	if reconciled.PaymentStatus != ordersdomain.PaymentPaid {
		t.Errorf("payment_status = %q, want %q", reconciled.PaymentStatus, ordersdomain.PaymentPaid)
	}
	if reconciled.Status != ordersdomain.StatusPending {
		t.Errorf("status = %q, want %q: a paid order rejoins the queue", reconciled.Status, ordersdomain.StatusPending)
	}
	if reconciled.NeedsReconciliation {
		t.Error("needs_reconciliation = true, want false once the stock is secured")
	}
	if reconciled.ReconciliationReason != ordersdomain.ReconciliationStockSecured {
		t.Errorf("reconciliation_reason = %q, want %q", reconciled.ReconciliationReason, ordersdomain.ReconciliationStockSecured)
	}

	available, reserved := e.stockSnapshot(t, broiler)
	if available != 346 || reserved != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (346, 0)", available, reserved)
	}

	// The decision is on the record, attributed to the system rather than to
	// an administrator who never touched it.
	entry := e.auditEntry(t, audit.ActionOrderLatePayment)
	if entry.ActorRole != audit.ActorSystem || entry.ActorID != nil {
		t.Errorf("audit actor = (role %q, id %v), want the system with no id", entry.ActorRole, entry.ActorID)
	}
	if entry.Metadata["outcome"] != ordersdomain.ReconciliationStockSecured {
		t.Errorf("audit outcome = %v, want %q", entry.Metadata["outcome"], ordersdomain.ReconciliationStockSecured)
	}
}

func TestLatePaymentIsFlaggedForRefundWhenTheStockHasGone(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("late-refund"))
	ram := e.newProduct(t, "Ram", "livestock", "head", 12_000_000, 2)
	e.fillCart(t, buyer, ram, 2)

	order := e.mustCheckout(t, buyer)
	reference := e.initializePayment(t, order.ID, buyer)

	e.expireReservations(t, order.ID)

	// With the hold gone, a second customer buys and pays for both rams.
	second := e.newCustomer(t, uniqueEmail("in-time"))
	e.fillCart(t, second, ram, 2)
	secondOrder := e.mustCheckout(t, second)
	e.pay(t, secondOrder.ID, second)

	// Only now does the first customer's payment land.
	e.provider.settle(reference, "success")
	e.deliverWebhook(t, "charge.success", reference)

	flagged := e.order(t, order.ID)
	if !flagged.NeedsReconciliation {
		t.Error("needs_reconciliation = false, want true: money was taken for goods that are gone")
	}
	if flagged.ReconciliationReason != ordersdomain.ReconciliationRefundRequired {
		t.Errorf("reconciliation_reason = %q, want %q", flagged.ReconciliationReason, ordersdomain.ReconciliationRefundRequired)
	}
	if flagged.Status != ordersdomain.StatusCancelled {
		t.Errorf("status = %q, want it left %q until a human decides", flagged.Status, ordersdomain.StatusCancelled)
	}

	// Nothing is oversold: the rams stay with the customer who paid in time.
	available, reserved := e.stockSnapshot(t, ram)
	if available != 0 || reserved != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (0, 0)", available, reserved)
	}

	// The customer is not told their order is on its way; the farm is told to
	// act.
	if count := e.outboxCount(t, "order.payment_unreconciled"); count != 1 {
		t.Errorf("order.payment_unreconciled events = %d, want 1", count)
	}
	if count := e.outboxCount(t, "order.paid"); count != 1 {
		t.Errorf("order.paid events = %d, want only the order that was filled", count)
	}

	// The dashboard can list exactly the orders waiting on a decision.
	needs := true
	awaiting, total, err := e.container.OrderService.List(ctx(t), ordersdomain.Filter{NeedsReconciliation: &needs}, 1, 20)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if total != 1 || len(awaiting) != 1 || awaiting[0].ID != order.ID {
		t.Fatalf("List(needs_reconciliation) returned %d orders, want only %s", len(awaiting), order.OrderNumber)
	}

	// Refunding is the way out, and it goes to the provider.
	attempt, err := e.container.PaymentService.Get(ctx(t), reference, buyer.ID, false)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}

	refunded, err := e.container.PaymentService.Refund(ctx(t), attempt.ID, paymentsapp.Actor{
		ID:    adminActor.ID,
		Email: adminActor.Email,
		Role:  adminActor.Role,
	})
	if err != nil {
		t.Fatalf("Refund() returned error: %v", err)
	}
	if refunded.Status != paymentsdomain.StatusRefunded {
		t.Errorf("attempt status = %q, want %q", refunded.Status, paymentsdomain.StatusRefunded)
	}
	if refunds := e.provider.refunded(); len(refunds) != 1 || refunds[0] != reference {
		t.Errorf("the provider was asked to refund %v, want [%s]", refunds, reference)
	}
	if e.order(t, order.ID).PaymentStatus != ordersdomain.PaymentRefunded {
		t.Errorf("order payment_status = %q, want %q", e.order(t, order.ID).PaymentStatus, ordersdomain.PaymentRefunded)
	}

	// The customer is told their money is coming back.
	if count := e.outboxCount(t, "order.refunded"); count != 1 {
		t.Errorf("order.refunded events = %d, want 1", count)
	}
}

// expireReservations ages an order past the reservation window and runs the
// sweep the worker runs, which is what a customer's abandoned checkout meets.
func (e *environment) expireReservations(t *testing.T, orderID string) {
	t.Helper()

	if _, err := e.db.Collection("orders").UpdateByID(ctx(t), mustObjectID(t, orderID), bson.M{
		"$set": bson.M{"created_at": time.Now().UTC().Add(-2 * time.Hour)},
	}); err != nil {
		t.Fatalf("could not age the order: %v", err)
	}

	released, err := e.container.OrderService.ExpireStaleReservations(ctx(t), time.Hour)
	if err != nil {
		t.Fatalf("ExpireStaleReservations() returned error: %v", err)
	}
	if released != 1 {
		t.Fatalf("ExpireStaleReservations() freed %d orders, want 1", released)
	}

	if expired := e.order(t, orderID); expired.Status != ordersdomain.StatusCancelled {
		t.Fatalf("status after the sweep = %q, want %q", expired.Status, ordersdomain.StatusCancelled)
	}
}

// auditEntry returns the newest audit entry for an action.
func (e *environment) auditEntry(t *testing.T, action string) audit.Entry {
	t.Helper()

	entries, _, err := e.container.Audit.List(ctx(t), 1, 50)
	if err != nil {
		t.Fatalf("could not read the audit trail: %v", err)
	}

	for _, entry := range entries {
		if entry.Action == action {
			return entry
		}
	}

	t.Fatalf("no %q entry in the audit trail", action)
	return audit.Entry{}
}

// TestConsultationFeeIsPaidThroughPaystack walks a guest consultation booking
// through the same provider handshake an order uses: initialize, pay at the
// provider, webhook back. The booking is what settles, not an order, so this
// covers the branch that tells the two apart.
func TestConsultationFeeIsPaidThroughPaystack(t *testing.T) {
	e := require(t)
	c := ctx(t)

	typeID := e.seedConsultations(t)
	date, slotTime := e.bookableSlot(t, typeID)

	booking, err := e.book(c, typeID, date, slotTime, uniqueEmail("fee"))
	if err != nil {
		t.Fatalf("Book() returned error: %v", err)
	}

	if booking.PaymentStatus != consultdomain.PaymentPending {
		t.Errorf("new booking payment_status = %q, want %q", booking.PaymentStatus, consultdomain.PaymentPending)
	}
	if !booking.Payable {
		t.Error("a new booking with a fee should be payable")
	}

	// A guest has no session, so the booking reference is what stands in for
	// one. The wrong reference must not open an attempt.
	if _, err := e.container.PaymentService.InitializeBooking(c, booking.ID, "CB-00000", bson.ObjectID{}, false); err == nil {
		t.Error("InitializeBooking() accepted a mismatched booking reference")
	}

	attempt, err := e.container.PaymentService.InitializeBooking(c, booking.ID, booking.Reference, bson.ObjectID{}, false)
	if err != nil {
		t.Fatalf("InitializeBooking() returned error: %v", err)
	}
	if attempt.Purpose != paymentsdomain.PurposeConsultation {
		t.Errorf("purpose = %q, want %q", attempt.Purpose, paymentsdomain.PurposeConsultation)
	}
	if attempt.Amount != booking.TypePrice {
		t.Errorf("amount = %d, want the consultation fee %d", attempt.Amount, booking.TypePrice)
	}
	if attempt.OrderID != "" {
		t.Errorf("order_id = %q, want it empty on a consultation payment", attempt.OrderID)
	}

	e.provider.settle(attempt.Reference, "success")
	e.deliverWebhook(t, "charge.success", attempt.Reference)

	settled, err := e.container.ConsultationService.GetBooking(c, booking.ID, consultapp.Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("GetBooking() returned error: %v", err)
	}
	if settled.PaymentStatus != consultdomain.PaymentPaid {
		t.Errorf("payment_status = %q, want %q", settled.PaymentStatus, consultdomain.PaymentPaid)
	}
	if settled.Status != consultdomain.StatusConfirmed {
		t.Errorf("status = %q, want %q: a paid consultation is a confirmed one", settled.Status, consultdomain.StatusConfirmed)
	}
	if settled.Payable {
		t.Error("a paid booking should no longer be payable")
	}
	if settled.PaymentRef != attempt.Reference {
		t.Errorf("payment_ref = %q, want %q", settled.PaymentRef, attempt.Reference)
	}

	// A replayed webhook changes nothing.
	e.deliverWebhook(t, "charge.success", attempt.Reference)

	again, err := e.container.ConsultationService.GetBooking(c, booking.ID, consultapp.Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("GetBooking() after the replay returned error: %v", err)
	}
	if again.PaidAt == nil || settled.PaidAt == nil || *again.PaidAt != *settled.PaidAt {
		t.Error("a replayed webhook moved the paid_at timestamp")
	}

	// The fee is settled, so there is nothing left to open an attempt against.
	if _, err := e.container.PaymentService.InitializeBooking(c, booking.ID, booking.Reference, bson.ObjectID{}, false); !errors.Is(err, consultdomain.ErrNotPayable) {
		t.Errorf("InitializeBooking() on a paid booking error = %v, want ErrNotPayable", err)
	}
}

// TestDeclinedConsultationFeeKeepsTheSlot covers the deliberate difference
// from an order: a consultation time is not consumed by a failed attempt, so
// the booking keeps it and the customer can pay again.
func TestDeclinedConsultationFeeKeepsTheSlot(t *testing.T) {
	e := require(t)
	c := ctx(t)

	typeID := e.seedConsultations(t)
	date, slotTime := e.bookableSlot(t, typeID)

	booking, err := e.book(c, typeID, date, slotTime, uniqueEmail("declined"))
	if err != nil {
		t.Fatalf("Book() returned error: %v", err)
	}

	first, err := e.container.PaymentService.InitializeBooking(c, booking.ID, booking.Reference, bson.ObjectID{}, false)
	if err != nil {
		t.Fatalf("InitializeBooking() returned error: %v", err)
	}

	e.provider.settle(first.Reference, "failed")
	e.deliverWebhook(t, "charge.failed", first.Reference)

	declined, err := e.container.ConsultationService.GetBooking(c, booking.ID, consultapp.Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("GetBooking() returned error: %v", err)
	}
	if declined.PaymentStatus != consultdomain.PaymentFailed {
		t.Errorf("payment_status = %q, want %q", declined.PaymentStatus, consultdomain.PaymentFailed)
	}
	if declined.Status != consultdomain.StatusPending {
		t.Errorf("status = %q, want %q: a declined fee must not cancel the booking", declined.Status, consultdomain.StatusPending)
	}
	if !declined.Payable {
		t.Error("a booking with a declined fee should still be payable")
	}

	// The decline is announced, so the customer knows the fee did not go
	// through rather than waiting for a confirmation that will not come.
	if count := e.outboxCount(t, "consultation.payment_failed"); count != 1 {
		t.Errorf("consultation.payment_failed events = %d, want 1", count)
	}

	// The slot is still held, so nobody else can take that hour.
	if _, err := e.book(c, typeID, date, slotTime, uniqueEmail("declined-other")); !errors.Is(err, consultdomain.ErrSlotUnavailable) {
		t.Errorf("Book() on a slot held by an unpaid booking error = %v, want ErrSlotUnavailable", err)
	}

	// The retry is a second attempt with its own reference, and it settles.
	second, err := e.container.PaymentService.InitializeBooking(c, booking.ID, booking.Reference, bson.ObjectID{}, false)
	if err != nil {
		t.Fatalf("InitializeBooking() on retry returned error: %v", err)
	}
	if second.Reference == first.Reference {
		t.Error("a retry reused the failed attempt's reference")
	}

	e.provider.settle(second.Reference, "success")
	e.deliverWebhook(t, "charge.success", second.Reference)

	paid, err := e.container.ConsultationService.GetBooking(c, booking.ID, consultapp.Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("GetBooking() after the retry returned error: %v", err)
	}
	if paid.PaymentStatus != consultdomain.PaymentPaid {
		t.Errorf("payment_status after retry = %q, want %q", paid.PaymentStatus, consultdomain.PaymentPaid)
	}
}

// TestRefundedConsultationFeeTellsTheCustomer covers the other half of the
// consultation payment story: once the farm gives the fee back, the customer
// is told, the same way a refunded order is.
func TestRefundedConsultationFeeTellsTheCustomer(t *testing.T) {
	e := require(t)
	c := ctx(t)

	typeID := e.seedConsultations(t)
	date, slotTime := e.bookableSlot(t, typeID)

	booking, err := e.book(c, typeID, date, slotTime, uniqueEmail("refund"))
	if err != nil {
		t.Fatalf("Book() returned error: %v", err)
	}

	attempt, err := e.container.PaymentService.InitializeBooking(c, booking.ID, booking.Reference, bson.ObjectID{}, false)
	if err != nil {
		t.Fatalf("InitializeBooking() returned error: %v", err)
	}

	e.provider.settle(attempt.Reference, "success")
	e.deliverWebhook(t, "charge.success", attempt.Reference)

	if count := e.outboxCount(t, "consultation.booked"); count != 1 {
		t.Fatalf("consultation.booked events = %d, want 1 before the refund", count)
	}

	record, err := e.container.PaymentService.Get(c, attempt.Reference, bson.ObjectID{}, true)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}

	refunded, err := e.container.PaymentService.Refund(ctx(t), record.ID, paymentsapp.Actor{
		ID:    adminActor.ID,
		Email: adminActor.Email,
		Role:  adminActor.Role,
	})
	if err != nil {
		t.Fatalf("Refund() returned error: %v", err)
	}
	if refunded.Status != paymentsdomain.StatusRefunded {
		t.Errorf("attempt status = %q, want %q", refunded.Status, paymentsdomain.StatusRefunded)
	}
	if refunds := e.provider.refunded(); len(refunds) != 1 || refunds[0] != attempt.Reference {
		t.Errorf("the provider was asked to refund %v, want [%s]", refunds, attempt.Reference)
	}

	after, err := e.container.ConsultationService.GetBooking(c, booking.ID, consultapp.Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("GetBooking() after the refund returned error: %v", err)
	}
	if after.PaymentStatus != consultdomain.PaymentRefunded {
		t.Errorf("payment_status = %q, want %q", after.PaymentStatus, consultdomain.PaymentRefunded)
	}

	if count := e.outboxCount(t, "consultation.refunded"); count != 1 {
		t.Errorf("consultation.refunded events = %d, want 1", count)
	}
}
