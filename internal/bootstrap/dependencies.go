// Package bootstrap wires the application together: it opens the
// infrastructure connections, constructs the repositories, services and
// handlers, and assembles the HTTP router. It is the only package allowed to
// know about every other one.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Dev-folabi/denisco_backend/internal/audit"
	adminapp "github.com/Dev-folabi/denisco_backend/internal/modules/admin/application"
	admintransport "github.com/Dev-folabi/denisco_backend/internal/modules/admin/transport"
	authapp "github.com/Dev-folabi/denisco_backend/internal/modules/auth/application"
	authinfra "github.com/Dev-folabi/denisco_backend/internal/modules/auth/infrastructure"
	authtransport "github.com/Dev-folabi/denisco_backend/internal/modules/auth/transport"
	cartapp "github.com/Dev-folabi/denisco_backend/internal/modules/cart/application"
	cartinfra "github.com/Dev-folabi/denisco_backend/internal/modules/cart/infrastructure"
	carttransport "github.com/Dev-folabi/denisco_backend/internal/modules/cart/transport"
	categoriesapp "github.com/Dev-folabi/denisco_backend/internal/modules/categories/application"
	categoriesinfra "github.com/Dev-folabi/denisco_backend/internal/modules/categories/infrastructure"
	categoriestransport "github.com/Dev-folabi/denisco_backend/internal/modules/categories/transport"
	consultapp "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/application"
	consultinfra "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/infrastructure"
	consulttransport "github.com/Dev-folabi/denisco_backend/internal/modules/consultations/transport"
	inventoryapp "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/application"
	inventoryinfra "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/infrastructure"
	inventorytransport "github.com/Dev-folabi/denisco_backend/internal/modules/inventory/transport"
	ordersapp "github.com/Dev-folabi/denisco_backend/internal/modules/orders/application"
	ordersinfra "github.com/Dev-folabi/denisco_backend/internal/modules/orders/infrastructure"
	orderstransport "github.com/Dev-folabi/denisco_backend/internal/modules/orders/transport"
	paymentsapp "github.com/Dev-folabi/denisco_backend/internal/modules/payments/application"
	paymentsinfra "github.com/Dev-folabi/denisco_backend/internal/modules/payments/infrastructure"
	paymentstransport "github.com/Dev-folabi/denisco_backend/internal/modules/payments/transport"
	productsapp "github.com/Dev-folabi/denisco_backend/internal/modules/products/application"
	productsinfra "github.com/Dev-folabi/denisco_backend/internal/modules/products/infrastructure"
	productstransport "github.com/Dev-folabi/denisco_backend/internal/modules/products/transport"
	usersinfra "github.com/Dev-folabi/denisco_backend/internal/modules/users/infrastructure"
	"github.com/Dev-folabi/denisco_backend/internal/outbox"
	"github.com/Dev-folabi/denisco_backend/internal/platform/config"
	"github.com/Dev-folabi/denisco_backend/internal/platform/database"
	"github.com/Dev-folabi/denisco_backend/internal/platform/mail"
	"github.com/Dev-folabi/denisco_backend/internal/platform/payment"
	"github.com/Dev-folabi/denisco_backend/internal/platform/redis"
	"github.com/Dev-folabi/denisco_backend/internal/platform/security"
	"github.com/Dev-folabi/denisco_backend/internal/platform/storage"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Container holds the long-lived dependencies shared by the API, worker and
// outbox processes.
type Container struct {
	Config config.Config
	Logger *slog.Logger

	Mongo    *mongo.Client
	Database *mongo.Database
	Redis    *redis.Client

	Hasher *security.Hasher
	Signer *security.TokenSigner
	Media  *storage.ImageKit
	Mail   mail.Sender

	Outbox outbox.Writer
	Audit  *audit.MongoRecorder
	Tx     *database.TxRunner

	AuthService *authapp.Service
	AuthHandler *authtransport.Handler

	CategoryService *categoriesapp.Service
	CategoryHandler *categoriestransport.Handler

	ProductService *productsapp.Service
	ProductHandler *productstransport.Handler

	InventoryService *inventoryapp.Service
	InventoryHandler *inventorytransport.Handler

	CartService *cartapp.Service
	CartHandler *carttransport.Handler

	OrderService *ordersapp.Service
	OrderHandler *orderstransport.Handler

	PaymentService *paymentsapp.Service
	PaymentHandler *paymentstransport.Handler

	ConsultationService *consultapp.Service
	ConsultationHandler *consulttransport.Handler

	AdminService *adminapp.Service
	AdminHandler *admintransport.Handler

	// Outbox reader, used by the publisher process.
	OutboxReader outbox.Reader
}

// NewContainer opens the infrastructure connections and constructs every
// module. MongoDB is mandatory; Redis is not yet required to serve auth and
// catalogue traffic, so a failure there is logged and surfaced through the
// readiness probe rather than preventing startup.
func NewContainer(ctx context.Context, cfg config.Config, log *slog.Logger) (*Container, error) {
	mongoClient, err := database.Connect(cfg.Mongo.URI, cfg.Mongo.MaxPoolSize)
	if err != nil {
		return nil, err
	}

	db := mongoClient.Database(cfg.Mongo.Database)

	if err := database.EnsureIndexes(ctx, db); err != nil {
		_ = database.Disconnect(mongoClient)
		return nil, err
	}
	log.Info("mongodb connected", slog.String("database", cfg.Mongo.Database))

	var redisClient *redis.Client
	if client, err := redis.Connect(cfg.Redis.URL); err != nil {
		log.Warn("redis unavailable; background processing and caching are degraded",
			slog.String("error", err.Error()),
		)
	} else {
		redisClient = client
		log.Info("redis connected")
	}

	signer, err := security.NewTokenSigner(security.SignerOptions{
		Secret:   cfg.JWT.Secret,
		Seed:     cfg.JWT.Ed25519Seed,
		Issuer:   cfg.JWT.Issuer,
		Audience: cfg.JWT.Audience,
		TTL:      cfg.JWT.AccessTTL,
	})
	if err != nil {
		_ = database.Disconnect(mongoClient)
		return nil, fmt.Errorf("bootstrap: token signer: %w", err)
	}

	hasher := security.NewHasher(cfg.Auth.BcryptCost)
	events := outbox.NewMongoWriter(db)

	userRepo := usersinfra.NewMongoRepository(db)
	refreshRepo := authinfra.NewRefreshTokenRepository(db)
	resetRepo := authinfra.NewPasswordResetRepository(db)

	authService := authapp.NewService(userRepo, refreshRepo, resetRepo, events, hasher, signer, cfg.Auth, cfg.App)

	// Catalogue and stock. The catalogue reads stock through a small port so
	// the two modules stay independent; the adapter lives in adapters.go.
	auditor := audit.NewMongoRecorder(db)
	media := storage.NewImageKit(cfg.ImageKit)

	categoryRepo := categoriesinfra.NewMongoRepository(db)
	productRepo := productsinfra.NewMongoRepository(db)
	inventoryRepo := inventoryinfra.NewMongoRepository(db)

	categoryService := categoriesapp.NewService(categoryRepo)
	inventoryService := inventoryapp.NewService(inventoryRepo, auditor)
	// E-commerce. Checkout and payment settlement run in transactions, so the
	// shared runner is handed to the order module.
	txRunner := database.NewTxRunner(mongoClient)

	cartRepo := cartinfra.NewMongoRepository(db)
	orderRepo := ordersinfra.NewMongoRepository(db)
	paymentRepo := paymentsinfra.NewMongoRepository(db)

	productService := productsapp.NewService(
		productRepo,
		categoryRepo,
		catalogueStock{inventory: inventoryService},
		catalogueSales{orders: orderRepo},
		media,
		auditor,
	)

	cartService := cartapp.NewService(
		cartRepo,
		cartCatalog{products: productService},
		cartStock{inventory: inventoryService},
	)

	orderService := ordersapp.NewService(
		orderRepo,
		orderCart{carts: cartRepo},
		orderCatalog{products: productService},
		orderStock{inventory: inventoryService},
		txRunner,
		events,
		auditor,
	)

	// Consultations. They are built before payments because a consultation
	// fee is settled through Paystack, so the payments module holds a port
	// onto this service.
	consultService := consultapp.NewService(
		consultinfra.NewTypeRepository(db),
		consultinfra.NewSlotRepository(db),
		consultinfra.NewBookingRepository(db),
		events,
		auditor,
	)

	paymentService := paymentsapp.NewService(
		paymentRepo,
		paymentOrders{orders: orderService},
		paymentBookings{consultations: consultService},
		payment.NewPaystack(cfg.Paystack),
		auditor,
		cfg.App.WebURL+"/payment/callback",
	)

	// The administrative read models own no data of their own; they read
	// through the modules they summarise.
	adminService := adminapp.NewService(
		userRepo,
		orderRepo,
		orderService,
		productService,
		consultService,
		auditor,
	)

	return &Container{
		Config:           cfg,
		Logger:           log,
		Mongo:            mongoClient,
		Database:         db,
		Redis:            redisClient,
		Hasher:           hasher,
		Signer:           signer,
		Media:            media,
		Outbox:           events,
		Audit:            auditor,
		Tx:               txRunner,
		AuthService:      authService,
		AuthHandler:      authtransport.NewHandler(authService, cfg.Auth),
		CategoryService:  categoryService,
		CategoryHandler:  categoriestransport.NewHandler(categoryService),
		ProductService:   productService,
		ProductHandler:   productstransport.NewHandler(productService, media, auditor),
		InventoryService: inventoryService,
		InventoryHandler: inventorytransport.NewHandler(inventoryService),
		CartService:      cartService,
		CartHandler:      carttransport.NewHandler(cartService),
		OrderService:     orderService,
		OrderHandler:     orderstransport.NewHandler(orderService),
		PaymentService:   paymentService,
		PaymentHandler:   paymentstransport.NewHandler(paymentService),

		ConsultationService: consultService,
		ConsultationHandler: consulttransport.NewHandler(consultService),

		AdminService: adminService,
		AdminHandler: admintransport.NewHandler(adminService),

		Mail:         mail.NewResend(cfg.Resend),
		OutboxReader: events,
	}, nil
}

// Close releases every infrastructure connection.
func (c *Container) Close() {
	if c.Redis != nil {
		if err := c.Redis.Close(); err != nil {
			c.Logger.Warn("redis close failed", slog.String("error", err.Error()))
		}
	}
	if c.Mongo != nil {
		if err := database.Disconnect(c.Mongo); err != nil {
			c.Logger.Warn("mongodb disconnect failed", slog.String("error", err.Error()))
		}
	}
}
