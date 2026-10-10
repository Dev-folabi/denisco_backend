// Package infrastructure holds the MongoDB implementations of the
// consultation repositories.
package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/modules/consultations/domain"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TypeRepository persists consultation services in `consultation_types`.
type TypeRepository struct {
	collection *mongo.Collection
}

// NewTypeRepository wires the repository to a database handle.
func NewTypeRepository(db *mongo.Database) *TypeRepository {
	return &TypeRepository{collection: db.Collection(database.CollConsultationTypes)}
}

// List returns types ordered by their display position.
func (r *TypeRepository) List(ctx context.Context, activeOnly bool) ([]domain.Type, error) {
	filter := bson.M{}
	if activeOnly {
		filter["active"] = true
	}

	cursor, err := r.collection.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "position", Value: 1}, {Key: "created_at", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("consultations: list types: %w", err)
	}
	defer cursor.Close(ctx)

	types := make([]domain.Type, 0, 8)
	if err := cursor.All(ctx, &types); err != nil {
		return nil, fmt.Errorf("consultations: decode types: %w", err)
	}

	return types, nil
}

// FindByID loads a type by identifier.
func (r *TypeRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Type, error) {
	var found domain.Type
	if err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&found); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrTypeNotFound
		}
		return nil, fmt.Errorf("consultations: find type: %w", err)
	}
	return &found, nil
}

// Create inserts a consultation type.
func (r *TypeRepository) Create(ctx context.Context, consultationType *domain.Type) error {
	if consultationType.ID.IsZero() {
		consultationType.ID = bson.NewObjectID()
	}
	if _, err := r.collection.InsertOne(ctx, consultationType); err != nil {
		return fmt.Errorf("consultations: insert type: %w", err)
	}
	return nil
}

// Update applies changed fields and stamps updated_at.
func (r *TypeRepository) Update(ctx context.Context, id bson.ObjectID, fields bson.M) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()

	result, err := r.collection.UpdateByID(ctx, id, bson.M{"$set": fields})
	if err != nil {
		return fmt.Errorf("consultations: update type: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrTypeNotFound
	}
	return nil
}

// Delete removes a type.
func (r *TypeRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("consultations: delete type: %w", err)
	}
	if result.DeletedCount == 0 {
		return domain.ErrTypeNotFound
	}
	return nil
}

// Upsert creates or refreshes a seeded type by its prototype code, leaving
// created_at intact so re-running the seed is harmless.
func (r *TypeRepository) Upsert(ctx context.Context, consultationType *domain.Type) error {
	now := time.Now().UTC()

	result, err := r.collection.UpdateOne(ctx,
		bson.M{"code": consultationType.Code},
		bson.M{
			"$set": bson.M{
				"name":             consultationType.Name,
				"description":      consultationType.Description,
				"duration_minutes": consultationType.DurationMinutes,
				"price":            consultationType.Price,
				"currency":         domain.Currency,
				"position":         consultationType.Position,
				"active":           consultationType.Active,
				"updated_at":       now,
			},
			"$setOnInsert": bson.M{"code": consultationType.Code, "created_at": now},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("consultations: upsert type: %w", err)
	}

	if id, ok := result.UpsertedID.(bson.ObjectID); ok {
		consultationType.ID = id
	}
	return nil
}

// SlotRepository persists the availability grid in `consultation_slots`.
type SlotRepository struct {
	collection *mongo.Collection
}

// NewSlotRepository wires the repository to a database handle.
func NewSlotRepository(db *mongo.Database) *SlotRepository {
	return &SlotRepository{collection: db.Collection(database.CollConsultationSlots)}
}

// List returns slots in an inclusive date range, ordered for display.
func (r *SlotRepository) List(ctx context.Context, from, to string) ([]domain.Slot, error) {
	filter := bson.M{}
	if from != "" || to != "" {
		rang := bson.M{}
		if from != "" {
			rang["$gte"] = from
		}
		if to != "" {
			rang["$lte"] = to
		}
		filter["date"] = rang
	}

	cursor, err := r.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("consultations: list slots: %w", err)
	}
	defer cursor.Close(ctx)

	slots := make([]domain.Slot, 0, 64)
	if err := cursor.All(ctx, &slots); err != nil {
		return nil, fmt.Errorf("consultations: decode slots: %w", err)
	}

	// Times are 12-hour labels, so they are sorted by clock position rather
	// than alphabetically ("09:00 AM" before "01:00 PM").
	sort.SliceStable(slots, func(i, j int) bool {
		if slots[i].Date != slots[j].Date {
			return slots[i].Date < slots[j].Date
		}
		return domain.TimeToMinutes(slots[i].Time) < domain.TimeToMinutes(slots[j].Time)
	})

	return slots, nil
}

// Exists reports whether a date and time are offered.
func (r *SlotRepository) Exists(ctx context.Context, date, slotTime string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx,
		bson.M{"date": date, "time": slotTime}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("consultations: slot exists: %w", err)
	}
	return count > 0, nil
}

// Add creates every date and time combination, skipping existing ones.
//
// The unique index on (date, time) makes this idempotent: duplicates are
// rejected individually and the rest of the batch still applies.
func (r *SlotRepository) Add(ctx context.Context, dates, times []string) (int, error) {
	if len(dates) == 0 || len(times) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	documents := make([]any, 0, len(dates)*len(times))
	for _, date := range dates {
		for _, slotTime := range times {
			documents = append(documents, domain.Slot{
				ID:        bson.NewObjectID(),
				Date:      date,
				Time:      slotTime,
				CreatedAt: now,
			})
		}
	}

	result, err := r.collection.InsertMany(ctx, documents, options.InsertMany().SetOrdered(false))
	inserted := 0
	if result != nil {
		inserted = len(result.InsertedIDs)
	}
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return inserted, fmt.Errorf("consultations: add slots: %w", err)
	}

	return inserted, nil
}

// Remove deletes slots matching the given dates and times; an empty list means
// "any", so removing a date clears every time on it.
func (r *SlotRepository) Remove(ctx context.Context, dates, times []string) (int64, error) {
	filter := bson.M{}
	if len(dates) > 0 {
		filter["date"] = bson.M{"$in": dates}
	}
	if len(times) > 0 {
		filter["time"] = bson.M{"$in": times}
	}
	if len(filter) == 0 {
		return 0, nil
	}

	result, err := r.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("consultations: remove slots: %w", err)
	}
	return result.DeletedCount, nil
}

// DeleteByID removes one slot.
func (r *SlotRepository) DeleteByID(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("consultations: delete slot: %w", err)
	}
	if result.DeletedCount == 0 {
		return apperrorNotFound()
	}
	return nil
}

// DistinctTimes returns the times currently offered, in clock order.
func (r *SlotRepository) DistinctTimes(ctx context.Context) ([]string, error) {
	var decoded []string
	if err := r.collection.Distinct(ctx, "time", bson.M{}).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("consultations: distinct times: %w", err)
	}

	sort.SliceStable(decoded, func(i, j int) bool {
		return domain.TimeToMinutes(decoded[i]) < domain.TimeToMinutes(decoded[j])
	})

	return decoded, nil
}

// PurgeBefore removes slots whose date has passed, keeping the grid bounded.
func (r *SlotRepository) PurgeBefore(ctx context.Context, date string) (int64, error) {
	result, err := r.collection.DeleteMany(ctx, bson.M{"date": bson.M{"$lt": date}})
	if err != nil {
		return 0, fmt.Errorf("consultations: purge slots: %w", err)
	}
	return result.DeletedCount, nil
}

// BookingRepository persists bookings in `consultation_bookings`.
type BookingRepository struct {
	collection *mongo.Collection
}

// NewBookingRepository wires the repository to a database handle.
func NewBookingRepository(db *mongo.Database) *BookingRepository {
	return &BookingRepository{collection: db.Collection(database.CollConsultationBookings)}
}

// Create inserts a booking.
//
// A duplicate-key error means the partial unique index rejected a second live
// booking for the same slot, so it is reported as the slot being taken.
func (r *BookingRepository) Create(ctx context.Context, booking *domain.Booking) error {
	if booking.ID.IsZero() {
		booking.ID = bson.NewObjectID()
	}

	if _, err := r.collection.InsertOne(ctx, booking); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrSlotUnavailable
		}
		return fmt.Errorf("consultations: insert booking: %w", err)
	}

	return nil
}

// FindByID loads a booking by identifier.
func (r *BookingRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Booking, error) {
	var booking domain.Booking
	if err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&booking); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrBookingNotFound
		}
		return nil, fmt.Errorf("consultations: find booking: %w", err)
	}
	return &booking, nil
}

// ListForUser returns a customer's booking history, newest first.
func (r *BookingRepository) ListForUser(ctx context.Context, userID bson.ObjectID, page, limit int) ([]domain.Booking, int64, error) {
	return r.list(ctx, bson.M{"user_id": userID}, page, limit)
}

// List returns a filtered page for the admin dashboard.
func (r *BookingRepository) List(ctx context.Context, filter domain.BookingFilter, page, limit int) ([]domain.Booking, int64, error) {
	query := bson.M{}

	if filter.Status != "" {
		query["status"] = filter.Status
	}
	if filter.TypeID != nil {
		query["type_id"] = *filter.TypeID
	}
	if filter.Date != "" {
		query["date"] = filter.Date
	}
	if filter.Search != "" {
		pattern := bson.M{"$regex": regexp.QuoteMeta(filter.Search), "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"reference": pattern},
			bson.M{"name": pattern},
			bson.M{"email": pattern},
		}
	}

	return r.list(ctx, query, page, limit)
}

// list runs a paginated, newest-first query.
func (r *BookingRepository) list(ctx context.Context, query bson.M, page, limit int) ([]domain.Booking, int64, error) {
	total, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("consultations: count bookings: %w", err)
	}

	cursor, err := r.collection.Find(ctx, query,
		options.Find().
			SetSort(bson.D{{Key: "created_at", Value: -1}}).
			SetSkip(int64((page-1)*limit)).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("consultations: list bookings: %w", err)
	}
	defer cursor.Close(ctx)

	bookings := make([]domain.Booking, 0, limit)
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, 0, fmt.Errorf("consultations: decode bookings: %w", err)
	}

	return bookings, total, nil
}

// TakenSlots returns a lookup of "date|time" keys already held by a live
// booking, whichever consultation type took them.
func (r *BookingRepository) TakenSlots(ctx context.Context, from, to string) (map[string]bool, error) {
	query := bson.M{"active": true}
	if from != "" || to != "" {
		rang := bson.M{}
		if from != "" {
			rang["$gte"] = from
		}
		if to != "" {
			rang["$lte"] = to
		}
		query["date"] = rang
	}

	cursor, err := r.collection.Find(ctx, query,
		options.Find().SetProjection(bson.M{"date": 1, "time": 1}))
	if err != nil {
		return nil, fmt.Errorf("consultations: taken slots: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Date string `bson:"date"`
		Time string `bson:"time"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("consultations: decode taken slots: %w", err)
	}

	taken := make(map[string]bool, len(rows))
	for _, row := range rows {
		taken[row.Date+"|"+row.Time] = true
	}

	return taken, nil
}

// UpdateStatus changes a booking's status.
//
// When the new status frees the slot, `active` is unset, which removes the
// booking from the partial unique index and lets someone else book that time.
func (r *BookingRepository) UpdateStatus(ctx context.Context, id bson.ObjectID, status string, releaseSlot bool) (bool, error) {
	now := time.Now().UTC()

	update := bson.M{"$set": bson.M{"status": status, "updated_at": now}}
	if releaseSlot {
		update["$unset"] = bson.M{"active": ""}
		update["$set"].(bson.M)["cancelled_at"] = now
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "status": bson.M{"$ne": status}}, update)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, domain.ErrSlotUnavailable
		}
		return false, fmt.Errorf("consultations: update booking status: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// CountForType reports how many bookings reference a consultation type.
func (r *BookingRepository) CountForType(ctx context.Context, typeID bson.ObjectID) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"type_id": typeID})
	if err != nil {
		return 0, fmt.Errorf("consultations: count bookings for type: %w", err)
	}
	return count, nil
}

// MarkPaid settles the consultation fee and confirms the booking.
//
// The guard is the same shape as the order repository's: the update applies
// only while the fee is outstanding, so a webhook delivered twice settles the
// booking once. A booking the admin has already cancelled is left alone —
// the refund is then the admin's decision, not a side effect of a late
// callback.
func (r *BookingRepository) MarkPaid(ctx context.Context, id bson.ObjectID, reference string, paidAt time.Time) (bool, error) {
	result, err := r.collection.UpdateOne(ctx,
		bson.M{
			"_id":            id,
			"payment_status": bson.M{"$in": bson.A{domain.PaymentPending, domain.PaymentFailed}},
			"status":         bson.M{"$nin": bson.A{domain.StatusCancelled, domain.StatusNoShow}},
		},
		bson.M{"$set": bson.M{
			"payment_status": domain.PaymentPaid,
			"payment_ref":    reference,
			"paid_at":        paidAt,
			// A paid consultation is a confirmed one: the farm has the fee and
			// the slot is committed on both sides.
			"status":     domain.StatusConfirmed,
			"updated_at": time.Now().UTC(),
		}},
	)
	if err != nil {
		return false, fmt.Errorf("consultations: mark booking paid: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// MarkPaymentFailed records a declined or abandoned attempt.
//
// The slot is deliberately kept: unlike stock, a consultation time is not
// consumed by the attempt, and taking it away would punish a customer whose
// card was declined. The admin sees the outstanding fee and can cancel the
// booking, which is what frees the time.
func (r *BookingRepository) MarkPaymentFailed(ctx context.Context, id bson.ObjectID, status string) (bool, error) {
	result, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "payment_status": domain.PaymentPending},
		bson.M{"$set": bson.M{
			"payment_status": status,
			"updated_at":     time.Now().UTC(),
		}},
	)
	if err != nil {
		return false, fmt.Errorf("consultations: mark booking payment failed: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// MarkRefunded records a refunded fee against a paid booking.
func (r *BookingRepository) MarkRefunded(ctx context.Context, id bson.ObjectID) (bool, error) {
	result, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "payment_status": domain.PaymentPaid},
		bson.M{"$set": bson.M{
			"payment_status": domain.PaymentRefunded,
			"updated_at":     time.Now().UTC(),
		}},
	)
	if err != nil {
		return false, fmt.Errorf("consultations: mark booking refunded: %w", err)
	}

	return result.ModifiedCount == 1, nil
}

// ReferenceExists reports whether a booking reference is taken.
func (r *BookingRepository) ReferenceExists(ctx context.Context, reference string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx,
		bson.M{"reference": reference}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("consultations: reference exists: %w", err)
	}
	return count > 0, nil
}

// Count returns the total number of bookings.
func (r *BookingRepository) Count(ctx context.Context) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0, fmt.Errorf("consultations: count bookings: %w", err)
	}
	return count, nil
}

// Recent returns the newest bookings for the dashboard.
func (r *BookingRepository) Recent(ctx context.Context, limit int) ([]domain.Booking, error) {
	cursor, err := r.collection.Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("consultations: recent bookings: %w", err)
	}
	defer cursor.Close(ctx)

	bookings := make([]domain.Booking, 0, limit)
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, fmt.Errorf("consultations: decode recent bookings: %w", err)
	}

	return bookings, nil
}
