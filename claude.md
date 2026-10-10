# DENISCO Backend — Implementation Plan

**Technology:** Go, Chi, MongoDB, Redis, Asynq, JWT, bcrypt, Paystack, Resend, ImageKit
**Architecture:** Modular monolith with layered separation
**Source of truth:** `denisco_prototype.html` (features/behavior) + `DENISCO_Architecture.md` (technical spec)

---

> **API & Business Logic Rule — Non-negotiable:** Every endpoint, business rule, data model, seed value, and behavioral constraint must match `denisco_prototype.html` and `DENISCO_Architecture.md` exactly. Do not invent defaults — always check the prototype for the authoritative behavior (delivery fees, order number formats, booking ref formats, stock thresholds, consultation times, payment states, etc.).

---

## 1. Project Scaffold

- [x] Create directory structure below
- [x] Initialize `go.mod`
- [x] Create `Makefile` with build/run/test targets
- [x] Create `.env.example`
- [x] Create `README.md`

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
- [x] Load from environment variables
- [x] Required: `API_PORT`, `MONGODB_URI`, `MONGODB_DATABASE`, `REDIS_URL`, `JWT_*`, `WEB_ORIGIN`, `ADMIN_ORIGIN`
- [x] Validate all required vars at startup; fail fast on missing config

### 2.2 MongoDB (`internal/platform/database/`)
- [x] Connect with official Go driver over TLS
- [x] Create indexes on startup (users.email unique, products.slug unique, orders.order_number unique, payments.reference unique, inventory.product_id unique, etc.)
- [x] Connection pool settings appropriate for 2-core VPS

### 2.3 Redis (`internal/platform/redis/`)
- [x] Connect via `REDIS_URL`
- [x] Used for Asynq job queue and selective caching

### 2.4 HTTP Router (`internal/platform/http/`)
- [x] Chi router with middleware chain:
  1. Request ID (`X-Request-ID`)
  2. Structured logging
  3. Recovery (panic → 500)
  4. CORS (exact origins for web + admin)
  5. Rate limiting
- [x] Response helpers: `JSON(w, status, data)`, `Error(w, status, code, message)`
- [x] Health check: `GET /healthz`

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
- [x] `log/slog` with JSON output
- [x] Include request ID in all log entries
- [x] Log: route, method, status, duration, auth failures, business events
- [x] Never log passwords, tokens, or secrets

---

## 3. Phase 2 — Authentication

### 3.1 Domain Models

- [x] User struct
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

- [x] RefreshToken struct
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

- [x] `POST /api/v1/auth/register` — Customer registration (Public)
- [x] `POST /api/v1/auth/login` — Customer login (Public)
- [x] `POST /api/v1/auth/admin/login` — Admin login, admin/super_admin only (Public)
- [x] `POST /api/v1/auth/refresh` — Rotate refresh token, issue new access (Cookie)
- [x] `POST /api/v1/auth/logout` — Revoke refresh token (Cookie)
- [x] `POST /api/v1/auth/forgot-password` — Send password reset email (Public)
- [x] `POST /api/v1/auth/reset-password` — Reset password with token (Public)
- [x] `POST /api/v1/auth/change-password` — Change own password, requires current password (Authenticated)
- [x] `GET /api/v1/auth/me` — Get current user (Bearer)

### 3.3 JWT Strategy
- [x] Access token: 15 min, signed with EdDSA or RS256
- [x] Claims: `sub` (user_id), `role`, `iss` (denisco_backend), `aud` (denisco_api), `iat`, `exp`, `jti`
- [x] Refresh token: opaque random string, stored as SHA-256 hash in MongoDB
- [x] Rotation: new refresh token on every use, old revoked
- [x] Reuse detection: revoke entire token family if reused token detected

### 3.4 Token Delivery
- [x] Access JWT: returned in response body, kept in browser memory
- [x] Refresh token: `Set-Cookie: Secure; HttpOnly; SameSite=<configured>; Path=/api/v1/auth`
- [x] CSRF protection on `/refresh` and `/logout`

### 3.5 Password Hashing
- [x] bcrypt with cost 12 (configurable)
- [x] Password length: 8–72 characters (bcrypt limit)
- [x] Rehash on login if cost factor changed

### 3.6 Rate Limiting
- [x] Auth endpoints: strict (e.g., 5 requests/min per IP for login)
- [x] Password reset: strict (e.g., 3/hour per email)
- [x] General API: moderate

---

> **Phases 1–2 status (2026-10-05):** Implemented and verified against MongoDB Atlas
> and Redis. Also delivered alongside them: `Dockerfile.api`, `Dockerfile.worker`,
> `Dockerfile.outbox-publisher`, `deployments/compose.yaml`, the CI workflow,
> `api/openapi.yaml` (health + auth), and `scripts/seed-admin` for provisioning the
> first administrator. The worker and outbox-publisher processes start, verify Redis
> and idle — their handlers arrive in Phase 7.

---

## 4. Phase 3 — Catalogue

### 4.1 Product Model
- [x] Implement Product struct
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
- [x] Implement categories

| Slug | Label |
|---|---|
| `poultry` | Poultry |
| `livestock` | Livestock |
| `piggery` | Piggery |
| `snail` | Snail Farming |
| `crops` | Crop Farming |

### 4.3 Products from Prototype (seed data)

- [x] Create seed script for products

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
- [x] `GET /api/v1/products` — List with filters: `category`, `status`, `featured`, `search`, `sort`, pagination
- [x] `GET /api/v1/products/{id}` — Single product by ID
- [x] `GET /api/v1/products/slug/{slug}` — Single product by slug
- [x] `GET /api/v1/categories` — All categories

### 4.5 Admin Endpoints
- [x] `POST /api/v1/admin/products` — Create product (admin+)
- [x] `PATCH /api/v1/admin/products/{id}` — Update product (admin+)
- [x] `DELETE /api/v1/admin/products/{id}` — Archive product (admin+)
- [x] `GET /api/v1/admin/products` — List all including archived
- [x] `POST /api/v1/admin/products/upload-signature` — ImageKit auth signature

### 4.6 ImageKit Upload Flow
- [x] Admin requests upload signature from backend
- [x] Backend validates admin auth, generates short-lived signature
- [x] Admin frontend uploads directly to ImageKit
- [x] Admin frontend sends resulting file metadata to backend
- [x] Backend validates and stores image reference

---

## 5. Phase 4 — Inventory

### 5.1 Model
- [x] Implement Inventory struct
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

- [x] Implement InventoryMovement struct
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
- [x] **Reserve:** Atomic `findOneAndUpdate` with `{availableQty - reservedQty >= requestedQty}` condition
- [x] **Release:** Decrement `reservedQty` when reservation expires or order cancelled
- [x] **Deduct:** After payment confirmed, decrement `availableQty` and `reservedQty`
- [x] **Adjust:** Manual admin adjustment with required reason, recorded in movements

### 5.3 Stock Badge Rules (from prototype)
- `stock <= 0` → Out of Stock (disable add-to-cart)
- `stock <= 10` → Low Stock
- `stock > 10` → In Stock

---

## 6. Phase 5 — E-commerce

### 6.1 Cart
- [x] Server-side cart stored in MongoDB (keyed by user_id)
- [x] Endpoints: GET, POST (add item), PATCH (update qty), DELETE (remove item), DELETE (clear)
- [x] Never trust client-submitted prices

### 6.2 Order Creation Flow
- [x] Receive authenticated checkout request
- [x] Retrieve current product prices from DB (not from client)
- [x] Validate stock availability
- [x] Calculate totals: subtotal + delivery fee (₦2,500 for delivery, ₦0 for pickup)
- [x] Create order with `pending_payment` status
- [x] Reserve inventory atomically (MongoDB transaction)
- [x] Create payment attempt with unique reference
- [x] Initialize Paystack transaction
- [x] Return checkout URL

### 6.3 Order Model
- [x] Implement Order struct
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
- [x] `POST /api/v1/payments/initialize` → creates order, reserves stock, returns Paystack checkout URL
- [x] Customer completes payment on Paystack
- [x] `POST /api/v1/webhooks/paystack` → validate signature, verify amount/currency, update payment + order atomically, write outbox event
- [x] `GET /api/v1/payments/{reference}` → frontend callback page queries for status
- [x] `POST /api/v1/payments/verify` → explicit verification

### 6.5 Payment Idempotency
- [x] Unique reference per payment attempt
- [x] Webhook processing checks: already processed? → return 200 OK, do nothing
- [x] Separate `payment_attempts` records; never overwrite failed attempts

### 6.6 Delivery Fee Logic (from prototype)
- Home Delivery: ₦2,500 flat (within Abuja, configurable)
- Farm Pickup: Free (₦0)

---

> **Phases 3–5 status (2026-10-05):** Implemented and verified against MongoDB
> Atlas. Deviations from the wording above, all taken from
> `denisco_prototype.html`, which the plan names as the source of truth:
>
> - Order numbers use the prototype's `DG-XXXXXX` format (§6.3 above said
>   `DEN-2026-XXXXXX`); payment references use `PSK-…`.
> - An order carries a fulfillment `status` (`pending → processing →
>   dispatched → completed`, cancellable until dispatch) separate from
>   `payment_status` (`pending | paid | failed | refunded`), matching the
>   prototype's admin dropdown and the already-built admin UI. The plan's
>   `pending_payment`/`shipped`/`delivered` map onto these.
> - Inventory field names follow the Go struct in §5.1 (`available_qty`,
>   `reserved_qty`) rather than the architecture document's
>   `quantity_on_hand`/`quantity_reserved`.
>
> Also delivered: an audit trail for catalogue, stock, order-status and refund
> actions; `scripts/seed-catalogue`; and the OpenAPI spec extended to 41 paths.

---

## 7. Phase 6 — Consultation

### 7.1 Consultation Types (seed data from prototype)

- [x] Create seed script for consultation types

| ID | Name | Duration | Price (₦) |
|---|---|---|---|
| CT-01 | General Farm Setup Consultation | 60 mins | 15,000 |
| CT-02 | Poultry Health & Management Advisory | 45 mins | 10,000 |
| CT-03 | Livestock (Ruminant) Management Consultation | 60 mins | 15,000 |
| CT-04 | Piggery & Snail Farming Consultation | 45 mins | 10,000 |
| CT-05 | Crop Production & Soil Advisory | 60 mins | 15,000 |

### 7.2 Availability Model
- [x] Admin configures available dates (up to 31 days ahead) and time slots
- [x] Default times: 09:00 AM, 11:00 AM, 01:00 PM, 03:00 PM
- [x] Stored in `consultation_slots` collection

### 7.3 Booking
- [x] Atomic conditional update: `{typeId, date, time}` must not already have an active booking
- [x] Booking reference format: `CB-XXXXX`
- [x] Lifecycle: `pending → confirmed → completed` or `cancelled / no_show`
- [x] Store timestamps in UTC, display in Africa/Lagos

### 7.4 Endpoints
- [x] `GET /api/v1/consultations/types` — Public
- [x] `GET /api/v1/consultations/slots` — Public
- [x] `POST /api/v1/consultations/bookings` — Customer
- [x] `GET /api/v1/consultations/bookings` — Customer
- [x] `GET /api/v1/consultations/bookings/{id}` — Customer
- [x] `POST /api/v1/consultations/bookings/{id}/cancel` — Customer
- [x] `POST /api/v1/admin/consultations/types` — Admin
- [x] `PATCH /api/v1/admin/consultations/types/{id}` — Admin
- [x] `DELETE /api/v1/admin/consultations/types/{id}` — Admin
- [x] `GET /api/v1/admin/consultations/bookings` — Admin
- [x] `PATCH /api/v1/admin/consultations/bookings/{id}/status` — Admin
- [x] `GET /api/v1/admin/consultations/slots` — Admin
- [x] `POST /api/v1/admin/consultations/slots` — Admin
- [x] `DELETE /api/v1/admin/consultations/slots/{id}` — Admin

---

## 8. Phase 7 — Background Processing

### 8.1 Architecture
- [x] **Outbox Publisher:** Polls `outbox_events` for unpublished events, enqueues into Redis via Asynq
- [x] **Worker:** Processes Asynq tasks (email delivery, reservation cleanup, etc.)

### 8.2 Email Notifications (via Resend)

- [x] UserRegistered → Customer → Welcome email
- [x] PasswordResetRequested → Customer → Reset link
- [x] OrderCreated → Customer → Order confirmation
- [x] OrderPaid → Customer → Payment confirmation
- [x] OrderStatusChanged → Customer → Status update
- [x] ConsultationBooked → Customer + Admin → Booking confirmation
- [x] ConsultationCancelled → Customer → Cancellation notice

### 8.3 Worker Requirements
- [x] Idempotent (use event ID as idempotency key)
- [x] Bounded retries with exponential backoff
- [x] Per-job timeouts
- [x] Dead-letter handling for permanently failed jobs
- [x] Structured logging per job

---

## 9. Phase 8 — Administration

### 9.1 Dashboard Aggregation Endpoint
- [x] `GET /api/v1/admin/dashboard/overview` returns:
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
- [x] `GET /api/v1/admin/customers` — List with search, pagination
- [x] `GET /api/v1/admin/customers/{id}` — Customer detail with order/booking history

### 9.3 Audit Logs
- [x] `GET /api/v1/admin/audit-logs` — Paginated, filterable
- [x] Record: product create/update/delete, inventory adjustments, order status changes, refunds, booking changes, admin access changes
- [x] Immutable records with actor ID, action, resource, timestamp

---

> **Phases 6–8 status (2026-10-05):** Implemented and verified against MongoDB
> Atlas, Redis and a local mail stand-in. Notes on how the plan was read:
>
> - **Guest booking.** The consultation page is public in the prototype and a
>   visitor can book without an account, so `POST /consultations/bookings`
>   takes *optional* authentication: a signed-in customer's booking is attached
>   to them and appears in their history, a guest's is not.
> - **Slot uniqueness** is enforced by a partial unique index on
>   `(type_id, date, time)` filtered to live bookings, not by a read-then-write
>   check. Cancelling clears the flag, which returns the time to the calendar.
> - **Availability** is one grid shared by every service, as in the prototype;
>   what makes a time unbookable is an existing booking *for that service*.
> - **Scheduling** runs inside the worker through Asynq's scheduler rather than
>   as a fourth process, so the deployment keeps three containers.
>
> Also delivered: `scripts/seed-consultations`, the audit trail extended to
> consultation changes with the actor's email resolved at read time, and the
> OpenAPI spec grown to 57 paths.

---

## 10. Phase 9 — Testing

### 10.1 Unit Tests
- [x] User domain validation (email normalization, password length)
- [x] Price calculations (subtotal, delivery fee, total)
- [x] Order state machine transitions
- [x] Inventory sellable quantity calculation
- [x] JWT claim validation (signature, algorithm, issuer, audience, expiry)
- [x] Payment state transitions
- [x] Booking slot uniqueness rules

### 10.2 Integration Tests
- [x] MongoDB repository CRUD operations — `tests/integration/repositories_test.go` covers users, products, inventory, cart, orders, payments, consultations, the outbox and the audit trail
- [x] MongoDB transaction (order + inventory reservation) — `tests/integration/transactions_test.go`, including an aborted checkout that leaves neither an order nor a reservation behind
- [x] Auth flow (register → login → refresh → logout) — now MongoDB-backed in `tests/integration/auth_test.go`, alongside the service-level suite
- [x] Token rotation and reuse detection
- [x] Paystack client mock (init, verify, webhook) — a stand-in HTTP server speaking the provider's contract, so the client's requests, its decoding and the webhook signature check are all exercised

### 10.3 Concurrency Tests
- [x] Two customers purchase the last item → only one succeeds — 1 of 6 simultaneous checkouts succeeds, under `-race` against Atlas
- [x] Duplicate Paystack webhook → processed once — sequential replays and four concurrent deliveries both deduct once and email once
- [x] Customer retries payment → separate attempts
- [x] Two customers book same slot → only one succeeds — enforced by a partial unique index, 1 of 6 under `-race`
- [x] Price change after cart creation → checkout uses current price
- [x] Payment after reservation expiry → reconciliation path — both branches: stock secured and the order revived, or flagged `needs_reconciliation` for a refund

---

## 11. Deployment

### Docker Setup
- [x] Create `deployments/compose.yaml` — all seven services with memory limits, health checks, restart policies and capped logs; only Caddy publishes ports
- [x] `deployments/Caddyfile` — TLS for the three hostnames, security headers, Cloudflare-aware client addresses
- [x] `deployments/redis.conf` — append-only persistence, bounded memory, `noeviction`
- [x] `deployments/backup.sh` and `restore.sh` — MongoDB and queue backup, with a restore that lands beside the live database until it is checked
- [x] `deployments/README.md` — the runbook: host hardening, Cloudflare DNS, releases and rollback, health checks, recovery behaviour, the restore drill
- [x] CI validates the Compose file, the Caddyfile and the backup scripts
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
- [x] Create production `.env` from `.env.example` — `.env.example` documents every API variable and `deployments/.env.example` the stack variables Compose substitutes (hostnames, image tag, public URLs). The production values, including the secrets, are written at deploy time into `/etc/denisco/api.env` on the VPS; §4 of `deployments/README.md` lists the ones that differ from the defaults.

---

> **Phases 9 and deployment status (2026-10-06):** The testing phase is
> complete and verified against MongoDB Atlas; the deployment phase is
> complete as configuration, which is as far as it goes without the VPS.
>
> - **The integration suite lives in `tests/integration`** and wires the real
>   `bootstrap.NewContainer`, so it exercises production wiring rather than a
>   parallel assembly. It skips itself unless `MONGODB_TEST_URI` (or
>   `MONGODB_URI`) is set, refuses to run against the configured application
>   database, and empties only its own. `make test-integration` runs it.
> - **Transactions need a replica set.** Atlas provides one; CI starts a
>   single-node `--replSet` MongoDB for the same reason. A standalone server
>   skips the half of the suite that matters.
> - **The late-payment reconciliation path was built, not just tested.** The
>   architecture (§4.4.7) requires a payment arriving after its reservation
>   expired to "either secure inventory or initiate the appropriate refund
>   procedure". `MarkPaid` now does exactly that: it re-takes the stock and
>   returns the order to `pending` when the units are still sellable, or sets
>   `needs_reconciliation` with reason `refund_required` and alerts staff
>   when they are not. Both outcomes are recorded in the audit trail against
>   the system actor, and the admin order list takes a
>   `needs_reconciliation` filter. The admin dashboard has no control for that
>   filter yet: the prototype, which the UI rule makes the source of truth for
>   every visual decision, has none, so staff hear about a flagged order
>   through the notification email and the API. Adding a control is a design
>   decision rather than a backend one.
> - **A defect surfaced while writing the retry test:** a declined card left
>   the order permanently unpayable, because `IsPayable` and the repository's
>   `MarkPaid` guard both insisted on `payment_status = pending`. Since the
>   cart is emptied at checkout, the customer had no way to pay at all. Both
>   now accept a failed attempt, which is what makes "separate attempts per
>   retry" in §6.5 usable.
> - **Two deployment decisions, revisited after review.** Caddy's health check
>   points at `/healthz` (liveness) rather than `/health/ready`: there is one
>   API container, so taking it out of rotation would replace the API's own
>   error responses — which carry a request ID — with Caddy's 502 page.
>   Readiness is for alerting, and now separates dependencies the API cannot
>   work without from ones it can: MongoDB unreachable is `not_ready` (503),
>   Redis unreachable is `degraded` (200), because orders still commit and
>   their events wait in the outbox.
> - **Client addresses are no longer read from a forgeable header.** Rate
>   limiting and the audit log attribute a request to `httpx.ClientIP`, which
>   used to read `X-Forwarded-For` first. Anyone can send that, and Cloudflare
>   appends the real address to whatever the client sent rather than replacing
>   it, so a caller could pick their own rate-limit bucket. The API now trusts
>   one header, named by `CLIENT_IP_HEADER`, refuses to start if that is
>   `X-Forwarded-For`, and falls back to the peer address; Caddy sets
>   `X-Real-IP` from `{client_ip}`, which it resolves from the forwarded chain
>   only for connections from Cloudflare's own ranges. Search terms are
>   length-bounded for the same reason — see `docs/security-review.md`.
> - **Not done here, and deliberately:** nothing was provisioned. Cloudflare
>   records, the VPS, SSH hardening, the firewall and the first `docker
>   compose up` are host actions, written up as a runbook with the exact
>   commands. The Compose file and Caddyfile are validated by CI rather than
>   by this machine, which has no Docker daemon.

---

## 12. Changes after Phase 9 (2026-10-10)

See §7 of the root `claude.md` for the full list. What it means in this
repository:

- **`internal/platform/database/indexes.go`** replaces the per-service slot
  index with `uniq_active_slot_datetime` on `(date, time)`, drops the
  superseded `uniq_active_slot` by name on boot (MongoDB refuses to change an
  existing index's keys), and refuses to start when the data already violates
  the new rule rather than failing on index creation. `scripts/resolve-slot-collisions`
  is the tool for that case.
- **The payments module settles two kinds of record.** `Payment.Purpose` is
  `order` or `consultation`; an empty value on a stored record means `order`,
  which is all there was before. `Initialize` and `InitializeBooking` share one
  `initialize` core, and `apply`, `Refund` and the ownership check branch on
  the purpose. `BookingPort`, declared in the payments module and implemented
  by `paymentBookings` in the wiring, keeps the dependency one-way: payments
  knows about consultations, not the reverse — which is why the web client
  books first and pays second, exactly as checkout does.
- **Guest capability rule.** A booking with no `user_id` is reachable by
  presenting its reference with its identifier (`authorizeWithReference`), and
  a payment with no `user_id` by its reference alone (`authorizeView`). A
  record that *does* belong to an account still requires that account; a
  reference is not a way around ownership.
- **`DELETE /admin/products/{id}`** returns `{deleted, images_deleted}`.
  `Service.Remove` asks `SalesPort` whether the product has ever been ordered,
  then either archives it or deletes it along with its stock record
  (`StockPort.PurgeRecord`) and its media (`MediaPort.DeleteFile`). The order
  check and the delete are not one transaction, and the comment on `Remove`
  says why that is the right trade here.
- **New tests:** cross-service slot exclusivity under `-race`; a consultation
  fee paid through the provider stub, replayed webhook included; a declined fee
  keeping its slot and being retried; the guest capability rule; and both
  branches of product removal. The suite is 35 integration tests.
