package application

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// errInsufficientStock stands in for the inventory module's error.
var errInsufficientStock = apperror.New(http.StatusConflict, "INSUFFICIENT_STOCK", "There is not enough stock available for this product.")

// harness wires the service to in-memory doubles.
type harness struct {
	service *Service
	orders  *fakeOrderRepo
	carts   *fakeCart
	catalog *fakeCatalog
	stock   *fakeStock
	events  *fakeOutbox

	broiler bson.ObjectID
	ram     bson.ObjectID
	user    bson.ObjectID
}

// newHarness builds a service with two products in stock: 350 broilers at
// ₦6,500 and 2 rams at ₦120,000, matching the prototype's catalogue.
func newHarness(t *testing.T) *harness {
	t.Helper()

	broiler := bson.NewObjectID()
	ram := bson.NewObjectID()

	catalog := &fakeCatalog{products: map[bson.ObjectID]CatalogProduct{
		broiler: {ID: broiler, Name: "Broiler Chicken", Slug: "broiler-chicken", Unit: "bird", Price: 650_000, Active: true},
		ram:     {ID: ram, Name: "Ram", Slug: "ram", Unit: "head", Price: 12_000_000, Active: true},
	}}

	stock := newFakeStock()
	stock.available[broiler] = 350
	stock.available[ram] = 2

	orders := newFakeOrderRepo()
	carts := newFakeCart()
	events := &fakeOutbox{}

	return &harness{
		service: NewService(orders, carts, catalog, stock, fakeTx{}, events, nil),
		orders:  orders,
		carts:   carts,
		catalog: catalog,
		stock:   stock,
		events:  events,
		broiler: broiler,
		ram:     ram,
		user:    bson.NewObjectID(),
	}
}

// checkout runs a pickup checkout for the harness's customer.
func (h *harness) checkout(t *testing.T) (*View, error) {
	t.Helper()

	return h.service.Checkout(context.Background(), h.user, CheckoutInput{
		Name:           "Ada Obi",
		Email:          "ada@denisco.com",
		Phone:          "+2348012345678",
		DeliveryMethod: domain.DeliveryPickup,
	})
}

func TestCheckoutPricesFromCatalogue(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 4})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	// 4 × ₦6,500 = ₦26,000, free pickup.
	if order.Subtotal != 2_600_000 {
		t.Errorf("subtotal = %d, want 2600000 kobo (₦26,000)", order.Subtotal)
	}
	if order.DeliveryFee != 0 {
		t.Errorf("delivery fee = %d, want 0 for farm pickup", order.DeliveryFee)
	}
	if order.Total != 2_600_000 {
		t.Errorf("total = %d, want 2600000", order.Total)
	}
	if order.Status != domain.StatusPending || order.PaymentStatus != domain.PaymentPending {
		t.Errorf("new order status = (%q, %q), want (pending, pending)", order.Status, order.PaymentStatus)
	}
	if order.OrderNumber[:3] != "DG-" || len(order.OrderNumber) != 9 {
		t.Errorf("order number = %q, want the DG-XXXXXX format", order.OrderNumber)
	}
}

func TestCheckoutChargesDeliveryFee(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 1})

	order, err := h.service.Checkout(context.Background(), h.user, CheckoutInput{
		Name:           "Ada Obi",
		Email:          "ada@denisco.com",
		Phone:          "+2348012345678",
		DeliveryMethod: domain.DeliveryHome,
		Address:        "12 Sample Close, Gwarimpa, Abuja",
	})
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	// ₦6,500 + ₦2,500 delivery.
	if order.DeliveryFee != 250_000 {
		t.Errorf("delivery fee = %d, want 250000 kobo (₦2,500)", order.DeliveryFee)
	}
	if order.Total != 900_000 {
		t.Errorf("total = %d, want 900000 kobo (₦9,000)", order.Total)
	}
}

func TestCheckoutUsesCurrentPriceNotCartPrice(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 2})

	// The price rises while the cart sits open.
	h.catalog.setPrice(h.broiler, 700_000)

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	if order.Subtotal != 1_400_000 {
		t.Errorf("subtotal = %d, want 1400000 — checkout must use the current price", order.Subtotal)
	}
	if order.Items[0].UnitPrice != 700_000 {
		t.Errorf("unit price = %d, want the catalogue's current price", order.Items[0].UnitPrice)
	}
}

func TestCheckoutReservesStockAndClearsCart(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.ram, Quantity: 2})

	if _, err := h.checkout(t); err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	available, reserved := h.stock.snapshot(h.ram)
	if available != 2 || reserved != 2 {
		t.Errorf("stock = (available %d, reserved %d), want (2, 2): reserving must not reduce stock on hand", available, reserved)
	}
	if h.stock.sellable(h.ram) != 0 {
		t.Errorf("sellable = %d, want 0 once the last rams are reserved", h.stock.sellable(h.ram))
	}

	lines, _ := h.carts.Lines(context.Background(), h.user)
	if len(lines) != 0 {
		t.Error("the cart was not cleared after checkout")
	}
}

func TestCheckoutRejectsEmptyCart(t *testing.T) {
	h := newHarness(t)

	if _, err := h.checkout(t); !errors.Is(err, ErrCartEmpty) {
		t.Errorf("Checkout() error = %v, want ErrCartEmpty", err)
	}
}

func TestCheckoutRejectsArchivedProduct(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 1})
	h.catalog.setActive(h.broiler, false)

	if _, err := h.checkout(t); !errors.Is(err, ErrItemUnavailable) {
		t.Errorf("Checkout() error = %v, want ErrItemUnavailable", err)
	}
}

func TestCheckoutRejectsInsufficientStock(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.ram, Quantity: 3}) // only 2 in stock

	_, err := h.checkout(t)
	if err == nil {
		t.Fatal("Checkout() succeeded with more items than are in stock")
	}

	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != "INSUFFICIENT_STOCK" {
		t.Errorf("Checkout() error = %v, want INSUFFICIENT_STOCK", err)
	}
}

func TestCheckoutRequiresAddressForDelivery(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 1})

	_, err := h.service.Checkout(context.Background(), h.user, CheckoutInput{
		Name:           "Ada Obi",
		Email:          "ada@denisco.com",
		Phone:          "+2348012345678",
		DeliveryMethod: domain.DeliveryHome,
	})
	if !errors.Is(err, domain.ErrAddressRequired) {
		t.Errorf("Checkout() error = %v, want ErrAddressRequired", err)
	}
}

func TestConcurrentCheckoutsDoNotOversell(t *testing.T) {
	h := newHarness(t)

	// One ram left, and eight customers racing for it.
	h.stock.available[h.ram] = 1

	const customers = 8
	users := make([]bson.ObjectID, customers)
	for i := range users {
		users[i] = bson.NewObjectID()
		h.carts.set(users[i], CartLine{ProductID: h.ram, Quantity: 1})
	}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
	)

	wg.Add(customers)
	for _, user := range users {
		go func(user bson.ObjectID) {
			defer wg.Done()

			_, err := h.service.Checkout(context.Background(), user, CheckoutInput{
				Name:           "Race Tester",
				Email:          "race@denisco.com",
				Phone:          "+2348012345678",
				DeliveryMethod: domain.DeliveryPickup,
			})
			if err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}(user)
	}
	wg.Wait()

	if succeeded != 1 {
		t.Errorf("%d of %d concurrent checkouts succeeded for a single unit, want exactly 1", succeeded, customers)
	}

	available, reserved := h.stock.snapshot(h.ram)
	if reserved != 1 || available != 1 {
		t.Errorf("stock = (available %d, reserved %d), want (1, 1)", available, reserved)
	}
}

func TestCancelReleasesStock(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.ram, Quantity: 2})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	if h.stock.sellable(h.ram) != 0 {
		t.Fatalf("sellable = %d, want 0 after reservation", h.stock.sellable(h.ram))
	}

	viewer := Viewer{UserID: h.user}
	cancelled, err := h.service.Cancel(context.Background(), order.ID, viewer)
	if err != nil {
		t.Fatalf("Cancel() returned error: %v", err)
	}

	if cancelled.Status != domain.StatusCancelled {
		t.Errorf("status = %q, want cancelled", cancelled.Status)
	}
	if h.stock.sellable(h.ram) != 2 {
		t.Errorf("sellable = %d, want 2 once the reservation is released", h.stock.sellable(h.ram))
	}

	// Cancelling twice must not release the stock a second time.
	if _, err := h.service.Cancel(context.Background(), order.ID, viewer); !errors.Is(err, domain.ErrNotCancellable) {
		t.Errorf("Cancel() error on a cancelled order = %v, want ErrNotCancellable", err)
	}
	if h.stock.sellable(h.ram) != 2 {
		t.Errorf("sellable = %d, want 2 — a repeated cancel released stock twice", h.stock.sellable(h.ram))
	}
}

func TestCancelRejectsAnotherCustomersOrder(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 1})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	// A stranger gets "not found" rather than "forbidden", so order
	// identifiers cannot be probed for existence.
	stranger := Viewer{UserID: bson.NewObjectID()}
	if _, err := h.service.Get(context.Background(), order.ID, stranger); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Errorf("Get() error = %v, want ErrOrderNotFound", err)
	}
}

func TestMarkPaidIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 3})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	orderID, _ := bson.ObjectIDFromHex(order.ID)
	paidAt := time.Now().UTC()

	// The webhook and the callback page's verify call both land.
	for i := 0; i < 3; i++ {
		if err := h.service.MarkPaid(context.Background(), orderID, "PSK-TEST", "card", paidAt); err != nil {
			t.Fatalf("MarkPaid() attempt %d returned error: %v", i+1, err)
		}
	}

	available, reserved := h.stock.snapshot(h.broiler)
	if available != 347 || reserved != 0 {
		t.Errorf("stock = (available %d, reserved %d), want (347, 0): the deduction must apply exactly once", available, reserved)
	}

	paid, err := h.service.Get(context.Background(), order.ID, Viewer{UserID: h.user})
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if paid.PaymentStatus != domain.PaymentPaid {
		t.Errorf("payment status = %q, want paid", paid.PaymentStatus)
	}
	if paid.PaymentRef != "PSK-TEST" {
		t.Errorf("payment reference = %q, want PSK-TEST", paid.PaymentRef)
	}
}

func TestFailedPaymentReleasesStock(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.ram, Quantity: 2})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	orderID, _ := bson.ObjectIDFromHex(order.ID)

	if err := h.service.MarkPaymentFailed(context.Background(), orderID, "failed"); err != nil {
		t.Fatalf("MarkPaymentFailed() returned error: %v", err)
	}

	if h.stock.sellable(h.ram) != 2 {
		t.Errorf("sellable = %d, want 2 once an abandoned checkout releases its hold", h.stock.sellable(h.ram))
	}
}

func TestLatePaymentSecuresStockWhenItIsStillAvailable(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 2})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	orderID, _ := bson.ObjectIDFromHex(order.ID)

	// The reservation expires and the order is cancelled, then the payment
	// the customer had already started lands.
	if _, err := h.service.ExpireStaleReservations(context.Background(), 0); err != nil {
		t.Fatalf("ExpireStaleReservations() returned error: %v", err)
	}

	if err := h.service.MarkPaid(context.Background(), orderID, "PSK-LATE", "card", time.Now().UTC()); err != nil {
		t.Fatalf("MarkPaid() returned error: %v", err)
	}

	// There were still broilers on the shelf, so the order is filled from
	// them exactly once and rejoins the fulfillment queue.
	available, reserved := h.stock.snapshot(h.broiler)
	if available != 348 || reserved != 0 {
		t.Errorf("stock = (available %d, reserved %d), want (348, 0)", available, reserved)
	}

	paid, err := h.service.Get(context.Background(), order.ID, Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}

	if paid.Status != domain.StatusPending {
		t.Errorf("status = %q, want %q once the stock is secured", paid.Status, domain.StatusPending)
	}
	if paid.PaymentStatus != domain.PaymentPaid {
		t.Errorf("payment_status = %q, want %q", paid.PaymentStatus, domain.PaymentPaid)
	}
	if paid.NeedsReconciliation {
		t.Error("needs_reconciliation = true, want false once the stock is secured")
	}
	if paid.ReconciliationReason != domain.ReconciliationStockSecured {
		t.Errorf("reconciliation_reason = %q, want %q", paid.ReconciliationReason, domain.ReconciliationStockSecured)
	}

	if events := h.events.recorded(); events[len(events)-1] != EventOrderPaid {
		t.Errorf("last event = %q, want %q", events[len(events)-1], EventOrderPaid)
	}
}

func TestLatePaymentIsFlaggedWhenTheStockIsGone(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.ram, Quantity: 2})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	orderID, _ := bson.ObjectIDFromHex(order.ID)

	// The reservation expires, another customer buys both rams, and only
	// then does the first customer's payment arrive.
	if _, err := h.service.ExpireStaleReservations(context.Background(), 0); err != nil {
		t.Fatalf("ExpireStaleReservations() returned error: %v", err)
	}

	second := bson.NewObjectID()
	h.carts.set(second, CartLine{ProductID: h.ram, Quantity: 2})
	secondOrder, err := h.service.Checkout(context.Background(), second, CheckoutInput{
		Name:           "Bola Ade",
		Email:          "bola@denisco.com",
		Phone:          "+2348098765432",
		DeliveryMethod: domain.DeliveryPickup,
	})
	if err != nil {
		t.Fatalf("Checkout() for the second customer returned error: %v", err)
	}

	secondID, _ := bson.ObjectIDFromHex(secondOrder.ID)
	if err := h.service.MarkPaid(context.Background(), secondID, "PSK-SECOND", "card", time.Now().UTC()); err != nil {
		t.Fatalf("MarkPaid() for the second customer returned error: %v", err)
	}

	if err := h.service.MarkPaid(context.Background(), orderID, "PSK-LATE", "card", time.Now().UTC()); err != nil {
		t.Fatalf("MarkPaid() returned error: %v", err)
	}

	// Nothing is oversold: the rams stay with the customer who paid in time.
	available, reserved := h.stock.snapshot(h.ram)
	if available != 0 || reserved != 0 {
		t.Errorf("stock = (available %d, reserved %d), want (0, 0)", available, reserved)
	}

	flagged, err := h.service.Get(context.Background(), order.ID, Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}

	if !flagged.NeedsReconciliation {
		t.Error("needs_reconciliation = false, want true when a late payment cannot be filled")
	}
	if flagged.ReconciliationReason != domain.ReconciliationRefundRequired {
		t.Errorf("reconciliation_reason = %q, want %q", flagged.ReconciliationReason, domain.ReconciliationRefundRequired)
	}
	if flagged.Status != domain.StatusCancelled {
		t.Errorf("status = %q, want %q while the refund decision is pending", flagged.Status, domain.StatusCancelled)
	}

	// Staff are told, and the customer is not promised an order that cannot
	// be shipped.
	events := h.events.recorded()
	if events[len(events)-1] != EventPaymentUnreconciled {
		t.Errorf("last event = %q, want %q", events[len(events)-1], EventPaymentUnreconciled)
	}
	paidEvents := 0
	for _, event := range events {
		if event == EventOrderPaid {
			paidEvents++
		}
	}
	if paidEvents != 1 {
		t.Errorf("order.paid events = %d, want 1: only the order that was filled may be confirmed", paidEvents)
	}
}

func TestDuplicateLatePaymentReconcilesOnce(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 2})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	orderID, _ := bson.ObjectIDFromHex(order.ID)

	if _, err := h.service.ExpireStaleReservations(context.Background(), 0); err != nil {
		t.Fatalf("ExpireStaleReservations() returned error: %v", err)
	}

	// A webhook and a verify call both report the same late payment.
	for i := 0; i < 2; i++ {
		if err := h.service.MarkPaid(context.Background(), orderID, "PSK-LATE", "card", time.Now().UTC()); err != nil {
			t.Fatalf("MarkPaid() call %d returned error: %v", i+1, err)
		}
	}

	available, _ := h.stock.snapshot(h.broiler)
	if available != 348 {
		t.Errorf("available = %d, want 348: the second delivery must change nothing", available)
	}
}

func TestUpdateStatusEnforcesTransitions(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 1})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	actor := Actor{ID: bson.NewObjectID(), Role: "admin"}

	if _, err := h.service.UpdateStatus(context.Background(), order.ID, domain.StatusCompleted, actor); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("UpdateStatus(pending → completed) error = %v, want ErrInvalidTransition", err)
	}

	for _, status := range []string{domain.StatusProcessing, domain.StatusDispatched, domain.StatusCompleted} {
		if _, err := h.service.UpdateStatus(context.Background(), order.ID, status, actor); err != nil {
			t.Fatalf("UpdateStatus(%q) returned error: %v", status, err)
		}
	}
}

func TestAdminCancellationReleasesStock(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.ram, Quantity: 1})

	order, err := h.checkout(t)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	if _, err := h.service.UpdateStatus(context.Background(), order.ID, domain.StatusCancelled, Actor{ID: bson.NewObjectID(), Role: "admin"}); err != nil {
		t.Fatalf("UpdateStatus(cancelled) returned error: %v", err)
	}

	if h.stock.sellable(h.ram) != 2 {
		t.Errorf("sellable = %d, want 2 after an administrator cancels the order", h.stock.sellable(h.ram))
	}
}

func TestCheckoutEmitsEvent(t *testing.T) {
	h := newHarness(t)
	h.carts.set(h.user, CartLine{ProductID: h.broiler, Quantity: 1})

	if _, err := h.checkout(t); err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	events := h.events.recorded()
	if len(events) != 1 || events[0] != EventOrderCreated {
		t.Errorf("events = %v, want [%s]", events, EventOrderCreated)
	}
}
