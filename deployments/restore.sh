#!/usr/bin/env bash
#
# Restores a dump taken by backup.sh.
#
#   ./restore.sh /var/backups/denisco/20261006T021500Z            # into a copy
#   ./restore.sh /var/backups/denisco/20261006T021500Z --in-place # over the live data
#
# By default the database is restored under a new name
# (<database>_restore_<stamp>) so the contents can be checked before anything
# live is touched — which also makes this safe to run as a drill. --in-place
# overwrites the configured database and asks for confirmation first.
#
# Redis is restored by stopping the container, replacing the contents of its
# volume and starting it again; its data is the job queue, so it is only worth
# restoring when recovering a host rather than when inspecting a dump.

set -euo pipefail

cd "$(dirname "$0")"

ARCHIVE_DIR="${1:-}"
MODE="${2:-}"

if [[ -z "$ARCHIVE_DIR" || ! -d "$ARCHIVE_DIR" ]]; then
	echo "usage: $0 <backup directory> [--in-place]" >&2
	exit 1
fi

if [[ -f .env ]]; then
	set -a
	# shellcheck disable=SC1091
	source .env
	set +a
fi

API_ENV_FILE="${ENV_FILE:-../.env}"

MONGODB_URI="$(grep -E '^MONGODB_URI=' "$API_ENV_FILE" | head -1 | cut -d= -f2-)"
MONGODB_DATABASE="$(grep -E '^MONGODB_DATABASE=' "$API_ENV_FILE" | head -1 | cut -d= -f2-)"

if [[ -z "$MONGODB_URI" || -z "$MONGODB_DATABASE" ]]; then
	echo "restore: MONGODB_URI or MONGODB_DATABASE is missing from $API_ENV_FILE" >&2
	exit 1
fi

if [[ ! -f "$ARCHIVE_DIR/mongodb.archive.gz" ]]; then
	echo "restore: $ARCHIVE_DIR holds no mongodb.archive.gz" >&2
	exit 1
fi

cat "$ARCHIVE_DIR/manifest.txt" 2>/dev/null || true

TARGET_DB="${MONGODB_DATABASE}_restore_$(date -u +%Y%m%dT%H%M%SZ)"
DROP_FLAG=""

if [[ "$MODE" == "--in-place" ]]; then
	TARGET_DB="$MONGODB_DATABASE"
	# Without --drop, a restore merges into whatever is already there and
	# leaves a mixture of old and new documents behind.
	DROP_FLAG="--drop"

	echo
	echo "This will REPLACE the live database '$MONGODB_DATABASE'."
	read -r -p "Type the database name to continue: " confirmation
	if [[ "$confirmation" != "$MONGODB_DATABASE" ]]; then
		echo "restore: aborted" >&2
		exit 1
	fi

	echo "restore: stopping the writers so nothing is written mid-restore"
	docker compose stop api worker outbox
fi

echo "restore: restoring MongoDB into '$TARGET_DB'"

docker run --rm \
	--env "MONGODB_URI=$MONGODB_URI" \
	--volume "$ARCHIVE_DIR:/dump:ro" \
	mongo:8 \
	sh -c "mongorestore --uri=\"\$MONGODB_URI\" --gzip --archive=/dump/mongodb.archive.gz \
		--nsFrom='$MONGODB_DATABASE.*' --nsTo='$TARGET_DB.*' $DROP_FLAG"

if [[ -f "$ARCHIVE_DIR/redis-data.tar.gz" && "$MODE" == "--in-place" ]]; then
	echo "restore: restoring Redis"

	docker compose stop redis
	# The volume is reached through a throwaway container because the Redis
	# container itself is down.
	docker run --rm \
		--volumes-from "$(docker compose ps -aq redis)" \
		--volume "$ARCHIVE_DIR:/dump:ro" \
		alpine:3.22 \
		sh -c 'rm -rf /data/* && tar -xzf /dump/redis-data.tar.gz -C /data'
	docker compose up -d redis
fi

if [[ "$MODE" == "--in-place" ]]; then
	echo "restore: starting the writers again"
	docker compose up -d api worker outbox

	echo "restore: waiting for the API to report ready"
	for _ in $(seq 1 30); do
		if docker compose exec -T api curl -fsS http://localhost:4000/health/ready >/dev/null 2>&1; then
			echo "restore: the API is ready"
			break
		fi
		sleep 2
	done
else
	echo
	echo "restore: the dump is in '$TARGET_DB', beside the live database."
	echo "Check it, then either point MONGODB_DATABASE at it or re-run with --in-place."
fi

echo "restore: done"
