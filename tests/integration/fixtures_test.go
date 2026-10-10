package integration

import (
	"context"
	"testing"
	"time"

	"bytes"
	"net/http"
	"net/http/httptest"

	authapp "github.com/Dev-folabi/denisco_backend/internal/modules/auth/application"
	categoriesdomain "github.com/Dev-folabi/denisco_backend/internal/modules/categories/domain"
	categoriesinfra "github.com/Dev-folabi/denisco_backend/internal/modules/categories/infrastructure"
	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	inventoryapp "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/application"
	ordersapp "github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	productsapp "github.com/Dev-folabi/denisco_backend/internal/modules/products/application"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Fixtures build the state a test starts from through the same services the
// admin dashboard and storefront use, so the data is shaped exactly as
// production data is.

// adminActor is the administrator behind fixture writes.
var adminActor = productsapp.StockActor{ID: bson.NewObjectID(), Email: "admin@denisco.com", Role: usersdomain.RoleAdmin}

// seedCategories writes the prototype's five categories.
func (e *environment) seedCategories(t *testing.T) {
	t.Helper()

	repo := categoriesinfra.NewMongoRepository(e.db)

	for _, category := range categoriesdomain.Seed() {
		record := category
		if err := repo.Upsert(ctx(t), &record); err != nil {
			t.Fatalf("could not seed category %q: %v", record.Slug, err)
		}
	}
}

// product is a catalogue entry created for a test, with its stock set.
type product struct {
	ID    bson.ObjectID
	Slug  string
	Price int64
}

// newProduct creates an active product in the given category with a stock
// level, and returns what the tests need to refer to it.
func (e *environment) newProduct(t *testing.T, name, category, unit string, priceKobo int64, stock int) product {
	t.Helper()

	view, err := e.container.ProductService.Create(ctx(t), productsapp.CreateInput{
		Name:         name,
		Description:  name + " raised on the farm.",
		CategorySlug: category,
		Price:        priceKobo,
		Unit:         unit,
		Status:       "active",
		Stock:        stock,
	}, adminActor)
	if err != nil {
		t.Fatalf("could not create product %q: %v", name, err)
	}

	id, err := bson.ObjectIDFromHex(view.ID)
	if err != nil {
		t.Fatalf("product %q has an unreadable id %q: %v", name, view.ID, err)
	}

	return product{ID: id, Slug: view.Slug, Price: view.Price}
}

// customer is a registered account a test acts as.
type customer struct {
	ID    bson.ObjectID
	Email string
}

// newCustomer registers an account through the auth service.
func (e *environment) newCustomer(t *testing.T, email string) customer {
	t.Helper()

	session, err := e.container.AuthService.Register(ctx(t), authapp.RegisterInput{
		FirstName: "Ada",
		LastName:  "Obi",
		Email:     email,
		Phone:     "+2348012345678",
		Password:  "farm-secret-1",
	})
	if err != nil {
		t.Fatalf("could not register %q: %v", email, err)
	}

	id, err := bson.ObjectIDFromHex(session.User.ID)
	if err != nil {
		t.Fatalf("registered user has an unreadable id %q: %v", session.User.ID, err)
	}

	return customer{ID: id, Email: session.User.Email}
}

// fillCart adds a product to a customer's cart.
func (e *environment) fillCart(t *testing.T, buyer customer, item product, quantity int) {
	t.Helper()

	if _, err := e.container.CartService.AddItem(ctx(t), buyer.ID, item.ID, quantity); err != nil {
		t.Fatalf("could not add %d × %s to the cart: %v", quantity, item.Slug, err)
	}
}

// checkoutInput is the contact and fulfilment detail a checkout carries. Farm
// pickup keeps the delivery fee out of the way of tests that are not about it.
func checkoutInput(buyer customer) ordersapp.CheckoutInput {
	return ordersapp.CheckoutInput{
		Name:           "Ada Obi",
		Email:          buyer.Email,
		Phone:          "+2348012345678",
		DeliveryMethod: ordersdomain.DeliveryPickup,
	}
}

// checkout turns a customer's cart into an order.
func (e *environment) checkout(t *testing.T, buyer customer) (*ordersapp.View, error) {
	t.Helper()

	return e.container.OrderService.Checkout(ctx(t), buyer.ID, checkoutInput(buyer))
}

// mustCheckout fails the test when checkout does not succeed.
func (e *environment) mustCheckout(t *testing.T, buyer customer) *ordersapp.View {
	t.Helper()

	order, err := e.checkout(t, buyer)
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	return order
}

// orderViewer is the customer asking about their own order.
func orderViewer(buyer customer) ordersapp.Viewer {
	return ordersapp.Viewer{UserID: buyer.ID}
}

// order reads an order back as an administrator sees it.
func (e *environment) order(t *testing.T, id string) *ordersapp.View {
	t.Helper()

	view, err := e.container.OrderService.Get(ctx(t), id, ordersapp.Viewer{IsAdmin: true})
	if err != nil {
		t.Fatalf("could not read order %s: %v", id, err)
	}

	return view
}

// deliverWebhook posts a provider callback to the real webhook endpoint,
// signed as Paystack signs it, so the signature check and the idempotency
// rules are both exercised.
func (e *environment) deliverWebhook(t *testing.T, event, reference string) {
	t.Helper()

	recorder := e.postWebhook(t, event, reference)
	if recorder.Code != http.StatusOK {
		t.Fatalf("webhook %s returned %d: %s", event, recorder.Code, recorder.Body.String())
	}
}

// postWebhook delivers a signed callback and hands back the response, for the
// tests that are about how a callback is refused.
func (e *environment) postWebhook(t *testing.T, event, reference string) *httptest.ResponseRecorder {
	t.Helper()

	body, signature, err := e.provider.webhookBody(event, reference)
	if err != nil {
		t.Fatalf("could not build the webhook body: %v", err)
	}

	return e.post(t, "/api/v1/webhooks/paystack", body, signature)
}

// post sends a webhook body with the given signature header.
func (e *environment) post(t *testing.T, path string, body []byte, signature string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if signature != "" {
		request.Header.Set("X-Paystack-Signature", signature)
	}

	recorder := httptest.NewRecorder()
	e.api.ServeHTTP(recorder, request)

	return recorder
}

// sellable reads how many units of a product may still be sold.
func (e *environment) sellable(t *testing.T, item product) int {
	t.Helper()

	level, err := e.container.InventoryService.LevelFor(ctx(t), item.ID)
	if err != nil {
		t.Fatalf("could not read stock for %s: %v", item.Slug, err)
	}

	return level.Sellable
}

// stockSnapshot reads the on-hand and reserved quantities.
func (e *environment) stockSnapshot(t *testing.T, item product) (available, reserved int) {
	t.Helper()

	level, err := e.container.InventoryService.LevelFor(ctx(t), item.ID)
	if err != nil {
		t.Fatalf("could not read stock for %s: %v", item.Slug, err)
	}

	return level.Available, level.Reserved
}

// seedConsultations writes the prototype's consultation types and opens the
// default availability grid, then returns the first type's identifier.
func (e *environment) seedConsultations(t *testing.T) string {
	t.Helper()

	if _, err := e.container.ConsultationService.SeedTypes(ctx(t)); err != nil {
		t.Fatalf("could not seed consultation types: %v", err)
	}
	if _, err := e.container.ConsultationService.EnsureDefaultAvailability(ctx(t)); err != nil {
		t.Fatalf("could not open consultation availability: %v", err)
	}

	types, err := e.container.ConsultationService.ListTypes(ctx(t), true)
	if err != nil {
		t.Fatalf("could not list consultation types: %v", err)
	}
	if len(types) == 0 {
		t.Fatal("no consultation types were seeded")
	}

	return types[0].ID
}

// consultationTypeIDs returns every seeded consultation type, for the tests
// that need two different services.
func (e *environment) consultationTypeIDs(t *testing.T) []string {
	t.Helper()

	types, err := e.container.ConsultationService.ListTypes(ctx(t), true)
	if err != nil {
		t.Fatalf("could not list consultation types: %v", err)
	}

	ids := make([]string, 0, len(types))
	for _, consultationType := range types {
		ids = append(ids, consultationType.ID)
	}
	return ids
}

// bookableSlot returns a date and time that the availability grid offers for a
// consultation type and that nothing has taken yet.
func (e *environment) bookableSlot(t *testing.T, typeID string) (date, slotTime string) {
	t.Helper()

	availability, err := e.container.ConsultationService.Availability(ctx(t), typeID)
	if err != nil {
		t.Fatalf("could not read availability: %v", err)
	}

	for _, slot := range availability.Slots {
		if slot.Available {
			return slot.Date, slot.Time
		}
	}

	t.Fatal("the availability grid offers no free slot")
	return "", ""
}

// anotherBookableTime returns a free slot on the same date other than the one
// given, for the tests that need two live bookings.
func (e *environment) anotherBookableTime(t *testing.T, typeID, date, exclude string) string {
	t.Helper()

	availability, err := e.container.ConsultationService.Availability(ctx(t), typeID)
	if err != nil {
		t.Fatalf("could not read availability: %v", err)
	}

	for _, slot := range availability.Slots {
		if slot.Date == date && slot.Time != exclude && slot.Available {
			return slot.Time
		}
	}

	t.Fatalf("no second free slot on %s", date)
	return ""
}

// book places a consultation booking as a guest. It takes a context rather
// than a *testing.T so the concurrency tests can call it from goroutines.
func (e *environment) book(c context.Context, typeID, date, slotTime, email string) (*consultapp.BookingView, error) {
	return e.container.ConsultationService.Book(c, consultapp.BookInput{
		TypeID: typeID,
		Date:   date,
		Time:   slotTime,
		Name:   "Ada Obi",
		Email:  email,
		Phone:  "+2348012345678",
	})
}

// pay walks a payment from initialization to a confirmed webhook, the way a
// real checkout does: initialize, the customer pays at the provider, then
// Paystack calls back.
func (e *environment) pay(t *testing.T, orderID string, buyer customer) string {
	t.Helper()

	t.Helper()

	reference := e.initializePayment(t, orderID, buyer)
	e.provider.settle(reference, "success")
	e.deliverWebhook(t, "charge.success", reference)

	return reference
}

// initializePayment starts an attempt and returns its reference.
func (e *environment) initializePayment(t *testing.T, orderID string, buyer customer) string {
	t.Helper()

	view, err := e.container.PaymentService.Initialize(ctx(t), orderID, buyer.ID, false)
	if err != nil {
		t.Fatalf("could not initialize a payment: %v", err)
	}

	return view.Reference
}

// updatePrice is a catalogue price change, as the admin form sends one.
func updatePrice(price int64) productsapp.UpdateInput {
	return productsapp.UpdateInput{Price: &price}
}

// adjustBy is a signed stock correction.
func adjustBy(productID bson.ObjectID, delta int) inventoryapp.AdjustInput {
	return inventoryapp.AdjustInput{
		ProductID: productID,
		Quantity:  delta,
		Reason:    "Stocktake",
	}
}

// uniqueEmail keeps accounts distinct across a test's customers.
func uniqueEmail(prefix string) string {
	return prefix + "-" + bson.NewObjectID().Hex() + "@denisco.com"
}

// recently is a timestamp a test can compare against without flaking.
func recently() time.Time {
	return time.Now().UTC().Add(-time.Minute)
}
