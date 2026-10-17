package integration

import (
	"errors"
	"testing"

	deliveryapp "github.com/Dev-folabi/denisco_backend/internal/modules/delivery/application"
	deliverydomain "github.com/Dev-folabi/denisco_backend/internal/modules/delivery/domain"
	ordersapp "github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// The delivery offering is the checkout's only source for a home-delivery
// fee, so these tests pin the whole path: an area is created, quoted to the
// customer, charged at checkout, and hidden again when it is retired or the
// offering is switched off.

// newLocation creates a delivery area through the service, as the settings
// page does.
func (e *environment) newLocation(t *testing.T, name string, fee int64) string {
	t.Helper()

	view, err := e.container.DeliveryService.CreateLocation(ctx(t), deliveryapp.LocationInput{
		Name: &name,
		Fee:  &fee,
	}, deliveryActor())
	if err != nil {
		t.Fatalf("could not create delivery location %q: %v", name, err)
	}
	return view.ID
}

// deliveryActor is the administrator behind delivery-setting writes.
func deliveryActor() deliveryapp.Actor {
	return deliveryapp.Actor{ID: bson.NewObjectID(), Email: "admin@denisco.com", Role: "admin"}
}

// deliveryCheckout is a home-delivery checkout against a named area.
func deliveryCheckout(buyer customer, locationID string) ordersapp.CheckoutInput {
	input := checkoutInput(buyer)
	input.DeliveryMethod = ordersdomain.DeliveryHome
	input.Address = "12 Sample Close, Gwarimpa, Abuja"
	input.LocationID = locationID
	return input
}

func TestDeliveryOfferingStartsClosedAndOpensWithAnArea(t *testing.T) {
	e := require(t)

	// No area has been configured, so the checkout must see nothing to sell
	// even though the switch defaults to on.
	offering, err := e.container.DeliveryService.Options(ctx(t))
	if err != nil {
		t.Fatalf("Options() returned error: %v", err)
	}
	if offering.Enabled {
		t.Error("the offering reports itself enabled with no delivery area configured")
	}

	e.newLocation(t, "Gwarimpa", 300_000)

	offering, err = e.container.DeliveryService.Options(ctx(t))
	if err != nil {
		t.Fatalf("Options() returned error: %v", err)
	}
	if !offering.Enabled {
		t.Error("the offering is still closed after an area was added")
	}
	if len(offering.Locations) != 1 || offering.Locations[0].Fee != 300_000 {
		t.Errorf("locations = %+v, want one area at 300000 kobo (₦3,000)", offering.Locations)
	}
	if offering.Locations[0].Name != "Gwarimpa" {
		t.Errorf("location name = %q, want Gwarimpa", offering.Locations[0].Name)
	}
}

func TestCheckoutChargesTheConfiguredAreaFee(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	locationID := e.newLocation(t, "Wuse", 250_000)

	buyer := e.newCustomer(t, uniqueEmail("delivery"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 1)

	order, err := e.container.OrderService.Checkout(ctx(t), buyer.ID, deliveryCheckout(buyer, locationID))
	if err != nil {
		t.Fatalf("Checkout() returned error: %v", err)
	}

	// ₦6,500 + the area's ₦2,500.
	if order.DeliveryFee != 250_000 {
		t.Errorf("delivery fee = %d, want the area's 250000 kobo", order.DeliveryFee)
	}
	if order.Total != 900_000 {
		t.Errorf("total = %d, want 900000 kobo (₦9,000)", order.Total)
	}
	if order.DeliveryLocationName != "Wuse" {
		t.Errorf("location name = %q, want the area snapshotted onto the order", order.DeliveryLocationName)
	}
}

func TestCheckoutRefusesAnAreaThatIsNotOnOffer(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	buyer := e.newCustomer(t, uniqueEmail("delivery-refuse"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 1)

	// No area chosen.
	_, err := e.container.OrderService.Checkout(ctx(t), buyer.ID, deliveryCheckout(buyer, ""))
	if !errors.Is(err, ordersapp.ErrLocationRequired) {
		t.Errorf("Checkout() with no area = %v, want ErrLocationRequired", err)
	}

	// An area that never existed.
	_, err = e.container.OrderService.Checkout(ctx(t), buyer.ID, deliveryCheckout(buyer, bson.NewObjectID().Hex()))
	if !errors.Is(err, ordersapp.ErrLocationUnavailable) {
		t.Errorf("Checkout() with an unknown area = %v, want ErrLocationUnavailable", err)
	}

	// An area that was retired after the customer opened the checkout.
	// The stock and cart must be untouched by the refusals above.
	other := e.newCustomer(t, uniqueEmail("delivery-retired"))
	e.fillCart(t, other, broiler, 1)
	locationID := e.newLocation(t, "Asokoro", 400_000)

	enabled := false
	if _, err := e.container.DeliveryService.UpdateLocation(ctx(t), locationID, deliveryapp.LocationInput{
		Active: &enabled,
	}, deliveryActor()); err != nil {
		t.Fatalf("could not retire the area: %v", err)
	}

	_, err = e.container.OrderService.Checkout(ctx(t), other.ID, deliveryCheckout(other, locationID))
	if !errors.Is(err, ordersapp.ErrLocationUnavailable) {
		t.Errorf("Checkout() with a retired area = %v, want ErrLocationUnavailable", err)
	}

	// The cart is intact: a refused checkout must not consume it.
	cart, err := e.container.CartService.Get(ctx(t), other.ID)
	if err != nil {
		t.Fatalf("could not read the cart: %v", err)
	}
	if len(cart.Items) != 1 {
		t.Errorf("cart lines after a refused checkout = %d, want 1", len(cart.Items))
	}
}

func TestCheckoutRefusesHomeDeliveryWhileTheOfferingIsOff(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	locationID := e.newLocation(t, "Lugbe", 350_000)

	buyer := e.newCustomer(t, uniqueEmail("delivery-off"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 1)

	disabled := false
	if _, err := e.container.DeliveryService.SetEnabled(ctx(t), deliveryapp.SetEnabledInput{
		Enabled: &disabled,
	}, deliveryActor()); err != nil {
		t.Fatalf("could not switch the offering off: %v", err)
	}

	_, err := e.container.OrderService.Checkout(ctx(t), buyer.ID, deliveryCheckout(buyer, locationID))
	if !errors.Is(err, ordersapp.ErrLocationUnavailable) {
		t.Errorf("Checkout() while the offering is off = %v, want ErrLocationUnavailable", err)
	}

	// Pickup still works with the offering off: the farm gate does not
	// depend on a delivery fleet.
	e.fillCart(t, buyer, broiler, 1)
	if _, err := e.checkout(t, buyer); err != nil {
		t.Errorf("pickup Checkout() with the offering off returned error: %v", err)
	}

	// The public offering tells the checkout to hide the option entirely.
	offering, err := e.container.DeliveryService.Options(ctx(t))
	if err != nil {
		t.Fatalf("Options() returned error: %v", err)
	}
	if offering.Enabled {
		t.Error("the offering reports itself enabled after being switched off")
	}
}

func TestDeliveryAreaNamesAreUniqueCaseInsensitively(t *testing.T) {
	e := require(t)

	// Every test shares one database, so the seed name is this test's own.
	e.newLocation(t, "Nyanya", 300_000)

	_, err := e.container.DeliveryService.CreateLocation(ctx(t), deliveryapp.LocationInput{
		Name: stringPtr("NYANYA"),
		Fee:  int64Ptr(300_000),
	}, deliveryActor())
	if !errors.Is(err, deliverydomain.ErrNameTaken) {
		t.Errorf("duplicate name error = %v, want the name-taken conflict", err)
	}
}

func stringPtr(s string) *string { return &s }
func int64Ptr(v int64) *int64    { return &v }
