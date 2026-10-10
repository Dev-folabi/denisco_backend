// Package application coordinates stock workflows: the reservation lifecycle
// used by checkout and payment, and the manual adjustments admins make.
package application

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	"github.com/Dev-folabi/denisco_backend/internal/modules/inventory/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/logger"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Service implements the stock workflows.
type Service struct {
	repo    domain.Repository
	auditor audit.Recorder
}

// NewService wires the service to its repository.
func NewService(repo domain.Repository, auditor audit.Recorder) *Service {
	return &Service{repo: repo, auditor: auditor}
}

// StockLevel is the read model other modules and the storefront consume.
type StockLevel struct {
	ProductID bson.ObjectID
	Available int
	Reserved  int
	Sellable  int
	Badge     string
	UpdatedAt time.Time
}

// newStockLevel projects a record into the read model.
func newStockLevel(record domain.Inventory) StockLevel {
	sellable := record.Sellable()
	return StockLevel{
		ProductID: record.ProductID,
		Available: record.AvailableQty,
		Reserved:  record.ReservedQty,
		Sellable:  sellable,
		Badge:     domain.Badge(sellable),
		UpdatedAt: record.UpdatedAt,
	}
}

// LevelsFor returns stock for a set of products. Products without a record
// are reported as zero stock rather than omitted, so callers never have to
// distinguish "no record" from "sold out".
func (s *Service) LevelsFor(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]StockLevel, error) {
	records, err := s.repo.ListByProducts(ctx, productIDs)
	if err != nil {
		return nil, apperror.From(err)
	}

	levels := make(map[bson.ObjectID]StockLevel, len(productIDs))
	for _, id := range productIDs {
		if record, ok := records[id]; ok {
			levels[id] = newStockLevel(record)
			continue
		}
		levels[id] = StockLevel{ProductID: id, Badge: domain.BadgeOutOfStock}
	}

	return levels, nil
}

// LevelFor returns stock for a single product.
func (s *Service) LevelFor(ctx context.Context, productID bson.ObjectID) (StockLevel, error) {
	record, err := s.repo.Get(ctx, productID)
	if err != nil {
		return StockLevel{ProductID: productID, Badge: domain.BadgeOutOfStock}, apperror.From(err)
	}
	return newStockLevel(*record), nil
}

// EnsureRecord creates the stock record for a newly created product.
func (s *Service) EnsureRecord(ctx context.Context, productID bson.ObjectID) error {
	if err := s.repo.EnsureRecord(ctx, productID); err != nil {
		return apperror.From(err)
	}
	return nil
}

// SetStock replaces a product's on-hand quantity, which is what the admin
// product form does. It refuses to go below stock already reserved for unpaid
// orders, because those units are spoken for.
func (s *Service) SetStock(ctx context.Context, productID bson.ObjectID, quantity int, actor *Actor) error {
	if quantity < 0 {
		return apperror.Validation.WithDetails(map[string]any{"stock": "cannot be negative"})
	}

	if err := s.repo.EnsureRecord(ctx, productID); err != nil {
		return apperror.From(err)
	}

	before, err := s.repo.Get(ctx, productID)
	if err != nil {
		return apperror.From(err)
	}

	if before.AvailableQty == quantity {
		return nil
	}

	applied, err := s.repo.SetAvailable(ctx, productID, quantity)
	if err != nil {
		return apperror.From(err)
	}
	if !applied {
		return domain.ErrInvalidAdjustment.WithDetails(map[string]any{
			"reserved":  before.ReservedQty,
			"requested": quantity,
		})
	}

	s.recordMovement(ctx, productID, domain.MovementAdjustment, quantity-before.AvailableQty, "Stock set from the product form", actor, "")
	return nil
}

// Adjust applies a signed correction with a mandatory reason, and records both
// a stock movement and an audit entry.
func (s *Service) Adjust(ctx context.Context, input AdjustInput, actor *Actor) (*StockLevel, error) {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, domain.ErrReasonRequired
	}
	if input.Quantity == 0 {
		return nil, apperror.Validation.WithDetails(map[string]any{"quantity": "must not be zero"})
	}

	if err := s.repo.EnsureRecord(ctx, input.ProductID); err != nil {
		return nil, apperror.From(err)
	}

	applied, err := s.repo.Adjust(ctx, input.ProductID, input.Quantity)
	if err != nil {
		return nil, apperror.From(err)
	}
	if !applied {
		return nil, domain.ErrInvalidAdjustment
	}

	s.recordMovement(ctx, input.ProductID, domain.MovementAdjustment, input.Quantity, reason, actor, "")

	if s.auditor != nil && actor != nil {
		s.auditor.Record(ctx, audit.Entry{
			ActorID:    &actor.ID,
			ActorEmail: actor.Email,
			ActorRole:  actor.Role,
			Action:     audit.ActionInventoryAdjusted,
			Resource:   "inventory",
			ResourceID: input.ProductID.Hex(),
			Metadata:   map[string]any{"quantity": input.Quantity, "reason": reason},
		})
	}

	level, err := s.LevelFor(ctx, input.ProductID)
	if err != nil {
		return nil, err
	}
	return &level, nil
}

// Reserve promises stock to an order. It returns ErrInsufficientStock when the
// sellable quantity no longer covers the request, which is how checkout
// discovers that another customer got there first.
func (s *Service) Reserve(ctx context.Context, productID bson.ObjectID, quantity int, reference string) error {
	reserved, err := s.repo.Reserve(ctx, productID, quantity)
	if err != nil {
		return apperror.From(err)
	}
	if !reserved {
		return domain.ErrInsufficientStock
	}

	s.recordMovement(ctx, productID, domain.MovementReservation, quantity, "Reserved for an order", nil, reference)
	return nil
}

// Release returns reserved stock to the sellable pool.
func (s *Service) Release(ctx context.Context, productID bson.ObjectID, quantity int, reference, reason string) error {
	released, err := s.repo.Release(ctx, productID, quantity)
	if err != nil {
		return apperror.From(err)
	}
	if !released {
		// The reservation is already gone — expired, or released by a
		// concurrent cancellation. Nothing is owed, so this is not an error.
		logger.FromContext(ctx).Warn("stock release found no matching reservation",
			slog.String("module", "inventory"),
			slog.String("product_id", productID.Hex()),
			slog.String("reference", reference),
		)
		return nil
	}

	s.recordMovement(ctx, productID, domain.MovementRelease, -quantity, reason, nil, reference)
	return nil
}

// Deduct removes paid-for stock from the reserved and on-hand quantities.
func (s *Service) Deduct(ctx context.Context, productID bson.ObjectID, quantity int, reference string) error {
	deducted, err := s.repo.Deduct(ctx, productID, quantity)
	if err != nil {
		return apperror.From(err)
	}
	if !deducted {
		// Reaching here means the reservation was released before payment
		// landed. The payment still stands, so the order is flagged rather
		// than failed, and the shortfall is reconciled manually.
		logger.FromContext(ctx).Error("paid stock could not be deducted; reservation was missing",
			slog.String("module", "inventory"),
			slog.String("product_id", productID.Hex()),
			slog.String("reference", reference),
			slog.Int("quantity", quantity),
		)
		return domain.ErrInsufficientStock
	}

	s.recordMovement(ctx, productID, domain.MovementDeduction, -quantity, "Deducted after payment", nil, reference)
	return nil
}

// SellableFor returns how many units of each product may still be sold. The
// order module uses it to decide whether a late payment can be honoured.
func (s *Service) SellableFor(ctx context.Context, productIDs []bson.ObjectID) (map[bson.ObjectID]int, error) {
	levels, err := s.LevelsFor(ctx, productIDs)
	if err != nil {
		return nil, err
	}

	sellable := make(map[bson.ObjectID]int, len(levels))
	for id, level := range levels {
		sellable[id] = level.Sellable
	}

	return sellable, nil
}

// Sell removes stock for an order that holds no reservation. It is the
// recovery step for a payment confirmed after its reservation expired: the
// units are taken only if they are still sellable, so the order ahead of it
// in the queue keeps its claim.
func (s *Service) Sell(ctx context.Context, productID bson.ObjectID, quantity int, reference, reason string) error {
	sold, err := s.repo.SellWithoutReservation(ctx, productID, quantity)
	if err != nil {
		return apperror.From(err)
	}
	if !sold {
		logger.FromContext(ctx).Error("stock could not be secured for a late payment",
			slog.String("module", "inventory"),
			slog.String("product_id", productID.Hex()),
			slog.String("reference", reference),
			slog.Int("quantity", quantity),
		)
		return domain.ErrInsufficientStock
	}

	s.recordMovement(ctx, productID, domain.MovementDeduction, -quantity, reason, nil, reference)
	return nil
}

// PurgeProduct forgets a deleted product's stock entirely.
//
// The catalogue calls this only for a product it is deleting outright, which
// it does only while no order refers to it.
func (s *Service) PurgeProduct(ctx context.Context, productID bson.ObjectID) error {
	if err := s.repo.PurgeProduct(ctx, productID); err != nil {
		return apperror.From(err)
	}
	return nil
}

// Overview returns a page of stock records for the admin dashboard.
func (s *Service) Overview(ctx context.Context, page, limit int) ([]StockLevel, int64, error) {
	records, total, err := s.repo.List(ctx, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}

	levels := make([]StockLevel, 0, len(records))
	for _, record := range records {
		levels = append(levels, newStockLevel(record))
	}

	return levels, total, nil
}

// Movements returns a page of stock history.
func (s *Service) Movements(ctx context.Context, productID *bson.ObjectID, page, limit int) ([]domain.Movement, int64, error) {
	movements, total, err := s.repo.Movements(ctx, productID, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}
	return movements, total, nil
}

// recordMovement appends to the stock history, logging rather than failing
// when the append does not land: the quantity change itself has committed.
func (s *Service) recordMovement(ctx context.Context, productID bson.ObjectID, movementType string, quantity int, reason string, actor *Actor, reference string) {
	movement := &domain.Movement{
		ProductID: productID,
		Type:      movementType,
		Quantity:  quantity,
		Reason:    reason,
		Reference: reference,
	}
	if actor != nil {
		id := actor.ID
		movement.ActorID = &id
	}

	if err := s.repo.RecordMovement(ctx, movement); err != nil {
		logger.FromContext(ctx).Error("could not record stock movement",
			slog.String("module", "inventory"),
			slog.String("product_id", productID.Hex()),
			slog.String("type", movementType),
			slog.String("error", err.Error()),
		)
	}
}
