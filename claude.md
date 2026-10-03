# DENISCO Backend — Implementation Plan

**Technology:** Go, Chi, MongoDB, Redis, Asynq, JWT, bcrypt, Paystack, Resend, ImageKit
**Architecture:** Modular monolith with layered separation
**Source of truth:** `denisco_prototype.html` (features/behavior) + `DENISCO_Architecture.md` (technical spec)

---

## 1. Project Scaffold

- [ ] Create directory structure below
- [ ] Initialize `go.mod`
- [ ] Create `Makefile` with build/run/test targets
- [ ] Create `.env.example`
- [ ] Create `README.md`

```
denisco_backend/
├── cmd/
│   ├── api/main.go            # HTTP server entry point
│   ├── worker/main.go         # Background job worker
│   └── outbox-publisher/main.go
├── internal/
│   ├── modules/               # Business modules
│   │   ├── auth/              # Authentication & authorization
│   │   ├── users/             # User/profile management
│   │   ├── products/          # Product catalogue
│   │   ├── inventory/         # Stock management
│   │   ├── cart/              # Shopping cart
│   │   ├── orders/            # Order lifecycle
│   │   ├── payments/          # Paystack integration
│   │   ├── consultations/     # Booking system
│   │   ├── notifications/     # Email via Resend
│   │   └── admin/             # Admin-specific orchestration
│   ├── platform/              # Cross-cutting infrastructure
│   │   ├── config/            # Env var loading
│   │   ├── database/          # MongoDB client, indexes
│   │   ├── redis/             # Redis client
│   │   ├── queue/             # Asynq client, task types, handlers
│   │   ├── events/            # Domain events, outbox
│   │   ├── http/              # Router setup, response helpers, error mapping
│   │   ├── middleware/        # Auth, CORS, rate limit, request ID, recovery
│   │   ├── security/          # JWT, bcrypt, crypto
│   │   ├── storage/           # ImageKit client
│   │   └── logger/            # slog setup
│   └── bootstrap/             # Dependency wiring
│       ├── api.go
│       ├── worker.go
│       └── dependencies.go
├── api/openapi.yaml
├── scripts/                   # Index creation, seeding
├── tests/                     # Integration, e2e, concurrency
├── deployments/               # Docker, Caddy, compose
├── .github/workflows/
├── .env.example
├── go.mod
├── Makefile
└── README.md
```

Each module follows: `domain/` → `application/` → `infrastructure/` → `transport/`

---

## 2. Phase 1 — Foundation

### 2.1 Configuration (`internal/platform/config/`)
- [ ] Load from environment variables
- [ ] Required: `APP_PORT`, `MONGODB_URI`, `MONGODB_DATABASE`, `REDIS_URL`, `JWT_*`, `WEB_ORIGIN`, `ADMIN_ORIGIN`
- [ ] Validate all required vars at startup; fail fast on missing config

### 2.2 MongoDB (`internal/platform/database/`)
- [ ] Connect with official Go driver over TLS
- [ ] Create indexes on startup (users.email unique, products.slug unique, orders.order_number unique, payments.reference unique, inventory.product_id unique, etc.)
- [ ] Connection pool settings appropriate for 2-core VPS

### 2.3 Redis (`internal/platform/redis/`)
- [ ] Connect via `REDIS_URL`
- [ ] Used for Asynq job queue and selective caching

### 2.4 HTTP Router (`internal/platform/http/`)
- [ ] Chi router with middleware chain:
  1. Request ID (`X-Request-ID`)
  2. Structured logging
  3. Recovery (panic → 500)
  4. CORS (exact origins for web + admin)
  5. Rate limiting
- [ ] Response helpers: `JSON(w, status, data)`, `Error(w, status, code, message)`
- [ ] Health check: `GET /healthz`

### 2.5 Response Format
All responses follow the architecture convention:

```json
// Success
{"success": true, "message": "...", "data": {...}}

// Paginated
{"success": true, "message": "...", "data": [...], "meta": {"page": 1, "limit": 20, "total": 100, "total_pages": 5}}

// Error
{"success": false, "error": {"code": "MACHINE_CODE", "message": "Human-readable message"}}
```

### 2.6 Logging
- [ ] `log/slog` with JSON output
- [ ] Include request ID in all log entries
- [ ] Log: route, method, status, duration, auth failures, business events
- [ ] Never log passwords, tokens, or secrets

---

## 3. Phase 2 — Authentication

### 3.1 Domain Models

- [ ] User struct
```go
type User struct {
    ID            primitive.ObjectID
    FirstName     string
    LastName      string
    Email         string    // normalized, unique
    Phone         string
    PasswordHash  string
    Role          Role      // customer | admin | super_admin
    Status        string    // active | inactive | suspended
    EmailVerified bool
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

- [ ] RefreshToken struct
```go
type RefreshToken struct {
    ID         primitive.ObjectID
    UserID     primitive.ObjectID
    TokenHash  string    // SHA-256 of opaque token
    FamilyID   string    // for reuse detection
    ExpiresAt  time.Time
    Revoked    bool
    CreatedAt  time.Time
}
```

### 3.2 Endpoints

- [ ] `POST /api/v1/auth/register` — Customer registration (Public)
- [ ] `POST /api/v1/auth/login` — Customer login (Public)
- [ ] `POST /api/v1/auth/admin/login` — Admin login, admin/super_admin only (Public)
- [ ] `POST /api/v1/auth/refresh` — Rotate refresh token, issue new access (Cookie)
- [ ] `POST /api/v1/auth/logout` — Revoke refresh token (Cookie)
- [ ] `POST /api/v1/auth/forgot-password` — Send password reset email (Public)
- [ ] `POST /api/v1/auth/reset-password` — Reset password with token (Public)
- [ ] `GET /api/v1/auth/me` — Get current user (Bearer)

### 3.3 JWT Strategy
- [ ] Access token: 15 min, signed with EdDSA or RS256
- [ ] Claims: `sub` (user_id), `role`, `iss` (denisco_backend), `aud` (denisco_api), `iat`, `exp`, `jti`
- [ ] Refresh token: opaque random string, stored as SHA-256 hash in MongoDB
- [ ] Rotation: new refresh token on every use, old revoked
- [ ] Reuse detection: revoke entire token family if reused token detected

### 3.4 Token Delivery
- [ ] Access JWT: returned in response body, kept in browser memory
- [ ] Refresh token: `Set-Cookie: Secure; HttpOnly; SameSite=<configured>; Path=/api/v1/auth`
- [ ] CSRF protection on `/refresh` and `/logout`

### 3.5 Password Hashing
- [ ] bcrypt with cost 12 (configurable)
- [ ] Password length: 8–72 characters (bcrypt limit)
- [ ] Rehash on login if cost factor changed

### 3.6 Rate Limiting
- [ ] Auth endpoints: strict (e.g., 5 requests/min per IP for login)
- [ ] Password reset: strict (e.g., 3/hour per email)
- [ ] General API: moderate

---

## 4. Phase 3 — Catalogue

### 4.1 Product Model
- [ ] Implement Product struct
```go
type Product struct {
    ID          primitive.ObjectID
    Name        string
    Slug        string      // unique, auto-generated
    Description string
    CategoryID  primitive.ObjectID
    Price       int64       // kobo (minor units)
    Currency    string      // "NGN"
    Unit        string      // "bird", "head", "bag", "100kg bag", "50kg bag"
    Images      []ProductImage
    Status      string      // active | archived
    Featured    bool
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### 4.2 Categories from Prototype
- [ ] Implement categories

| Slug | Label |
|---|---|
| `poultry` | Poultry |
| `livestock` | Livestock |
| `piggery` | Piggery |
| `snail` | Snail Farming |
| `crops` | Crop Farming |

### 4.3 Products from Prototype (seed data)

- [ ] Create seed script for products

| Name | Category | Unit | Price (₦) | Stock |
|---|---|---|---|---|
| Broiler Chicken | poultry | bird | 6,500 | 350 |
| Noiler Chicken | poultry | bird | 7,000 | 224 |
| Guinea Fowl | poultry | bird | 9,500 | 45 |
| Duck (Duckfowl) | poultry | bird | 8,500 | 25 |
| Local (Native) Chicken | poultry | bird | 7,500 | 30 |
| Cow (Cattle) | livestock | head | 850,000 | 5 |
| Goat | livestock | head | 65,000 | 5 |
| Sheep | livestock | head | 70,000 | 5 |
| Ram | livestock | head | 120,000 | 2 |
| Live Pig | piggery | head | 95,000 | 0 |
| Farm-Raised Snail | snail | bag | 25,000 | 35 |
| Maize (Corn) | crops | 100kg bag | 38,000 | 60 |
| Guinea Corn (Sorghum) | crops | 100kg bag | 42,000 | 40 |
| Groundnut | crops | 100kg bag | 55,000 | 20 |
| Rice (Paddy) | crops | 50kg bag | 45,000 | 15 |

### 4.4 Public Endpoints
- [ ] `GET /api/v1/products` — List with filters: `category`, `status`, `featured`, `search`, `sort`, pagination
- [ ] `GET /api/v1/products/{id}` — Single product by ID
- [ ] `GET /api/v1/products/slug/{slug}` — Single product by slug
- [ ] `GET /api/v1/categories` — All categories

### 4.5 Admin Endpoints
- [ ] `POST /api/v1/admin/products` — Create product (admin+)
- [ ] `PATCH /api/v1/admin/products/{id}` — Update product (admin+)
- [ ] `DELETE /api/v1/admin/products/{id}` — Archive product (admin+)
- [ ] `GET /api/v1/admin/products` — List all including archived
- [ ] `POST /api/v1/admin/products/upload-signature` — ImageKit auth signature

### 4.6 ImageKit Upload Flow
- [ ] Admin requests upload signature from backend
- [ ] Backend validates admin auth, generates short-lived signature
- [ ] Admin frontend uploads directly to ImageKit
- [ ] Admin frontend sends resulting file metadata to backend
- [ ] Backend validates and stores image reference

---

## 5. Phase 4 — Inventory

### 5.1 Model
- [ ] Implement Inventory struct
```go
type Inventory struct {
    ID               primitive.ObjectID
    ProductID        primitive.ObjectID  // unique index
    AvailableQty     int
    ReservedQty      int
    UpdatedAt        time.Time
}
// Sellable = AvailableQty - ReservedQty
```

- [ ] Implement InventoryMovement struct
```go
type InventoryMovement struct {
    ID         primitive.ObjectID
    ProductID  primitive.ObjectID
    Type       string  // "reservation", "release", "deduction", "adjustment"
    Quantity   int     // positive or negative
    Reason     string
    ActorID    primitive.ObjectID
    CreatedAt  time.Time
}
```

### 5.2 Operations
- [ ] **Reserve:** Atomic `findOneAndUpdate` with `{availableQty - reservedQty >= requestedQty}` condition
- [ ] **Release:** Decrement `reservedQty` when reservation expires or order cancelled
- [ ] **Deduct:** After payment confirmed, decrement `availableQty` and `reservedQty`
- [ ] **Adjust:** Manual admin adjustment with required reason, recorded in movements

### 5.3 Stock Badge Rules (from prototype)
- `stock <= 0` → Out of Stock (disable add-to-cart)
- `stock <= 10` → Low Stock
- `stock > 10` → In Stock

---

## 6. Phase 5 — E-commerce

### 6.1 Cart
- [ ] Server-side cart stored in MongoDB (keyed by user_id)
- [ ] Endpoints: GET, POST (add item), PATCH (update qty), DELETE (remove item), DELETE (clear)
- [ ] Never trust client-submitted prices

### 6.2 Order Creation Flow
- [ ] Receive authenticated checkout request
- [ ] Retrieve current product prices from DB (not from client)
- [ ] Validate stock availability
- [ ] Calculate totals: subtotal + delivery fee (₦2,500 for delivery, ₦0 for pickup)
- [ ] Create order with `pending_payment` status
- [ ] Reserve inventory atomically (MongoDB transaction)
- [ ] Create payment attempt with unique reference
- [ ] Initialize Paystack transaction
- [ ] Return checkout URL

### 6.3 Order Model
- [ ] Implement Order struct
```go
type Order struct {
    ID              primitive.ObjectID
    OrderNumber     string      // "DEN-2026-XXXXXX"
    UserID          primitive.ObjectID
    Items           []OrderItem
    Subtotal        int64       // kobo
    DeliveryFee     int64       // kobo
    Total           int64       // kobo
    Currency        string      // "NGN"
    DeliveryMethod  string      // "delivery" | "pickup"
    Address         string
    Status          string      // pending_payment, paid, processing, shipped, delivered, cancelled, expired
    PaymentStatus   string      // pending, paid, failed
    PaymentRef      string
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
```

### 6.4 Payment Flow (Paystack)
- [ ] `POST /api/v1/payments/initialize` → creates order, reserves stock, returns Paystack checkout URL
- [ ] Customer completes payment on Paystack
- [ ] `POST /api/v1/webhooks/paystack` → validate signature, verify amount/currency, update payment + order atomically, write outbox event
- [ ] `GET /api/v1/payments/{reference}` → frontend callback page queries for status
- [ ] `POST /api/v1/payments/verify` → explicit verification

### 6.5 Payment Idempotency
- [ ] Unique reference per payment attempt
- [ ] Webhook processing checks: already processed? → return 200 OK, do nothing
- [ ] Separate `payment_attempts` records; never overwrite failed attempts

### 6.6 Delivery Fee Logic (from prototype)
- Home Delivery: ₦2,500 flat (within Abuja, configurable)
- Farm Pickup: Free (₦0)

---

## 7. Phase 6 — Consultation

### 7.1 Consultation Types (seed data from prototype)

- [ ] Create seed script for consultation types

| ID | Name | Duration | Price (₦) |
|---|---|---|---|
| CT-01 | General Farm Setup Consultation | 60 mins | 15,000 |
| CT-02 | Poultry Health & Management Advisory | 45 mins | 10,000 |
| CT-03 | Livestock (Ruminant) Management Consultation | 60 mins | 15,000 |
| CT-04 | Piggery & Snail Farming Consultation | 45 mins | 10,000 |
| CT-05 | Crop Production & Soil Advisory | 60 mins | 15,000 |

### 7.2 Availability Model
- [ ] Admin configures available dates (up to 31 days ahead) and time slots
- [ ] Default times: 09:00 AM, 11:00 AM, 01:00 PM, 03:00 PM
- [ ] Stored in `consultation_slots` collection

### 7.3 Booking
- [ ] Atomic conditional update: `{typeId, date, time}` must not already have an active booking
- [ ] Booking reference format: `CB-XXXXX`
- [ ] Lifecycle: `pending → confirmed → completed` or `cancelled / no_show`
- [ ] Store timestamps in UTC, display in Africa/Lagos

### 7.4 Endpoints
- [ ] `GET /api/v1/consultations/types` — Public
- [ ] `GET /api/v1/consultations/slots` — Public
- [ ] `POST /api/v1/consultations/bookings` — Customer
- [ ] `GET /api/v1/consultations/bookings` — Customer
- [ ] `GET /api/v1/consultations/bookings/{id}` — Customer
- [ ] `POST /api/v1/consultations/bookings/{id}/cancel` — Customer
- [ ] `POST /api/v1/admin/consultations/types` — Admin
- [ ] `PATCH /api/v1/admin/consultations/types/{id}` — Admin
- [ ] `DELETE /api/v1/admin/consultations/types/{id}` — Admin
- [ ] `GET /api/v1/admin/consultations/bookings` — Admin
- [ ] `PATCH /api/v1/admin/consultations/bookings/{id}/status` — Admin
- [ ] `GET /api/v1/admin/consultations/slots` — Admin
- [ ] `POST /api/v1/admin/consultations/slots` — Admin
- [ ] `DELETE /api/v1/admin/consultations/slots/{id}` — Admin

---

## 8. Phase 7 — Background Processing

### 8.1 Architecture
- [ ] **Outbox Publisher:** Polls `outbox_events` for unpublished events, enqueues into Redis via Asynq
- [ ] **Worker:** Processes Asynq tasks (email delivery, reservation cleanup, etc.)

### 8.2 Email Notifications (via Resend)

- [ ] UserRegistered → Customer → Welcome email
- [ ] PasswordResetRequested → Customer → Reset link
- [ ] OrderCreated → Customer → Order confirmation
- [ ] OrderPaid → Customer → Payment confirmation
- [ ] OrderStatusChanged → Customer → Status update
- [ ] ConsultationBooked → Customer + Admin → Booking confirmation
- [ ] ConsultationCancelled → Customer → Cancellation notice

### 8.3 Worker Requirements
- [ ] Idempotent (use event ID as idempotency key)
- [ ] Bounded retries with exponential backoff
- [ ] Per-job timeouts
- [ ] Dead-letter handling for permanently failed jobs
- [ ] Structured logging per job

---

## 9. Phase 8 — Administration

### 9.1 Dashboard Aggregation Endpoint
- [ ] `GET /api/v1/admin/dashboard/overview` returns:
  - Total revenue (sum of successful payments)
  - Total orders count
  - Total customers count
  - Total products count
  - Pending orders count
  - Total consultation bookings count
  - Recent orders (last 5)
  - Recent bookings (last 5)
  - Sales data for chart (last 7 days, daily revenue)

### 9.2 Customer Management
- [ ] `GET /api/v1/admin/customers` — List with search, pagination
- [ ] `GET /api/v1/admin/customers/{id}` — Customer detail with order/booking history

### 9.3 Audit Logs
- [ ] `GET /api/v1/admin/audit-logs` — Paginated, filterable
- [ ] Record: product create/update/delete, inventory adjustments, order status changes, refunds, booking changes, admin access changes
- [ ] Immutable records with actor ID, action, resource, timestamp

---

## 10. Phase 9 — Testing

### 10.1 Unit Tests
- [ ] User domain validation (email normalization, password length)
- [ ] Price calculations (subtotal, delivery fee, total)
- [ ] Order state machine transitions
- [ ] Inventory sellable quantity calculation
- [ ] JWT claim validation
- [ ] Payment state transitions
- [ ] Booking slot uniqueness rules

### 10.2 Integration Tests
- [ ] MongoDB repository CRUD operations
- [ ] MongoDB transaction (order + inventory reservation)
- [ ] Auth flow (register → login → refresh → logout)
- [ ] Token rotation and reuse detection
- [ ] Paystack client mock (init, verify, webhook)

### 10.3 Concurrency Tests
- [ ] Two customers purchase the last item → only one succeeds
- [ ] Duplicate Paystack webhook → processed once
- [ ] Customer retries payment → separate attempts
- [ ] Two customers book same slot → only one succeeds
- [ ] Price change after cart creation → checkout uses current price
- [ ] Payment after reservation expiry → reconciliation path

---

## 11. Deployment

### Docker Setup
- [ ] Create `deployments/compose.yaml`
```yaml
# deployments/compose.yaml
services:
  caddy:      # Reverse proxy, HTTPS
  api:        # Go API server (port 4000)
  worker:     # Asynq worker
  outbox:     # Outbox publisher
  redis:      # Queue and cache
  web:        # Next.js customer app (port 3000)
  admin:      # Next.js admin app (port 3001)
```

### Resource Constraints (2 CPU, 2 GB RAM)
- Go API: ~50-100 MB
- Go Worker: ~30-50 MB
- Go Outbox: ~20-30 MB
- Redis: ~50 MB (bounded)
- Each Next.js: ~150-200 MB (standalone)
- Caddy: ~20 MB
- OS overhead: ~200-300 MB
- Total target: < 1.5 GB (leave headroom)

### Environment Variables
- [ ] Create production `.env` from `.env.example` in architecture document
