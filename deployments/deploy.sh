#!/usr/bin/env bash
#
# Deploys one or more services to a new image tag, restarts only those
# services, verifies their health, and rolls the tag back if the health
# check fails. Called by the GitHub Actions deploy jobs over SSH:
#
#   ./deploy.sh api,worker,outbox sha-0123…      # backend repository
#   ./deploy.sh web           sha-4567…          # web repository
#   ./deploy.sh admin         sha-89ab…          # admin repository
#
# The image tags live in deployments/.env (API_TAG, WEB_TAG, …). Only the
# lines for the named services are touched, a flock keeps two concurrent
# deployments from interleaving their writes, and Caddy, Redis and the other
# applications are left running untouched.
#
# On any failure — a pull that cannot find the tag, a container that never
# becomes healthy, an API whose readiness check fails — the previous tags are
# restored and the affected services restarted, so a bad release costs a
# minute rather than an outage.

set -euo pipefail

cd "$(dirname "$0")"

SERVICES="${1:-}"
TAG="${2:-}"

if [[ -z "$SERVICES" || -z "$TAG" ]]; then
	echo "usage: $0 <service[,service…]> <tag>" >&2
	echo "  services: api worker outbox web admin" >&2
	exit 2
fi

if [[ ! -f .env ]]; then
	echo "deploy: .env is missing from $(pwd)" >&2
	exit 1
fi

# Map a service name to the variable in .env holding its image tag.
tag_var() {
	case "$1" in
	api) echo API_TAG ;;
	worker) echo WORKER_TAG ;;
	outbox) echo OUTBOX_TAG ;;
	web) echo WEB_TAG ;;
	admin) echo ADMIN_TAG ;;
	*)
		echo "deploy: unknown service '$1' (api worker outbox web admin)" >&2
		return 1
		;;
	esac
}

IFS=',' read -r -a SERVICE_LIST <<<"$SERVICES"

# Validate every name before touching anything.
for svc in "${SERVICE_LIST[@]}"; do
	tag_var "$svc" >/dev/null
done

# One deployment at a time, across every repository's workflow.
exec 9>"$(pwd)/.deploy.lock"
if ! flock -w 300 9; then
	echo "deploy: another deployment holds $(pwd)/.deploy.lock" >&2
	exit 1
fi

env_get() { grep -E "^$1=" .env | head -1 | cut -d= -f2-; }

env_set() {
	local var="$1" value="$2"
	local escaped
	escaped="$(printf '%s' "$value" | sed -e 's/[\/&]/\\&/g')"
	sed -i "s/^$var=.*/$var=$escaped/" .env
}

# The tags being replaced, for the rollback path.
declare -a PREV_VARS=() PREV_VALUES=()
for svc in "${SERVICE_LIST[@]}"; do
	var="$(tag_var "$svc")"
	PREV_VARS+=("$var")
	PREV_VALUES+=("$(env_get "$var")")
	echo "deploy: $svc  $(env_get "$var") -> $TAG"
	env_set "$var" "$TAG"
done

rollback() {
	echo "deploy: FAILED — restoring previous tags" >&2
	local i=0
	for var in "${PREV_VARS[@]}"; do
		env_set "$var" "${PREV_VALUES[$i]}"
		i=$((i + 1))
	done
	docker compose pull "${SERVICE_LIST[@]}" || true
	docker compose up -d --no-deps "${SERVICE_LIST[@]}" || true
	echo "deploy: rolled back to ${PREV_VALUES[*]}" >&2
	exit 1
}

trap rollback ERR

# --------------------------------------------------------------------------
# Pull and restart only the named services.
# --------------------------------------------------------------------------
docker compose pull "${SERVICE_LIST[@]}"
docker compose up -d --no-deps "${SERVICE_LIST[@]}"

# --------------------------------------------------------------------------
# Health.
#
# api, web and admin carry Docker HEALTHCHECKs; worker and outbox do not, so
# for them "healthy" means the container is still running a few seconds after
# start rather than having exited or crash-looped.
# --------------------------------------------------------------------------
container_id() { docker compose ps -q "$1" | head -1; }

container_health() {
	local id
	id="$(container_id "$1")"
	[[ -n "$id" ]] || { echo "missing"; return; }
	docker inspect --format '{{.State.Status}}/{{if .State.Health}}{{.State.Health.Status}}{{else}}nohealthcheck{{end}}' "$id"
}

wait_healthy() {
	local svc="$1" timeout="${2:-90}" waited=0 status
	while ((waited < timeout)); do
		status="$(container_health "$svc")"
		case "$status" in
		running/healthy | running/nohealthcheck)
			# For a service with no healthcheck, require it to have been up
			# long enough that a crash loop would have shown itself.
			if [[ "$status" == "running/nohealthcheck" ]]; then
				sleep 5
				status="$(container_health "$svc")"
				[[ "$status" == "running/nohealthcheck" ]] || continue
			fi
			echo "deploy: $svc is $status"
			return 0
			;;
		missing)
			echo "deploy: $svc has no container" >&2
			;;
		*)
			echo "deploy: $svc is $status (${waited}s)" >&2
			;;
		esac
		sleep 3
		waited=$((waited + 3))
	done
	echo "deploy: $svc did not become healthy within ${timeout}s" >&2
	return 1
}

for svc in "${SERVICE_LIST[@]}"; do
	case "$svc" in
	api) wait_healthy api 90 ;;
	web | admin) wait_healthy "$svc" 90 ;;
	*) wait_healthy "$svc" 30 ;;
	esac
done

# The API's own readiness check: MongoDB reachable, Redis state reported.
# /health/ready rather than /healthz — a container that is up but cannot
# reach the database is not a successful deployment.
if [[ " ${SERVICE_LIST[*]} " == *" api "* ]]; then
	echo "deploy: checking /health/ready"
	if ! docker compose exec -T api curl -fsS http://localhost:4000/health/ready >/dev/null; then
		echo "deploy: API readiness check failed" >&2
		exit 1
	fi
	echo "deploy: API is ready"
fi

trap - ERR
echo "deploy: ${SERVICES} deployed at ${TAG}"
