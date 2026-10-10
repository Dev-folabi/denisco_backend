// Package application assembles the administrative read models: the dashboard
// overview, customer management and the audit trail.
//
// The admin module owns no data of its own. It reads through the other
// modules' services and repositories, which keeps the aggregation in one place
// without any module having to know it is being summarised.
package application

import (
	"context"
	"time"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	ordersapp "github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	ordersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/orders/domain"
	productsapp "github.com/Dev-folabi/denisco_backend/internal/modules/products/application"
	usersdomain "github.com/Dev-folabi/denisco_backend/internal/modules/users/domain"
	"github.com/Dev-folabi/denisco_backend/internal/shared/apperror"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Dashboard tuning, matching the prototype's admin overview.
const (
	recentLimit    = 5
	salesChartDays = 7
)

// Service builds the administrative read models.
type Service struct {
	users     usersdomain.AdminRepository
	orderRepo ordersdomain.Repository
	orders    *ordersapp.Service
	products  *productsapp.Service
	consults  *consultapp.Service
	auditor   *audit.MongoRecorder
}

// NewService wires the admin module to the modules it summarises.
func NewService(
	users usersdomain.AdminRepository,
	orderRepo ordersdomain.Repository,
	orders *ordersapp.Service,
	products *productsapp.Service,
	consults *consultapp.Service,
	auditor *audit.MongoRecorder,
) *Service {
	return &Service{
		users:     users,
		orderRepo: orderRepo,
		orders:    orders,
		products:  products,
		consults:  consults,
		auditor:   auditor,
	}
}

// Overview returns the dashboard's stat cards, activity lists and sales chart.
func (s *Service) Overview(ctx context.Context) (*OverviewView, error) {
	orderStats, err := s.orders.Stats(ctx)
	if err != nil {
		return nil, err
	}

	customers, err := s.users.Count(ctx, usersdomain.RoleCustomer)
	if err != nil {
		return nil, apperror.From(err)
	}

	products, err := s.products.CountActive(ctx)
	if err != nil {
		return nil, err
	}

	bookings, err := s.consults.Stats(ctx)
	if err != nil {
		return nil, err
	}

	recentOrders, err := s.orderRepo.Recent(ctx, recentLimit)
	if err != nil {
		return nil, apperror.From(err)
	}

	recentBookings, err := s.consults.RecentBookings(ctx, recentLimit)
	if err != nil {
		return nil, err
	}

	sales, err := s.salesSeries(ctx)
	if err != nil {
		return nil, err
	}

	return &OverviewView{
		TotalRevenue:         orderStats.Revenue,
		TotalOrders:          orderStats.TotalOrders,
		TotalCustomers:       customers,
		TotalProducts:        products,
		PendingOrders:        orderStats.PendingOrders,
		ConsultationBookings: bookings,
		Currency:             ordersdomain.Currency,
		RecentOrders:         toRecentOrders(recentOrders),
		RecentBookings:       recentBookings,
		Sales:                sales,
	}, nil
}

// salesSeries builds the last seven days of revenue, including days with no
// sales so the chart keeps an even axis.
func (s *Service) salesSeries(ctx context.Context) ([]SalesPoint, error) {
	location := consultLocation()
	today := time.Now().In(location)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location).
		AddDate(0, 0, -(salesChartDays - 1))

	revenue, err := s.orderRepo.DailyRevenue(ctx, start.UTC())
	if err != nil {
		return nil, apperror.From(err)
	}

	points := make([]SalesPoint, 0, salesChartDays)
	for day := 0; day < salesChartDays; day++ {
		date := start.AddDate(0, 0, day)
		key := date.Format("2006-01-02")
		points = append(points, SalesPoint{
			Date:    key,
			Label:   date.Format("Mon"),
			Revenue: revenue[key],
		})
	}

	return points, nil
}

// ListCustomers returns a page of customer accounts.
func (s *Service) ListCustomers(ctx context.Context, search string, page, limit int) ([]CustomerView, int64, error) {
	users, total, err := s.users.List(ctx, usersdomain.AdminFilter{Search: search}, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}

	views := make([]CustomerView, 0, len(users))
	for _, user := range users {
		views = append(views, newCustomerView(user))
	}

	return views, total, nil
}

// Customer returns one customer with their purchase summary and history.
func (s *Service) Customer(ctx context.Context, id string) (*CustomerDetailView, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, usersdomain.ErrUserNotFound
	}

	user, err := s.users.FindByID(ctx, objectID)
	if err != nil {
		return nil, apperror.From(err)
	}

	orders, total, err := s.orders.List(ctx, ordersdomain.Filter{UserID: &objectID}, 1, 50)
	if err != nil {
		return nil, err
	}

	bookings, _, err := s.consults.ListForCustomer(ctx, objectID, 1, 50)
	if err != nil {
		return nil, err
	}

	var spent int64
	for _, order := range orders {
		if order.PaymentStatus == ordersdomain.PaymentPaid {
			spent += order.Total
		}
	}

	profile := newCustomerView(*user)

	return &CustomerDetailView{
		Customer:    &profile,
		Orders:      orders,
		Bookings:    bookings,
		TotalOrders: total,
		TotalSpent:  spent,
	}, nil
}

// AuditLogs returns a page of the administrative audit trail.
//
// Entries store the actor's id, because that is all an access token carries.
// The actor's email is resolved here, at read time, so the log reads as names
// rather than identifiers without the write path needing a user lookup.
func (s *Service) AuditLogs(ctx context.Context, page, limit int) ([]AuditView, int64, error) {
	entries, total, err := s.auditor.List(ctx, page, limit)
	if err != nil {
		return nil, 0, apperror.From(err)
	}

	emails := s.resolveActorEmails(ctx, entries)

	views := make([]AuditView, 0, len(entries))
	for _, entry := range entries {
		view := newAuditView(entry)
		if view.ActorEmail == "" && entry.ActorID != nil {
			view.ActorEmail = emails[*entry.ActorID]
		}
		views = append(views, view)
	}

	return views, total, nil
}

// resolveActorEmails looks up each distinct actor once, rather than once per
// entry.
func (s *Service) resolveActorEmails(ctx context.Context, entries []audit.Entry) map[bson.ObjectID]string {
	emails := map[bson.ObjectID]string{}

	for _, entry := range entries {
		if entry.ActorID == nil || entry.ActorEmail != "" {
			continue
		}
		if _, seen := emails[*entry.ActorID]; seen {
			continue
		}

		user, err := s.users.FindByID(ctx, *entry.ActorID)
		if err != nil {
			// A deleted account leaves its id in the trail, which is the
			// point of an immutable log.
			emails[*entry.ActorID] = ""
			continue
		}
		emails[*entry.ActorID] = user.Email
	}

	return emails
}
