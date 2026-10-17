# Deployment runbook

One VPS (2 vCPU, 2 GB RAM) runs the whole platform behind Cloudflare. MongoDB
is Atlas; everything else is a container on this host.

```
            internet → Cloudflare → Caddy (80/443, the only published ports)
                                      │
                 ┌────────────────────┼────────────────────┐
                 │                    │                    │
          web (3000)           admin (3001)           api (4000)
            Next.js              Next.js                 Go
                                                          │
                                        ┌─────────────────┼─────────────┐
                                        │                 │             │
                                   worker (Asynq)   outbox publisher   redis
                                        │
                      Atlas · Paystack · Resend · ImageKit
```

Files in this directory:

| File | Purpose |
|---|---|
| `compose.yaml` | The seven services, their limits, health checks and restart policies |
| `Caddyfile` | TLS, the three hostnames, security headers, client-address handling |
| `redis.conf` | Queue persistence and the memory ceiling |
| `.env.example` | Stack variables Compose substitutes — copy to `.env` |
| `Dockerfile.api`, `Dockerfile.worker`, `Dockerfile.outbox-publisher` | The Go images |
| `backup.sh`, `restore.sh` | Database and queue backup, and the restore path |
| `compose.local.yaml` | A workstation overlay that publishes the application ports and leaves Caddy out |

---

## 1. Memory budget

The limits in `compose.yaml` are deliberate, not nominal: without them two
Next.js processes and a Go API will happily exceed 2 GB together and the kernel
will choose what to kill.

| Service | Limit | Typical |
|---|---|---|
| caddy | 64 MB | ~20 MB |
| api | 256 MB | 50–100 MB |
| worker | 192 MB | 30–50 MB |
| outbox | 96 MB | 20–30 MB |
| redis | 128 MB | ~50 MB (capped at 96 MB of data) |
| web | 320 MB | 150–200 MB |
| admin | 256 MB | 150–200 MB |
| **Total** | **1.3 GB** | leaves ~700 MB for the OS and page cache |

Watch it with `docker stats --no-stream`. A service that is restarted
repeatedly by the OOM killer shows up as a climbing restart count in
`docker compose ps`.

---

## 2. Host preparation

Run as root once. These notes were written for Debian or Ubuntu; the host in
use runs **AlmaLinux 9.8**, so the firewall section below gives both paths.

```bash
# A login that is not root.
adduser --gecos "" deploy
usermod -aG sudo deploy      # Debian/Ubuntu; on AlmaLinux/RHEL: usermod -aG wheel deploy
usermod -aG docker deploy    # the deploy user runs docker compose directly
install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
# Paste the operator's public key — key authentication only, no passwords.
install -m 600 -o deploy -g deploy /dev/stdin /home/deploy/.ssh/authorized_keys <<'KEY'
ssh-ed25519 AAAA… ops@denisco
KEY
```

SSH hardening — in `/etc/ssh/sshd_config`:

```
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
AllowUsers deploy
X11Forwarding no
MaxAuthTries 3
ClientAliveInterval 300
```

```bash
sshd -t && systemctl reload ssh     # Debian/Ubuntu
sshd -t && systemctl reload sshd    # AlmaLinux/RHEL
```

Keep the existing session open until a second one logs in successfully.

Firewall — only SSH and the web ports. Debian/Ubuntu:

```bash
apt-get install -y ufw fail2ban
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80,443/tcp
ufw allow 443/udp                  # HTTP/3
ufw enable
systemctl enable --now fail2ban    # ships with an sshd jail
```

AlmaLinux/RHEL (firewalld is already running; fail2ban comes from EPEL):

```bash
firewall-cmd --permanent --add-service=ssh
firewall-cmd --permanent --add-service=http
firewall-cmd --permanent --add-service=https
firewall-cmd --permanent --add-port=443/udp      # HTTP/3
firewall-cmd --reload

dnf install -y epel-release && dnf install -y fail2ban
systemctl enable --now fail2ban
```

Docker publishes container ports by writing its own iptables rules, which ufw
does not see. That is why `compose.yaml` uses `expose` rather than `ports` for
everything but Caddy: an accidental `ports:` entry on Redis would be reachable
from the internet despite the rules above.

Docker, swap and unattended upgrades:

```bash
curl -fsSL https://get.docker.com | sh
usermod -aG docker deploy

# 1 GB of swap turns a brief memory spike into slowness rather than a kill.
fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab
sysctl -w vm.swappiness=10 && echo 'vm.swappiness=10' > /etc/sysctl.d/99-swap.conf

apt-get install -y unattended-upgrades
```

---

## 3. Cloudflare DNS

Four records, all proxied (orange cloud), pointing at the VPS address. The
apex is canonical: `www` is served by Caddy as a 301 back to it:

| Type | Name | Content | Proxy |
|---|---|---|---|
| A | `@` (apex) | VPS IPv4 | Proxied |
| A | `www` | VPS IPv4 | Proxied |
| A | `admin` | VPS IPv4 | Proxied |
| A | `api` | VPS IPv4 | Proxied |

Settings that matter:

- **SSL/TLS mode: Full (strict).** Caddy holds a real Let's Encrypt
  certificate, so anything less would be either a broken chain or an
  unencrypted hop.
- **Always Use HTTPS: on.** Caddy also redirects, this just saves the trip.
- **Caching: bypass `api.deniscoglobalagroltd.com`.** A cached `POST` response or a cached
  authenticated `GET` would serve one customer another's data. The site and
  dashboard are Next.js apps that set their own cache headers.
- **WAF/Bot Fight Mode: keep it off `api.deniscoglobalagroltd.com`**, or Paystack's
  webhook deliveries will be challenged and silently dropped.

Issuing certificates works through the proxy over HTTP-01. If a wildcard
certificate is ever wanted, build Caddy with `caddy-dns/cloudflare` and switch
to DNS-01 — see the comment at the top of the `Caddyfile`.

---

## 4. First deployment

```bash
sudo install -d -m 750 -o deploy -g deploy /opt/denisco
cd /opt/denisco
git clone https://github.com/Dev-folabi/denisco_backend.git
git clone https://github.com/Dev-folabi/denisco_web.git      # only needed to build locally
git clone https://github.com/Dev-folabi/denisco_admin.git

# The API's secrets, beside the checkouts rather than inside one, so no
# clone can carry them. ENV_FILE in deployments/.env points here.
sudo install -o deploy -g deploy -m 600 denisco_backend/.env.example /opt/denisco/.env
sudo editor /opt/denisco/.env
```

Production values that are not defaults:

```ini
APP_ENV=production
API_BASE_URL=https://api.deniscoglobalagroltd.com
WEB_ORIGIN=https://deniscoglobalagroltd.com
ADMIN_ORIGIN=https://admin.deniscoglobalagroltd.com
COOKIE_SECURE=true                 # refused at startup otherwise
COOKIE_DOMAIN=.deniscoglobalagroltd.com   # so api. and the site share the refresh cookie
COOKIE_SAMESITE=lax
CLIENT_IP_HEADER=X-Real-IP          # set by Caddy from {client_ip}; see §6
AUTH_EXPOSE_RESET_TOKEN=false
RATE_LIMIT_ENABLED=true
LOG_LEVEL=info
JWT_ED25519_SEED=                  # openssl rand -base64 32
JWT_ACCESS_SECRET=                 # openssl rand -base64 48
MONGODB_URI=                       # Atlas SRV string, least-privilege user
PAYSTACK_SECRET_KEY=               # live key
RESEND_API_KEY=
ADMIN_NOTIFICATION_EMAIL=deniscoglobalagroltd@gmail.com
```

The Atlas user needs `readWrite` on the one database and nothing else, and the
cluster's network access list should hold the VPS address only — not
`0.0.0.0/0`.

Then the stack:

```bash
cd denisco_backend/deployments
cp .env.example .env && editor .env     # hostnames, registry, TAG, public URLs

docker compose pull                     # images from CI
docker compose up -d
docker compose ps                       # every service healthy?
curl -fsS https://api.deniscoglobalagroltd.com/health/ready
```

To build on the host instead of pulling (slow, and memory-hungry for the
Next.js builds — avoid on a 2 GB box unless the stack is stopped):

```bash
docker compose build && docker compose up -d
```

Seed the first administrator, the catalogue and the consultation types once.
These are Go programs under `scripts/`, and the VPS has no toolchain, so they
run in a throwaway container against the production environment file:

```bash
cd /opt/denisco/denisco_backend

seed() {
  docker run --rm -it \
    --env-file /opt/denisco/.env \
    --volume "$PWD:/src" --workdir /src \
    golang:1.27-alpine go run "$@"
}

seed ./scripts/seed-admin -email deniscoglobalagroltd@gmail.com
seed ./scripts/seed-catalogue
seed ./scripts/seed-consultations
```

`seed-admin` prompts for the password; the catalogue and consultation seeds
are idempotent, so re-running them updates rather than duplicates.

---

## 5. Releases and rollback

CI builds and pushes `denisco-api`, `denisco-worker`, `denisco-outbox`,
`denisco-web` and `denisco-admin`, tagged with the commit SHA. A push to
`main` then deploys automatically, each repository restarting only its own
services through `deployments/deploy.sh`:

| Repository | Services restarted |
|---|---|
| `denisco_backend` | `api`, `worker`, `outbox` |
| `denisco_web` | `web` |
| `denisco_admin` | `admin` |

The deploy job in each workflow needs three things configured on GitHub:

- **Secret `VPS_SSH_KEY`** — a private key whose public half is in
  `deploy`'s `authorized_keys` on the VPS.
- **Variable `VPS_HOST`** — the VPS address (or hostname).
- **Variable `DEPLOY_ENABLED`** — set it to `true` to turn the deploy job
  on. Until then the job is skipped and CI only builds and publishes.

`deploy.sh` writes the new SHA tag into `.env` for the affected services,
pulls, restarts only those containers, and health-checks them. On failure it
restores the previous tags and restarts again, so a bad release costs a
minute rather than an outage. The deploy jobs also appear under each
workflow's "Run workflow" button, where the optional `tag` input rolls any
service back to an earlier SHA.

To roll back manually on the host:

```bash
cd /opt/denisco/denisco_backend/deployments
sed -i 's/^API_TAG=.*/API_TAG=<previous-sha>/' .env   # or WORKER_TAG, WEB_TAG, …
docker compose pull && docker compose up -d
docker compose ps && curl -fsS https://api.deniscoglobalagroltd.com/health/ready
```

Because the API shuts down gracefully — it stops accepting connections,
finishes the requests in flight, then exits — a release drops no in-flight
checkout.

Database migrations are not part of a release: indexes are created on startup
and are idempotent.

Housekeeping, monthly:

```bash
docker image prune -af --filter "until=720h"
```

---

## 6. Health and logs

| Check | Command |
|---|---|
| Liveness | `curl -fsS https://api.deniscoglobalagroltd.com/healthz` |
| Readiness (Mongo + Redis) | `curl -fsS https://api.deniscoglobalagroltd.com/health/ready` |
| Container state | `docker compose ps` |
| Memory | `docker stats --no-stream` |
| Logs (one service) | `docker compose logs -f --tail=100 api` |
| Queue depth | `docker compose exec redis redis-cli INFO keyspace` |
| Unpublished events | `mongosh "$MONGODB_URI" --eval 'db.outbox_events.countDocuments({status:{$ne:"published"}})'` |

Logs are JSON on stdout, captured by Docker and capped at 3 × 10 MB per
service. Every API line carries the request ID, so a customer's report can be
traced through the API, the publisher and the worker.

The client address in those logs, and in the rate limiter's buckets, comes from
one header only: the one `CLIENT_IP_HEADER` names, which this deployment sets
to `X-Real-IP`. Caddy writes it from `{client_ip}`, which it resolves from the
forwarded chain only for connections from the Cloudflare ranges in its
`trusted_proxies` list and from the peer address otherwise. Leave
`CLIENT_IP_HEADER` empty in any deployment where the API can be reached without
passing through Caddy, and never set it to `X-Forwarded-For` — the API refuses
to start if you do.

`/health/ready` separates dependencies it cannot work without from ones it can:
MongoDB unreachable is `not_ready` (503), Redis unreachable is `degraded`
(200), because orders still commit and their events wait in the outbox. Alert
on either, but only the first means the API cannot serve.

Two numbers are worth alerting on. The first is outbox events stuck
unpublished — the worker or Redis is unwell. The second is orders flagged
`needs_reconciliation`: a payment that arrived after its stock reservation had
expired and could not be filled, so money is held against goods that cannot be
shipped and someone has to restock or refund. Each one emails
`ADMIN_NOTIFICATION_EMAIL` as it happens, and the standing list is:

```bash
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'https://api.deniscoglobalagroltd.com/api/v1/admin/orders?needs_reconciliation=true'
```

The admin dashboard does not yet surface that filter — the prototype, which is
the design's source of truth, has no control for it — so for now the email and
this endpoint are the way staff hear about one.

---

## 7. Restart and recovery behaviour

| Event | What happens |
|---|---|
| API restart | Caddy's liveness check takes it out of rotation; in-flight requests finish first |
| Worker restart | Asynq re-queues unacknowledged jobs; handlers are idempotent, so a repeated delivery sends no duplicate email |
| Redis restart | The append-only file replays the queue; the API keeps serving, and the publisher retries |
| Redis unavailable | Business writes still commit — events wait in `outbox_events` and publish when it returns. `/health/ready` reports `degraded`, but Caddy keeps routing, because it checks liveness |
| MongoDB unreachable | Writes fail cleanly with 5xx; `/health/ready` goes red; nothing is half-applied, because every multi-document change is a transaction |
| Host reboot | `restart: unless-stopped` brings every service back; Caddy's certificates are on a volume, so no re-issuance |

---

## 8. Backups

`backup.sh` writes a timestamped directory holding a gzipped `mongodump`
archive and a tar of Redis's data, keeps `BACKUP_RETENTION_DAYS` of them and
prunes the rest.

```bash
cd /opt/denisco/denisco_backend/deployments
./backup.sh
```

Nightly:

```cron
15 2 * * * cd /opt/denisco/denisco_backend/deployments && ./backup.sh >> /var/log/denisco-backup.log 2>&1
```

Copy the directory off the host as well — a backup on the disk that fails is
not a backup. Atlas's own continuous backups cover the database; this exists so
a restore does not depend on the provider's console, and so the path has been
walked at least once.

### The restore drill

Run this quarterly, and after any change to the data model. It touches nothing
live: the dump is restored beside the real database, under its own name.

```bash
./restore.sh /var/backups/denisco/<stamp>
# restore: the dump is in 'denisco_restore_<stamp>', beside the live database.
```

Then check the restored copy is usable, not merely present:

```bash
mongosh "$MONGODB_URI" --eval '
  const db = db.getSiblingDB("denisco_restore_<stamp>");
  ["users","orders","payments","consultation_bookings","products"].forEach(c =>
    print(c, db.getCollection(c).countDocuments()));
  printjson(db.orders.find().sort({created_at:-1}).limit(1).toArray()[0]);
'
```

The counts should match the live database within a day's trading, and the
newest order should look like an order. Drop the copy when finished:

```bash
mongosh "$MONGODB_URI" --eval 'db.getSiblingDB("denisco_restore_<stamp>").dropDatabase()'
```

A real recovery, when the live data is gone or corrupt:

```bash
./restore.sh /var/backups/denisco/<stamp> --in-place
```

That stops the API, worker and publisher, replaces the database and Redis's
volume, starts them again and waits for `/health/ready`. It asks for the
database name before doing anything.

---

## 9. Secrets

- `/opt/denisco/.env` — deploy-owned, mode 600, beside the checkouts rather
  than inside one, never in the repository.
- Rotating `JWT_ED25519_SEED` or `JWT_ACCESS_SECRET` invalidates every access
  token immediately; refresh tokens survive, so customers are signed back in
  on their next refresh rather than logged out.
- Rotating the Paystack key: update Atlas-side nothing, set the new key, then
  `docker compose up -d api worker` — the webhook signature is checked with
  the same key, so update Paystack's dashboard and the host together.
- `docker compose config` prints substituted values. Keep the API's secrets in
  `ENV_FILE` (which Compose does not expand) rather than in `deployments/.env`.
