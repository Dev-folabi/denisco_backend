package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	cartinfra "github.com/Dev-folabi/denisco_backend/internal/modules/cart/infrastructure"
	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	consultdomain "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	consultinfra "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/infrastructure"
	inventorydomain "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/domain"
	inventoryinfra "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/infrastructure"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	ordersinfra "github.com/Dev-folabi/denisco_backend/internal/modules/orders/infrastructure"
	paymentsdomain "github.com/Dev-folabi/denisco_backend/internal/modules/payments/domain"
	paymentsinfra "github.com/Dev-folabi/denisco_backend/internal/modules/payments/infrastructure"
	productsdomain "github.com/Dev-folabi/denisco_backend/internal/modules/products/domain"
	productsinfra "github.com/Dev-folabi/denisco_backend/internal/modules/products/infrastructure"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	usersinfra "github.com/Dev-folabi/denisco_backend/internal/modules/users/infrastructure"
	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUserRepository(t *testing.T) {
	e := require(t)
	repo := usersinfra.NewMongoRepository(e.db)
	c := ctx(t)

	user := &usersdomain.User{
		FirstName:    "Ada",
		LastName:     "Obi",
		Email:        "ada@denisco.com",
		Phone:        "+2348012345678",
		PasswordHash: "hash",
		Role:         usersdomain.RoleCustomer,
		Status:       usersdomain.StatusActive,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := repo.Create(c, user); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if user.ID.IsZero() {
		t.Fatal("Create() left the identifier unset")
	}

	// The unique index, not application code, is what guarantees one account
	// per address.
	duplicate := *user
	duplicate.ID = bson.NilObjectID
	if err := repo.Create(c, &duplicate); !errors.Is(err, usersdomain.ErrEmailTaken) {
		t.Errorf("Create() with a taken email error = %v, want ErrEmailTaken", err)
	}

	byID, err := repo.FindByID(c, user.ID)
	if err != nil {
		t.Fatalf("FindByID() returned error: %v", err)
	}
	if byID.Email != user.Email {
		t.Errorf("FindByID() email = %q, want %q", byID.Email, user.Email)
	}

	// Lookups normalise, so the mixed-case address a customer types still
	// finds the account.
	byEmail, err := repo.FindByEmail(c, "ADA@denisco.com")
	if err != nil {
		t.Fatalf("FindByEmail() returned error: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Errorf("FindByEmail() id = %s, want %s", byEmail.ID.Hex(), user.ID.Hex())
	}

	if _, err := repo.FindByEmail(c, "nobody@denisco.com"); !errors.Is(err, usersdomain.ErrUserNotFound) {
		t.Errorf("FindByEmail() for an unknown address error = %v, want ErrUserNotFound", err)
	}

	if err := repo.UpdatePasswordHash(c, user.ID, "rehashed"); err != nil {
		t.Fatalf("UpdatePasswordHash() returned error: %v", err)
	}

	loginAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.TouchLastLogin(c, user.ID, loginAt); err != nil {
		t.Fatalf("TouchLastLogin() returned error: %v", err)
	}

	updated, err := repo.FindByID(c, user.ID)
	if err != nil {
		t.Fatalf("FindByID() returned error: %v", err)
	}
	if updated.PasswordHash != "rehashed" {
		t.Errorf("password hash = %q, want %q", updated.PasswordHash, "rehashed")
	}
	if updated.LastLoginAt == nil || updated.LastLoginAt.Before(recently()) {
		t.Errorf("last_login_at = %v, want a recent timestamp", updated.LastLoginAt)
	}

	customers, total, err := repo.List(c, usersdomain.AdminFilter{Search: "ada"}, 1, 20)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if total != 1 || len(customers) != 1 {
		t.Errorf("List() = %d customers (total %d), want 1 (1)", len(customers), total)
	}

	count, err := repo.Count(c, usersdomain.RoleCustomer)
	if err != nil {
		t.Fatalf("Count() returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("Count(customer) = %d, want 1", count)
	}
}

func TestProductRepository(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	repo := productsinfra.NewMongoRepository(e.db)
	c := ctx(t)

	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	ram := e.newProduct(t, "Ram", "livestock", "head", 12_000_000, 2)

	stored, err := repo.FindBySlug(c, broiler.Slug)
	if err != nil {
		t.Fatalf("FindBySlug() returned error: %v", err)
	}
	if stored.Price != 650_000 || stored.Currency != productsdomain.Currency {
		t.Errorf("stored product = (price %d, currency %q), want (650000, %q)", stored.Price, stored.Currency, productsdomain.Currency)
	}

	taken, err := repo.SlugExists(c, broiler.Slug)
	if err != nil {
		t.Fatalf("SlugExists() returned error: %v", err)
	}
	if !taken {
		t.Error("SlugExists() = false for a slug that is in use")
	}

	// A second product with the same name must not silently share the slug.
	clash := &productsdomain.Product{
		Name:      "Broiler Chicken",
		Slug:      broiler.Slug,
		Price:     650_000,
		Currency:  productsdomain.Currency,
		Unit:      "bird",
		Status:    productsdomain.StatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := repo.Create(c, clash); !errors.Is(err, productsdomain.ErrSlugTaken) {
		t.Errorf("Create() with a taken slug error = %v, want ErrSlugTaken", err)
	}

	poultry, _, err := repo.List(c, productsdomain.Filter{
		Status: productsdomain.StatusActive,
		Search: "broiler",
	}, 1, 20)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(poultry) != 1 || poultry[0].Slug != broiler.Slug {
		t.Errorf("List(search=broiler) returned %d products, want only %q", len(poultry), broiler.Slug)
	}

	ascending, _, err := repo.List(c, productsdomain.Filter{Sort: productsdomain.SortPriceAsc}, 1, 20)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(ascending) != 2 || ascending[0].Slug != broiler.Slug {
		t.Errorf("List(price-asc) put %q first, want %q", ascending[0].Slug, broiler.Slug)
	}

	if err := repo.Update(c, ram.ID, bson.M{"price": int64(13_000_000)}); err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	repriced, err := repo.FindByID(c, ram.ID)
	if err != nil {
		t.Fatalf("FindByID() returned error: %v", err)
	}
	if repriced.Price != 13_000_000 {
		t.Errorf("price = %d, want 13000000", repriced.Price)
	}

	// Nothing has ordered this product, so removing it deletes it outright
	// rather than leaving an archived row behind.
	result, err := e.container.ProductService.Remove(c, ram.ID.Hex(), adminActor)
	if err != nil {
		t.Fatalf("Remove() returned error: %v", err)
	}
	if !result.Deleted {
		t.Error("Remove() archived a product that has no orders; want it deleted")
	}

	if _, err := repo.FindByID(c, ram.ID); !errors.Is(err, productsdomain.ErrProductNotFound) {
		t.Errorf("FindByID() after deletion error = %v, want ErrProductNotFound", err)
	}

	// The stock record goes with it, so no orphan is left behind. The
	// collection is read directly because the inventory read model
	// deliberately zero-fills a product it has no record for.
	remaining, err := e.db.Collection(database.CollInventory).
		CountDocuments(c, bson.M{"product_id": ram.ID})
	if err != nil {
		t.Fatalf("could not count stock records: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d stock records survived the product's deletion, want 0", remaining)
	}

	active, err := repo.Count(c, productsdomain.StatusActive)
	if err != nil {
		t.Fatalf("Count() returned error: %v", err)
	}
	if active != 1 {
		t.Errorf("Count(active) = %d, want 1", active)
	}
}

func TestInventoryRepositoryConditionalUpdates(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	repo := inventoryinfra.NewMongoRepository(e.db)
	c := ctx(t)

	item := e.newProduct(t, "Guinea Fowl", "poultry", "bird", 950_000, 10)

	record, err := repo.Get(c, item.ID)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if record.AvailableQty != 10 || record.ReservedQty != 0 {
		t.Fatalf("stock = (%d available, %d reserved), want (10, 0)", record.AvailableQty, record.ReservedQty)
	}

	reserved, err := repo.Reserve(c, item.ID, 8)
	if err != nil {
		t.Fatalf("Reserve() returned error: %v", err)
	}
	if !reserved {
		t.Fatal("Reserve(8 of 10) = false, want true")
	}

	// The guard is sellable stock, not on-hand stock: 2 are left.
	if reserved, err := repo.Reserve(c, item.ID, 3); err != nil || reserved {
		t.Errorf("Reserve(3 with 2 sellable) = (%v, %v), want (false, nil)", reserved, err)
	}

	if released, err := repo.Release(c, item.ID, 3); err != nil || !released {
		t.Errorf("Release(3) = (%v, %v), want (true, nil)", released, err)
	}

	if deducted, err := repo.Deduct(c, item.ID, 5); err != nil || !deducted {
		t.Errorf("Deduct(5) = (%v, %v), want (true, nil)", deducted, err)
	}

	after, err := repo.Get(c, item.ID)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if after.AvailableQty != 5 || after.ReservedQty != 0 {
		t.Errorf("stock = (%d available, %d reserved), want (5, 0)", after.AvailableQty, after.ReservedQty)
	}

	// An adjustment may not leave less on hand than is already promised.
	if _, err := repo.Reserve(c, item.ID, 4); err != nil {
		t.Fatalf("Reserve() returned error: %v", err)
	}
	if applied, err := repo.Adjust(c, item.ID, -3); err != nil || applied {
		t.Errorf("Adjust(-3 with 4 reserved of 5) = (%v, %v), want (false, nil)", applied, err)
	}
	if applied, err := repo.Adjust(c, item.ID, 10); err != nil || !applied {
		t.Errorf("Adjust(+10) = (%v, %v), want (true, nil)", applied, err)
	}

	// 15 on hand with 4 promised leaves 11 sellable. A late payment may take
	// from those 11 without a reservation behind it, and no more: the four
	// reserved units belong to an order that is still in flight.
	if sold, err := repo.SellWithoutReservation(c, item.ID, 12); err != nil || sold {
		t.Errorf("SellWithoutReservation(12 of 11 sellable) = (%v, %v), want (false, nil)", sold, err)
	}
	if sold, err := repo.SellWithoutReservation(c, item.ID, 2); err != nil || !sold {
		t.Errorf("SellWithoutReservation(2 of 11 sellable) = (%v, %v), want (true, nil)", sold, err)
	}

	secured, err := repo.Get(c, item.ID)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if secured.AvailableQty != 13 || secured.ReservedQty != 4 {
		t.Errorf("stock = (%d available, %d reserved), want (13, 4)", secured.AvailableQty, secured.ReservedQty)
	}

	if err := repo.RecordMovement(c, &inventorydomain.Movement{
		ProductID: item.ID,
		Type:      inventorydomain.MovementAdjustment,
		Quantity:  10,
		Reason:    "Stocktake",
	}); err != nil {
		t.Fatalf("RecordMovement() returned error: %v", err)
	}

	movements, total, err := repo.Movements(c, &item.ID, 1, 50)
	if err != nil {
		t.Fatalf("Movements() returned error: %v", err)
	}
	if total == 0 || len(movements) == 0 {
		t.Error("Movements() returned nothing, want the recorded history")
	}
}

func TestCartRepository(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	repo := cartinfra.NewMongoRepository(e.db)
	c := ctx(t)

	buyer := e.newCustomer(t, uniqueEmail("cart"))
	maize := e.newProduct(t, "Maize (Corn)", "crops", "100kg bag", 3_800_000, 60)

	// A customer with no cart gets an empty one rather than an error.
	empty, err := repo.Get(c, buyer.ID)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if len(empty.Items) != 0 {
		t.Errorf("a new cart holds %d items, want 0", len(empty.Items))
	}

	if err := repo.AddItem(c, buyer.ID, maize.ID, 2); err != nil {
		t.Fatalf("AddItem() returned error: %v", err)
	}
	// Adding the same product again increases the line rather than repeating it.
	if err := repo.AddItem(c, buyer.ID, maize.ID, 3); err != nil {
		t.Fatalf("AddItem() returned error: %v", err)
	}

	cart, err := repo.Get(c, buyer.ID)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if len(cart.Items) != 1 || cart.Items[0].Quantity != 5 {
		t.Errorf("cart = %d lines with quantity %d, want 1 line of 5", len(cart.Items), cart.Items[0].Quantity)
	}

	if applied, err := repo.SetQuantity(c, buyer.ID, maize.ID, 4); err != nil || !applied {
		t.Errorf("SetQuantity() = (%v, %v), want (true, nil)", applied, err)
	}
	if applied, err := repo.SetQuantity(c, buyer.ID, bson.NewObjectID(), 1); err != nil || applied {
		t.Errorf("SetQuantity() for a line that is not in the cart = (%v, %v), want (false, nil)", applied, err)
	}

	if removed, err := repo.RemoveItem(c, buyer.ID, maize.ID); err != nil || !removed {
		t.Errorf("RemoveItem() = (%v, %v), want (true, nil)", removed, err)
	}

	if err := repo.AddItem(c, buyer.ID, maize.ID, 1); err != nil {
		t.Fatalf("AddItem() returned error: %v", err)
	}
	if err := repo.Clear(c, buyer.ID); err != nil {
		t.Fatalf("Clear() returned error: %v", err)
	}

	cleared, err := repo.Get(c, buyer.ID)
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if len(cleared.Items) != 0 {
		t.Errorf("cart holds %d lines after Clear(), want 0", len(cleared.Items))
	}
}

func TestOrderRepository(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	repo := ordersinfra.NewMongoRepository(e.db)
	c := ctx(t)

	buyer := e.newCustomer(t, uniqueEmail("orders"))
	broiler := e.newProduct(t, "Broiler Chicken", "poultry", "bird", 650_000, 350)
	e.fillCart(t, buyer, broiler, 3)

	placed := e.mustCheckout(t, buyer)
	orderID, err := bson.ObjectIDFromHex(placed.ID)
	if err != nil {
		t.Fatalf("order id %q is unreadable: %v", placed.ID, err)
	}

	byNumber, err := repo.FindByNumber(c, placed.OrderNumber)
	if err != nil {
		t.Fatalf("FindByNumber() returned error: %v", err)
	}
	if byNumber.ID != orderID {
		t.Errorf("FindByNumber() id = %s, want %s", byNumber.ID.Hex(), orderID.Hex())
	}

	if taken, err := repo.NumberExists(c, placed.OrderNumber); err != nil || !taken {
		t.Errorf("NumberExists(%q) = (%v, %v), want (true, nil)", placed.OrderNumber, taken, err)
	}

	if _, err := repo.FindByNumber(c, "DG-000000"); !errors.Is(err, ordersdomain.ErrOrderNotFound) {
		t.Errorf("FindByNumber() for an unknown number error = %v, want ErrOrderNotFound", err)
	}

	// Orders older than the cut-off and still holding stock are what the
	// expiry sweep looks for.
	if err := repo.Update(c, orderID, bson.M{"created_at": time.Now().UTC().Add(-2 * time.Hour)}); err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	stale, err := repo.StaleReserved(c, time.Now().UTC().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("StaleReserved() returned error: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != orderID {
		t.Errorf("StaleReserved() returned %d orders, want the unpaid one", len(stale))
	}

	paidAt := time.Now().UTC()
	applied, err := repo.MarkPaid(c, orderID, "PSK-REPO", paidAt)
	if err != nil {
		t.Fatalf("MarkPaid() returned error: %v", err)
	}
	if !applied {
		t.Fatal("MarkPaid() = false on an unpaid order, want true")
	}

	// The guard is what makes a replayed webhook harmless.
	if applied, err := repo.MarkPaid(c, orderID, "PSK-REPO", paidAt); err != nil || applied {
		t.Errorf("MarkPaid() a second time = (%v, %v), want (false, nil)", applied, err)
	}

	byRef, err := repo.FindByPaymentRef(c, "PSK-REPO")
	if err != nil {
		t.Fatalf("FindByPaymentRef() returned error: %v", err)
	}
	if byRef.ID != orderID {
		t.Errorf("FindByPaymentRef() id = %s, want %s", byRef.ID.Hex(), orderID.Hex())
	}

	if released, err := repo.ReleaseReservation(c, orderID); err != nil || !released {
		t.Errorf("ReleaseReservation() = (%v, %v), want (true, nil)", released, err)
	}
	if released, err := repo.ReleaseReservation(c, orderID); err != nil || released {
		t.Errorf("ReleaseReservation() a second time = (%v, %v), want (false, nil)", released, err)
	}

	stats, err := repo.Stats(c)
	if err != nil {
		t.Fatalf("Stats() returned error: %v", err)
	}
	if stats.TotalOrders != 1 || stats.PaidOrders != 1 || stats.Revenue != placed.Total {
		t.Errorf("Stats() = %+v, want 1 order, 1 paid, revenue %d", stats, placed.Total)
	}

	revenue, err := repo.DailyRevenue(c, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		t.Fatalf("DailyRevenue() returned error: %v", err)
	}
	if len(revenue) != 1 {
		t.Errorf("DailyRevenue() covers %d days, want 1", len(revenue))
	}

	flagged := true
	if orders, _, err := repo.List(c, ordersdomain.Filter{NeedsReconciliation: &flagged}, 1, 20); err != nil || len(orders) != 0 {
		t.Errorf("List(needs_reconciliation) returned %d orders, want none", len(orders))
	}

	clear := false
	if orders, _, err := repo.List(c, ordersdomain.Filter{NeedsReconciliation: &clear}, 1, 20); err != nil || len(orders) != 1 {
		t.Errorf("List(not flagged) returned %d orders, want 1", len(orders))
	}

	if orders, total, err := repo.List(c, ordersdomain.Filter{Search: placed.OrderNumber}, 1, 20); err != nil || total != 1 || len(orders) != 1 {
		t.Errorf("List(search by order number) = %d of %d, want 1 of 1", len(orders), total)
	}

	history, total, err := repo.ListForUser(c, buyer.ID, 1, 20)
	if err != nil {
		t.Fatalf("ListForUser() returned error: %v", err)
	}
	if total != 1 || len(history) != 1 {
		t.Errorf("ListForUser() = %d of %d, want 1 of 1", len(history), total)
	}
}

func TestPaymentRepository(t *testing.T) {
	e := require(t)
	repo := paymentsinfra.NewMongoRepository(e.db)
	c := ctx(t)

	userID := bson.NewObjectID()
	orderID := bson.NewObjectID()

	attempt := &paymentsdomain.Payment{
		Reference:   "PSK-FIRST",
		OrderID:     orderID,
		OrderNumber: "DG-100001",
		UserID:      userID,
		Email:       "ada@denisco.com",
		Amount:      1_950_000,
		Currency:    "NGN",
		Status:      paymentsdomain.StatusInitialized,
		Provider:    paymentsdomain.ProviderPaystack,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := repo.Create(c, attempt); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if taken, err := repo.ReferenceExists(c, "PSK-FIRST"); err != nil || !taken {
		t.Errorf("ReferenceExists() = (%v, %v), want (true, nil)", taken, err)
	}

	paidAt := time.Now().UTC()
	settled, err := repo.Settle(c, "PSK-FIRST", paymentsdomain.SettlementUpdate{
		Status:            paymentsdomain.StatusSuccessful,
		ProviderReference: "1000001",
		Channel:           "card",
		GatewayResponse:   "Successful",
		PaidAt:            &paidAt,
	})
	if err != nil {
		t.Fatalf("Settle() returned error: %v", err)
	}
	if !settled {
		t.Fatal("Settle() = false on a pending attempt, want true")
	}

	// A final state is final: a replayed webhook settles nothing twice.
	if settled, err := repo.Settle(c, "PSK-FIRST", paymentsdomain.SettlementUpdate{
		Status: paymentsdomain.StatusFailed,
	}); err != nil || settled {
		t.Errorf("Settle() on a settled attempt = (%v, %v), want (false, nil)", settled, err)
	}

	stored, err := repo.FindByReference(c, "PSK-FIRST")
	if err != nil {
		t.Fatalf("FindByReference() returned error: %v", err)
	}
	if stored.Status != paymentsdomain.StatusSuccessful || stored.PaidAt == nil {
		t.Errorf("attempt = (status %q, paid_at %v), want successful and a timestamp", stored.Status, stored.PaidAt)
	}

	// A retry is its own record, so the failed attempt stays auditable.
	retry := *attempt
	retry.ID = bson.NilObjectID
	retry.Reference = "PSK-SECOND"
	retry.Status = paymentsdomain.StatusInitialized
	if err := repo.Create(c, &retry); err != nil {
		t.Fatalf("Create() for a retry returned error: %v", err)
	}

	byOrder, total, err := repo.List(c, paymentsdomain.Filter{OrderID: &orderID}, 1, 20)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if total != 2 || len(byOrder) != 2 {
		t.Errorf("List(order) = %d of %d, want 2 of 2", len(byOrder), total)
	}

	if refunded, err := repo.MarkRefunded(c, stored.ID); err != nil || !refunded {
		t.Errorf("MarkRefunded() = (%v, %v), want (true, nil)", refunded, err)
	}
	if refunded, err := repo.MarkRefunded(c, stored.ID); err != nil || refunded {
		t.Errorf("MarkRefunded() a second time = (%v, %v), want (false, nil)", refunded, err)
	}

	if _, err := repo.FindByReference(c, "PSK-UNKNOWN"); !errors.Is(err, paymentsdomain.ErrPaymentNotFound) {
		t.Errorf("FindByReference() for an unknown reference error = %v, want ErrPaymentNotFound", err)
	}
}

func TestConsultationRepositories(t *testing.T) {
	e := require(t)
	c := ctx(t)

	types := consultinfra.NewTypeRepository(e.db)
	slots := consultinfra.NewSlotRepository(e.db)
	bookings := consultinfra.NewBookingRepository(e.db)

	typeID := e.seedConsultations(t)
	typeObjectID, err := bson.ObjectIDFromHex(typeID)
	if err != nil {
		t.Fatalf("consultation type id %q is unreadable: %v", typeID, err)
	}

	stored, err := types.List(c, true)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(stored) != 5 {
		t.Errorf("List() returned %d types, want the prototype's 5", len(stored))
	}

	date, slotTime := e.bookableSlot(t, typeID)

	if offered, err := slots.Exists(c, date, slotTime); err != nil || !offered {
		t.Errorf("Exists(%s %s) = (%v, %v), want (true, nil)", date, slotTime, offered, err)
	}

	booking, err := e.book(c, typeID, date, slotTime, "ada@denisco.com")
	if err != nil {
		t.Fatalf("Book() returned error: %v", err)
	}

	taken, err := bookings.TakenSlots(c, date, date)
	if err != nil {
		t.Fatalf("TakenSlots() returned error: %v", err)
	}
	if !taken[date+"|"+slotTime] {
		t.Errorf("TakenSlots() = %v, want the booked slot to appear", taken)
	}

	// The partial unique index is what forbids a second live booking at the
	// same date and time.
	if _, err := e.book(c, typeID, date, slotTime, "bola@denisco.com"); !errors.Is(err, consultdomain.ErrSlotUnavailable) {
		t.Errorf("Book() for a taken slot error = %v, want ErrSlotUnavailable", err)
	}

	if count, err := bookings.CountForType(c, typeObjectID); err != nil || count != 1 {
		t.Errorf("CountForType() = (%d, %v), want (1, nil)", count, err)
	}

	bookingID, err := bson.ObjectIDFromHex(booking.ID)
	if err != nil {
		t.Fatalf("booking id %q is unreadable: %v", booking.ID, err)
	}

	// Cancelling returns the time to the calendar.
	if applied, err := bookings.UpdateStatus(c, bookingID, consultdomain.StatusCancelled, true); err != nil || !applied {
		t.Errorf("UpdateStatus(cancelled) = (%v, %v), want (true, nil)", applied, err)
	}

	if _, err := e.book(c, typeID, date, slotTime, "bola@denisco.com"); err != nil {
		t.Errorf("Book() after a cancellation returned error: %v, want the slot to be free again", err)
	}

	// Availability for dates that have gone is removed by the worker.
	if _, err := slots.Add(c, []string{"2020-01-01"}, []string{"09:00 AM"}); err != nil {
		t.Fatalf("Add() returned error: %v", err)
	}

	purged, err := slots.PurgeBefore(c, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		t.Fatalf("PurgeBefore() returned error: %v", err)
	}
	if purged != 1 {
		t.Errorf("PurgeBefore() removed %d slots, want 1", purged)
	}

	times, err := slots.DistinctTimes(c)
	if err != nil {
		t.Fatalf("DistinctTimes() returned error: %v", err)
	}
	if len(times) != 4 {
		t.Errorf("DistinctTimes() = %v, want the prototype's four times", times)
	}
}

func TestOutboxRoundTrip(t *testing.T) {
	e := require(t)
	c := ctx(t)

	writer := outbox.NewMongoWriter(e.db)
	aggregate := bson.NewObjectID().Hex()

	if err := writer.Append(c, outbox.TypeUserRegistered, aggregate, map[string]any{
		"email": "ada@denisco.com",
		"name":  "Ada Obi",
	}); err != nil {
		t.Fatalf("Append() returned error: %v", err)
	}

	// stuckAfter is how long a claimed event may stay in flight before
	// another publisher assumes the first one died.
	const stuckAfter = 5 * time.Minute

	claimed, err := writer.Claim(c, 10, stuckAfter)
	if err != nil {
		t.Fatalf("Claim() returned error: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Type != outbox.TypeUserRegistered {
		t.Fatalf("Claim() returned %d events, want the appended one", len(claimed))
	}

	// A claimed event is in flight, so a second publisher running beside the
	// first sees nothing.
	if again, err := writer.Claim(c, 10, stuckAfter); err != nil || len(again) != 0 {
		t.Errorf("Claim() while in flight returned %d events, want none", len(again))
	}

	// Once the claim is older than the cut-off, the event is assumed
	// abandoned and is picked up again: that is how a publisher crash
	// recovers rather than losing the event.
	if reclaimed, err := writer.Claim(c, 10, 0); err != nil || len(reclaimed) != 1 {
		t.Errorf("Claim() of a stale in-flight event returned %d events, want 1", len(reclaimed))
	}

	if err := writer.MarkFailed(c, claimed[0].ID, "provider unreachable"); err != nil {
		t.Fatalf("MarkFailed() returned error: %v", err)
	}

	// A failed attempt returns the event to the queue to be retried.
	retried, err := writer.Claim(c, 10, stuckAfter)
	if err != nil {
		t.Fatalf("Claim() returned error: %v", err)
	}
	if len(retried) != 1 {
		t.Fatalf("Claim() after a failure returned %d events, want 1", len(retried))
	}
	if retried[0].Attempts == 0 {
		t.Error("the retried event records no attempts, want the failure counted")
	}

	if err := writer.MarkPublished(c, retried[0].ID); err != nil {
		t.Fatalf("MarkPublished() returned error: %v", err)
	}

	if settled, err := writer.Claim(c, 10, stuckAfter); err != nil || len(settled) != 0 {
		t.Errorf("Claim() after publication returned %d events, want none", len(settled))
	}
}

func TestAuditTrail(t *testing.T) {
	e := require(t)
	c := ctx(t)

	recorder := e.container.Audit
	actorID := bson.NewObjectID()

	recorder.Record(c, audit.Entry{
		ActorID:    &actorID,
		ActorRole:  usersdomain.RoleAdmin,
		Action:     audit.ActionInventoryAdjusted,
		Resource:   "inventory",
		ResourceID: bson.NewObjectID().Hex(),
		Metadata:   map[string]any{"delta": 5, "reason": "Stocktake"},
	})

	entries, total, err := recorder.List(c, 1, 20)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("List() = %d of %d, want 1 of 1", len(entries), total)
	}
	if entries[0].Action != audit.ActionInventoryAdjusted || entries[0].CreatedAt.Before(recently()) {
		t.Errorf("entry = %+v, want a recent inventory adjustment", entries[0])
	}
}

// TestRemovingAnOrderedProductArchivesIt covers the other half of the delete
// rule: a product a customer has actually bought keeps its row, because the
// order lines that name it — and the customer's own order history — have to go
// on resolving.
func TestRemovingAnOrderedProductArchivesIt(t *testing.T) {
	e := require(t)
	e.seedCategories(t)

	repo := productsinfra.NewMongoRepository(e.db)
	c := ctx(t)

	goat := e.newProduct(t, "Goat", "livestock", "head", 6_500_000, 4)
	buyer := e.newCustomer(t, uniqueEmail("archive"))
	e.fillCart(t, buyer, goat, 1)
	e.mustCheckout(t, buyer)

	result, err := e.container.ProductService.Remove(c, goat.ID.Hex(), adminActor)
	if err != nil {
		t.Fatalf("Remove() returned error: %v", err)
	}
	if result.Deleted {
		t.Error("Remove() deleted a product that has an order; want it archived")
	}

	archived, err := repo.FindByID(c, goat.ID)
	if err != nil {
		t.Fatalf("FindByID() after archiving returned error: %v", err)
	}
	if archived.Status != productsdomain.StatusArchived {
		t.Errorf("status = %q, want %q", archived.Status, productsdomain.StatusArchived)
	}
}

// TestGuestReadsOwnBookingByReference covers the capability rule the guest
// confirmation page depends on: no account, so the reference presented with
// the identifier is what proves the booking is theirs — and neither half works
// on its own.
func TestGuestReadsOwnBookingByReference(t *testing.T) {
	e := require(t)
	c := ctx(t)

	typeID := e.seedConsultations(t)
	date, slotTime := e.bookableSlot(t, typeID)

	booking, err := e.book(c, typeID, date, slotTime, uniqueEmail("guest"))
	if err != nil {
		t.Fatalf("Book() returned error: %v", err)
	}

	guest := consultapp.Viewer{}

	if _, err := e.container.ConsultationService.GetBookingWithReference(c, booking.ID, booking.Reference, guest); err != nil {
		t.Errorf("a guest holding the reference could not read their booking: %v", err)
	}

	if _, err := e.container.ConsultationService.GetBookingWithReference(c, booking.ID, "", guest); !errors.Is(err, consultdomain.ErrBookingNotFound) {
		t.Errorf("read without a reference error = %v, want ErrBookingNotFound", err)
	}

	if _, err := e.container.ConsultationService.GetBookingWithReference(c, booking.ID, "CB-00000", guest); !errors.Is(err, consultdomain.ErrBookingNotFound) {
		t.Errorf("read with the wrong reference error = %v, want ErrBookingNotFound", err)
	}

	// A booking that does belong to an account is not reachable by reference:
	// ownership still decides.
	owner := e.newCustomer(t, uniqueEmail("owner"))
	ownerID := owner.ID
	second, err := e.container.ConsultationService.Book(c, consultapp.BookInput{
		TypeID: typeID,
		Date:   date,
		Time:   e.anotherBookableTime(t, typeID, date, slotTime),
		Name:   "Ada Obi",
		Email:  owner.Email,
		Phone:  "+2348012345678",
		UserID: &ownerID,
	})
	if err != nil {
		t.Fatalf("Book() for a signed-in customer returned error: %v", err)
	}

	if _, err := e.container.ConsultationService.GetBookingWithReference(c, second.ID, second.Reference, guest); !errors.Is(err, consultdomain.ErrBookingNotFound) {
		t.Errorf("a reference opened a booking that belongs to an account: %v", err)
	}
}
