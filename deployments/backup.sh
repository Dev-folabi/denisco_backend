#!/usr/bin/env bash
#
# Backs up everything that cannot be rebuilt from the repository:
#
#   - the MongoDB Atlas database (orders, payments, bookings, accounts)
#   - Redis's append-only file and snapshot (queued jobs not yet delivered)
#
# Both go into one timestamped directory under BACKUP_DIR, with a manifest
# recording what was taken and from where, and dumps older than
# BACKUP_RETENTION_DAYS are removed. Run it from the deployments directory:
#
#   ./backup.sh
#
# As a nightly cron entry, writing its output where it can be read afterwards:
#
#   15 2 * * * cd /srv/denisco/denisco_backend/deployments \
#       && ./backup.sh >> /var/log/denisco-backup.log 2>&1
#
# Atlas keeps its own continuous backups; this exists so a restore does not
# depend on the provider's console, and so the restore path is one we have
# actually run. See the drill in README.md.

set -euo pipefail

cd "$(dirname "$0")"

# Stack variables (BACKUP_DIR, ENV_FILE, retention).
if [[ -f .env ]]; then
	set -a
	# shellcheck disable=SC1091
	source .env
	set +a
fi

BACKUP_DIR="${BACKUP_DIR:-/var/backups/denisco}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
API_ENV_FILE="${ENV_FILE:-../.env}"

if [[ ! -r "$API_ENV_FILE" ]]; then
	echo "backup: cannot read the API environment file at $API_ENV_FILE" >&2
	exit 1
fi

# MONGODB_URI carries credentials, so it is read from the API's environment
# file and never passed on a command line where `ps` would show it.
MONGODB_URI="$(grep -E '^MONGODB_URI=' "$API_ENV_FILE" | head -1 | cut -d= -f2-)"
MONGODB_DATABASE="$(grep -E '^MONGODB_DATABASE=' "$API_ENV_FILE" | head -1 | cut -d= -f2-)"

if [[ -z "$MONGODB_URI" || -z "$MONGODB_DATABASE" ]]; then
	echo "backup: MONGODB_URI or MONGODB_DATABASE is missing from $API_ENV_FILE" >&2
	exit 1
fi

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
TARGET="$BACKUP_DIR/$STAMP"
mkdir -p "$TARGET"
chmod 700 "$BACKUP_DIR" "$TARGET"

echo "backup: writing to $TARGET"

# ---------------------------------------------------------------------------
# MongoDB. mongodump runs in a throwaway container so the VPS needs no
# MongoDB tooling of its own; the URI is handed over through the environment.
# ---------------------------------------------------------------------------
docker run --rm \
	--env "MONGODB_URI=$MONGODB_URI" \
	--volume "$TARGET:/dump" \
	mongo:8 \
	sh -c "mongodump --uri=\"\$MONGODB_URI\" --db=$MONGODB_DATABASE --gzip --archive=/dump/mongodb.archive.gz"

# ---------------------------------------------------------------------------
# Redis. BGSAVE first so the snapshot on disk is current, then copy both the
# snapshot and the append-only file out of the volume.
# ---------------------------------------------------------------------------
if docker compose ps --status running --services | grep -qx redis; then
	docker compose exec -T redis redis-cli BGSAVE >/dev/null

	# BGSAVE is asynchronous: wait for it rather than copying a half-written
	# file. rdb_bgsave_in_progress drops to 0 when it is done.
	for _ in $(seq 1 30); do
		if docker compose exec -T redis redis-cli INFO persistence |
			tr -d '\r' | grep -qx 'rdb_bgsave_in_progress:0'; then
			break
		fi
		sleep 1
	done

	docker compose exec -T redis tar -cf - -C /data . >"$TARGET/redis-data.tar"
	gzip -f "$TARGET/redis-data.tar"
else
	echo "backup: redis is not running; skipping its snapshot" >&2
fi

# ---------------------------------------------------------------------------
# Manifest, so a restore knows what it is looking at.
# ---------------------------------------------------------------------------
cat >"$TARGET/manifest.txt" <<EOF
taken_at:        $STAMP
host:            $(hostname)
mongodb_db:      $MONGODB_DATABASE
image_tag:       ${TAG:-unset}
files:
$(cd "$TARGET" && ls -lh | tail -n +2)
EOF

chmod 600 "$TARGET"/*

# ---------------------------------------------------------------------------
# Retention. Keeping every dump fills a small disk, which is itself an outage.
# ---------------------------------------------------------------------------
find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -mtime "+$BACKUP_RETENTION_DAYS" \
	-exec rm -rf {} + -print |
	while read -r pruned; do echo "backup: pruned $pruned"; done

echo "backup: done"
du -sh "$TARGET"
