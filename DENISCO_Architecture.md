# Backend Architecture

Go

Modular Monolith

MongoDB

Redis

JWT Authentication

This document defines the backend architecture for DENISCO GLOBAL AGRICULTURE LTD's website, e-commerce platform, consultation booking system and administration dashboard.

The backend will be built as a modular monolith in Go, with clear separation of business domains, asynchronous background processing and independently deployable API and worker processes.

The functional HTML/CSS/JavaScript prototype in the project root is the primary reference for the agreed features, workflows, UI-related data requirements and intended business behaviour. The backend must support both the customer-facing website and the admin dashboard without duplicating business logic.

Core architectural decisions

* Language: Go

* Architecture: Modular monolith

* HTTP router: Chi

* Database: MongoDB Atlas (managed)

* Cache and queue: Redis

* Background jobs: Asynq

* Authentication: JWT access tokens and rotating refresh tokens

* Password hashing: bcrypt

* Payments: Paystack

* File storage: ImageKit

* Email: Resend

* API documentation: OpenAPI

* Deployment: Docker on a VPS, with MongoDB hosted externally

# 1. Project structure

The backend is maintained in its own repository, separate from the customer-facing frontend, admin frontend and root-level prototype.

```
denisco/
├── denisco_prototype.html
│
├── denisco_backend/
│   ├── cmd/
│   │   ├── api/
│   │   │   └── main.go
│   │   ├── worker/
│   │   │   └── main.go
│   │   └── outbox/
│   │       └── main.go
│   │
│   ├── internal/
│   │   ├── config/
│   │   ├── platform/
│   │   │   ├── database/
│   │   │   ├── redis/
│   │   │   ├── logger/
│   │   │   ├── mail/
│   │   │   ├── storage/
│   │   │   └── payment/
│   │   │
│   │   ├── middleware/
│   │   ├── shared/
│   │   │
│   │   ├── modules/
│   │   │   ├── auth/
│   │   │   ├── users/
│   │   │   ├── products/
│   │   │   ├── categories/
│   │   │   ├── inventory/
│   │   │   ├── cart/
│   │   │   ├── orders/
│   │   │   ├── payments/
│   │   │   ├── consultations/
│   │   │   ├── notifications/
│   │   │   └── admin/
│   │   │
│   │   ├── jobs/
│   │   └── outbox/
│   │
│   ├── api/
│   │   └── openapi.yaml
│   │
│   ├── migrations/
│   ├── scripts/
│   ├── tests/
│   │
│   ├── deployments/
│   │   ├── Dockerfile.api
│   │   ├── Dockerfile.worker
│   │   ├── Dockerfile.outbox
│   │   └── compose.yaml
│   │
│   ├── .github/
│   │   └── workflows/
│   ├── .env.example
│   ├── .gitignore
│   ├── go.mod
│   ├── go.sum
│   └── README.md
│
├── denisco_web/
└── denisco_admin/
```

## 1.1 Directory responsibilities

| Directory             | Responsibility                                                                             |
| --------------------- | ------------------------------------------------------------------------------------------ |
| `cmd/api`             | Starts the HTTP API server and registers routes.                                           |
| `cmd/worker`          | Starts asynchronous background job processing.                                             |
| `cmd/outbox`          | Publishes committed outbox events to Redis.                                                |
| `internal/config`     | Loads and validates environment configuration.                                             |
| `internal/platform`   | Infrastructure integrations, such as MongoDB, Redis, email, storage and payments.          |
| `internal/middleware` | Authentication, authorization, CORS, rate limiting, request IDs and other HTTP middleware. |
| `internal/shared`     | Small, genuinely cross-cutting types and utilities.                                        |
| `internal/modules`    | Business-domain modules and their application logic.                                       |
| `internal/jobs`       | Asynq task definitions and handlers.                                                       |
| `internal/outbox`     | Transactional outbox persistence and publishing.                                           |
| `api`                 | OpenAPI specification for the HTTP API.                                                    |
| `migrations`          | Database index and schema migration scripts.                                               |
| `tests`               | Integration and end-to-end tests.                                                          |
| `deployments`         | Dockerfiles and deployment configuration.                                                  |

Each business module should own its domain models, repository interfaces, application services, HTTP handlers and validation rules. Infrastructure-specific implementations should remain separate from business rules.

# 2. Architectural style

HTTP layer

Chi router · Middleware · Handlers · Request validation

Application layer

Use cases · Business workflows · Authorization · Transactions

Domain and repository interfaces

Business rules · Entities · Repository contracts

Infrastructure layer

MongoDB · Redis · Paystack · ImageKit · Resend

The system uses a modular monolith rather than microservices. All modules share a deployable codebase, but each module has a defined responsibility and communicates through application interfaces or explicitly defined domain events.

### Architectural rules

1. HTTP handlers must not contain business logic or database queries.

2. Application services coordinate business operations and enforce workflows.

3. Domain rules must not depend on HTTP, MongoDB or Redis.

4. Repository interfaces belong close to the modules that use them.

5. A module must not modify another module's MongoDB documents directly.

6. External integrations must be isolated behind interfaces.

7. Critical operations must be atomic where possible and idempotent where retries are possible.

8. Background jobs must be safe to retry.

9. Cross-module dependencies should be explicit and kept minimal.

# 3. Technology stack

| Component         | Technology                             | Purpose                              |
| ----------------- | -------------------------------------- | ------------------------------------ |
| Language          | Go                                     | Backend implementation               |
| Router            | Chi                                    | HTTP routing and middleware          |
| Database          | MongoDB Atlas                          | Persistent application data          |
| Database driver   | Official MongoDB Go driver             | Database access                      |
| Cache             | Redis                                  | Caching, coordination and job queues |
| Job processing    | Asynq                                  | Background task execution            |
| Authentication    | JWT                                    | Short-lived access tokens            |
| Refresh tokens    | Cryptographically random opaque tokens | Session renewal and revocation       |
| Password hashing  | bcrypt                                 | Secure password storage              |
| Logging           | `log/slog`                             | Structured application logging       |
| API documentation | OpenAPI 3.x                            | HTTP contract                        |
| Payments          | Paystack                               | Checkout and payment verification    |
| File storage      | ImageKit                               | Product and media assets             |
| Email             | Resend                                 | Transactional email                  |
| Testing           | Go testing                             | Unit, integration and API tests      |
| Containerization  | Docker                                 | Packaging and deployment             |
| Reverse proxy     | Caddy                                  | HTTPS termination and routing        |

# 4. Business modules

## 4.1 Authentication (`auth`)

Handles registration, login, logout, token refresh, password resets and authentication-related security.

Responsibilities

* Customer registration and login.

* Password hashing and verification using bcrypt.

* Access JWT issuance and validation.

* Refresh-token rotation and revocation.

* Password-reset requests and confirmation.

* Account status checks.

* Authentication-related rate limiting.

Password security

Use `golang.org/x/crypto/bcrypt` for password hashing.

Go

```
package password

import "golang.org/x/crypto/bcrypt"

const DefaultCost = 12

func Hash(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword(
        []byte(password),
        DefaultCost,
    )
    if err != nil {
        return "", err
    }

    return string(hash), nil
}

func Verify(hash, password string) error {
    return bcrypt.CompareHashAndPassword(
        []byte(hash),
        []byte(password),
    )
}
```

The cost should be benchmarked on the production VPS. Enforce a password-length limit compatible with bcrypt's 72-byte input limit, and never silently truncate passwords.

Token strategy

| Token                | Purpose                   | Storage                                                              |
| -------------------- | ------------------------- | -------------------------------------------------------------------- |
| Access JWT           | Authenticate API requests | Client-side memory where practical                                   |
| Refresh token        | Obtain new access tokens  | Secure, HTTP-only cookie where applicable; hashed server-side record |
| Password-reset token | Authorize password resets | Hashed, expiring database record                                     |

Access tokens should be short-lived. Refresh tokens should rotate on use, with their hashes stored in MongoDB to support revocation and reuse detection.

JWTs should include a user identifier, role or relevant authorization claims, issue time and expiry. The backend must still check account status and enforce authorization on protected operations.

## 4.2 Users (`users`)

Manages customer accounts and profile information.

Responsibilities

* Customer profile retrieval and updates.

* Account activation and deactivation.

* Address management.

* Account deletion requests.

* User lookup for authorized administrative workflows.

Admin roles and permissions must be enforced on the server, not merely hidden in the frontend.

## 4.3 Products (`products`)

Manages the agricultural products and other purchasable items displayed in the shop.

Responsibilities

* Product creation and updates.

* Product descriptions, prices and images.

* Product publication and availability status.

* Product detail retrieval.

* Product filtering, search and pagination.

* Product archival.

Products should distinguish between published, draft and archived states. Archived products should remain accessible in historical orders.

## 4.4 Categories (`categories`)

Manages product categorization.

Responsibilities

* Create and update categories.

* Assign products to categories.

* List and filter categories.

* Activate and deactivate categories.

Use stable category identifiers rather than relying on names as references.

## 4.5 Inventory (`inventory`)

Owns stock quantities, reservations and inventory movements.

Responsibilities

* Track available and reserved stock.

* Reserve stock during checkout.

* Release expired or abandoned reservations.

* Deduct stock after confirmed payment.

* Record adjustments and stock movements.

* Prevent overselling.

A stock change must create an inventory movement record. This provides an audit trail for purchases, cancellations, manual adjustments and refunds.

## 4.6 Cart (`cart`)

Manages the customer's shopping cart.

Responsibilities

* Add products to a cart.

* Update quantities.

* Remove cart items.

* Retrieve the current cart.

* Validate product availability and prices during checkout.

The cart is a convenience, not an authoritative order record. The backend must revalidate all product prices, quantities and stock when creating an order.

## 4.7 Orders (`orders`)

Owns the order lifecycle.

Responsibilities

* Create orders from validated cart items.

* Calculate authoritative order totals.

* Reserve inventory.

* Track order status.

* Retrieve customer order history.

* Support authorized administrative order updates.

* Handle cancellations and eligible refunds.

Order items should contain immutable snapshots of product names, prices, quantities and other relevant purchase details. Changes to a product must not alter an existing order.

Suggested order statuses:

| Status            | Meaning                         |
| ----------------- | ------------------------------- |
| `pending_payment` | Order created, awaiting payment |
| `paid`            | Payment confirmed               |
| `processing`      | Order being prepared            |
| `fulfilled`       | Order fulfilled                 |
| `cancelled`       | Order cancelled                 |
| `payment_failed`  | Payment attempt failed          |
| `refund_pending`  | Refund initiated                |
| `refunded`        | Refund confirmed                |

Use explicit, validated status transitions. Avoid allowing arbitrary status updates from the admin interface.

## 4.8 Payments (`payments`)

Integrates Paystack and manages payment records.

Responsibilities

* Initialize payment transactions.

* Verify payment references.

* Process Paystack webhooks.

* Maintain payment status.

* Prevent duplicate payment processing.

* Coordinate successful payments with orders.

* Handle refund requests and status updates.

Payment records should retain the provider reference, amount, currency, order identifier, status and relevant timestamps. Do not store card details.

## 4.9 Consultations (`consultations`)

Manages consultation services and booking availability.

Responsibilities

* Create and manage consultation types.

* Configure durations and availability.

* List available time slots.

* Create and cancel bookings.

* Prevent overlapping bookings.

* Track booking status.

* Provide customers with booking history.

* Support authorized administrative scheduling.

The backend must validate availability when creating a booking. A slot displayed as available by the frontend is not a guarantee that it remains available at submission time.

Suggested booking statuses:

* `pending`

* `confirmed`

* `cancelled`

* `completed`

* `no_show`

If consultation payments are required by the approved prototype, the booking workflow should integrate with the payment module rather than implementing a separate payment system.

## 4.10 Notifications (`notifications`)

Handles customer and administrative notifications.

Responsibilities

* Order confirmation emails.

* Payment confirmation emails.

* Password-reset emails.

* Consultation booking confirmations.

* Booking reminders.

* Administrative alerts where required.

Email delivery should run asynchronously. Failed email delivery must not roll back a successfully completed order or payment.

## 4.11 Admin (`admin`)

Provides administrative application workflows through protected API endpoints.

Responsibilities

* Dashboard summary data.

* Product and inventory administration.

* Order management.

* Customer lookup and management.

* Payment and transaction visibility.

* Consultation booking management.

* Audit log access.

The admin module coordinates authorized workflows across other modules. It should not duplicate product, order or payment business logic.

# 5. Database architecture

MongoDB Atlas

External managed database

MongoDB is the primary persistent database. It will run on a managed service rather than on the VPS, keeping database storage, backups and database availability independent of the application's server.

Use the official MongoDB Go driver and define indexes through version-controlled migration scripts.

## 5.1 Collections

| Collection              | Module        | Purpose                                              |
| ----------------------- | ------------- | ---------------------------------------------------- |
| `users`                 | Users         | Customer profiles and account information            |
| `refresh_tokens`        | Auth          | Hashed refresh tokens and revocation metadata        |
| `password_resets`       | Auth          | Password-reset token hashes and expiration           |
| `products`              | Products      | Product details, prices and publication status       |
| `categories`            | Categories    | Product categories                                   |
| `carts`                 | Cart          | Customer shopping carts                              |
| `orders`                | Orders        | Order records and item snapshots                     |
| `payments`              | Payments      | Payment references, amounts and statuses             |
| `inventory`             | Inventory     | Stock quantities and reservations                    |
| `inventory_movements`   | Inventory     | Stock adjustment history                             |
| `consultation_types`    | Consultations | Available consultation services                      |
| `consultation_slots`    | Consultations | Availability and slot allocation                     |
| `consultation_bookings` | Consultations | Customer consultation bookings                       |
| `outbox_events`         | Outbox        | Events awaiting publication                          |
| `audit_logs`            | Admin         | Administrative actions and security-relevant changes |

Use embedded documents for small, tightly coupled data, such as order items and addresses captured at checkout. Use separate collections for entities that need independent querying, lifecycle management or indexing.

## 5.2 Example MongoDB documents

Product

JSON

```
{
  "_id": "product_id",
  "name": "Organic Fertilizer",
  "slug": "organic-fertilizer",
  "description": "Organic agricultural fertilizer",
  "price": 15000,
  "currency": "NGN",
  "category_id": "category_id",
  "images": [
    {
      "url": "https://example.com/product.jpg",
      "file_id": "imagekit_file_id"
    }
  ],
  "status": "published",
  "created_at": "2026-10-02T10:00:00Z",
  "updated_at": "2026-10-02T10:00:00Z"
}
```

Order

JSON

```
{
  "_id": "order_id",
  "user_id": "user_id",
  "items": [
    {
      "product_id": "product_id",
      "name": "Organic Fertilizer",
      "unit_price": 15000,
      "quantity": 2,
      "subtotal": 30000
    }
  ],
  "currency": "NGN",
  "subtotal": 30000,
  "delivery_fee": 0,
  "total": 30000,
  "status": "pending_payment",
  "payment_status": "pending",
  "created_at": "2026-10-02T10:00:00Z",
  "updated_at": "2026-10-02T10:00:00Z"
}
```

The example amounts are illustrative. Production totals must be calculated by the backend using integer minor units or another explicitly defined exact monetary representation. Never use floating-point arithmetic for money.

Inventory

JSON

```
{
  "_id": "inventory_id",
  "product_id": "product_id",
  "quantity_on_hand": 100,
  "quantity_reserved": 5,
  "updated_at": "2026-10-02T10:00:00Z"
}
```

Available stock is derived as:

Available stock=Quantity on hand−Quantity reserved\text{Available stock} = \text{Quantity on hand} - \text{Quantity reserved}Available stock=Quantity on hand−Quantity reserved

The backend must prevent either quantity from becoming invalid through concurrent requests.

## 5.3 Index strategy

Create indexes based on actual query patterns, including:

| Collection              | Index                       | Purpose                          |
| ----------------------- | --------------------------- | -------------------------------- |
| `users`                 | Unique `email`              | Prevent duplicate accounts       |
| `products`              | Unique `slug`               | Product lookup                   |
| `products`              | `status`, `category_id`     | Published product filtering      |
| `products`              | `name`                      | Product search, if needed        |
| `orders`                | `user_id`, `created_at`     | Customer order history           |
| `orders`                | `status`, `created_at`      | Administrative order filtering   |
| `payments`              | Unique `provider_reference` | Payment idempotency              |
| `payments`              | `order_id`                  | Payment lookup by order          |
| `inventory`             | Unique `product_id`         | One inventory record per product |
| `inventory_movements`   | `product_id`, `created_at`  | Inventory history                |
| `consultation_bookings` | `user_id`, `created_at`     | Booking history                  |
| `consultation_bookings` | `slot_id`, `status`         | Slot occupancy queries           |
| `refresh_tokens`        | `user_id`                   | Token management                 |
| `refresh_tokens`        | `expires_at` with TTL       | Expired token cleanup            |
| `outbox_events`         | `status`, `created_at`      | Pending event discovery          |

TTL indexes should only be used for records whose expiration semantics permit automatic deletion. They are cleanup mechanisms, not substitutes for authorization or expiry validation.

MongoDB transactions require a replica set. Confirm the managed MongoDB deployment supports transactions before implementing multi-document checkout and booking operations.

# 6. API architecture

The API is versioned and exposed under `/api/v1`.

```
/api/v1
├── /auth
├── /users
├── /products
├── /categories
├── /cart
├── /orders
├── /payments
├── /consultations
└── /admin
    ├── /dashboard
    ├── /products
    ├── /inventory
    ├── /orders
    ├── /customers
    ├── /payments
    └── /consultations
```

The customer website and admin dashboard communicate directly with the backend over HTTPS. Neither frontend requires a BFF.

## 6.1 API conventions

* Use RESTful resource-oriented endpoints.

* Use JSON for request and response bodies.

* Use appropriate HTTP status codes.

* Validate request bodies, query parameters and path parameters.

* Use pagination for potentially large collections.

* Apply authentication and authorization at the route or application-service level.

* Document every public endpoint in OpenAPI.

* Use consistent error responses.

* Avoid exposing internal database document structures.

### Success response

Every successful API response with a body must include `success`, `message` and, where applicable, `data`.

JSON

```
{
  "success": true,
  "message": "Product retrieved successfully.",
  "data": {
    "id": "product_id",
    "name": "Organic Fertilizer",
    "price": 15000
  }
}
```

For paginated responses:

JSON

```
{
  "success": true,
  "message": "Products retrieved successfully.",
  "data": [
    {
      "id": "product_1",
      "name": "Organic Fertilizer",
      "price": 15000
    }
  ],
  "meta": {
    "page": 1,
    "limit": 20,
    "total": 45,
    "total_pages": 3
  }
}
```

For operations that do not return a resource, a successful response can include a message without `data`. Use a JSON response with an appropriate status code when a success message is required; a `204 No Content` response is the exception because it has no body.

### Error response

JSON

```
{
  "success": false,
  "message": "The request could not be completed.",
  "error": {
    "code": "INSUFFICIENT_STOCK",
    "details": {
      "product_id": "product_id",
      "available": 2,
      "requested": 5
    }
  }
}
```

Do not expose stack traces, database errors, secrets or internal implementation details in API responses.

## 6.2 HTTP status codes

| Status                      | Usage                                      |
| --------------------------- | ------------------------------------------ |
| `200 OK`                    | Successful retrieval or update             |
| `201 Created`               | Resource successfully created              |
| `202 Accepted`              | Asynchronous operation accepted            |
| `204 No Content`            | Successful operation with no response body |
| `400 Bad Request`           | Invalid request                            |
| `401 Unauthorized`          | Missing or invalid authentication          |
| `403 Forbidden`             | Authenticated but not authorized           |
| `404 Not Found`             | Resource not found                         |
| `409 Conflict`              | Duplicate resource or state conflict       |
| `422 Unprocessable Entity`  | Valid syntax but invalid business input    |
| `429 Too Many Requests`     | Rate limit exceeded                        |
| `500 Internal Server Error` | Unexpected server error                    |
| `502 Bad Gateway`           | Upstream service failure                   |
| `503 Service Unavailable`   | Temporary service unavailability           |

## 6.3 Core endpoint specification

| Method   | Endpoint                              | Purpose                                  | Access             |
| -------- | ------------------------------------- | ---------------------------------------- | ------------------ |
| `POST`   | `/auth/register`                      | Register a customer                      | Public             |
| `POST`   | `/auth/login`                         | Authenticate                             | Public             |
| `POST`   | `/auth/refresh`                       | Rotate refresh token                     | Refresh token      |
| `POST`   | `/auth/logout`                        | Revoke refresh token                     | Authenticated      |
| `POST`   | `/auth/forgot-password`               | Request password reset                   | Public             |
| `POST`   | `/auth/reset-password`                | Reset password                           | Reset token        |
| `GET`    | `/users/me`                           | Retrieve profile                         | Customer           |
| `PATCH`  | `/users/me`                           | Update profile                           | Customer           |
| `GET`    | `/products`                           | List published products                  | Public             |
| `GET`    | `/products/{id}`                      | Retrieve product                         | Public             |
| `GET`    | `/categories`                         | List active categories                   | Public             |
| `GET`    | `/cart`                               | Retrieve cart                            | Customer           |
| `POST`   | `/cart/items`                         | Add item                                 | Customer           |
| `PATCH`  | `/cart/items/{id}`                    | Update quantity                          | Customer           |
| `DELETE` | `/cart/items/{id}`                    | Remove item                              | Customer           |
| `POST`   | `/orders`                             | Create order and reserve stock           | Customer           |
| `GET`    | `/orders`                             | List customer orders                     | Customer           |
| `GET`    | `/orders/{id}`                        | Retrieve an order                        | Owner or admin     |
| `POST`   | `/payments/initialize`                | Initialize payment for an eligible order | Customer           |
| `GET`    | `/payments/{reference}`               | Retrieve payment status                  | Owner or admin     |
| `POST`   | `/payments/webhook/paystack`          | Receive payment webhook                  | Paystack signature |
| `GET`    | `/consultations/types`                | List consultation services               | Public             |
| `GET`    | `/consultations/availability`         | List available slots                     | Public             |
| `POST`   | `/consultations/bookings`             | Create booking                           | Customer           |
| `GET`    | `/consultations/bookings`             | List customer bookings                   | Customer           |
| `POST`   | `/consultations/bookings/{id}/cancel` | Cancel booking                           | Owner or admin     |
| `GET`    | `/admin/dashboard`                    | Dashboard overview                       | Admin              |
| `POST`   | `/admin/products`                     | Create product                           | Admin              |
| `PATCH`  | `/admin/products/{id}`                | Update product                           | Admin              |
| `PATCH`  | `/admin/inventory/{productId}`        | Adjust inventory                         | Authorized admin   |
| `GET`    | `/admin/orders`                       | List orders                              | Admin              |
| `PATCH`  | `/admin/orders/{id}/status`           | Update eligible order status             | Authorized admin   |
| `GET`    | `/admin/customers`                    | List customers                           | Admin              |
| `GET`    | `/admin/payments`                     | List transactions                        | Admin              |
| `GET`    | `/admin/consultations/bookings`       | List bookings                            | Admin              |

The endpoint list is the initial API contract. It should be reconciled with the HTML prototype before implementation, particularly for checkout, delivery details, consultation payment and order-management workflows.

# 7. Authentication and authorization

## 7.1 JWT authentication

The system uses short-lived JWT access tokens with refresh-token rotation.

Customer login

Email and password

Backend authentication

Verify bcrypt hash and account status

Issue tokens

Short-lived access JWT + refresh token

Authenticated requests

JWT validation and authorization

Recommended initial configuration:

| Setting               | Policy                                                             |
| --------------------- | ------------------------------------------------------------------ |
| Access token          | Short-lived, initially 15 minutes                                  |
| Refresh token         | Longer-lived, initially 7–30 days, subject to product requirements |
| Password hashing      | bcrypt, initially cost 12                                          |
| Token rotation        | Every successful refresh                                           |
| Refresh-token storage | Hash stored in MongoDB                                             |
| Password reset        | Single-use, expiring token                                         |
| Logout                | Revoke refresh-token record                                        |

Store signing keys and provider secrets in environment variables or a suitable secret-management service. Never commit them to Git.

For the browser applications, prefer keeping access tokens in memory and using `Secure`, `HttpOnly`, appropriately configured `SameSite` cookies for refresh tokens. Cookie-based refresh endpoints must include appropriate CSRF protection.

## 7.2 Role-based access control

Initial roles:

* `customer`

* `admin`

Authorization should be enforced server-side. Admin operations should use explicit permission checks, especially for inventory adjustments, refunds, order transitions and customer-account changes.

If more granular administrative roles are needed later, introduce permissions such as `products.write`, `orders.manage` and `payments.refund`, rather than scattering role-name checks throughout the codebase.

# 8. Critical business workflows

## 8.1 E-commerce checkout

Checkout must be implemented as a backend-controlled workflow. A frontend cart or displayed price is never authoritative.

Diagram options

![](data\:image/svg+xml;utf8,%3Csvg%20id%3D%22mermaid-_r_1bj_%22%20width%3D%22636.8414306640625%22%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20class%3D%22flowchart%22%20height%3D%222217.400146484375%22%20viewBox%3D%224%204%20636.8414306640625%202217.400146484375%22%20role%3D%22graphics-document%20document%22%20aria-roledescription%3D%22flowchart-v2%22%3E%3Cstyle%3E%23mermaid-_r_1bj_%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A14px%3Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%40keyframes%20edge-animation-frame%7Bfrom%7Bstroke-dashoffset%3A0%3B%7D%7D%40keyframes%20dash%7Bto%7Bstroke-dashoffset%3A0%3B%7D%7D%23mermaid-_r_1bj_%20.edge-animation-slow%7Bstroke-dasharray%3A9%2C5!important%3Bstroke-dashoffset%3A900%3Banimation%3Adash%2050s%20linear%20infinite%3Bstroke-linecap%3Around%3B%7D%23mermaid-_r_1bj_%20.edge-animation-fast%7Bstroke-dasharray%3A9%2C5!important%3Bstroke-dashoffset%3A900%3Banimation%3Adash%2020s%20linear%20infinite%3Bstroke-linecap%3Around%3B%7D%23mermaid-_r_1bj_%20.error-icon%7Bfill%3Argb\(249%2C%20249%2C%20249\)%3B%7D%23mermaid-_r_1bj_%20.error-text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3Bstroke%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20.edge-thickness-normal%7Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bj_%20.edge-thickness-thick%7Bstroke-width%3A3.5px%3B%7D%23mermaid-_r_1bj_%20.edge-pattern-solid%7Bstroke-dasharray%3A0%3B%7D%23mermaid-_r_1bj_%20.edge-thickness-invisible%7Bstroke-width%3A0%3Bfill%3Anone%3B%7D%23mermaid-_r_1bj_%20.edge-pattern-dashed%7Bstroke-dasharray%3A3%3B%7D%23mermaid-_r_1bj_%20.edge-pattern-dotted%7Bstroke-dasharray%3A2%3B%7D%23mermaid-_r_1bj_%20.marker%7Bfill%3Argb\(93%2C%2093%2C%2093\)%3Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bj_%20.marker.cross%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bj_%20svg%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A14px%3B%7D%23mermaid-_r_1bj_%20p%7Bmargin%3A0%3B%7D%23mermaid-_r_1bj_%20.label%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20.cluster-label%20text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20.cluster-label%20span%7Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20.cluster-label%20span%20p%7Bbackground-color%3Atransparent%3B%7D%23mermaid-_r_1bj_%20.label%20text%2C%23mermaid-_r_1bj_%20span%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20.node%20rect%2C%23mermaid-_r_1bj_%20.node%20circle%2C%23mermaid-_r_1bj_%20.node%20ellipse%2C%23mermaid-_r_1bj_%20.node%20polygon%2C%23mermaid-_r_1bj_%20.node%20path%7Bfill%3Argb\(222%2C%20234%2C%20251\)%3Bstroke%3Argb\(83%2C%20154%2C%20248\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bj_%20.rough-node%20.label%20text%2C%23mermaid-_r_1bj_%20.node%20.label%20text%2C%23mermaid-_r_1bj_%20.image-shape%20.label%2C%23mermaid-_r_1bj_%20.icon-shape%20.label%7Btext-anchor%3Amiddle%3B%7D%23mermaid-_r_1bj_%20.node%20.katex%20path%7Bfill%3A%23000%3Bstroke%3A%23000%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bj_%20.rough-node%20.label%2C%23mermaid-_r_1bj_%20.node%20.label%2C%23mermaid-_r_1bj_%20.image-shape%20.label%2C%23mermaid-_r_1bj_%20.icon-shape%20.label%7Btext-align%3Acenter%3B%7D%23mermaid-_r_1bj_%20.node.clickable%7Bcursor%3Apointer%3B%7D%23mermaid-_r_1bj_%20.root%20.anchor%20path%7Bfill%3Argb\(93%2C%2093%2C%2093\)!important%3Bstroke-width%3A0%3Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bj_%20.arrowheadPath%7Bfill%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bj_%20.edgePath%20.path%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3Bstroke-width%3A2.0px%3B%7D%23mermaid-_r_1bj_%20.flowchart-link%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3Bfill%3Anone%3B%7D%23mermaid-_r_1bj_%20.edgeLabel%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Btext-align%3Acenter%3B%7D%23mermaid-_r_1bj_%20.edgeLabel%20p%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bj_%20.edgeLabel%20rect%7Bopacity%3A0.5%3Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bfill%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bj_%20.labelBkg%7Bbackground-color%3Argba\(252%2C%20252%2C%20252%2C%200.5\)%3B%7D%23mermaid-_r_1bj_%20.cluster%20rect%7Bfill%3Argb\(249%2C%20249%2C%20249\)%3Bstroke%3Argba\(0%2C%200%2C%200%2C%200.05\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bj_%20.cluster%20text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20.cluster%20span%7Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20div.mermaidTooltip%7Bposition%3Aabsolute%3Btext-align%3Acenter%3Bmax-width%3A200px%3Bpadding%3A2px%3Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A12px%3Bbackground%3Argb\(249%2C%20249%2C%20249\)%3Bborder%3A1px%20solid%20rgba\(0%2C%200%2C%200%2C%200.05\)%3Bborder-radius%3A2px%3Bpointer-events%3Anone%3Bz-index%3A100%3B%7D%23mermaid-_r_1bj_%20.flowchartTitleText%7Btext-anchor%3Amiddle%3Bfont-size%3A18px%3Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bj_%20rect.text%7Bfill%3Anone%3Bstroke-width%3A0%3B%7D%23mermaid-_r_1bj_%20.icon-shape%2C%23mermaid-_r_1bj_%20.image-shape%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Btext-align%3Acenter%3B%7D%23mermaid-_r_1bj_%20.icon-shape%20p%2C%23mermaid-_r_1bj_%20.image-shape%20p%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bpadding%3A2px%3B%7D%23mermaid-_r_1bj_%20.icon-shape%20rect%2C%23mermaid-_r_1bj_%20.image-shape%20rect%7Bopacity%3A0.5%3Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bfill%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bj_%20.label-icon%7Bdisplay%3Ainline-block%3Bheight%3A1em%3Boverflow%3Avisible%3Bvertical-align%3A-0.125em%3B%7D%23mermaid-_r_1bj_%20.node%20.label-icon%20path%7Bfill%3AcurrentColor%3Bstroke%3Arevert%3Bstroke-width%3Arevert%3B%7D%23mermaid-_r_1bj_%20.node%20text%7Bfont-size%3A16px%3Bfont-weight%3A600%3Bletter-spacing%3A-0.32px%3Bfill%3A%23004f99%3B%7D%23mermaid-_r_1bj_%20.edgeLabels%20text%7Bfont-size%3A13px%3Bfont-weight%3A600%3Bletter-spacing%3A-0.08px%3Bfill%3A%23004f99%3B%7D%23mermaid-_r_1bj_%20.node%20tspan%5Bfont-weight%3D%22normal%22%5D%2C%23mermaid-_r_1bj_%20.edgeLabels%20tspan%5Bfont-weight%3D%22normal%22%5D%7Bfont-weight%3A600%3B%7D%23mermaid-_r_1bj_%20.edgeLabel%20.label%20rect%7Bopacity%3A1%3Brx%3A13px%3Bry%3A13px%3Bfill%3A%23f5faff%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bj_%20.node%20rect%2C%23mermaid-_r_1bj_%20.node%20circle%2C%23mermaid-_r_1bj_%20.node%20ellipse%2C%23mermaid-_r_1bj_%20.node%20polygon%2C%23mermaid-_r_1bj_%20.node%20path%7Bfill%3Argb\(229%2C%20243%2C%20255\)%3Bstroke%3Argba\(0%2C%200%2C%200%2C%200.1\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bj_%20.node%20rect%7Brx%3A16px%3Bry%3A16px%3B%7D%23mermaid-_r_1bj_%20.node.mermaid-decision%20.label-container%7Bfill%3A%23f5faff%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-dasharray%3A2%202%3B%7D%23mermaid-_r_1bj_%20.edgePaths%20.flowchart-link%7Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-width%3A1px%3Bstroke-linecap%3Around%3Bstroke-linejoin%3Around%3B%7D%23mermaid-_r_1bj_%20.marker%7Bfill%3Argb\(206%2C%20219%2C%20229\)%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3B%7D%23mermaid-_r_1bj_%20.node%7Bcolor-scheme%3Alight%3B%7D%23mermaid-_r_1bj_%20%3Aroot%7B--mermaid-font-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3B%7D%3C%2Fstyle%3E%3Cg%3E%3Cmarker%20id%3D%22mermaid-_r_1bj__flowchart-v2-pointEnd%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%22-5%20-5%2010%2010%22%20refX%3D%220%22%20refY%3D%220%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2210%22%20markerHeight%3D%2210%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%200%200%20L%204%200%20M%200.8180194846605362%20-3.181980515339464%20L%204%200%20L%200.8180194846605362%203.181980515339464%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%20none%3B%20fill%3A%20none%3B%20stroke-linecap%3A%20round%3B%20stroke-linejoin%3A%20round%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bj__flowchart-v2-pointStart%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%22-5%20-5%2010%2010%22%20refX%3D%220%22%20refY%3D%220%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2210%22%20markerHeight%3D%2210%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%200%200%20L%20-4%200%20M%20-0.8180194846605362%20-3.181980515339464%20L%20-4%200%20L%20-0.8180194846605362%203.181980515339464%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%20none%3B%20fill%3A%20none%3B%20stroke-linecap%3A%20round%3B%20stroke-linejoin%3A%20round%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bj__flowchart-v2-circleEnd%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%220%200%2010%2010%22%20refX%3D%2211%22%20refY%3D%225%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Ccircle%20cx%3D%225%22%20cy%3D%225%22%20r%3D%225%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fcircle%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bj__flowchart-v2-circleStart%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%220%200%2010%2010%22%20refX%3D%22-1%22%20refY%3D%225%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Ccircle%20cx%3D%225%22%20cy%3D%225%22%20r%3D%225%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fcircle%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bj__flowchart-v2-crossEnd%22%20class%3D%22marker%20cross%20flowchart-v2%22%20viewBox%3D%220%200%2011%2011%22%20refX%3D%2212%22%20refY%3D%225.2%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%201%2C1%20l%209%2C9%20M%2010%2C1%20l%20-9%2C9%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%202%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bj__flowchart-v2-crossStart%22%20class%3D%22marker%20cross%20flowchart-v2%22%20viewBox%3D%220%200%2011%2011%22%20refX%3D%22-1%22%20refY%3D%225.2%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%201%2C1%20l%209%2C9%20M%2010%2C1%20l%20-9%2C9%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%202%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3C%2Fg%3E%3Cg%20class%3D%22subgraphs%22%3E%3C%2Fg%3E%3Cg%20class%3D%22nodes%22%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-A-0%22%20transform%3D%22translate\(361.14349365234375%2C%2046.29999923706055\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-103.0390625%22%20y%3D%22-34.29999923706055%22%20width%3D%22206.078125%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECustomer%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20submits%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Echeckout%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-B-1%22%20transform%3D%22translate\(361.14349365234375%2C%20154.89999771118164\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-127.96349334716797%22%20y%3D%22-34.29999923706055%22%20width%3D%22255.92698669433594%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EValidate%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20cart%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20and%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20delivery%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Edetails%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%20%20mermaid-decision%22%20id%3D%22flowchart-C-3%22%20transform%3D%22translate\(361.14349365234375%2C%20263.49999618530273\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-122.6015625%22%20y%3D%22-34.29999923706055%22%20width%3D%22245.203125%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EProducts%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20and%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20quantities%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Evalid%3F%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-D-5%22%20transform%3D%22translate\(129.40182495117188%2C%20433.7999954223633\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-117.40182495117188%22%20y%3D%22-30%22%20width%3D%22234.80364990234375%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EReturn%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20validation%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20error%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-E-7%22%20transform%3D%22translate\(402.01068115234375%2C%20433.7999954223633\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-115.20703125%22%20y%3D%22-30%22%20width%3D%22230.4140625%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EMongoDB%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20transaction%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-F-9%22%20transform%3D%22translate\(402.01068115234375%2C%20533.7999954223633\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-135.68305206298828%22%20y%3D%22-30%22%20width%3D%22271.36610412597656%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ERevalidate%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20prices%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20and%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20stock%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%20%20mermaid-decision%22%20id%3D%22flowchart-G-11%22%20transform%3D%22translate\(402.01068115234375%2C%20633.7999954223633\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-94.65625%22%20y%3D%22-30%22%20width%3D%22189.3125%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EStock%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20available%3F%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-H-13%22%20transform%3D%22translate\(211.66224416097003%2C%20799.7999954223633\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-99.8671875%22%20y%3D%22-30%22%20width%3D%22199.734375%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EAbort%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20transaction%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-I-15%22%20transform%3D%22translate\(433.56276448567706%2C%20799.7999954223633\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-82.03333282470703%22%20y%3D%22-30%22%20width%3D%22164.06666564941406%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECreate%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20order%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-J-17%22%20transform%3D%22translate\(433.56276448567706%2C%20899.7999954223633\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-87.55805206298828%22%20y%3D%22-30%22%20width%3D%22175.11610412597656%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EReserve%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20stock%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-K-19%22%20transform%3D%22translate\(433.56276448567706%2C%201004.0999946594238\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-125.859375%22%20y%3D%22-34.29999923706055%22%20width%3D%22251.71875%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECreate%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20pending%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20payment%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Erecord%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-L-21%22%20transform%3D%22translate\(433.56276448567706%2C%201108.3999938964844\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-107.94921875%22%20y%3D%22-30%22%20width%3D%22215.8984375%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECommit%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20transaction%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-M-23%22%20transform%3D%22translate\(433.56276448567706%2C%201208.3999938964844\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-134.58984375%22%20y%3D%22-30%22%20width%3D%22269.1796875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EInitialize%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20Paystack%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20payment%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%20%20mermaid-decision%22%20id%3D%22flowchart-N-25%22%20transform%3D%22translate\(433.56276448567706%2C%201308.3999938964844\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-124.72265625%22%20y%3D%22-30%22%20width%3D%22249.4453125%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EInitialization%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20successful%3F%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-O-27%22%20transform%3D%22translate\(234.5828145345052%2C%201478.699993133545\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-99.39791870117188%22%20y%3D%22-34.29999923706055%22%20width%3D%22198.79583740234375%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EReturn%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20payment%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Einitialization%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20error%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-P-29%22%20transform%3D%22translate\(475.13698323567706%2C%201478.699993133545\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-101.15625%22%20y%3D%22-34.29999923706055%22%20width%3D%22202.3125%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EReturn%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20payment%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Eauthorization%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20URL%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-Q-31%22%20transform%3D%22translate\(475.13698323567706%2C%201587.299991607666\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-112.078125%22%20y%3D%22-34.29999923706055%22%20width%3D%22224.15625%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECustomer%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20completes%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Epayment%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-R-33%22%20transform%3D%22translate\(475.13698323567706%2C%201695.899990081787\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-103.44140625%22%20y%3D%22-34.29999923706055%22%20width%3D%22206.8828125%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EVerify%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20payment%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20via%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Ewebhook%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20or%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20API%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%20%20mermaid-decision%22%20id%3D%22flowchart-S-35%22%20transform%3D%22translate\(475.13698323567706%2C%201800.1999893188477\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-101.2421875%22%20y%3D%22-30%22%20width%3D%22202.484375%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EPayment%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20verified%3F%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-T-37%22%20transform%3D%22translate\(247.61867014567054%2C%201970.4999885559082\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-116.06050109863281%22%20y%3D%22-34.29999923706055%22%20width%3D%22232.12100219726562%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EKeep%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20pending%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20or%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20mark%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Efailed%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-U-39%22%20transform%3D%22translate\(508.8843790690104%2C%201970.4999885559082\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-105.20520782470703%22%20y%3D%22-34.29999923706055%22%20width%3D%22210.41041564941406%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EAtomically%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20confirm%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Epayment%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20and%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20order%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-V-41%22%20transform%3D%22translate\(508.8843790690104%2C%202074.7999877929688\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-123.95703125%22%20y%3D%22-30%22%20width%3D%22247.9140625%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EFinalize%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20stock%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20deduction%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-W-43%22%20transform%3D%22translate\(508.8843790690104%2C%202179.0999870300293\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-108.12109375%22%20y%3D%22-34.29999923706055%22%20width%3D%22216.2421875%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EQueue%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20confirmation%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Enotification%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edges%20edgePaths%22%3E%3Cpath%20d%3D%22M361.14349365234375%2C80.5999984741211L361.14349365234375%2C108.5999984741211%22%20id%3D%22L_A_B_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_A_B_0%22%20data-points%3D%22W3sieCI6MzYxLjE0MzQ5MzY1MjM0Mzc1LCJ5Ijo4MC41OTk5OTg0NzQxMjExfSx7IngiOjM2MS4xNDM0OTM2NTIzNDM3NSwieSI6MTEyLjU5OTk5ODQ3NDEyMTF9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M361.14349365234375%2C189.1999969482422L361.14349365234375%2C217.1999969482422%22%20id%3D%22L_B_C_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_B_C_0%22%20data-points%3D%22W3sieCI6MzYxLjE0MzQ5MzY1MjM0Mzc1LCJ5IjoxODkuMTk5OTk2OTQ4MjQyMn0seyJ4IjozNjEuMTQzNDkzNjUyMzQzNzUsInkiOjIyMS4xOTk5OTY5NDgyNDIyfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M320.27630615234375%2C297.7999954223633L320.27630615234375%2C311.01703939706863Q320.27630615234375%2C312.7999954223633%20319.19051971471686%2C314.2142089847364L319.19051971471686%2C314.2142089847364Q318.10473327708996%2C315.6284225471095%20316.69051971471686%2C316.7142089847364L316.69051971471686%2C316.7142089847364Q315.27630615234375%2C317.7999954223633%20313.4933501270491%2C317.7999954223633L136.18478097646653%2C317.7999954223633Q134.40182495117188%2C317.7999954223633%20132.98761138879877%2C318.8857818599902L132.98761138879877%2C318.8857818599902Q131.5733978264257%2C319.97156829761707%20130.4876113887988%2C321.3857818599902L130.48761138879877%2C321.3857818599902Q129.40182495117188%2C322.7999954223633%20129.40182495117188%2C324.58295144765793L129.40182495117188%2C391.7999954223633%22%20id%3D%22L_C_D_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_C_D_0%22%20data-points%3D%22W3sieCI6MzIwLjI3NjMwNjE1MjM0Mzc1LCJ5IjoyOTcuNzk5OTk1NDIyMzYzM30seyJ4IjozMjAuMjc2MzA2MTUyMzQzNzUsInkiOjMxNy43OTk5OTU0MjIzNjMzfSx7IngiOjEyOS40MDE4MjQ5NTExNzE4OCwieSI6MzE3Ljc5OTk5NTQyMjM2MzN9LHsieCI6MTI5LjQwMTgyNDk1MTE3MTg4LCJ5IjozOTUuNzk5OTk1NDIyMzYzM31d%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M402.01068115234375%2C297.7999954223633L402.01068115234375%2C391.7999954223633%22%20id%3D%22L_C_E_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_C_E_0%22%20data-points%3D%22W3sieCI6NDAyLjAxMDY4MTE1MjM0Mzc1LCJ5IjoyOTcuNzk5OTk1NDIyMzYzM30seyJ4Ijo0MDIuMDEwNjgxMTUyMzQzNzUsInkiOjM5NS43OTk5OTU0MjIzNjMzfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M402.01068115234375%2C463.7999954223633L402.01068115234375%2C491.7999954223633%22%20id%3D%22L_E_F_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_E_F_0%22%20data-points%3D%22W3sieCI6NDAyLjAxMDY4MTE1MjM0Mzc1LCJ5Ijo0NjMuNzk5OTk1NDIyMzYzM30seyJ4Ijo0MDIuMDEwNjgxMTUyMzQzNzUsInkiOjQ5NS43OTk5OTU0MjIzNjMzfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M402.01068115234375%2C563.7999954223633L402.01068115234375%2C591.7999954223633%22%20id%3D%22L_F_G_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_F_G_0%22%20data-points%3D%22W3sieCI6NDAyLjAxMDY4MTE1MjM0Mzc1LCJ5Ijo1NjMuNzk5OTk1NDIyMzYzM30seyJ4Ijo0MDIuMDEwNjgxMTUyMzQzNzUsInkiOjU5NS43OTk5OTU0MjIzNjMzfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M370.45859781901044%2C663.7999954223633L370.4585978190104%2C676.7289276104977Q370.4585978190104%2C683.7999954223633%20363.3875300071449%2C683.7999954223633L218.44520018626469%2C683.7999954223633Q216.66224416097003%2C683.7999954223633%20215.24803059859693%2C684.8857818599902L215.24803059859693%2C684.8857818599902Q213.83381703622385%2C685.9715682976171%20212.74803059859696%2C687.3857818599902L212.74803059859693%2C687.3857818599902Q211.66224416097003%2C688.7999954223633%20211.66224416097003%2C690.5829514476579L211.66224416097003%2C757.7999954223633%22%20id%3D%22L_G_H_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_G_H_0%22%20data-points%3D%22W3sieCI6MzcwLjQ1ODU5NzgxOTAxMDQ0LCJ5Ijo2NjMuNzk5OTk1NDIyMzYzM30seyJ4IjozNzAuNDU4NTk3ODE5MDEwNCwieSI6NjgzLjc5OTk5NTQyMjM2MzN9LHsieCI6MjExLjY2MjI0NDE2MDk3MDAzLCJ5Ijo2ODMuNzk5OTk1NDIyMzYzM30seyJ4IjoyMTEuNjYyMjQ0MTYwOTcwMDMsInkiOjc2MS43OTk5OTU0MjIzNjMzfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M433.56276448567706%2C663.7999954223633L433.56276448567706%2C757.7999954223633%22%20id%3D%22L_G_I_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_G_I_0%22%20data-points%3D%22W3sieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5Ijo2NjMuNzk5OTk1NDIyMzYzM30seyJ4Ijo0MzMuNTYyNzY0NDg1Njc3MDYsInkiOjc2MS43OTk5OTU0MjIzNjMzfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M433.56276448567706%2C829.7999954223633L433.56276448567706%2C857.7999954223633%22%20id%3D%22L_I_J_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_I_J_0%22%20data-points%3D%22W3sieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5Ijo4MjkuNzk5OTk1NDIyMzYzM30seyJ4Ijo0MzMuNTYyNzY0NDg1Njc3MDYsInkiOjg2MS43OTk5OTU0MjIzNjMzfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M433.56276448567706%2C929.7999954223633L433.56276448567706%2C957.7999954223633%22%20id%3D%22L_J_K_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_J_K_0%22%20data-points%3D%22W3sieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5Ijo5MjkuNzk5OTk1NDIyMzYzM30seyJ4Ijo0MzMuNTYyNzY0NDg1Njc3MDYsInkiOjk2MS43OTk5OTU0MjIzNjMzfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M433.56276448567706%2C1038.3999938964844L433.56276448567706%2C1066.3999938964844%22%20id%3D%22L_K_L_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_K_L_0%22%20data-points%3D%22W3sieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5IjoxMDM4LjM5OTk5Mzg5NjQ4NDR9LHsieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5IjoxMDcwLjM5OTk5Mzg5NjQ4NDR9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M433.56276448567706%2C1138.3999938964844L433.56276448567706%2C1166.3999938964844%22%20id%3D%22L_L_M_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_L_M_0%22%20data-points%3D%22W3sieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5IjoxMTM4LjM5OTk5Mzg5NjQ4NDR9LHsieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5IjoxMTcwLjM5OTk5Mzg5NjQ4NDR9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M433.56276448567706%2C1238.3999938964844L433.56276448567706%2C1266.3999938964844%22%20id%3D%22L_M_N_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_M_N_0%22%20data-points%3D%22W3sieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5IjoxMjM4LjM5OTk5Mzg5NjQ4NDR9LHsieCI6NDMzLjU2Mjc2NDQ4NTY3NzA2LCJ5IjoxMjcwLjM5OTk5Mzg5NjQ4NDR9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M391.988545735677%2C1338.3999938964844L391.98854573567706%2C1351.3289260846188Q391.98854573567706%2C1358.3999938964844%20384.9174779238116%2C1358.3999938964844L241.36577055979984%2C1358.3999938964844Q239.5828145345052%2C1358.3999938964844%20238.16860097213208%2C1359.4857803341113L238.16860097213208%2C1359.4857803341113Q236.754387409759%2C1360.5715667717382%20235.6686009721321%2C1361.9857803341113L235.66860097213208%2C1361.9857803341113Q234.5828145345052%2C1363.3999938964844%20234.5828145345052%2C1365.182949921779L234.5828145345052%2C1432.3999938964844%22%20id%3D%22L_N_O_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_N_O_0%22%20data-points%3D%22W3sieCI6MzkxLjk4ODU0NTczNTY3NywieSI6MTMzOC4zOTk5OTM4OTY0ODQ0fSx7IngiOjM5MS45ODg1NDU3MzU2NzcwNiwieSI6MTM1OC4zOTk5OTM4OTY0ODQ0fSx7IngiOjIzNC41ODI4MTQ1MzQ1MDUyLCJ5IjoxMzU4LjM5OTk5Mzg5NjQ4NDR9LHsieCI6MjM0LjU4MjgxNDUzNDUwNTIsInkiOjE0MzYuMzk5OTkzODk2NDg0NH1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M475.1369832356771%2C1338.3999938964844L475.13698323567706%2C1432.3999938964844%22%20id%3D%22L_N_P_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_N_P_0%22%20data-points%3D%22W3sieCI6NDc1LjEzNjk4MzIzNTY3NzEsInkiOjEzMzguMzk5OTkzODk2NDg0NH0seyJ4Ijo0NzUuMTM2OTgzMjM1Njc3MDYsInkiOjE0MzYuMzk5OTkzODk2NDg0NH1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M475.13698323567706%2C1512.9999923706055L475.13698323567706%2C1540.9999923706055%22%20id%3D%22L_P_Q_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_P_Q_0%22%20data-points%3D%22W3sieCI6NDc1LjEzNjk4MzIzNTY3NzA2LCJ5IjoxNTEyLjk5OTk5MjM3MDYwNTV9LHsieCI6NDc1LjEzNjk4MzIzNTY3NzA2LCJ5IjoxNTQ0Ljk5OTk5MjM3MDYwNTV9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M475.13698323567706%2C1621.5999908447266L475.13698323567706%2C1649.5999908447266%22%20id%3D%22L_Q_R_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_Q_R_0%22%20data-points%3D%22W3sieCI6NDc1LjEzNjk4MzIzNTY3NzA2LCJ5IjoxNjIxLjU5OTk5MDg0NDcyNjZ9LHsieCI6NDc1LjEzNjk4MzIzNTY3NzA2LCJ5IjoxNjUzLjU5OTk5MDg0NDcyNjZ9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M475.13698323567706%2C1730.1999893188477L475.13698323567706%2C1758.1999893188477%22%20id%3D%22L_R_S_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_R_S_0%22%20data-points%3D%22W3sieCI6NDc1LjEzNjk4MzIzNTY3NzA2LCJ5IjoxNzMwLjE5OTk4OTMxODg0Nzd9LHsieCI6NDc1LjEzNjk4MzIzNTY3NzA2LCJ5IjoxNzYyLjE5OTk4OTMxODg0Nzd9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M441.38958740234375%2C1830.1999893188477L441.38958740234375%2C1843.417033293553Q441.38958740234375%2C1845.1999893188477%20440.30380096471686%2C1846.6142028812208L440.30380096471686%2C1846.6142028812208Q439.21801452708996%2C1848.0284164435939%20437.80380096471686%2C1849.1142028812208L437.80380096471686%2C1849.1142028812208Q436.38958740234375%2C1850.1999893188477%20434.6066313770491%2C1850.1999893188477L254.4016261709652%2C1850.1999893188477Q252.61867014567054%2C1850.1999893188477%20251.20445658329743%2C1851.2857757564745L251.20445658329743%2C1851.2857757564745Q249.79024302092435%2C1852.3715621941014%20248.70445658329746%2C1853.7857757564745L248.70445658329743%2C1853.7857757564745Q247.61867014567054%2C1855.1999893188477%20247.61867014567054%2C1856.9829453441423L247.61867014567054%2C1924.1999893188477%22%20id%3D%22L_S_T_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_S_T_0%22%20data-points%3D%22W3sieCI6NDQxLjM4OTU4NzQwMjM0Mzc1LCJ5IjoxODMwLjE5OTk4OTMxODg0Nzd9LHsieCI6NDQxLjM4OTU4NzQwMjM0Mzc1LCJ5IjoxODUwLjE5OTk4OTMxODg0Nzd9LHsieCI6MjQ3LjYxODY3MDE0NTY3MDU0LCJ5IjoxODUwLjE5OTk4OTMxODg0Nzd9LHsieCI6MjQ3LjYxODY3MDE0NTY3MDU0LCJ5IjoxOTI4LjE5OTk4OTMxODg0Nzd9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M508.8843790690104%2C1830.1999893188477L508.8843790690104%2C1924.1999893188477%22%20id%3D%22L_S_U_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_S_U_0%22%20data-points%3D%22W3sieCI6NTA4Ljg4NDM3OTA2OTAxMDQsInkiOjE4MzAuMTk5OTg5MzE4ODQ3N30seyJ4Ijo1MDguODg0Mzc5MDY5MDEwNCwieSI6MTkyOC4xOTk5ODkzMTg4NDc3fV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M508.8843790690104%2C2004.7999877929688L508.8843790690104%2C2032.7999877929688%22%20id%3D%22L_U_V_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_U_V_0%22%20data-points%3D%22W3sieCI6NTA4Ljg4NDM3OTA2OTAxMDQsInkiOjIwMDQuNzk5OTg3NzkyOTY4OH0seyJ4Ijo1MDguODg0Mzc5MDY5MDEwNCwieSI6MjAzNi43OTk5ODc3OTI5Njg4fV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M508.8843790690104%2C2104.7999877929688L508.8843790690104%2C2132.7999877929688%22%20id%3D%22L_V_W_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_V_W_0%22%20data-points%3D%22W3sieCI6NTA4Ljg4NDM3OTA2OTAxMDQsInkiOjIxMDQuNzk5OTg3NzkyOTY4OH0seyJ4Ijo1MDguODg0Mzc5MDY5MDEwNCwieSI6MjEzNi43OTk5ODc3OTI5Njg4fV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bj__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabels%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_A_B_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_B_C_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(129.14791870117188%2C%20350.7999954223633\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_C_D_0%22%20transform%3D%22translate\(-8.74609375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2241.4921875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ENo%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(401.60443115234375%2C%20350.7999954223633\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_C_E_0%22%20transform%3D%22translate\(-11.09375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2246.1875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EYes%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_E_F_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_F_G_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(211.40833791097003%2C%20716.7999954223633\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_G_H_0%22%20transform%3D%22translate\(-8.74609375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2241.4921875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ENo%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(433.15651448567706%2C%20716.7999954223633\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_G_I_0%22%20transform%3D%22translate\(-11.09375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2246.1875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EYes%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_I_J_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_J_K_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_K_L_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_L_M_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_M_N_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(234.3289082845052%2C%201391.3999938964844\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_N_O_0%22%20transform%3D%22translate\(-8.74609375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2241.4921875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ENo%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(474.73073323567706%2C%201391.3999938964844\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_N_P_0%22%20transform%3D%22translate\(-11.09375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2246.1875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EYes%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_P_Q_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_Q_R_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_R_S_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(247.36476389567054%2C%201883.1999893188477\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_S_T_0%22%20transform%3D%22translate\(-8.74609375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2241.4921875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ENo%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(508.4781290690104%2C%201883.1999893188477\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_S_U_0%22%20transform%3D%22translate\(-11.09375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2246.1875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EYes%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_U_V_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_V_W_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fsvg%3E)

The implementation must handle a payment initialization failure after the order and reservation have been committed. The order should remain recoverable, and stock must eventually be released if payment is not completed.

Use an idempotency key for checkout creation so that a retried request does not create duplicate orders.

## 8.2 Paystack payment processing

Payment verification must never rely solely on a frontend redirect or success query parameter.

The backend should:

1. Create the order and reserve stock.

2. Create a pending payment record.

3. Initialize the payment with Paystack.

4. Return the authorization URL to the frontend.

5. Receive the Paystack webhook.

6. Verify the webhook signature using the raw request body and the configured Paystack secret.

7. Independently verify the payment reference with Paystack when necessary.

8. Check the expected amount, currency, reference and order association.

9. Atomically transition the payment and order to their appropriate states.

10. Record an outbox event for notifications and other post-payment work.

Payment processing must be idempotent. Repeated webhooks, delayed delivery and client retries must not result in duplicate stock deductions, duplicate orders or duplicate confirmations.

If a payment arrives after its reservation has expired, do not blindly fulfil the order. Apply a defined recovery policy, such as attempting to reserve stock again or initiating a refund when fulfilment is impossible.

## 8.3 Inventory reservation

Inventory reservation should happen in the same MongoDB transaction as order creation.

Use conditional updates to prevent concurrent checkout requests from reserving more stock than is available. A reservation must have an expiry time or another defined release condition.

A background task should periodically identify expired reservations and release them safely. The release operation must be idempotent.

For an initial implementation, a reservation can be represented by a quantity on the inventory document plus an associated order and expiry record. If reservation complexity grows, introduce a dedicated reservation collection.

## 8.4 Consultation booking

Consultation bookings require protection against concurrent slot allocation.

Recommended workflow:

1. Customer requests available slots.

2. Backend calculates availability.

3. Customer selects a slot and submits booking details.

4. Backend validates the service, slot, customer and any payment requirements.

5. Backend atomically checks and reserves the slot.

6. Backend creates the booking.

7. Backend records an event for confirmation and reminders.

A unique slot allocation constraint or conditional update must prevent two customers from booking the same exclusive slot. The availability endpoint is advisory; booking creation is authoritative.

# 9. Background processing

Asynq + Redis

Asynq handles work that should not block an HTTP request. Redis is used as the queue backend, while MongoDB remains the authoritative store for business data.

### Background tasks

| Task                            | Trigger                      | Behaviour                          |
| ------------------------------- | ---------------------------- | ---------------------------------- |
| `email:order_confirmation`      | Order payment confirmed      | Send order confirmation            |
| `email:booking_confirmation`    | Booking created or confirmed | Send booking details               |
| `email:password_reset`          | Reset requested              | Send reset instructions            |
| `email:booking_reminder`        | Scheduled reminder           | Notify customer                    |
| `inventory:release_reservation` | Reservation expires          | Release reserved stock             |
| `payment:reconcile`             | Scheduled                    | Reconcile pending payments         |
| `outbox:publish`                | Outbox poller                | Publish pending events             |
| `media:cleanup`                 | Scheduled                    | Clean up eligible orphaned uploads |

All jobs must be idempotent. Configure bounded retries, exponential backoff and a failure-handling strategy. A permanently failing notification must not cause an otherwise successful payment workflow to be repeated.

## 9.1 Transactional outbox

MongoDB and Redis cannot participate in one shared transaction. Therefore, important events should be persisted in MongoDB within the same transaction as the business change that generates them.

For example, when an order is paid:

* Update the payment record.

* Update the order.

* Finalize the inventory operation.

* Insert an `order.paid` outbox event.

* Commit all changes together.

The outbox publisher subsequently reads pending events and dispatches the corresponding Asynq jobs.

```
MongoDB transaction
    ├── Update payment
    ├── Update order
    ├── Finalize inventory
    └── Insert outbox event
             |
             v
        Commit transaction
             |
             v
       Outbox publisher
             |
             v
          Redis
             |
             v
        Asynq worker
             |
             v
      External side effect
```

The publisher must tolerate crashes and duplicate publication. Workers must therefore be idempotent, using the event or business-operation identifier to avoid duplicate side effects.

# 10. External integrations

| Integration   | Responsibility             | Implementation                     |
| ------------- | -------------------------- | ---------------------------------- |
| MongoDB Atlas | Persistent data            | Official Go driver                 |
| Redis         | Cache and job queues       | `go-redis`                         |
| Paystack      | Payment processing         | Isolated payment client            |
| ImageKit      | Image uploads and delivery | Server-generated upload signatures |
| Resend        | Transactional email        | Isolated mail client               |

## 10.1 ImageKit uploads

The backend should generate short-lived upload authentication parameters. The browser can then upload images directly to ImageKit.

The backend must validate the resulting asset metadata before associating it with a product. Do not accept arbitrary external image URLs as trusted admin uploads.

## 10.2 Resend email

Email sending should use a dedicated service with templates and typed inputs. Avoid building HTML email bodies directly inside business handlers.

The notification worker should record delivery attempts and handle provider failures without affecting core business transactions.

## 10.3 External-service failure handling

* Apply request timeouts to all external calls.

* Use bounded retries for transient failures.

* Do not automatically retry non-idempotent provider operations without a safe idempotency strategy.

* Log provider failures with correlation identifiers.

* Keep provider secrets out of logs.

* Distinguish between temporary provider failure and definitive business failure.

# 11. Caching strategy

Redis may be used for frequently accessed data, but MongoDB remains the source of truth.

Suitable initial cache candidates:

* Published product listings.

* Product categories.

* Public consultation service definitions.

* Other read-heavy, relatively stable public data.

Use short, explicit TTLs and invalidate affected cache keys after successful updates.

Do not rely on cached data for payment verification, final checkout pricing, stock reservation, booking allocation or authorization.

For a small initial deployment, keep caching limited. Introduce it where measurements show a meaningful benefit rather than adding cache complexity to every repository.

# 12. Security architecture

Security controls should be applied consistently across the API.

| Area             | Requirement                                                |
| ---------------- | ---------------------------------------------------------- |
| Authentication   | Short-lived JWT access tokens                              |
| Passwords        | bcrypt with benchmarked cost                               |
| Authorization    | Server-side role and permission checks                     |
| Transport        | HTTPS                                                      |
| CORS             | Exact approved frontend origins                            |
| Rate limiting    | Login, password reset, checkout and other sensitive routes |
| Input validation | Validate all untrusted input                               |
| Database access  | Least-privilege database credentials                       |
| Secrets          | Environment or secret manager; never commit                |
| Payments         | Webhook signature verification and reference verification  |
| File uploads     | Short-lived signatures and metadata validation             |
| Logging          | Structured logs with sensitive values redacted             |
| Audit            | Record sensitive administrative actions                    |
| Dependencies     | Regular updates and vulnerability checks                   |

Additional requirements:

* Never log passwords, access tokens, refresh tokens, payment secrets or reset tokens.

* Avoid returning whether an email address exists in password-reset responses.

* Apply ownership checks when customers retrieve or modify their orders and bookings.

* Enforce limits on request size, pagination size and file metadata.

* Use MongoDB credentials scoped to the application's required database.

* Restrict Redis access to the application's private network.

* Do not expose internal infrastructure ports publicly.

* Validate webhook signatures against the raw request body before processing events.

# 13. Observability and error handling

## 13.1 Logging

Use Go's standard `log/slog` for structured logging.

Every HTTP request should have a request ID or correlation ID. Propagate relevant identifiers into asynchronous jobs and external-service calls.

Useful log fields include:

* `request_id`

* `user_id`, where appropriate

* `order_id`

* `payment_reference`

* `booking_id`

* `job_id`

* `module`

* `operation`

* `duration_ms`

* `error_code`

Avoid logging entire customer records or payment payloads.

## 13.2 Health checks

Expose separate endpoints for liveness and readiness.

| Endpoint            | Purpose                                    |
| ------------------- | ------------------------------------------ |
| `GET /health/live`  | Confirms the API process is running        |
| `GET /health/ready` | Checks required dependencies and readiness |

Readiness should verify that required dependencies are available without exposing credentials, database details or other sensitive information.

Workers should also emit operational logs for job completion, retries and failures.

## 13.3 Error handling

Use centralized error mapping to convert application errors into consistent HTTP responses.

Domain errors should represent meaningful business conditions, such as:

* `ErrProductNotFound`

* `ErrInsufficientStock`

* `ErrOrderNotPayable`

* `ErrPaymentAlreadyProcessed`

* `ErrSlotUnavailable`

* `ErrUnauthorized`

* `ErrForbidden`

Handlers should map these to appropriate HTTP responses without leaking implementation details.

# 14. Configuration management

Use environment variables for deployment-specific configuration and validate required settings at application startup.

Example `.env.example`:

dotenv

```
APP_ENV=development
APP_NAME=denisco-api
API_PORT=4000
API_BASE_URL=http://localhost:4000

WEB_ORIGIN=http://localhost:3000
ADMIN_ORIGIN=http://localhost:3001

MONGODB_URI=
MONGODB_DATABASE=denisco

REDIS_URL=redis://localhost:6379/0

JWT_ACCESS_SECRET=
JWT_ACCESS_TTL=15m
REFRESH_TOKEN_TTL=720h

BCRYPT_COST=12

PAYSTACK_SECRET_KEY=
PAYSTACK_BASE_URL=https://api.paystack.co

IMAGEKIT_PUBLIC_KEY=
IMAGEKIT_PRIVATE_KEY=
IMAGEKIT_URL_ENDPOINT=

RESEND_API_KEY=
EMAIL_FROM=

LOG_LEVEL=info
```

These are configuration examples, not production credentials. The application should fail fast when mandatory settings are absent or invalid.

Use separate environment configurations for local development, testing and production. Never use production secrets in local development or test fixtures.

# 15. Deployment architecture

Single VPS

2 vCPU / 2 GB RAM

The backend will run on the VPS alongside the frontend applications. MongoDB remains external.

Diagram options

![](data\:image/svg+xml;utf8,%3Csvg%20id%3D%22mermaid-_r_1bk_%22%20width%3D%22635.3366088867188%22%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20class%3D%22flowchart%22%20height%3D%22861.7999877929688%22%20viewBox%3D%224%204%20635.3366088867188%20861.7999877929688%22%20role%3D%22graphics-document%20document%22%20aria-roledescription%3D%22flowchart-v2%22%3E%3Cstyle%3E%23mermaid-_r_1bk_%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A14px%3Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%40keyframes%20edge-animation-frame%7Bfrom%7Bstroke-dashoffset%3A0%3B%7D%7D%40keyframes%20dash%7Bto%7Bstroke-dashoffset%3A0%3B%7D%7D%23mermaid-_r_1bk_%20.edge-animation-slow%7Bstroke-dasharray%3A9%2C5!important%3Bstroke-dashoffset%3A900%3Banimation%3Adash%2050s%20linear%20infinite%3Bstroke-linecap%3Around%3B%7D%23mermaid-_r_1bk_%20.edge-animation-fast%7Bstroke-dasharray%3A9%2C5!important%3Bstroke-dashoffset%3A900%3Banimation%3Adash%2020s%20linear%20infinite%3Bstroke-linecap%3Around%3B%7D%23mermaid-_r_1bk_%20.error-icon%7Bfill%3Argb\(249%2C%20249%2C%20249\)%3B%7D%23mermaid-_r_1bk_%20.error-text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3Bstroke%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20.edge-thickness-normal%7Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bk_%20.edge-thickness-thick%7Bstroke-width%3A3.5px%3B%7D%23mermaid-_r_1bk_%20.edge-pattern-solid%7Bstroke-dasharray%3A0%3B%7D%23mermaid-_r_1bk_%20.edge-thickness-invisible%7Bstroke-width%3A0%3Bfill%3Anone%3B%7D%23mermaid-_r_1bk_%20.edge-pattern-dashed%7Bstroke-dasharray%3A3%3B%7D%23mermaid-_r_1bk_%20.edge-pattern-dotted%7Bstroke-dasharray%3A2%3B%7D%23mermaid-_r_1bk_%20.marker%7Bfill%3Argb\(93%2C%2093%2C%2093\)%3Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bk_%20.marker.cross%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bk_%20svg%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A14px%3B%7D%23mermaid-_r_1bk_%20p%7Bmargin%3A0%3B%7D%23mermaid-_r_1bk_%20.label%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20.cluster-label%20text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20.cluster-label%20span%7Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20.cluster-label%20span%20p%7Bbackground-color%3Atransparent%3B%7D%23mermaid-_r_1bk_%20.label%20text%2C%23mermaid-_r_1bk_%20span%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20.node%20rect%2C%23mermaid-_r_1bk_%20.node%20circle%2C%23mermaid-_r_1bk_%20.node%20ellipse%2C%23mermaid-_r_1bk_%20.node%20polygon%2C%23mermaid-_r_1bk_%20.node%20path%7Bfill%3Argb\(222%2C%20234%2C%20251\)%3Bstroke%3Argb\(83%2C%20154%2C%20248\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bk_%20.rough-node%20.label%20text%2C%23mermaid-_r_1bk_%20.node%20.label%20text%2C%23mermaid-_r_1bk_%20.image-shape%20.label%2C%23mermaid-_r_1bk_%20.icon-shape%20.label%7Btext-anchor%3Amiddle%3B%7D%23mermaid-_r_1bk_%20.node%20.katex%20path%7Bfill%3A%23000%3Bstroke%3A%23000%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bk_%20.rough-node%20.label%2C%23mermaid-_r_1bk_%20.node%20.label%2C%23mermaid-_r_1bk_%20.image-shape%20.label%2C%23mermaid-_r_1bk_%20.icon-shape%20.label%7Btext-align%3Acenter%3B%7D%23mermaid-_r_1bk_%20.node.clickable%7Bcursor%3Apointer%3B%7D%23mermaid-_r_1bk_%20.root%20.anchor%20path%7Bfill%3Argb\(93%2C%2093%2C%2093\)!important%3Bstroke-width%3A0%3Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bk_%20.arrowheadPath%7Bfill%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bk_%20.edgePath%20.path%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3Bstroke-width%3A2.0px%3B%7D%23mermaid-_r_1bk_%20.flowchart-link%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3Bfill%3Anone%3B%7D%23mermaid-_r_1bk_%20.edgeLabel%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Btext-align%3Acenter%3B%7D%23mermaid-_r_1bk_%20.edgeLabel%20p%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bk_%20.edgeLabel%20rect%7Bopacity%3A0.5%3Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bfill%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bk_%20.labelBkg%7Bbackground-color%3Argba\(252%2C%20252%2C%20252%2C%200.5\)%3B%7D%23mermaid-_r_1bk_%20.cluster%20rect%7Bfill%3Argb\(249%2C%20249%2C%20249\)%3Bstroke%3Argba\(0%2C%200%2C%200%2C%200.05\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bk_%20.cluster%20text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20.cluster%20span%7Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20div.mermaidTooltip%7Bposition%3Aabsolute%3Btext-align%3Acenter%3Bmax-width%3A200px%3Bpadding%3A2px%3Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A12px%3Bbackground%3Argb\(249%2C%20249%2C%20249\)%3Bborder%3A1px%20solid%20rgba\(0%2C%200%2C%200%2C%200.05\)%3Bborder-radius%3A2px%3Bpointer-events%3Anone%3Bz-index%3A100%3B%7D%23mermaid-_r_1bk_%20.flowchartTitleText%7Btext-anchor%3Amiddle%3Bfont-size%3A18px%3Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bk_%20rect.text%7Bfill%3Anone%3Bstroke-width%3A0%3B%7D%23mermaid-_r_1bk_%20.icon-shape%2C%23mermaid-_r_1bk_%20.image-shape%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Btext-align%3Acenter%3B%7D%23mermaid-_r_1bk_%20.icon-shape%20p%2C%23mermaid-_r_1bk_%20.image-shape%20p%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bpadding%3A2px%3B%7D%23mermaid-_r_1bk_%20.icon-shape%20rect%2C%23mermaid-_r_1bk_%20.image-shape%20rect%7Bopacity%3A0.5%3Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bfill%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bk_%20.label-icon%7Bdisplay%3Ainline-block%3Bheight%3A1em%3Boverflow%3Avisible%3Bvertical-align%3A-0.125em%3B%7D%23mermaid-_r_1bk_%20.node%20.label-icon%20path%7Bfill%3AcurrentColor%3Bstroke%3Arevert%3Bstroke-width%3Arevert%3B%7D%23mermaid-_r_1bk_%20.node%20text%7Bfont-size%3A16px%3Bfont-weight%3A600%3Bletter-spacing%3A-0.32px%3Bfill%3A%23004f99%3B%7D%23mermaid-_r_1bk_%20.edgeLabels%20text%7Bfont-size%3A13px%3Bfont-weight%3A600%3Bletter-spacing%3A-0.08px%3Bfill%3A%23004f99%3B%7D%23mermaid-_r_1bk_%20.node%20tspan%5Bfont-weight%3D%22normal%22%5D%2C%23mermaid-_r_1bk_%20.edgeLabels%20tspan%5Bfont-weight%3D%22normal%22%5D%7Bfont-weight%3A600%3B%7D%23mermaid-_r_1bk_%20.edgeLabel%20.label%20rect%7Bopacity%3A1%3Brx%3A13px%3Bry%3A13px%3Bfill%3A%23f5faff%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bk_%20.node%20rect%2C%23mermaid-_r_1bk_%20.node%20circle%2C%23mermaid-_r_1bk_%20.node%20ellipse%2C%23mermaid-_r_1bk_%20.node%20polygon%2C%23mermaid-_r_1bk_%20.node%20path%7Bfill%3Argb\(229%2C%20243%2C%20255\)%3Bstroke%3Argba\(0%2C%200%2C%200%2C%200.1\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bk_%20.node%20rect%7Brx%3A16px%3Bry%3A16px%3B%7D%23mermaid-_r_1bk_%20.node.mermaid-decision%20.label-container%7Bfill%3A%23f5faff%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-dasharray%3A2%202%3B%7D%23mermaid-_r_1bk_%20.edgePaths%20.flowchart-link%7Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-width%3A1px%3Bstroke-linecap%3Around%3Bstroke-linejoin%3Around%3B%7D%23mermaid-_r_1bk_%20.marker%7Bfill%3Argb\(206%2C%20219%2C%20229\)%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3B%7D%23mermaid-_r_1bk_%20.node%7Bcolor-scheme%3Alight%3B%7D%23mermaid-_r_1bk_%20%3Aroot%7B--mermaid-font-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3B%7D%3C%2Fstyle%3E%3Cg%3E%3Cmarker%20id%3D%22mermaid-_r_1bk__flowchart-v2-pointEnd%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%22-5%20-5%2010%2010%22%20refX%3D%220%22%20refY%3D%220%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2210%22%20markerHeight%3D%2210%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%200%200%20L%204%200%20M%200.8180194846605362%20-3.181980515339464%20L%204%200%20L%200.8180194846605362%203.181980515339464%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%20none%3B%20fill%3A%20none%3B%20stroke-linecap%3A%20round%3B%20stroke-linejoin%3A%20round%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bk__flowchart-v2-pointStart%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%22-5%20-5%2010%2010%22%20refX%3D%220%22%20refY%3D%220%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2210%22%20markerHeight%3D%2210%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%200%200%20L%20-4%200%20M%20-0.8180194846605362%20-3.181980515339464%20L%20-4%200%20L%20-0.8180194846605362%203.181980515339464%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%20none%3B%20fill%3A%20none%3B%20stroke-linecap%3A%20round%3B%20stroke-linejoin%3A%20round%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bk__flowchart-v2-circleEnd%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%220%200%2010%2010%22%20refX%3D%2211%22%20refY%3D%225%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Ccircle%20cx%3D%225%22%20cy%3D%225%22%20r%3D%225%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fcircle%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bk__flowchart-v2-circleStart%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%220%200%2010%2010%22%20refX%3D%22-1%22%20refY%3D%225%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Ccircle%20cx%3D%225%22%20cy%3D%225%22%20r%3D%225%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fcircle%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bk__flowchart-v2-crossEnd%22%20class%3D%22marker%20cross%20flowchart-v2%22%20viewBox%3D%220%200%2011%2011%22%20refX%3D%2212%22%20refY%3D%225.2%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%201%2C1%20l%209%2C9%20M%2010%2C1%20l%20-9%2C9%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%202%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bk__flowchart-v2-crossStart%22%20class%3D%22marker%20cross%20flowchart-v2%22%20viewBox%3D%220%200%2011%2011%22%20refX%3D%22-1%22%20refY%3D%225.2%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%201%2C1%20l%209%2C9%20M%2010%2C1%20l%20-9%2C9%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%202%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3C%2Fg%3E%3Cg%20class%3D%22subgraphs%22%3E%3C%2Fg%3E%3Cg%20class%3D%22nodes%22%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-U-0%22%20transform%3D%22translate\(468.32643127441406%2C%2046.29999923706055\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-91.5%22%20y%3D%22-34.29999923706055%22%20width%3D%22183%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECustomers%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20and%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3Eadministrators%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-CF-1%22%20transform%3D%22translate\(468.32643127441406%2C%20150.5999984741211\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-73.8046875%22%20y%3D%22-30%22%20width%3D%22147.609375%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECloudflare%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-C-3%22%20transform%3D%22translate\(468.32643127441406%2C%20254.89999771118164\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-111.12057495117188%22%20y%3D%22-34.29999923706055%22%20width%3D%22222.24114990234375%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECaddy%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20reverse%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20proxy%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EHTTPS%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-WEB-5%22%20transform%3D%22translate\(111.1328125%2C%20379.1999969482422\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-99.1328125%22%20y%3D%22-30%22%20width%3D%22198.265625%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ECustomer%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20Next.js%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-ADMIN-7%22%20transform%3D%22translate\(336.796875%2C%20379.1999969482422\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-86.53125%22%20y%3D%22-30%22%20width%3D%22173.0625%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EAdmin%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20Next.js%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-API-9%22%20transform%3D%22translate\(523.88671875%2C%20379.1999969482422\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-60.55859375%22%20y%3D%22-30%22%20width%3D%22121.1171875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EGo%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20API%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-M-11%22%20transform%3D%22translate\(285.79327596028645%2C%20819.1999969482422\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-92.19921875%22%20y%3D%22-30%22%20width%3D%22184.3984375%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EMongoDB%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20Atlas%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-R-13%22%20transform%3D%22translate\(345.2189376831055%2C%20619.1999969482422\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-56.18359375%22%20y%3D%22-30%22%20width%3D%22112.3671875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ERedis%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-W-15%22%20transform%3D%22translate\(345.2189376831055%2C%20719.1999969482422\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-86.07776641845703%22%20y%3D%22-30%22%20width%3D%22172.15553283691406%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EAsynq%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20worker%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-O-17%22%20transform%3D%22translate\(363.9468022664388%2C%20519.1999969482422\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-98.72797012329102%22%20y%3D%22-30%22%20width%3D%22197.45594024658203%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EOutbox%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20publisher%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-P-23%22%20transform%3D%22translate\(524.6645299275716%2C%20823.4999961853027\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-106.67203521728516%22%20y%3D%22-34.29999923706055%22%20width%3D%22213.3440704345703%22%20height%3D%2268.5999984741211%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-18.299999237060547\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EPaystack%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20%2F%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20Resend%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20%2F%3C%2Ftspan%3E%3C%2Ftspan%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%221em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EImageKit%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edges%20edgePaths%22%3E%3Cpath%20d%3D%22M468.32643127441406%2C80.5999984741211L468.32643127441406%2C108.5999984741211%22%20id%3D%22L_U_CF_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_U_CF_0%22%20data-points%3D%22W3sieCI6NDY4LjMyNjQzMTI3NDQxNDA2LCJ5Ijo4MC41OTk5OTg0NzQxMjExfSx7IngiOjQ2OC4zMjY0MzEyNzQ0MTQwNiwieSI6MTEyLjU5OTk5ODQ3NDEyMTF9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M468.32643127441406%2C180.5999984741211L468.32643127441406%2C208.5999984741211%22%20id%3D%22L_CF_C_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_CF_C_0%22%20data-points%3D%22W3sieCI6NDY4LjMyNjQzMTI3NDQxNDA2LCJ5IjoxODAuNTk5OTk4NDc0MTIxMX0seyJ4Ijo0NjguMzI2NDMxMjc0NDE0MDYsInkiOjIxMi41OTk5OTg0NzQxMjExfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M412.76614379882807%2C289.1999969482422L412.7661437988281%2C302.1289291363767Q412.7661437988281%2C309.1999969482422%20405.69507598696265%2C309.1999969482422L117.91576852529465%2C309.1999969482422Q116.1328125%2C309.1999969482422%20114.71859893762691%2C310.2857833858691L114.71859893762691%2C310.2857833858691Q113.30438537525382%2C311.371569823496%20112.21859893762692%2C312.7857833858691L112.21859893762691%2C312.7857833858691Q111.1328125%2C314.1999969482422%20111.1328125%2C315.98295297353684L111.1328125%2C337.1999969482422%22%20id%3D%22L_C_WEB_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_C_WEB_0%22%20data-points%3D%22W3sieCI6NDEyLjc2NjE0Mzc5ODgyODA3LCJ5IjoyODkuMTk5OTk2OTQ4MjQyMn0seyJ4Ijo0MTIuNzY2MTQzNzk4ODI4MSwieSI6MzA5LjE5OTk5Njk0ODI0MjJ9LHsieCI6MTExLjEzMjgxMjUsInkiOjMwOS4xOTk5OTY5NDgyNDIyfSx7IngiOjExMS4xMzI4MTI1LCJ5IjozNDEuMTk5OTk2OTQ4MjQyMn1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M468.32643127441406%2C289.1999969482422L468.32643127441406%2C322.41704092294754Q468.32643127441406%2C324.1999969482422%20467.24064483678717%2C325.6142105106153L467.24064483678717%2C325.6142105106153Q466.1548583991603%2C327.0284240729884%20464.74064483678717%2C328.1142105106153L464.74064483678717%2C328.1142105106153Q463.32643127441406%2C329.1999969482422%20461.5434752491194%2C329.1999969482422L343.57983102529465%2C329.1999969482422Q341.796875%2C329.1999969482422%20340.3826614376269%2C330.2857833858691L340.3826614376269%2C330.2857833858691Q338.9684478752538%2C331.371569823496%20337.8826614376269%2C332.7857833858691L337.8826614376269%2C332.7857833858691Q336.796875%2C334.1999969482422%20336.796875%2C335.98295297353684L336.796875%2C339.1999969482422%22%20id%3D%22L_C_ADMIN_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_C_ADMIN_0%22%20data-points%3D%22W3sieCI6NDY4LjMyNjQzMTI3NDQxNDA2LCJ5IjoyODkuMTk5OTk2OTQ4MjQyMn0seyJ4Ijo0NjguMzI2NDMxMjc0NDE0MDYsInkiOjMyOS4xOTk5OTY5NDgyNDIyfSx7IngiOjMzNi43OTY4NzUsInkiOjMyOS4xOTk5OTY5NDgyNDIyfSx7IngiOjMzNi43OTY4NzUsInkiOjM0My4xOTk5OTY5NDgyNDIyfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M523.88671875%2C289.1999969482422L523.88671875%2C337.1999969482422%22%20id%3D%22L_C_API_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_C_API_0%22%20data-points%3D%22W3sieCI6NTIzLjg4NjcxODc1LCJ5IjoyODkuMTk5OTk2OTQ4MjQyMn0seyJ4Ijo1MjMuODg2NzE4NzUsInkiOjM0MS4xOTk5OTY5NDgyNDIyfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M487.5515625%2C409.1999969482422L487.5515625%2C422.41704092294754Q487.5515625%2C424.1999969482422%20486.4657760623731%2C425.6142105106153L486.4657760623731%2C425.6142105106153Q485.3799896247462%2C427.0284240729884%20483.9657760623731%2C428.1142105106153L483.9657760623731%2C428.1142105106153Q482.5515625%2C429.1999969482422%20480.76860647470534%2C429.1999969482422L230.0017919831397%2C429.1999969482422Q228.21883595784504%2C429.1999969482422%20226.80462239547194%2C430.2857833858691L226.80462239547194%2C430.2857833858691Q225.39040883309886%2C431.371569823496%20224.30462239547197%2C432.7857833858691L224.30462239547194%2C432.7857833858691Q223.21883595784504%2C434.1999969482422%20223.21883595784504%2C435.98295297353684L223.21883595784504%2C519.1999969482422L223.21883595784504%2C619.1999969482422L223.21883595784504%2C719.1999969482422L223.21883595784504%2C762.4170409229475Q223.21883595784504%2C764.1999969482422%20224.30462239547194%2C765.6142105106153L224.30462239547197%2C765.6142105106153Q225.39040883309886%2C767.0284240729884%20226.80462239547194%2C768.1142105106153L226.80462239547194%2C768.1142105106153Q228.21883595784504%2C769.1999969482422%20230.0017919831397%2C769.1999969482422L248.27724701832514%2C769.1999969482422Q250.0602030436198%2C769.1999969482422%20251.47441660599287%2C770.2857833858691L251.47441660599287%2C770.2857833858691Q252.88863016836598%2C771.371569823496%20253.97441660599287%2C772.7857833858691L253.97441660599287%2C772.7857833858691Q255.0602030436198%2C774.1999969482422%20255.0602030436198%2C775.9829529735368L255.0602030436198%2C779.1999969482422%22%20id%3D%22L_API_M_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_API_M_0%22%20data-points%3D%22W3sieCI6NDg3LjU1MTU2MjUsInkiOjQwOS4xOTk5OTY5NDgyNDIyfSx7IngiOjQ4Ny41NTE1NjI1LCJ5Ijo0MjkuMTk5OTk2OTQ4MjQyMn0seyJ4IjoyMjMuMjE4ODM1OTU3ODQ1MDQsInkiOjQyOS4xOTk5OTY5NDgyNDIyfSx7IngiOjIyMy4yMTg4MzU5NTc4NDUwNCwieSI6NTE5LjE5OTk5Njk0ODI0MjJ9LHsieCI6MjIzLjIxODgzNTk1Nzg0NTA0LCJ5Ijo2MTkuMTk5OTk2OTQ4MjQyMn0seyJ4IjoyMjMuMjE4ODM1OTU3ODQ1MDQsInkiOjcxOS4xOTk5OTY5NDgyNDIyfSx7IngiOjIyMy4yMTg4MzU5NTc4NDUwNCwieSI6NzY5LjE5OTk5Njk0ODI0MjJ9LHsieCI6MjU1LjA2MDIwMzA0MzYxOTgsInkiOjc2OS4xOTk5OTY5NDgyNDIyfSx7IngiOjI1NS4wNjAyMDMwNDM2MTk4LCJ5Ijo3ODMuMTk5OTk2OTQ4MjQyMn1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M511.775%2C409.1999969482422L511.775%2C442.41704092294754Q511.775%2C444.1999969482422%20510.6892135623731%2C445.6142105106153L510.6892135623731%2C445.6142105106153Q509.6034271247462%2C447.0284240729884%20508.1892135623731%2C448.1142105106153L508.1892135623731%2C448.1142105106153Q506.775%2C449.1999969482422%20504.9920439747053%2C449.1999969482422L251.0017919831397%2C449.1999969482422Q249.21883595784504%2C449.1999969482422%20247.80462239547194%2C450.2857833858691L247.80462239547194%2C450.2857833858691Q246.39040883309886%2C451.371569823496%20245.30462239547197%2C452.7857833858691L245.30462239547194%2C452.7857833858691Q244.21883595784504%2C454.1999969482422%20244.21883595784504%2C455.98295297353684L244.21883595784504%2C519.1999969482422L244.21883595784504%2C562.4170409229475Q244.21883595784504%2C564.1999969482422%20245.30462239547194%2C565.6142105106153L245.30462239547197%2C565.6142105106153Q246.39040883309886%2C567.0284240729884%20247.80462239547194%2C568.1142105106153L247.80462239547194%2C568.1142105106153Q249.21883595784504%2C569.1999969482422%20251.0017919831397%2C569.1999969482422L319.7081170744775%2C569.1999969482422Q321.49107309977217%2C569.1999969482422%20322.9052866621453%2C570.2857833858691L322.9052866621453%2C570.2857833858691Q324.3195002245184%2C571.371569823496%20325.4052866621453%2C572.7857833858691L325.4052866621453%2C572.7857833858691Q326.49107309977217%2C574.1999969482422%20326.49107309977217%2C575.9829529735368L326.49107309977217%2C579.1999969482422%22%20id%3D%22L_API_R_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_API_R_0%22%20data-points%3D%22W3sieCI6NTExLjc3NSwieSI6NDA5LjE5OTk5Njk0ODI0MjJ9LHsieCI6NTExLjc3NSwieSI6NDQ5LjE5OTk5Njk0ODI0MjJ9LHsieCI6MjQ0LjIxODgzNTk1Nzg0NTA0LCJ5Ijo0NDkuMTk5OTk2OTQ4MjQyMn0seyJ4IjoyNDQuMjE4ODM1OTU3ODQ1MDQsInkiOjUxOS4xOTk5OTY5NDgyNDIyfSx7IngiOjI0NC4yMTg4MzU5NTc4NDUwNCwieSI6NTY5LjE5OTk5Njk0ODI0MjJ9LHsieCI6MzI2LjQ5MTA3MzA5OTc3MjE3LCJ5Ijo1NjkuMTk5OTk2OTQ4MjQyMn0seyJ4IjozMjYuNDkxMDczMDk5NzcyMTcsInkiOjU4My4xOTk5OTY5NDgyNDIyfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M345.2189376831055%2C649.1999969482422L345.2189376831055%2C677.1999969482422%22%20id%3D%22L_R_W_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_R_W_0%22%20data-points%3D%22W3sieCI6MzQ1LjIxODkzNzY4MzEwNTUsInkiOjY0OS4xOTk5OTY5NDgyNDIyfSx7IngiOjM0NS4yMTg5Mzc2ODMxMDU1LCJ5Ijo2ODEuMTk5OTk2OTQ4MjQyMn1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M535.9984375%2C409.1999969482422L535.9984375%2C462.41704092294754Q535.9984375%2C464.1999969482422%20534.9126510623731%2C465.6142105106153L534.9126510623731%2C465.6142105106153Q533.8268646247462%2C467.0284240729884%20532.4126510623731%2C468.1142105106153L532.4126510623731%2C468.1142105106153Q530.9984375%2C469.1999969482422%20529.2154814747054%2C469.1999969482422L370.72975829173345%2C469.1999969482422Q368.9468022664388%2C469.1999969482422%20367.5325887040657%2C470.2857833858691L367.5325887040657%2C470.2857833858691Q366.1183751416926%2C471.371569823496%20365.0325887040657%2C472.7857833858691L365.0325887040657%2C472.7857833858691Q363.9468022664388%2C474.1999969482422%20363.9468022664388%2C475.98295297353684L363.9468022664388%2C479.1999969482422%22%20id%3D%22L_API_O_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_API_O_0%22%20data-points%3D%22W3sieCI6NTM1Ljk5ODQzNzUsInkiOjQwOS4xOTk5OTY5NDgyNDIyfSx7IngiOjUzNS45OTg0Mzc1LCJ5Ijo0NjkuMTk5OTk2OTQ4MjQyMn0seyJ4IjozNjMuOTQ2ODAyMjY2NDM4OCwieSI6NDY5LjE5OTk5Njk0ODI0MjJ9LHsieCI6MzYzLjk0NjgwMjI2NjQzODgsInkiOjQ4My4xOTk5OTY5NDgyNDIyfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M363.9468022664388%2C549.1999969482422L363.9468022664388%2C577.1999969482422%22%20id%3D%22L_O_R_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_O_R_0%22%20data-points%3D%22W3sieCI6MzYzLjk0NjgwMjI2NjQzODgsInkiOjU0OS4xOTk5OTY5NDgyNDIyfSx7IngiOjM2My45NDY4MDIyNjY0Mzg4LCJ5Ijo1ODEuMTk5OTk2OTQ4MjQyMn1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M316.52634887695314%2C749.1999969482422L316.52634887695314%2C777.1999969482422%22%20id%3D%22L_W_M_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_W_M_0%22%20data-points%3D%22W3sieCI6MzE2LjUyNjM0ODg3Njk1MzE0LCJ5Ijo3NDkuMTk5OTk2OTQ4MjQyMn0seyJ4IjozMTYuNTI2MzQ4ODc2OTUzMTQsInkiOjc4MS4xOTk5OTY5NDgyNDIyfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M373.9115264892578%2C749.1999969482422L373.9115264892578%2C762.4170409229475Q373.9115264892578%2C764.1999969482422%20374.9973129268847%2C765.6142105106153L374.9973129268847%2C765.6142105106153Q376.0830993645116%2C767.0284240729884%20377.4973129268847%2C768.1142105106153L377.4973129268847%2C768.1142105106153Q378.9115264892578%2C769.1999969482422%20380.6944825145525%2C769.1999969482422L482.3242288298486%2C769.1999969482422Q484.10718485514326%2C769.1999969482422%20485.52139841751637%2C770.2857833858691L485.52139841751637%2C770.2857833858691Q486.9356119798895%2C771.371569823496%20488.02139841751637%2C772.7857833858691L488.02139841751637%2C772.7857833858691Q489.10718485514326%2C774.1999969482422%20489.10718485514326%2C775.9829529735368L489.10718485514326%2C779.1999969482422%22%20id%3D%22L_W_P_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_W_P_0%22%20data-points%3D%22W3sieCI6MzczLjkxMTUyNjQ4OTI1NzgsInkiOjc0OS4xOTk5OTY5NDgyNDIyfSx7IngiOjM3My45MTE1MjY0ODkyNTc4LCJ5Ijo3NjkuMTk5OTk2OTQ4MjQyMn0seyJ4Ijo0ODkuMTA3MTg0ODU1MTQzMjYsInkiOjc2OS4xOTk5OTY5NDgyNDIyfSx7IngiOjQ4OS4xMDcxODQ4NTUxNDMyNiwieSI6NzgzLjE5OTk5Njk0ODI0MjJ9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M560.221875%2C409.1999969482422L560.221875%2C519.1999969482422L560.221875%2C619.1999969482422L560.221875%2C719.1999969482422L560.221875%2C777.1999969482422%22%20id%3D%22L_API_P_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_API_P_0%22%20data-points%3D%22W3sieCI6NTYwLjIyMTg3NSwieSI6NDA5LjE5OTk5Njk0ODI0MjJ9LHsieCI6NTYwLjIyMTg3NSwieSI6NTE5LjE5OTk5Njk0ODI0MjJ9LHsieCI6NTYwLjIyMTg3NSwieSI6NjE5LjE5OTk5Njk0ODI0MjJ9LHsieCI6NTYwLjIyMTg3NSwieSI6NzE5LjE5OTk5Njk0ODI0MjJ9LHsieCI6NTYwLjIyMTg3NSwieSI6NzgxLjE5OTk5Njk0ODI0MjJ9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bk__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabels%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_U_CF_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_CF_C_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_C_WEB_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_C_ADMIN_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_C_API_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_API_M_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_API_R_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_R_W_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_API_O_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_O_R_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_W_M_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_W_P_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_API_P_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fsvg%3E)

## 15.1 Deployment services

| Service           | Responsibility                    |
| ----------------- | --------------------------------- |
| Caddy             | TLS termination and reverse proxy |
| Customer frontend | Next.js customer website          |
| Admin frontend    | Next.js administration dashboard  |
| Go API            | HTTP and webhook endpoints        |
| Worker            | Asynchronous job processing       |
| Outbox publisher  | Publish committed outbox events   |
| Redis             | Cache and task queue              |
| MongoDB Atlas     | External persistent database      |

All application services should communicate over a private Docker network where possible. Redis must not be exposed publicly.

## 15.2 VPS resource constraints

With 2 GB RAM, memory is a significant constraint. The deployment should:

* Use multi-stage Docker builds.

* Build images in CI rather than on the production VPS.

* Use Next.js standalone output.

* Keep worker concurrency conservative.

* Set memory limits and monitor container restarts.

* Avoid running unnecessary development services in production.

* Monitor memory consumption and CPU during checkout and administrative workloads.

The actual resource budget must be measured after deployment. If the combined workloads exceed the available memory, the application may need a larger VPS or separated frontend hosting.

## 15.3 Docker services

Use separate Dockerfiles for the API, worker and outbox publisher. They may share the same compiled application binary or image where practical, but should run as separate processes with distinct commands.

A simplified service arrangement:

YAML

```
services:
  api:
    image: ghcr.io/your-org/denisco-backend:latest
    command: ["/app/api"]
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - app

  worker:
    image: ghcr.io/your-org/denisco-backend:latest
    command: ["/app/worker"]
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - app

  outbox:
    image: ghcr.io/your-org/denisco-backend:latest
    command: ["/app/outbox"]
    restart: unless-stopped
    env_file:
      - .env
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - app

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["redis-server", "--appendonly", "yes"]
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 3s
      retries: 5
    networks:
      - app

volumes:
  redis_data:

networks:
  app:
    driver: bridge
```

This is a starting point, not a complete production Compose file. Add the frontend services, Caddy configuration, image pinning, resource limits, health checks and appropriate secrets handling before production deployment. Redis persistence does not replace MongoDB as the source of truth.

# 16. Testing strategy

Testing should cover business rules, data consistency, integration boundaries and HTTP behaviour.

| Test type         | Scope                                                   |
| ----------------- | ------------------------------------------------------- |
| Unit tests        | Domain rules, validation, pricing and state transitions |
| Repository tests  | MongoDB persistence and queries                         |
| Integration tests | Transactions, Redis and external integration boundaries |
| API tests         | Request validation, response formats and authorization  |
| Workflow tests    | Checkout, payment verification and consultation booking |
| Security tests    | Authentication, access control and webhook validation   |
| Load tests        | Checkout, product listing and API resource usage        |

### Critical test scenarios

Authentication

* Duplicate registration.

* Incorrect password.

* Expired access token.

* Refresh-token rotation.

* Reuse of a revoked refresh token.

* Password-reset token expiry and reuse.

Inventory and checkout

* Insufficient stock.

* Simultaneous attempts to buy the last item.

* Duplicate checkout requests.

* Failed payment initialization.

* Expired reservation.

* Payment arriving after reservation expiry.

* Duplicate payment webhook.

Consultations

* Simultaneous booking of the same slot.

* Booking of an unavailable slot.

* Cancellation of an eligible booking.

* Reminder retries.

Administration

* Customer attempting to access admin endpoints.

* Unauthorized inventory adjustments.

* Invalid order-status transitions.

* Audit logging for sensitive actions.

Use deterministic test fixtures and a dedicated test database. Integration tests should verify that MongoDB transactions behave as expected on a replica-set-capable test environment.

# 17. CI/CD and development workflow

Use GitHub Actions for automated validation and deployment.

Diagram options

![](data\:image/svg+xml;utf8,%3Csvg%20id%3D%22mermaid-_r_1bl_%22%20width%3D%22456.1796875%22%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20class%3D%22flowchart%22%20height%3D%221042%22%20viewBox%3D%224%204%20456.1796875%201042%22%20role%3D%22graphics-document%20document%22%20aria-roledescription%3D%22flowchart-v2%22%3E%3Cstyle%3E%23mermaid-_r_1bl_%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A14px%3Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%40keyframes%20edge-animation-frame%7Bfrom%7Bstroke-dashoffset%3A0%3B%7D%7D%40keyframes%20dash%7Bto%7Bstroke-dashoffset%3A0%3B%7D%7D%23mermaid-_r_1bl_%20.edge-animation-slow%7Bstroke-dasharray%3A9%2C5!important%3Bstroke-dashoffset%3A900%3Banimation%3Adash%2050s%20linear%20infinite%3Bstroke-linecap%3Around%3B%7D%23mermaid-_r_1bl_%20.edge-animation-fast%7Bstroke-dasharray%3A9%2C5!important%3Bstroke-dashoffset%3A900%3Banimation%3Adash%2020s%20linear%20infinite%3Bstroke-linecap%3Around%3B%7D%23mermaid-_r_1bl_%20.error-icon%7Bfill%3Argb\(249%2C%20249%2C%20249\)%3B%7D%23mermaid-_r_1bl_%20.error-text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3Bstroke%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20.edge-thickness-normal%7Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bl_%20.edge-thickness-thick%7Bstroke-width%3A3.5px%3B%7D%23mermaid-_r_1bl_%20.edge-pattern-solid%7Bstroke-dasharray%3A0%3B%7D%23mermaid-_r_1bl_%20.edge-thickness-invisible%7Bstroke-width%3A0%3Bfill%3Anone%3B%7D%23mermaid-_r_1bl_%20.edge-pattern-dashed%7Bstroke-dasharray%3A3%3B%7D%23mermaid-_r_1bl_%20.edge-pattern-dotted%7Bstroke-dasharray%3A2%3B%7D%23mermaid-_r_1bl_%20.marker%7Bfill%3Argb\(93%2C%2093%2C%2093\)%3Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bl_%20.marker.cross%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bl_%20svg%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A14px%3B%7D%23mermaid-_r_1bl_%20p%7Bmargin%3A0%3B%7D%23mermaid-_r_1bl_%20.label%7Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20.cluster-label%20text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20.cluster-label%20span%7Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20.cluster-label%20span%20p%7Bbackground-color%3Atransparent%3B%7D%23mermaid-_r_1bl_%20.label%20text%2C%23mermaid-_r_1bl_%20span%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20.node%20rect%2C%23mermaid-_r_1bl_%20.node%20circle%2C%23mermaid-_r_1bl_%20.node%20ellipse%2C%23mermaid-_r_1bl_%20.node%20polygon%2C%23mermaid-_r_1bl_%20.node%20path%7Bfill%3Argb\(222%2C%20234%2C%20251\)%3Bstroke%3Argb\(83%2C%20154%2C%20248\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bl_%20.rough-node%20.label%20text%2C%23mermaid-_r_1bl_%20.node%20.label%20text%2C%23mermaid-_r_1bl_%20.image-shape%20.label%2C%23mermaid-_r_1bl_%20.icon-shape%20.label%7Btext-anchor%3Amiddle%3B%7D%23mermaid-_r_1bl_%20.node%20.katex%20path%7Bfill%3A%23000%3Bstroke%3A%23000%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bl_%20.rough-node%20.label%2C%23mermaid-_r_1bl_%20.node%20.label%2C%23mermaid-_r_1bl_%20.image-shape%20.label%2C%23mermaid-_r_1bl_%20.icon-shape%20.label%7Btext-align%3Acenter%3B%7D%23mermaid-_r_1bl_%20.node.clickable%7Bcursor%3Apointer%3B%7D%23mermaid-_r_1bl_%20.root%20.anchor%20path%7Bfill%3Argb\(93%2C%2093%2C%2093\)!important%3Bstroke-width%3A0%3Bstroke%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bl_%20.arrowheadPath%7Bfill%3Argb\(93%2C%2093%2C%2093\)%3B%7D%23mermaid-_r_1bl_%20.edgePath%20.path%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3Bstroke-width%3A2.0px%3B%7D%23mermaid-_r_1bl_%20.flowchart-link%7Bstroke%3Argb\(93%2C%2093%2C%2093\)%3Bfill%3Anone%3B%7D%23mermaid-_r_1bl_%20.edgeLabel%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Btext-align%3Acenter%3B%7D%23mermaid-_r_1bl_%20.edgeLabel%20p%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bl_%20.edgeLabel%20rect%7Bopacity%3A0.5%3Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bfill%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bl_%20.labelBkg%7Bbackground-color%3Argba\(252%2C%20252%2C%20252%2C%200.5\)%3B%7D%23mermaid-_r_1bl_%20.cluster%20rect%7Bfill%3Argb\(249%2C%20249%2C%20249\)%3Bstroke%3Argba\(0%2C%200%2C%200%2C%200.05\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bl_%20.cluster%20text%7Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20.cluster%20span%7Bcolor%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20div.mermaidTooltip%7Bposition%3Aabsolute%3Btext-align%3Acenter%3Bmax-width%3A200px%3Bpadding%3A2px%3Bfont-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3Bfont-size%3A12px%3Bbackground%3Argb\(249%2C%20249%2C%20249\)%3Bborder%3A1px%20solid%20rgba\(0%2C%200%2C%200%2C%200.05\)%3Bborder-radius%3A2px%3Bpointer-events%3Anone%3Bz-index%3A100%3B%7D%23mermaid-_r_1bl_%20.flowchartTitleText%7Btext-anchor%3Amiddle%3Bfont-size%3A18px%3Bfill%3Argb\(13%2C%2013%2C%2013\)%3B%7D%23mermaid-_r_1bl_%20rect.text%7Bfill%3Anone%3Bstroke-width%3A0%3B%7D%23mermaid-_r_1bl_%20.icon-shape%2C%23mermaid-_r_1bl_%20.image-shape%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Btext-align%3Acenter%3B%7D%23mermaid-_r_1bl_%20.icon-shape%20p%2C%23mermaid-_r_1bl_%20.image-shape%20p%7Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bpadding%3A2px%3B%7D%23mermaid-_r_1bl_%20.icon-shape%20rect%2C%23mermaid-_r_1bl_%20.image-shape%20rect%7Bopacity%3A0.5%3Bbackground-color%3Argb\(252%2C%20252%2C%20252\)%3Bfill%3Argb\(252%2C%20252%2C%20252\)%3B%7D%23mermaid-_r_1bl_%20.label-icon%7Bdisplay%3Ainline-block%3Bheight%3A1em%3Boverflow%3Avisible%3Bvertical-align%3A-0.125em%3B%7D%23mermaid-_r_1bl_%20.node%20.label-icon%20path%7Bfill%3AcurrentColor%3Bstroke%3Arevert%3Bstroke-width%3Arevert%3B%7D%23mermaid-_r_1bl_%20.node%20text%7Bfont-size%3A16px%3Bfont-weight%3A600%3Bletter-spacing%3A-0.32px%3Bfill%3A%23004f99%3B%7D%23mermaid-_r_1bl_%20.edgeLabels%20text%7Bfont-size%3A13px%3Bfont-weight%3A600%3Bletter-spacing%3A-0.08px%3Bfill%3A%23004f99%3B%7D%23mermaid-_r_1bl_%20.node%20tspan%5Bfont-weight%3D%22normal%22%5D%2C%23mermaid-_r_1bl_%20.edgeLabels%20tspan%5Bfont-weight%3D%22normal%22%5D%7Bfont-weight%3A600%3B%7D%23mermaid-_r_1bl_%20.edgeLabel%20.label%20rect%7Bopacity%3A1%3Brx%3A13px%3Bry%3A13px%3Bfill%3A%23f5faff%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bl_%20.node%20rect%2C%23mermaid-_r_1bl_%20.node%20circle%2C%23mermaid-_r_1bl_%20.node%20ellipse%2C%23mermaid-_r_1bl_%20.node%20polygon%2C%23mermaid-_r_1bl_%20.node%20path%7Bfill%3Argb\(229%2C%20243%2C%20255\)%3Bstroke%3Argba\(0%2C%200%2C%200%2C%200.1\)%3Bstroke-width%3A1px%3B%7D%23mermaid-_r_1bl_%20.node%20rect%7Brx%3A16px%3Bry%3A16px%3B%7D%23mermaid-_r_1bl_%20.node.mermaid-decision%20.label-container%7Bfill%3A%23f5faff%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-dasharray%3A2%202%3B%7D%23mermaid-_r_1bl_%20.edgePaths%20.flowchart-link%7Bstroke%3Argb\(206%2C%20219%2C%20229\)%3Bstroke-width%3A1px%3Bstroke-linecap%3Around%3Bstroke-linejoin%3Around%3B%7D%23mermaid-_r_1bl_%20.marker%7Bfill%3Argb\(206%2C%20219%2C%20229\)%3Bstroke%3Argb\(206%2C%20219%2C%20229\)%3B%7D%23mermaid-_r_1bl_%20.node%7Bcolor-scheme%3Alight%3B%7D%23mermaid-_r_1bl_%20%3Aroot%7B--mermaid-font-family%3A%22-apple-system%22%2C%22BlinkMacSystemFont%22%2C%22Segoe%20UI%22%2C%22Roboto%22%2C%22Oxygen%22%2C%22Ubuntu%22%2C%22Cantarell%22%2C%22Helvetica%20Neue%22%2C%22Arial%22%2C%22sans-serif%22%3B%7D%3C%2Fstyle%3E%3Cg%3E%3Cmarker%20id%3D%22mermaid-_r_1bl__flowchart-v2-pointEnd%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%22-5%20-5%2010%2010%22%20refX%3D%220%22%20refY%3D%220%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2210%22%20markerHeight%3D%2210%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%200%200%20L%204%200%20M%200.8180194846605362%20-3.181980515339464%20L%204%200%20L%200.8180194846605362%203.181980515339464%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%20none%3B%20fill%3A%20none%3B%20stroke-linecap%3A%20round%3B%20stroke-linejoin%3A%20round%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bl__flowchart-v2-pointStart%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%22-5%20-5%2010%2010%22%20refX%3D%220%22%20refY%3D%220%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2210%22%20markerHeight%3D%2210%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%200%200%20L%20-4%200%20M%20-0.8180194846605362%20-3.181980515339464%20L%20-4%200%20L%20-0.8180194846605362%203.181980515339464%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%20none%3B%20fill%3A%20none%3B%20stroke-linecap%3A%20round%3B%20stroke-linejoin%3A%20round%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bl__flowchart-v2-circleEnd%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%220%200%2010%2010%22%20refX%3D%2211%22%20refY%3D%225%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Ccircle%20cx%3D%225%22%20cy%3D%225%22%20r%3D%225%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fcircle%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bl__flowchart-v2-circleStart%22%20class%3D%22marker%20flowchart-v2%22%20viewBox%3D%220%200%2010%2010%22%20refX%3D%22-1%22%20refY%3D%225%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Ccircle%20cx%3D%225%22%20cy%3D%225%22%20r%3D%225%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%201%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fcircle%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bl__flowchart-v2-crossEnd%22%20class%3D%22marker%20cross%20flowchart-v2%22%20viewBox%3D%220%200%2011%2011%22%20refX%3D%2212%22%20refY%3D%225.2%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%201%2C1%20l%209%2C9%20M%2010%2C1%20l%20-9%2C9%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%202%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3Cmarker%20id%3D%22mermaid-_r_1bl__flowchart-v2-crossStart%22%20class%3D%22marker%20cross%20flowchart-v2%22%20viewBox%3D%220%200%2011%2011%22%20refX%3D%22-1%22%20refY%3D%225.2%22%20markerUnits%3D%22userSpaceOnUse%22%20markerWidth%3D%2211%22%20markerHeight%3D%2211%22%20orient%3D%22auto%22%3E%3Cpath%20d%3D%22M%201%2C1%20l%209%2C9%20M%2010%2C1%20l%20-9%2C9%22%20class%3D%22arrowMarkerPath%22%20style%3D%22stroke-width%3A%202%3B%20stroke-dasharray%3A%201%2C%200%3B%22%3E%3C%2Fpath%3E%3C%2Fmarker%3E%3C%2Fg%3E%3Cg%20class%3D%22subgraphs%22%3E%3C%2Fg%3E%3Cg%20class%3D%22nodes%22%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-A-0%22%20transform%3D%22translate\(135.484375%2C%2042\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-108.51953125%22%20y%3D%22-30%22%20width%3D%22217.0390625%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EPush%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20or%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20pull%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20request%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-B-1%22%20transform%3D%22translate\(135.484375%2C%20142\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-90.7109375%22%20y%3D%22-30%22%20width%3D%22181.421875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EFormat%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20and%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20lint%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-C-3%22%20transform%3D%22translate\(135.484375%2C%20242\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-86.02734375%22%20y%3D%22-30%22%20width%3D%22172.0546875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ERun%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20unit%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20tests%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-D-5%22%20transform%3D%22translate\(135.484375%2C%20342\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-111.70703125%22%20y%3D%22-30%22%20width%3D%22223.4140625%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3ERun%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20integration%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20tests%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-E-7%22%20transform%3D%22translate\(135.484375%2C%20442\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-97.20703125%22%20y%3D%22-30%22%20width%3D%22194.4140625%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EBuild%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20Go%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20binaries%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-F-9%22%20transform%3D%22translate\(135.484375%2C%20542\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-106.2734375%22%20y%3D%22-30%22%20width%3D%22212.546875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EBuild%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20Docker%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20image%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%20%20mermaid-decision%22%20id%3D%22flowchart-G-11%22%20transform%3D%22translate\(135.484375%2C%20642\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-105.15234375%22%20y%3D%22-30%22%20width%3D%22210.3046875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EBranch%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20and%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20checks%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-H-13%22%20transform%3D%22translate\(100.43359375%2C%20808\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-88.43359375%22%20y%3D%22-30%22%20width%3D%22176.8671875%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EReport%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20checks%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-I-15%22%20transform%3D%22translate\(340.5234375%2C%20808\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-111.65625%22%20y%3D%22-30%22%20width%3D%22223.3125%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EPush%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20image%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20to%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20GHCR%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-J-17%22%20transform%3D%22translate\(340.5234375%2C%20908\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-87.69921875%22%20y%3D%22-30%22%20width%3D%22175.3984375%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EDeploy%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20to%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20VPS%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22node%20default%22%20id%3D%22flowchart-K-19%22%20transform%3D%22translate\(340.5234375%2C%201008\)%22%3E%3Crect%20class%3D%22basic%20label-container%22%20style%3D%22%22%20x%3D%22-83.9239387512207%22%20y%3D%22-30%22%20width%3D%22167.8478775024414%22%20height%3D%2260%22%3E%3C%2Frect%3E%3Cg%20class%3D%22label%22%20style%3D%22%22%20transform%3D%22translate\(0%2C%20-9.5\)%22%3E%3Crect%3E%3C%2Frect%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EHealth%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20check%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edges%20edgePaths%22%3E%3Cpath%20d%3D%22M135.484375%2C72L135.484375%2C100%22%20id%3D%22L_A_B_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_A_B_0%22%20data-points%3D%22W3sieCI6MTM1LjQ4NDM3NSwieSI6NzJ9LHsieCI6MTM1LjQ4NDM3NSwieSI6MTA0fV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M135.484375%2C172L135.484375%2C200%22%20id%3D%22L_B_C_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_B_C_0%22%20data-points%3D%22W3sieCI6MTM1LjQ4NDM3NSwieSI6MTcyfSx7IngiOjEzNS40ODQzNzUsInkiOjIwNH1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M135.484375%2C272L135.484375%2C300%22%20id%3D%22L_C_D_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_C_D_0%22%20data-points%3D%22W3sieCI6MTM1LjQ4NDM3NSwieSI6MjcyfSx7IngiOjEzNS40ODQzNzUsInkiOjMwNH1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M135.484375%2C372L135.484375%2C400%22%20id%3D%22L_D_E_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_D_E_0%22%20data-points%3D%22W3sieCI6MTM1LjQ4NDM3NSwieSI6MzcyfSx7IngiOjEzNS40ODQzNzUsInkiOjQwNH1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M135.484375%2C472L135.484375%2C500%22%20id%3D%22L_E_F_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_E_F_0%22%20data-points%3D%22W3sieCI6MTM1LjQ4NDM3NSwieSI6NDcyfSx7IngiOjEzNS40ODQzNzUsInkiOjUwNH1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M135.484375%2C572L135.484375%2C600%22%20id%3D%22L_F_G_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_F_G_0%22%20data-points%3D%22W3sieCI6MTM1LjQ4NDM3NSwieSI6NTcyfSx7IngiOjEzNS40ODQzNzUsInkiOjYwNH1d%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M100.43359374999997%2C672L100.43359375%2C766%22%20id%3D%22L_G_H_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_G_H_0%22%20data-points%3D%22W3sieCI6MTAwLjQzMzU5Mzc0OTk5OTk3LCJ5Ijo2NzJ9LHsieCI6MTAwLjQzMzU5Mzc1LCJ5Ijo3NzB9XQ%3D%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M170.53515625000003%2C672L170.53515625%2C684.9289321881346Q170.53515625%2C692%20177.60622406186548%2C692L333.74048147470535%2C692Q335.5234375%2C692%20336.9376510623731%2C693.0857864376269L336.9376510623731%2C693.0857864376269Q338.3518646247462%2C694.1715728752538%20339.4376510623731%2C695.5857864376269L339.4376510623731%2C695.5857864376269Q340.5234375%2C697%20340.5234375%2C698.7829560252947L340.5234375%2C766%22%20id%3D%22L_G_I_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_G_I_0%22%20data-points%3D%22W3sieCI6MTcwLjUzNTE1NjI1MDAwMDAzLCJ5Ijo2NzJ9LHsieCI6MTcwLjUzNTE1NjI1LCJ5Ijo2OTJ9LHsieCI6MzQwLjUyMzQzNzUsInkiOjY5Mn0seyJ4IjozNDAuNTIzNDM3NSwieSI6NzcwfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M340.5234375%2C838L340.5234375%2C866%22%20id%3D%22L_I_J_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_I_J_0%22%20data-points%3D%22W3sieCI6MzQwLjUyMzQzNzUsInkiOjgzOH0seyJ4IjozNDAuNTIzNDM3NSwieSI6ODcwfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3Cpath%20d%3D%22M340.5234375%2C938L340.5234375%2C966%22%20id%3D%22L_J_K_0%22%20class%3D%22edge-thickness-normal%20edge-pattern-solid%20edge-thickness-normal%20edge-pattern-solid%20flowchart-link%22%20style%3D%22%3B%22%20data-edge%3D%22true%22%20data-et%3D%22edge%22%20data-id%3D%22L_J_K_0%22%20data-points%3D%22W3sieCI6MzQwLjUyMzQzNzUsInkiOjkzOH0seyJ4IjozNDAuNTIzNDM3NSwieSI6OTcwfV0%3D%22%20marker-end%3D%22url\(%23mermaid-_r_1bl__flowchart-v2-pointEnd\)%22%3E%3C%2Fpath%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabels%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22stroke%3A%20none%22%3E%3C%2Frect%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_A_B_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_B_C_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_C_D_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_D_E_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_E_F_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_F_G_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(100.2109375%2C%20725\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_G_H_0%22%20transform%3D%22translate\(-37.27734375%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%2298.5546875%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EPull%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20request%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%20transform%3D%22translate\(340.0859375%2C%20725\)%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_G_I_0%22%20transform%3D%22translate\(-70.5625%2C-8\)%22%3E%3Cg%3E%3Crect%20class%3D%22background%22%20style%3D%22%22%20x%3D%22-12%22%20y%3D%22-5%22%20width%3D%22165.125%22%20height%3D%2226%22%3E%3C%2Frect%3E%3Ctext%20y%3D%22-10.1%22%20style%3D%22%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3EApproved%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20main%3C%2Ftspan%3E%3Ctspan%20font-style%3D%22normal%22%20class%3D%22text-inner-tspan%22%20font-weight%3D%22normal%22%3E%20branch%3C%2Ftspan%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_I_J_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3Cg%20class%3D%22edgeLabel%22%3E%3Cg%20class%3D%22label%22%20data-id%3D%22L_J_K_0%22%20transform%3D%22translate\(0%2C%200\)%22%3E%3Ctext%20y%3D%22-10.1%22%3E%3Ctspan%20class%3D%22text-outer-tspan%22%20x%3D%220%22%20y%3D%22-0.1em%22%20dy%3D%221.1em%22%3E%3C%2Ftspan%3E%3C%2Ftext%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fg%3E%3C%2Fsvg%3E)

Recommended CI steps:

1. `gofmt` formatting check.

2. `go vet`.

3. Static analysis using a configured Go linter.

4. Unit tests.

5. Integration tests.

6. Build API, worker and outbox binaries.

7. Build and publish Docker images.

8. Deploy approved changes.

9. Run post-deployment health checks.

Use immutable image tags tied to commit SHAs rather than relying exclusively on `latest`. Keep deployment credentials in GitHub Actions secrets.

# 18. API documentation

The OpenAPI specification in `api/openapi.yaml` is the authoritative HTTP contract for both frontends.

It should define:

* Endpoint paths and HTTP methods.

* Authentication requirements.

* Request and response schemas.

* Success and error responses.

* Pagination conventions.

* Validation constraints.

* Relevant status transitions.

* Webhook request formats.

Do not generate or maintain shared TypeScript API types as part of this architecture. The customer website and admin dashboard maintain their own frontend types and API functions, while OpenAPI remains the backend's contract and documentation.

Whenever an endpoint changes, update the OpenAPI document and both frontend API clients as needed.

# 19. Implementation phases

Phase 1

Foundation

* Initialize the Go module.

* Configure Chi, MongoDB and Redis.

* Implement configuration validation and structured logging.

* Establish API response and error conventions.

* Add health checks and Docker builds.

* Set up CI and OpenAPI.

Phase 2

Authentication and users

* Implement registration and login.

* Add bcrypt password hashing.

* Implement JWT access tokens and refresh-token rotation.

* Add password reset and profile management.

* Implement authorization middleware.

Phase 3

Catalog and inventory

* Implement product and category management.

* Add product search and pagination.

* Integrate ImageKit uploads.

* Implement inventory records and movement history.

* Add administrative inventory controls.

Phase 4

Cart, orders and payments

* Implement cart operations.

* Build transactional checkout.

* Add stock reservations and expiration.

* Integrate Paystack.

* Implement payment verification and idempotent webhook processing.

* Add order history and administration.

Phase 5

Consultations and notifications

* Implement consultation types and availability.

* Add conflict-safe booking creation.

* Integrate asynchronous email notifications.

* Add booking reminders and cancellation workflows.

* Implement the transactional outbox.

Phase 6

Administration and production readiness

* Implement dashboard aggregation endpoints.

* Complete administrative workflows and audit logs.

* Add integration and end-to-end tests.

* Configure production deployment.

* Verify payment, inventory and booking recovery scenarios.

* Perform load and security testing.

# 20. Architectural decisions and constraints

| Decision                    | Chosen approach                            | Reason                                                     |
| --------------------------- | ------------------------------------------ | ---------------------------------------------------------- |
| Backend architecture        | Modular monolith                           | One deployable backend with clear domain boundaries        |
| Language                    | Go                                         | Strong typing, concurrency and straightforward deployment  |
| Router                      | Chi                                        | Lightweight routing and middleware                         |
| Database                    | Managed MongoDB                            | External persistence without VPS database overhead         |
| Cache and queues            | Redis                                      | Shared infrastructure for caching and asynchronous jobs    |
| Job processing              | Asynq                                      | Retryable background work                                  |
| Authentication              | JWT with refresh-token rotation            | Stateless access authentication with revocable renewal     |
| Password hashing            | bcrypt                                     | Established password-hashing algorithm                     |
| Payments                    | Paystack                                   | Payment provider integration                               |
| Image uploads               | ImageKit                                   | Direct uploads without routing large files through the API |
| Notifications               | Resend                                     | Transactional email delivery                               |
| API contract                | OpenAPI                                    | Consistent documentation for both frontends                |
| Deployment                  | Docker on VPS                              | Repeatable deployment                                      |
| Cross-service communication | Direct module interfaces and outbox events | Explicit dependencies and reliable asynchronous work       |

## Final architecture principle

The backend is the authority for production data, security and business transactions. The root HTML prototype is the canonical reference for the agreed product experience and intended workflows. Both frontends must follow that prototype, while the backend implements and enforces the corresponding production behaviour.

Where the prototype's demonstration behaviour conflicts with the requirements for secure, reliable production execution, document and reconcile the difference rather than reproducing unsafe behaviour.
