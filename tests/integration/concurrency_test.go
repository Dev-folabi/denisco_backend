package integration

import (
	"context"
	"errors"
	"sync"
	"testing"

	consultdomain "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	inventorydomain "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/domain"
	ordersapp "github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// The concurrency tests are the reason this suite needs a real deployment: the
// guarantees under test are conditional updates and unique indexes, and an
// in-memory double could only restate what it was told to.

func TestOnlyOneCustomerGetsTheLastItem(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	// One ram, six customers, all checking out at once.
	const contenders = 6

	ram := e.newProduct(t, "Ram", "livestock", "head", 12_000_000, 1)

	buyers := make([]customer, 0, contenders)
	for i := 0; i < contenders; i++ {
		buyer := e.newCustomer(t, uniqueEmail("race"))
		e.fillCart(t, buyer, ram, 1)
		buyers = append(buyers, buyer)
	}

	type outcome struct {
		order *ordersapp.View
		err   error
	}

	results := make([]outcome, contenders)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i, buyer := range buyers {
		wg.Add(1)
		go func(index int, buyer customer) {
			defer wg.Done()
			<-start

			order, err := e.container.OrderService.Checkout(context.Background(), buyer.ID, checkoutInput(buyer))
			results[index] = outcome{order: order, err: err}
		}(i, buyer)
	}

	close(start)
	wg.Wait()

	var winners int
	for _, result := range results {
		switch {
		case result.err == nil:
			winners++
		case errors.Is(result.err, inventorydomain.ErrInsufficientStock):
			// The expected outcome for everyone who lost the race.
		default:
			t.Errorf("Checkout() failed for an unexpected reason: %v", result.err)
		}
	}

	if winners != 1 {
		t.Errorf("%d of %d checkouts succeeded, want exactly 1", winners, contenders)
	}

	available, reserved := e.stockSnapshot(t, ram)
	if available != 1 || reserved != 1 {
		t.Errorf("stock = (%d available, %d reserved), want (1, 1): one ram, held once", available, reserved)
	}
	if sellable := e.sellable(t, ram); sellable != 0 {
		t.Errorf("sellable = %d, want 0", sellable)
	}
}

func TestOnlyOneBookingTakesASlot(t *testing.T) {
	e := require(t)

	const contenders = 6

	typeID := e.seedConsultations(t)
	date, slotTime := e.bookableSlot(t, typeID)

	errs := make([]error, contenders)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start

			_, err := e.book(context.Background(), typeID, date, slotTime, uniqueEmail("slot"))
			errs[index] = err
		}(i)
	}

	close(start)
	wg.Wait()

	var booked int
	for _, err := range errs {
		switch {
		case err == nil:
			booked++
		case errors.Is(err, consultdomain.ErrSlotUnavailable):
			// Everyone else is told the time has just been taken.
		default:
			t.Errorf("Book() failed for an unexpected reason: %v", err)
		}
	}

	if booked != 1 {
		t.Errorf("%d of %d bookings succeeded, want exactly 1", booked, contenders)
	}

	// The calendar agrees: that time is no longer on offer for this service.
	availability, err := e.container.ConsultationService.Availability(ctx(t), typeID)
	if err != nil {
		t.Fatalf("Availability() returned error: %v", err)
	}

	for _, slot := range availability.Slots {
		if slot.Date == date && slot.Time == slotTime && slot.Available {
			t.Errorf("%s %s is still offered after being booked", date, slotTime)
		}
	}
}

// TestOnlyOneServiceTakesASlot covers the rule that makes a consultation time
// exclusive across services, not just within one: the farm runs a single
// session at a time, so an hour booked for soil advisory is closed to poultry
// health too. Six goroutines spread over every seeded service contend for one
// hour; the partial unique index on (date, time) is what settles it.
func TestOnlyOneServiceTakesASlot(t *testing.T) {
	e := require(t)

	const contenders = 6

	firstType := e.seedConsultations(t)
	typeIDs := e.consultationTypeIDs(t)
	if len(typeIDs) < 2 {
		t.Fatalf("need at least two consultation types to test cross-service exclusivity, have %d", len(typeIDs))
	}

	date, slotTime := e.bookableSlot(t, firstType)

	errs := make([]error, contenders)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start

			// Each contender asks for a different service at the same hour.
			_, err := e.book(context.Background(), typeIDs[index%len(typeIDs)], date, slotTime, uniqueEmail("cross"))
			errs[index] = err
		}(i)
	}

	close(start)
	wg.Wait()

	var booked int
	for _, err := range errs {
		switch {
		case err == nil:
			booked++
		case errors.Is(err, consultdomain.ErrSlotUnavailable):
			// Everyone else is told the time has just been taken.
		default:
			t.Errorf("Book() failed for an unexpected reason: %v", err)
		}
	}

	if booked != 1 {
		t.Errorf("%d of %d bookings succeeded across %d services, want exactly 1",
			booked, contenders, len(typeIDs))
	}

	// Every service now reports that hour as unavailable, not only the one
	// that won it.
	for _, typeID := range typeIDs {
		availability, err := e.container.ConsultationService.Availability(ctx(t), typeID)
		if err != nil {
			t.Fatalf("Availability() returned error: %v", err)
		}

		for _, slot := range availability.Slots {
			if slot.Date == date && slot.Time == slotTime && slot.Available {
				t.Errorf("%s %s is still offered for service %s after another service booked it",
					date, slotTime, typeID)
			}
		}
	}
}

func TestConcurrentWebhooksDeductStockOnce(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("webhook-race"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 5)

	order := e.mustCheckout(t, buyer)
	reference := e.initializePayment(t, order.ID, buyer)
	e.provider.settle(reference, "success")

	// Paystack's retry and the customer's callback page arrive together.
	const deliveries = 4

	body, signature, err := e.provider.webhookBody("charge.success", reference)
	if err != nil {
		t.Fatalf("could not build the webhook body: %v", err)
	}

	codes := make([]int, deliveries)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < deliveries; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start

			codes[index] = e.post(t, "/api/v1/webhooks/paystack", body, signature).Code
		}(i)
	}

	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != 200 {
			t.Errorf("delivery %d returned %d, want 200 so Paystack stops retrying", i+1, code)
		}
	}

	available, reserved := e.stockSnapshot(t, broiler)
	if available != 345 || reserved != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (345, 0)", available, reserved)
	}

	if count := e.outboxCount(t, "order.paid"); count != 1 {
		t.Errorf("order.paid events = %d, want 1", count)
	}
}

func TestCheckoutUsesThePriceAtTheTillNotTheOneInTheCart(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("reprice"))
	maize := e.newProduct(t, "Maize (Corn)", "crops", "100kg bag", 3_800_000, 60)
	e.fillCart(t, buyer, maize, 2)

	// The farm raises the price while the cart sits open.
	raised := int64(4_200_000)
	if _, err := e.container.ProductService.Update(ctx(t), maize.ID.Hex(), updatePrice(raised), adminActor); err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	order := e.mustCheckout(t, buyer)

	if order.Items[0].UnitPrice != raised {
		t.Errorf("unit price = %d, want the current %d", order.Items[0].UnitPrice, raised)
	}
	if order.Subtotal != 2*raised {
		t.Errorf("subtotal = %d, want %d", order.Subtotal, 2*raised)
	}
}

func TestConcurrentAdjustmentsNeverOversell(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	// Ten units, ten customers reserving one each, plus an administrator
	// trying to remove five at the same time. However the operations
	// interleave, the ledger must never promise more than it holds.
	snail := e.newProduct(t, "Farm-Raised Snail", "micro-livestock", "bag", 2_500_000, 10)

	var wg sync.WaitGroup
	reserved := make([]bool, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			err := e.container.InventoryService.Reserve(context.Background(), snail.ID, 1, "race-"+bson.NewObjectID().Hex())
			reserved[index] = err == nil
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Ignored on purpose: whether the correction lands depends on the
		// interleaving. What matters is the invariant checked below.
		_, _ = e.container.InventoryService.Adjust(context.Background(), adjustBy(snail.ID, -5), nil)
	}()

	wg.Wait()

	available, held := e.stockSnapshot(t, snail)
	if held > available {
		t.Errorf("stock = (%d available, %d reserved): more is promised than is held", available, held)
	}

	var successes int
	for _, ok := range reserved {
		if ok {
			successes++
		}
	}
	if successes > 10 {
		t.Errorf("%d reservations succeeded against 10 units", successes)
	}
}
