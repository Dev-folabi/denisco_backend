package application

import (
	"context"
	"sync"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---------------------------------------------------------------------------
// In-memory doubles. The conditional operations mirror the MongoDB
// implementations: a guarded update that reports whether it applied is what
// makes reservations, payment settlement and cancellation safe under
// concurrency, so the fakes reproduce exactly that behaviour under a mutex.
// ---------------------------------------------------------------------------

type fakeOrderRepo struct {
	mu     sync.Mutex
	orders map[bson.ObjectID]*domain.Order
}

func newFakeOrderRepo() *fakeOrderRepo {
	return &fakeOrderRepo{orders: map[bson.ObjectID]*domain.Order{}}
}

func (f *fakeOrderRepo) Create(_ context.Context, order *domain.Order) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	clone := *order
	f.orders[order.ID] = &clone
	return nil
}

func (f *fakeOrderRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	order, ok := f.orders[id]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	clone := *order
	return &clone, nil
}

func (f *fakeOrderRepo) FindByNumber(_ context.Context, number string) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, order := range f.orders {
		if order.OrderNumber == number {
			clone := *order
			return &clone, nil
		}
	}
	return nil, domain.ErrOrderNotFound
}

func (f *fakeOrderRepo) FindByPaymentRef(_ context.Context, reference string) (*domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, order := range f.orders {
		if order.PaymentRef == reference {
			clone := *order
			return &clone, nil
		}
	}
	return nil, domain.ErrOrderNotFound
}

func (f *fakeOrderRepo) ListForUser(_ context.Context, userID bson.ObjectID, _, _ int) ([]domain.Order, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	orders := make([]domain.Order, 0, len(f.orders))
	for _, order := range f.orders {
		if order.UserID == userID {
			orders = append(orders, *order)
		}
	}
	return orders, int64(len(orders)), nil
}

func (f *fakeOrderRepo) List(_ context.Context, _ domain.Filter, _, _ int) ([]domain.Order, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	orders := make([]domain.Order, 0, len(f.orders))
	for _, order := range f.orders {
		orders = append(orders, *order)
	}
	return orders, int64(len(orders)), nil
}

func (f *fakeOrderRepo) Update(_ context.Context, id bson.ObjectID, fields bson.M) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	order, ok := f.orders[id]
	if !ok {
		return domain.ErrOrderNotFound
	}

	for key, value := range fields {
		switch key {
		case "status":
			order.Status = value.(string)
		case "payment_status":
			order.PaymentStatus = value.(string)
		case "payment_method":
			order.PaymentMethod = value.(string)
		case "cancelled_at":
			if at, ok := value.(time.Time); ok {
				order.CancelledAt = &at
			} else {
				order.CancelledAt = nil
			}
		case "needs_reconciliation":
			order.NeedsReconciliation = value.(bool)
		case "reconciliation_reason":
			order.ReconciliationReason = value.(string)
		}
	}

	return nil
}

func (f *fakeOrderRepo) MarkPaid(_ context.Context, id bson.ObjectID, reference string, paidAt time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	order, ok := f.orders[id]
	if !ok || (order.PaymentStatus != domain.PaymentPending && order.PaymentStatus != domain.PaymentFailed) {
		return false, nil
	}

	order.PaymentStatus = domain.PaymentPaid
	order.PaymentRef = reference
	order.PaidAt = &paidAt
	return true, nil
}

func (f *fakeOrderRepo) ReleaseReservation(_ context.Context, id bson.ObjectID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	order, ok := f.orders[id]
	if !ok || !order.StockReserved {
		return false, nil
	}

	order.StockReserved = false
	return true, nil
}

func (f *fakeOrderRepo) CountForProduct(_ context.Context, productID bson.ObjectID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var count int64
	for _, order := range f.orders {
		for _, item := range order.Items {
			if item.ProductID == productID {
				count++
				break
			}
		}
	}
	return count, nil
}

func (f *fakeOrderRepo) NumberExists(_ context.Context, number string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, order := range f.orders {
		if order.OrderNumber == number {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeOrderRepo) Stats(_ context.Context) (domain.Stats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var stats domain.Stats
	for _, order := range f.orders {
		stats.TotalOrders++
		if order.Status == domain.StatusPending {
			stats.PendingOrders++
		}
		if order.PaymentStatus == domain.PaymentPaid {
			stats.PaidOrders++
			stats.Revenue += order.Total
		}
	}
	return stats, nil
}

// fakeCatalog serves fixed products.
type fakeCatalog struct {
	mu       sync.Mutex
	products map[bson.ObjectID]CatalogProduct
}

func (f *fakeCatalog) ProductsByIDs(_ context.Context, ids []bson.ObjectID) (map[bson.ObjectID]CatalogProduct, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	found := make(map[bson.ObjectID]CatalogProduct, len(ids))
	for _, id := range ids {
		if product, ok := f.products[id]; ok {
			found[id] = product
		}
	}
	return found, nil
}

// setPrice simulates a catalogue price change while a cart sits open.
func (f *fakeCatalog) setPrice(id bson.ObjectID, price int64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	product := f.products[id]
	product.Price = price
	f.products[id] = product
}

// setActive archives or restores a product.
func (f *fakeCatalog) setActive(id bson.ObjectID, active bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	product := f.products[id]
	product.Active = active
	f.products[id] = product
}

// fakeCart holds one cart per customer.
type fakeCart struct {
	mu    sync.Mutex
	lines map[bson.ObjectID][]CartLine
}

func newFakeCart() *fakeCart {
	return &fakeCart{lines: map[bson.ObjectID][]CartLine{}}
}

func (f *fakeCart) Lines(_ context.Context, userID bson.ObjectID) ([]CartLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]CartLine(nil), f.lines[userID]...), nil
}

func (f *fakeCart) Clear(_ context.Context, userID bson.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.lines, userID)
	return nil
}

func (f *fakeCart) set(userID bson.ObjectID, lines ...CartLine) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lines[userID] = lines
}

// fakeStock reproduces the conditional reserve/release/deduct operations.
type fakeStock struct {
	mu        sync.Mutex
	available map[bson.ObjectID]int
	reserved  map[bson.ObjectID]int
}

func newFakeStock() *fakeStock {
	return &fakeStock{available: map[bson.ObjectID]int{}, reserved: map[bson.ObjectID]int{}}
}

func (f *fakeStock) Reserve(_ context.Context, productID bson.ObjectID, quantity int, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.available[productID]-f.reserved[productID] < quantity {
		return errInsufficientStock
	}
	f.reserved[productID] += quantity
	return nil
}

func (f *fakeStock) Release(_ context.Context, productID bson.ObjectID, quantity int, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.reserved[productID] < quantity {
		return nil
	}
	f.reserved[productID] -= quantity
	return nil
}

func (f *fakeStock) Deduct(_ context.Context, productID bson.ObjectID, quantity int, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.reserved[productID] < quantity || f.available[productID] < quantity {
		return errInsufficientStock
	}
	f.reserved[productID] -= quantity
	f.available[productID] -= quantity
	return nil
}

// SellableFor reports what may still be sold, as the real ledger does.
func (f *fakeStock) SellableFor(_ context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	levels := make(map[bson.ObjectID]int, len(productIDs))
	for _, id := range productIDs {
		levels[id] = f.available[id] - f.reserved[id]
	}
	return levels, nil
}

// Sell takes stock that was never reserved, for a late payment.
func (f *fakeStock) Sell(_ context.Context, productID bson.ObjectID, quantity int, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.available[productID]-f.reserved[productID] < quantity {
		return errInsufficientStock
	}
	f.available[productID] -= quantity
	return nil
}

func (f *fakeStock) sellable(productID bson.ObjectID) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.available[productID] - f.reserved[productID]
}

func (f *fakeStock) snapshot(productID bson.ObjectID) (available, reserved int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.available[productID], f.reserved[productID]
}

// fakeTx runs the unit of work without a real transaction.
//
// It cannot roll back, so tests that rely on atomicity assert on the
// compensating behaviour the service performs rather than on the rollback.
type fakeTx struct{}

func (fakeTx) Run(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// fakeOutbox records emitted events.
type fakeOutbox struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeOutbox) Append(_ context.Context, eventType, _ string, _ map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.events = append(f.events, eventType)
	return nil
}

func (f *fakeOutbox) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.events...)
}

// Recent returns the newest orders, as the dashboard reads them.
func (f *fakeOrderRepo) Recent(_ context.Context, limit int) ([]domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	orders := make([]domain.Order, 0, len(f.orders))
	for _, order := range f.orders {
		orders = append(orders, *order)
	}
	if len(orders) > limit {
		orders = orders[:limit]
	}
	return orders, nil
}

// DailyRevenue sums paid totals per day, as the sales chart reads them.
func (f *fakeOrderRepo) DailyRevenue(_ context.Context, since time.Time) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	revenue := map[string]int64{}
	for _, order := range f.orders {
		if order.PaymentStatus != domain.PaymentPaid || order.CreatedAt.Before(since) {
			continue
		}
		revenue[order.CreatedAt.Format("2006-01-02")] += order.Total
	}
	return revenue, nil
}

// StaleReserved returns unpaid orders still holding stock past the cut-off.
func (f *fakeOrderRepo) StaleReserved(_ context.Context, before time.Time, limit int) ([]domain.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	stale := make([]domain.Order, 0, limit)
	for _, order := range f.orders {
		if order.StockReserved &&
			order.PaymentStatus == domain.PaymentPending &&
			order.Status != domain.StatusCancelled &&
			order.CreatedAt.Before(before) {
			stale = append(stale, *order)
		}
		if len(stale) == limit {
			break
		}
	}
	return stale, nil
}
