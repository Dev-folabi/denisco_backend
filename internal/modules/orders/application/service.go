package application

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	"github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Outbox event types emitted by the order lifecycle.
const (
	EventOrderCreated       = "order.created"
	EventOrderPaid          = "order.paid"
	EventOrderStatusChanged = "order.status_changed"
	EventOrderCancelled     = "order.cancelled"
	// EventPaymentUnreconciled alerts the farm that money was taken for an
	// order that can no longer be filled, so a refund decision is needed. It
	// goes to staff, not to the customer.
	EventPaymentUnreconciled = "order.payment_unreconciled"
)

// Service implements the checkout and order-lifecycle workflows.
type Service struct {
	orders  domain.Repository
	carts   CartPort
	catalog CatalogPort
	stock   StockPort
	tx      TxRunner
	events  outbox.Writer
	auditor audit.Recorder
}

// NewService wires the order module to its repository and ports.
func NewService(
	orders domain.Repository,
	carts CartPort,
	catalog CatalogPort,
	stock StockPort,
	tx TxRunner,
	events outbox.Writer,
	auditor audit.Recorder,
) *Service {
	return &Service{orders: orders, carts: carts, catalog: catalog, stock: stock, tx: tx, events: events, auditor: auditor}
}

// Checkout turns the customer's cart into an order and reserves the stock.
//
// Prices come from the catalogue, never from the client: a cart holds product
// references only, so a price change between adding to cart and checking out
// is always applied. Everything runs in one transaction, so a failure part way
// through cannot leave stock reserved against an order that was never created.
func (s *Service) Checkout(ctx context.Context, userID bson.ObjectID, input CheckoutInput) (*View, error) {
	if err := domain.ValidateDeliveryMethod(input.DeliveryMethod); err != nil {
		return nil, err
	}

	address, err := domain.ValidateAddress(input.DeliveryMethod, input.Address)
	if err != nil {
		return nil, err
	}

	customer, err := validateCustomer(input)
	if err != nil {
		return nil, err
	}

	lines, err := s.carts.Lines(ctx, userID)
	if err != nil {
		return nil, apperror.From(err)
	}
	if len(lines) == 0 {
		return nil, ErrCartEmpty
	}

	productIDs := make([]bson.ObjectID, 0, len(lines))
	for _, line := range lines {
		productIDs = append(productIDs, line.ProductID)
	}

	products, err := s.catalog.ProductsByIDs(ctx, productIDs)
	if err != nil {
		return nil, apperror.From(err)
	}

	// Build the lines and totals from the catalogue's current prices.
	items := make([]domain.Item, 0, len(lines))
	var subtotal int64

	for _, line := range lines {
		product, ok := products[line.ProductID]
		if !ok || !product.Active {
			return nil, ErrItemUnavailable.WithDetails(map[string]any{
				"product_id": line.ProductID.Hex(),
			})
		}

		lineTotal := product.Price * int64(line.Quantity)
		items = append(items, domain.Item{
			ProductID: product.ID,
			Name:      product.Name,
			Slug:      product.Slug,
			Image:     product.Image,
			Unit:      product.Unit,
			UnitPrice: product.Price,
			Quantity:  line.Quantity,
			LineTotal: lineTotal,
		})
		subtotal += lineTotal
	}

	deliveryFee := domain.DeliveryFeeFor(input.DeliveryMethod)

	orderNumber, err := newOrderNumber(ctx, s.orders.NumberExists)
	if err != nil {
		return nil, apperror.Internal.Wrap(err)
	}

	now := time.Now().UTC()
	order := &domain.Order{
		ID:             bson.NewObjectID(),
		OrderNumber:    orderNumber,
		UserID:         userID,
		Customer:       customer,
		Items:          items,
		Subtotal:       subtotal,
		DeliveryFee:    deliveryFee,
		Total:          subtotal + deliveryFee,
		Currency:       domain.Currency,
		DeliveryMethod: input.DeliveryMethod,
		Address:        address,
		Status:         domain.StatusPending,
		PaymentStatus:  domain.PaymentPending,
		StockReserved:  true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	err = s.tx.Run(ctx, func(txCtx context.Context) error {
		// Reserving inside the transaction means a failure on the third line
		// rolls back the first two; no compensation logic is needed.
		for _, item := range order.Items {
			if err := s.stock.Reserve(txCtx, item.ProductID, item.Quantity, order.OrderNumber); err != nil {
				return err
			}
		}

		if err := s.orders.Create(txCtx, order); err != nil {
			return err
		}

		return s.carts.Clear(txCtx, userID)
	})
	if err != nil {
		return nil, apperror.From(err)
	}

	s.emit(ctx, EventOrderCreated, order)

	logger.FromContext(ctx).Info("order created",
		slog.String("module", "orders"),
		slog.String("order_id", order.ID.Hex()),
		slog.String("order_number", order.OrderNumber),
		slog.Int64("total", order.Total),
	)

	view := newView(order)
	return &view, nil
}

// Get returns one order, enforcing ownership: a customer may only read their
// own orders, while an administrator may read any.
func (s *Service) Get(ctx context.Context, id string, viewer Viewer) (*View, error) {
	order, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := authorize(order, viewer); err != nil {
		return nil, err
	}

	view := newView(order)
	return &view, nil
}

// GetByNumber returns one order by its customer-facing number, which is what
// the order-confirmation page has in hand.
func (s *Service) GetByNumber(ctx context.Context, number string, viewer Viewer) (*View, error) {
	order, err := s.orders.FindByNumber(ctx, number)
	if err != nil {
		return nil, apperror.From(err)
	}

	if err := authorize(order, viewer); err != nil {
		return nil, err
	}

	view := newView(order)
	return &view, nil
}

// ListForCustomer returns a customer's order history.
func (s *Service) ListForCustomer(ctx context.Context, userID bson.ObjectID, page, limit int) ([]View, int64, error) {
	orders, total, err := s.orders.ListForUser(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}

	return toViews(orders), total, nil
}

// List returns a filtered page for the admin dashboard.
func (s *Service) List(ctx context.Context, filter domain.Filter, page, limit int) ([]View, int64, error) {
	orders, total, err := s.orders.List(ctx, filter, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}

	return toViews(orders), total, nil
}

// Cancel lets a customer withdraw an unpaid order and returns its stock.
func (s *Service) Cancel(ctx context.Context, id string, viewer Viewer) (*View, error) {
	order, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := authorize(order, viewer); err != nil {
		return nil, err
	}

	if !order.IsCancellable() {
		return nil, domain.ErrNotCancellable
	}

	now := time.Now().UTC()

	err = s.tx.Run(ctx, func(txCtx context.Context) error {
		if err := s.releaseStock(txCtx, order, "Order cancelled"); err != nil {
			return err
		}

		return s.orders.Update(txCtx, order.ID, bson.M{
			"status":       domain.StatusCancelled,
			"cancelled_at": now,
		})
	})
	if err != nil {
		return nil, apperror.From(err)
	}

	s.emit(ctx, EventOrderCancelled, order)

	logger.FromContext(ctx).Info("order cancelled",
		slog.String("module", "orders"),
		slog.String("order_id", order.ID.Hex()),
	)

	return s.Get(ctx, id, viewer)
}

// UpdateStatus moves an order through fulfillment. Cancelling a reserved
// order returns its stock.
func (s *Service) UpdateStatus(ctx context.Context, id, status string, actor Actor) (*View, error) {
	if err := domain.ValidateStatus(status); err != nil {
		return nil, err
	}

	order, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}

	if !domain.CanTransition(order.Status, status) {
		return nil, domain.ErrInvalidTransition.WithDetails(map[string]any{
			"from": order.Status,
			"to":   status,
		})
	}

	fields := bson.M{"status": status}
	if status == domain.StatusCancelled {
		fields["cancelled_at"] = time.Now().UTC()
	}

	err = s.tx.Run(ctx, func(txCtx context.Context) error {
		if status == domain.StatusCancelled {
			if err := s.releaseStock(txCtx, order, "Order cancelled by an administrator"); err != nil {
				return err
			}
		}
		return s.orders.Update(txCtx, order.ID, fields)
	})
	if err != nil {
		return nil, apperror.From(err)
	}

	s.audit(ctx, actor, audit.ActionOrderStatusChanged, order.ID.Hex(), map[string]any{
		"from": order.Status,
		"to":   status,
	})

	order.Status = status
	s.emit(ctx, EventOrderStatusChanged, order)

	return s.Get(ctx, id, Viewer{IsAdmin: true})
}

// MarkPaid records a confirmed payment and converts the reservation into a
// deduction.
//
// It is safe to call repeatedly: MarkPaid only applies while the order is
// still pending, and the stock deduction is guarded by the same flag, so a
// replayed webhook changes nothing.
//
// A payment that lands after the reservation has gone — an expired checkout
// the customer finished anyway — takes the reconciliation path instead of
// quietly recreating the reservation.
func (s *Service) MarkPaid(ctx context.Context, orderID bson.ObjectID, reference, method string, paidAt time.Time) error {
	order, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return apperror.From(err)
	}

	// Set inside the transaction and read after it commits, so the event and
	// the log line describe what was actually persisted.
	var outcome string

	err = s.tx.Run(ctx, func(txCtx context.Context) error {
		outcome = ""

		applied, err := s.orders.MarkPaid(txCtx, order.ID, reference, paidAt)
		if err != nil {
			return err
		}
		if !applied {
			// Already paid: a duplicate webhook or a verify racing it.
			return nil
		}

		if method != "" {
			if err := s.orders.Update(txCtx, order.ID, bson.M{"payment_method": method}); err != nil {
				return err
			}
		}

		// Turn the reservation into a deduction, exactly once.
		released, err := s.orders.ReleaseReservation(txCtx, order.ID)
		if err != nil {
			return err
		}
		if released {
			for _, item := range order.Items {
				if err := s.stock.Deduct(txCtx, item.ProductID, item.Quantity, order.OrderNumber); err != nil {
					return err
				}
			}
			return nil
		}

		outcome, err = s.reconcileLatePayment(txCtx, order)
		return err
	})
	if err != nil {
		return apperror.From(err)
	}

	log := logger.FromContext(ctx)

	if outcome == domain.ReconciliationRefundRequired {
		// The money is ours but the goods are not: nothing is promised to the
		// customer until a human decides between restocking and refunding.
		s.emit(ctx, EventPaymentUnreconciled, order)
		s.recordLatePayment(ctx, order, outcome)

		log.Error("payment landed after the reservation expired and could not be filled",
			slog.String("module", "orders"),
			slog.String("order_id", order.ID.Hex()),
			slog.String("order_number", order.OrderNumber),
			slog.String("payment_reference", reference),
		)

		return nil
	}

	order.PaymentStatus = domain.PaymentPaid
	order.PaymentRef = reference
	s.emit(ctx, EventOrderPaid, order)

	if outcome == domain.ReconciliationStockSecured {
		s.recordLatePayment(ctx, order, outcome)

		log.Warn("payment landed after the reservation expired; stock was secured again",
			slog.String("module", "orders"),
			slog.String("order_id", order.ID.Hex()),
			slog.String("order_number", order.OrderNumber),
			slog.String("payment_reference", reference),
		)
	}

	log.Info("order paid",
		slog.String("module", "orders"),
		slog.String("order_id", order.ID.Hex()),
		slog.String("order_number", order.OrderNumber),
		slog.String("payment_reference", reference),
	)

	return nil
}

// reconcileLatePayment decides what to do about a confirmed payment for an
// order that no longer holds its stock, and returns the reason it recorded.
//
// The architecture forbids silently recreating the reservation: either the
// units are still on the shelf and the order is revived, or the order is
// flagged so an administrator can refund. Both branches run inside the
// caller's transaction, and the caller only reaches here on the single call
// that flipped the order to paid, so this happens exactly once per payment.
func (s *Service) reconcileLatePayment(ctx context.Context, order *domain.Order) (string, error) {
	sellable, err := s.stock.SellableFor(ctx, order.ProductIDs())
	if err != nil {
		return "", err
	}

	for _, item := range order.Items {
		if sellable[item.ProductID] < item.Quantity {
			if err := s.orders.Update(ctx, order.ID, bson.M{
				"needs_reconciliation":  true,
				"reconciliation_reason": domain.ReconciliationRefundRequired,
			}); err != nil {
				return "", err
			}
			return domain.ReconciliationRefundRequired, nil
		}
	}

	// Every line can still be filled. Taking the stock here rather than
	// reserving it reflects what happened: the order is paid for, so the
	// units leave the shop now.
	for _, item := range order.Items {
		if err := s.stock.Sell(ctx, item.ProductID, item.Quantity, order.OrderNumber,
			"Secured after a payment that arrived late"); err != nil {
			return "", err
		}
	}

	fields := bson.M{
		"needs_reconciliation":  false,
		"reconciliation_reason": domain.ReconciliationStockSecured,
	}
	if order.Status == domain.StatusCancelled {
		// The expiry sweep had cancelled it; the paid order rejoins the queue.
		fields["status"] = domain.StatusPending
		fields["cancelled_at"] = nil
	}

	if err := s.orders.Update(ctx, order.ID, fields); err != nil {
		return "", err
	}

	return domain.ReconciliationStockSecured, nil
}

// recordLatePayment writes the reconciliation decision to the audit trail. It
// has no administrator behind it, so the entry is attributed to the system.
func (s *Service) recordLatePayment(ctx context.Context, order *domain.Order, outcome string) {
	if s.auditor == nil {
		return
	}

	s.auditor.Record(ctx, audit.Entry{
		ActorRole:  audit.ActorSystem,
		Action:     audit.ActionOrderLatePayment,
		Resource:   "order",
		ResourceID: order.ID.Hex(),
		Metadata: map[string]any{
			"order_number": order.OrderNumber,
			"outcome":      outcome,
			"total":        order.Total,
		},
	})
}

// MarkPaymentFailed records a failed or abandoned payment attempt and returns
// the reserved stock to the shop.
func (s *Service) MarkPaymentFailed(ctx context.Context, orderID bson.ObjectID, status string) error {
	order, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return apperror.From(err)
	}

	if order.IsPaid() {
		// A later successful attempt already settled this order.
		return nil
	}

	return s.tx.Run(ctx, func(txCtx context.Context) error {
		if err := s.releaseStock(txCtx, order, "Payment "+status); err != nil {
			return err
		}
		return s.orders.Update(txCtx, order.ID, bson.M{"payment_status": domain.PaymentFailed})
	})
}

// MarkRefunded records a refund against an order.
func (s *Service) MarkRefunded(ctx context.Context, orderID bson.ObjectID) error {
	if err := s.orders.Update(ctx, orderID, bson.M{"payment_status": domain.PaymentRefunded}); err != nil {
		return apperror.From(err)
	}
	return nil
}

// Stats returns the order figures for the admin dashboard.
func (s *Service) Stats(ctx context.Context) (domain.Stats, error) {
	stats, err := s.orders.Stats(ctx)
	if err != nil {
		return stats, apperror.From(err)
	}
	return stats, nil
}

// OrderForPayment loads an order that a payment attempt is about to be started
// for, checking ownership and that it is still awaiting payment.
func (s *Service) OrderForPayment(ctx context.Context, id string, viewer Viewer) (*domain.Order, error) {
	order, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := authorize(order, viewer); err != nil {
		return nil, err
	}

	if !order.IsPayable() {
		return nil, domain.ErrNotPayable
	}

	return order, nil
}

// load fetches an order by its hex identifier.
func (s *Service) load(ctx context.Context, id string) (*domain.Order, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, domain.ErrOrderNotFound
	}

	order, err := s.orders.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	return order, nil
}

// releaseStock returns an order's reservation, exactly once.
func (s *Service) releaseStock(ctx context.Context, order *domain.Order, reason string) error {
	released, err := s.orders.ReleaseReservation(ctx, order.ID)
	if err != nil {
		return err
	}
	if !released {
		return nil
	}

	for _, item := range order.Items {
		if err := s.stock.Release(ctx, item.ProductID, item.Quantity, order.OrderNumber, reason); err != nil {
			return err
		}
	}

	return nil
}

// emit records a domain event for the worker to act on.
func (s *Service) emit(ctx context.Context, eventType string, order *domain.Order) {
	if s.events == nil {
		return
	}

	payload := map[string]any{
		"order_id":     order.ID.Hex(),
		"order_number": order.OrderNumber,
		"user_id":      order.UserID.Hex(),
		"email":        order.Customer.Email,
		"name":         order.Customer.Name,
		"total":        order.Total,
		"status":       order.Status,
	}

	if err := s.events.Append(ctx, eventType, order.ID.Hex(), payload); err != nil {
		logger.FromContext(ctx).Error("could not append order event",
			slog.String("event_type", eventType),
			slog.String("order_id", order.ID.Hex()),
			slog.String("error", err.Error()),
		)
	}
}

// HasOrdersFor reports whether any order contains a product.
//
// The catalogue asks before deleting a product: one that has been bought is
// archived instead, because its order lines still name it.
func (s *Service) HasOrdersFor(ctx context.Context, productID bson.ObjectID) (bool, error) {
	count, err := s.orders.CountForProduct(ctx, productID)
	if err != nil {
		return false, apperror.From(err)
	}
	return count > 0, nil
}

// audit records an administrative order change.
func (s *Service) audit(ctx context.Context, actor Actor, action, resourceID string, metadata map[string]any) {
	if s.auditor == nil || actor.ID.IsZero() {
		return
	}

	id := actor.ID
	s.auditor.Record(ctx, audit.Entry{
		ActorID:    &id,
		ActorEmail: actor.Email,
		ActorRole:  actor.Role,
		Action:     action,
		Resource:   "order",
		ResourceID: resourceID,
		Metadata:   metadata,
	})
}

// Viewer is the caller asking to see or change an order.
type Viewer struct {
	UserID  bson.ObjectID
	IsAdmin bool
}

// NewViewer builds a viewer from an authenticated principal.
func NewViewer(userID bson.ObjectID, role string) Viewer {
	return Viewer{
		UserID:  userID,
		IsAdmin: role == usersdomain.RoleAdmin || role == usersdomain.RoleSuperAdmin,
	}
}

// authorize enforces ownership. A customer asking for someone else's order
// gets "not found" rather than "forbidden", so order identifiers cannot be
// probed for existence.
func authorize(order *domain.Order, viewer Viewer) error {
	if viewer.IsAdmin || order.UserID == viewer.UserID {
		return nil
	}
	return domain.ErrOrderNotFound
}

// toViews projects a slice of orders.
func toViews(orders []domain.Order) []View {
	views := make([]View, 0, len(orders))
	for i := range orders {
		views = append(views, newView(&orders[i]))
	}
	return views
}

// validateCustomer checks the contact details captured at checkout.
func validateCustomer(input CheckoutInput) (domain.Customer, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Customer{}, apperror.Validation.WithDetails(map[string]any{"name": "is required"})
	}
	if len(name) > 120 {
		return domain.Customer{}, apperror.Validation.WithDetails(map[string]any{"name": "must be 120 characters or fewer"})
	}

	email, err := usersdomain.ValidateEmail(input.Email)
	if err != nil {
		return domain.Customer{}, err
	}

	phone, err := usersdomain.ValidatePhone(input.Phone)
	if err != nil {
		return domain.Customer{}, err
	}

	return domain.Customer{Name: name, Email: email, Phone: phone}, nil
}

// Errors raised by checkout against the cart and catalogue. They repeat the
// cart module's codes so clients see one vocabulary, without this module
// importing that one.
var (
	ErrCartEmpty       = apperror.New(http.StatusUnprocessableEntity, "CART_EMPTY", "Your cart is empty.")
	ErrItemUnavailable = apperror.New(http.StatusUnprocessableEntity, "PRODUCT_UNAVAILABLE", "One of the products in your cart is no longer available.")
)

// expiryBatch bounds how many stale orders one sweep handles, so a backlog is
// worked through over several runs rather than in one long transaction.
const expiryBatch = 100

// ExpireStaleReservations returns stock held by orders that were never paid
// for, and marks those orders cancelled.
//
// Without this, an abandoned checkout would hold its units indefinitely: the
// reservation is only released on cancellation or a failed payment, and a
// customer who simply closes the tab does neither.
func (s *Service) ExpireStaleReservations(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-olderThan)

	stale, err := s.orders.StaleReserved(ctx, cutoff, expiryBatch)
	if err != nil {
		return 0, apperror.From(err)
	}

	released := 0
	for i := range stale {
		order := &stale[i]

		err := s.tx.Run(ctx, func(txCtx context.Context) error {
			if err := s.releaseStock(txCtx, order, "Reservation expired before payment"); err != nil {
				return err
			}
			return s.orders.Update(txCtx, order.ID, bson.M{
				"status":       domain.StatusCancelled,
				"cancelled_at": time.Now().UTC(),
			})
		})
		if err != nil {
			// One stuck order must not stop the sweep; the next run retries it.
			logger.FromContext(ctx).Error("could not expire an order's reservation",
				slog.String("module", "orders"),
				slog.String("order_id", order.ID.Hex()),
				slog.String("error", err.Error()),
			)
			continue
		}

		order.Status = domain.StatusCancelled
		s.emit(ctx, EventOrderCancelled, order)
		released++
	}

	return released, nil
}
