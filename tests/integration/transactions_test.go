package integration

import (
	"errors"
	"testing"
	"time"

	inventoryapp "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/application"
	inventorydomain "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/domain"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Checkout and payment settlement each touch several collections. These tests
// prove the multi-document transactions behind them commit as a whole and
// abort as a whole, which only a replica set can demonstrate.

func TestCheckoutCommitsOrderReservationAndCartTogether(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("checkout"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 4)

	order := e.mustCheckout(t, buyer)

	if order.Subtotal != 4*650_000 || order.DeliveryFee != 0 || order.Total != order.Subtotal {
		t.Errorf("order totals = (subtotal %d, delivery %d, total %d), want (2600000, 0, 2600000)",
			order.Subtotal, order.DeliveryFee, order.Total)
	}

	available, reserved := e.stockSnapshot(t, broiler)
	if available != 350 || reserved != 4 {
		t.Errorf("stock = (%d available, %d reserved), want (350, 4): the units are held, not yet sold",
			available, reserved)
	}

	cart, err := e.container.CartService.Get(ctx(t), buyer.ID)
	if err != nil {
		t.Fatalf("Get() on the cart returned error: %v", err)
	}
	if len(cart.Items) != 0 {
		t.Errorf("the cart still holds %d lines, want it emptied by the same transaction", len(cart.Items))
	}
}

func TestCheckoutRollsBackEveryWriteWhenOneLineCannotBeReserved(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("rollback"))
	maize := e.newProduct(t, "Maize (Corn)", "crops", "100kg bag", 3_800_000, 60)
	ram := e.newProduct(t, "Ram", "livestock", "head", 12_000_000, 2)

	// The cart is filled while both rams are on the shelf; the stocktake that
	// follows is what makes the ram line unfillable at checkout. A cart is
	// not a reservation, so this is the ordinary case, not a contrived one.
	e.fillCart(t, buyer, maize, 2)
	e.fillCart(t, buyer, ram, 2)

	if _, err := e.container.InventoryService.Adjust(ctx(t), inventoryapp.AdjustInput{
		ProductID: ram.ID,
		Quantity:  -1,
		Reason:    "Stocktake: one ram sold at the farm gate",
	}, &inventoryapp.Actor{ID: adminActor.ID, Email: adminActor.Email, Role: adminActor.Role}); err != nil {
		t.Fatalf("Adjust() returned error: %v", err)
	}

	_, err := e.checkout(t, buyer)
	if err == nil {
		t.Fatal("Checkout() succeeded, want it to fail on the ram line")
	}
	if !errors.Is(err, inventorydomain.ErrInsufficientStock) {
		t.Fatalf("Checkout() error = %v, want ErrInsufficientStock", err)
	}

	// The maize was reserved before the ram line failed. Because both ran in
	// one transaction, that reservation must be gone — no compensation logic
	// runs, the abort undoes it.
	if _, reserved := e.stockSnapshot(t, maize); reserved != 0 {
		t.Errorf("maize reserved = %d, want 0: the aborted transaction must not leave a hold", reserved)
	}
	if _, reserved := e.stockSnapshot(t, ram); reserved != 0 {
		t.Errorf("ram reserved = %d, want 0", reserved)
	}

	// No half-written order, and the cart is intact so the customer can fix
	// the quantity and try again.
	orders, total, err := e.container.OrderService.ListForCustomer(ctx(t), buyer.ID, 1, 20)
	if err != nil {
		t.Fatalf("ListForCustomer() returned error: %v", err)
	}
	if total != 0 || len(orders) != 0 {
		t.Errorf("the customer has %d orders, want none after a failed checkout", len(orders))
	}

	cart, err := e.container.CartService.Get(ctx(t), buyer.ID)
	if err != nil {
		t.Fatalf("Get() on the cart returned error: %v", err)
	}
	if len(cart.Items) != 2 {
		t.Errorf("the cart holds %d lines, want the 2 it had before the failed checkout", len(cart.Items))
	}
}

func TestPaymentConfirmationTurnsTheReservationIntoADeduction(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("paid"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 3)

	order := e.mustCheckout(t, buyer)
	reference := e.pay(t, order.ID, buyer)

	settled := e.order(t, order.ID)
	if settled.PaymentStatus != ordersdomain.PaymentPaid {
		t.Errorf("payment_status = %q, want %q", settled.PaymentStatus, ordersdomain.PaymentPaid)
	}
	if settled.PaymentRef != reference {
		t.Errorf("payment_ref = %q, want %q", settled.PaymentRef, reference)
	}

	available, reserved := e.stockSnapshot(t, broiler)
	if available != 347 || reserved != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (347, 0): the units are sold, not held",
			available, reserved)
	}

	// The movement log explains the change, in the same transaction.
	movements, _, err := e.container.InventoryService.Movements(ctx(t), &broiler.ID, 1, 50)
	if err != nil {
		t.Fatalf("Movements() returned error: %v", err)
	}

	var deductions int
	for _, movement := range movements {
		if movement.Type == inventorydomain.MovementDeduction && movement.Reference == order.OrderNumber {
			deductions++
		}
	}
	if deductions != 1 {
		t.Errorf("deduction movements for %s = %d, want 1", order.OrderNumber, deductions)
	}
}

func TestCancellationReturnsTheReservationInOneTransaction(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("cancelled"))
	ram := e.newProduct(t, "Ram", "livestock", "head", 12_000_000, 2)
	e.fillCart(t, buyer, ram, 2)

	order := e.mustCheckout(t, buyer)

	if sellable := e.sellable(t, ram); sellable != 0 {
		t.Fatalf("sellable = %d while the order holds both rams, want 0", sellable)
	}

	if _, err := e.container.OrderService.Cancel(ctx(t), order.ID, orderViewer(buyer)); err != nil {
		t.Fatalf("Cancel() returned error: %v", err)
	}

	cancelled := e.order(t, order.ID)
	if cancelled.Status != ordersdomain.StatusCancelled {
		t.Errorf("status = %q, want %q", cancelled.Status, ordersdomain.StatusCancelled)
	}

	if sellable := e.sellable(t, ram); sellable != 2 {
		t.Errorf("sellable = %d after cancellation, want 2", sellable)
	}
}

func TestExpirySweepReleasesStockAndCancelsTheOrder(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("expired"))
	duck := e.newProduct(t, "Duck (Duckfowl)", "poultry", "bird", 850_000, 25)
	e.fillCart(t, buyer, duck, 5)

	order := e.mustCheckout(t, buyer)

	// Age the order past the reservation window the worker sweeps with.
	if _, err := e.db.Collection("orders").UpdateByID(ctx(t), mustObjectID(t, order.ID), bson.M{
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

	swept := e.order(t, order.ID)
	if swept.Status != ordersdomain.StatusCancelled {
		t.Errorf("status = %q, want %q", swept.Status, ordersdomain.StatusCancelled)
	}

	available, reserved := e.stockSnapshot(t, duck)
	if available != 25 || reserved != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (25, 0)", available, reserved)
	}

	// A second sweep finds nothing: the orders it already handled no longer
	// hold stock.
	if again, err := e.container.OrderService.ExpireStaleReservations(ctx(t), time.Hour); err != nil || again != 0 {
		t.Errorf("a second sweep freed %d orders, want 0 (error %v)", again, err)
	}
}

// mustObjectID parses a hex identifier or fails the test.
func mustObjectID(t *testing.T, hex string) bson.ObjectID {
	t.Helper()

	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		t.Fatalf("identifier %q is unreadable: %v", hex, err)
	}

	return id
}
