# Security review — denisco_backend

**Date:** 2026-10-06
**Scope:** the API, worker and outbox publisher, their configuration, and the
deployment configuration in `deployments/`. The frontends are covered only where
they affect the API's trust boundaries.
**Method:** read the authentication, authorization, payment and data-access
paths against the architecture's §4.11 requirements; checked each finding
against the code rather than against the plan; exercised the exploitable ones
through the integration suite.

The review is a point-in-time read of the code at this date, not a penetration
test, and it does not cover the hosting account, the Atlas project's own access
controls or the third-party providers.

---

## Summary

| Area | Verdict |
|---|---|
| Authentication and session handling | Sound |
| Authorization and object ownership | Sound |
| Payment integrity | Sound |
| Input handling and injection | Sound, one hardening applied |
| Rate limiting and request size | Sound, one correctness fix applied |
| Secrets and logging | Sound |
| Transport and deployment | Sound as configured; four host actions remain |

Two changes came out of the review, both now in the code:

1. **Client addresses are no longer taken from an untrusted header.** Rate
   limiting and the audit log attribute a request to the address `ClientIP`
   returns, and that used to read `X-Forwarded-For` first. Anyone can send that
   header, and Cloudflare *appends* the real address to whatever the client
   sent rather than replacing it — so a caller could have chosen which bucket
   the limiter counted them in, and whose address appeared in the log. The API
   now trusts exactly one header, named by `CLIENT_IP_HEADER`, and refuses to
   start if that is set to `X-Forwarded-For`. With nothing configured it uses
   the peer address, which cannot be forged. Caddy sets `X-Real-IP` from
   `{client_ip}`, which it resolves from the forwarded chain only for
   connections from Cloudflare's published ranges.

2. **Search terms are bounded.** A search runs as a case-insensitive regular
   expression across a collection, so the term's length is work the server does
   per document, and the five search endpoints passed it through unbounded. They
   now go through `httpx.SearchParam`, which trims and caps at 100 characters.

---

## Authentication

- **Passwords** are hashed with bcrypt at cost 12 (`BCRYPT_COST`, validated to
  be 10–31 at startup), bounded to 8–72 characters because bcrypt ignores input
  past 72 bytes rather than failing. Login verifies against a fixed dummy hash
  when an address is not registered, so the response time does not reveal
  whether an account exists. A cost change is picked up on the next login,
  which rehashes.
- **Access tokens** are EdDSA JWTs with a 15-minute lifetime. Verification pins
  the algorithm (`WithValidMethods`), requires the issuer, the audience and an
  expiry, and rejects a token with no subject. A token signed with `none`, with
  HMAC, or for another audience is refused.
- **Refresh tokens** are opaque random strings; only their SHA-256 hash is
  stored, so a database disclosure cannot be replayed as a session. Every use
  rotates the token, and the rotation is a single conditional update — which is
  what makes reuse detectable without a race. A replayed token revokes the whole
  family, and the integration suite proves the successor stops working too.
- **Reset tokens** are single-use: redeeming one is a conditional update, and a
  successful reset revokes every session for the account. Requests are
  throttled per account as well as per IP (3 an hour by default), and an unknown
  address gets the same empty success as a known one, so the endpoint does not
  enumerate customers.
- **Token delivery.** The access token is returned in the body and held in
  browser memory; the refresh token is an `HttpOnly` cookie scoped to
  `/api/v1/auth`, with `Secure` enforced in production by a startup check and
  `SameSite` configurable. The integration suite asserts the token never
  appears in a login response body.
- **Admin login** is a separate endpoint that refuses a customer account, so a
  customer's credentials cannot produce an admin session even if the admin UI is
  reachable.

## Authorization

- Role checks are server-side, applied once per route group:
  `middleware.Authenticate` then `middleware.RequireRole(admin, super_admin)`
  for the whole `/admin` subtree. A customer presenting a valid token to an
  admin route gets 403 regardless of what the frontend shows.
- Ownership is checked on every object a customer can name: orders, payments and
  bookings each compare the caller against the record, and an order belonging to
  someone else answers **404 rather than 403**, so identifiers cannot be probed
  for existence. The same holds for a payment reference.
- Guest consultation bookings carry no account, so they are not reachable
  through the authenticated booking history at all.
- The ImageKit upload signature is admin-only, lasts 10 minutes and is recorded
  in the audit trail; the private key never leaves the API.

## Payment integrity

- The webhook's HMAC-SHA512 signature is verified against the **raw bytes**
  before anything is decoded, in constant time, and a request with a wrong or
  missing signature is refused with 401. The integration suite posts a forged
  one and asserts nothing changed.
- A successful transaction is checked against the order's own total and currency
  before it settles anything; a mismatch marks the attempt failed and leaves the
  order unpaid. Exercised by `TestWebhookPayingTheWrongAmountIsRefused`.
- Settling an attempt and marking an order paid are both conditional updates, so
  a replayed webhook — or a webhook racing the callback page's verify — applies
  once. Four concurrent deliveries deduct stock once and send one email.
- Verification is always server-to-server. A customer returning to the callback
  URL proves nothing, and the callback page calls verify rather than trusting
  the redirect.
- A payment that arrives after its reservation expired takes the reconciliation
  path rather than silently recreating a reservation: stock is re-taken if it is
  still available, or the order is flagged for a refund decision.
- Refunds are recorded against the attempt and the order separately, and only a
  successful attempt can be refunded.

## Input handling

- Bodies are capped at 1 MiB globally (`HTTP_MAX_BODY_BYTES`) and again inside
  the webhook handler, which reads through an explicit `LimitReader`.
- JSON decoding rejects unknown fields, so a request cannot set a field the
  endpoint did not mean to expose. Updates are built from explicit pointer
  structs rather than from the request body, which is what keeps mass assignment
  out of the admin endpoints.
- Every search term is regex-quoted with `regexp.QuoteMeta` before it reaches
  MongoDB, so a term cannot inject a pattern, and is now length-bounded.
- Identifiers are parsed as ObjectIDs before use; a malformed one is a domain
  "not found" rather than a driver error.
- Money is integer kobo end to end. There is no floating-point arithmetic in
  pricing, and the order total is recalculated from the catalogue at checkout —
  the client's prices are never trusted.
- Pagination is bounded (`MaxLimit` 100), so a single request cannot ask for the
  whole collection.

## Rate limiting

| Endpoint | Budget |
|---|---|
| `POST /auth/login`, `/auth/admin/login` | 5 a minute per address |
| `POST /auth/register` | 10 an hour per address |
| `POST /auth/forgot-password`, `/auth/reset-password` | 3 an hour per address, and 3 an hour per account |
| Everything else | 120 a minute per address |

The budgets are per client address, which is why the `ClientIP` change above
mattered. `RATE_LIMIT_ENABLED=false` exists for the end-to-end suites and must
stay true in production.

## Secrets and logging

- Configuration comes from the environment; `.env` is git-ignored, and the
  production file lives outside every checkout at `/opt/denisco/.env`, deploy-
  owned and mode 600.
- `MONGODB_URI` carries credentials and is never logged — the connection logs
  the database name only. The backup script reads it from the environment file
  rather than taking it on a command line, where `ps` would show it.
- Logs are structured JSON and carry a request ID, the route, status, duration
  and the error code. Passwords, tokens, cookies and provider keys are not
  logged. An internal error's detail goes to the log; the response carries only
  a machine code and a human message.
- `JWT_ACCESS_SECRET` is validated to be at least 32 characters unless an
  explicit Ed25519 seed is supplied.

## Transport and deployment

- Only Caddy publishes ports. The API, Redis and both frontends are reachable
  on the internal Compose network alone, which is why `compose.yaml` uses
  `expose` rather than `ports` for them — Docker writes its own iptables rules
  and would otherwise bypass the host firewall.
- Caddy terminates TLS, sends HSTS, `X-Content-Type-Options`, a referrer policy
  and a permissions policy, and strips its own version. The admin host also
  sends `X-Frame-Options: DENY` and `noindex`.
- CORS allows the two exact frontend origins; wildcards are rejected at startup
  because the API serves credentialed requests. Cookie-authenticated endpoints
  (`/auth/refresh`, `/auth/logout`) additionally require an approved `Origin`,
  falling back to `Referer`, which blocks a cross-site forgery even where
  `SameSite` does not apply.
- MongoDB is reached over TLS through a `mongodb+srv` string, with a user that
  has `readWrite` on one database and an Atlas network allow-list holding the
  VPS address only.

---

## Open items

| Item | Why it is open |
|---|---|
| Cloudflare records, SSL mode and WAF exemptions for the API host | Dashboard actions; specified in §3 of the deployment runbook |
| SSH hardening and the host firewall | Host actions; §2 of the runbook has the exact configuration |
| The restore drill | Needs the VPS; §8 of the runbook |
| Atlas project access and key rotation schedule | Account administration rather than code |

## Accepted risks

- **No account lockout.** Login is rate-limited per address but an account is
  never locked, so a distributed attempt against one account is only slowed,
  not stopped. Lockout trades one denial-of-service for another, and the
  architecture does not ask for it; if it is wanted later, the refresh-token
  family already gives a place to record it.
- **A 401 on session restore.** A visitor with no session triggers one
  `POST /auth/refresh` that answers 401. That is the cost of keeping the access
  token out of storage, and it is handled rather than surfaced.
- **Email as the only identity proof.** Email verification is modelled on the
  user record but not enforced at checkout, which matches the prototype: a
  customer can order before verifying.
