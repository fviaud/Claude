#!/usr/bin/env bash
# Smoke test of the Docker image: starts PostgreSQL and the API in
# containers, checks the main behaviours over HTTP, then cleans up.
#
# Usage: scripts/smoke-test.sh
# Variables:
#   IMAGE       image to test (default claude:smoke-test)
#   SKIP_BUILD  set to 1 to reuse an existing IMAGE instead of building it
#   PORT        host port for the API (default 18080)
set -euo pipefail

IMAGE=${IMAGE:-claude:smoke-test}
PORT=${PORT:-18080}
PREFIX=claude-smoke-$$
NET=$PREFIX-net
PG=$PREFIX-pg
API=$PREFIX-api
KEY=smoke-test-key
URL=http://127.0.0.1:$PORT
DB="postgres://claude:claude@$PG:5432/claude?sslmode=disable"

failures=0

cleanup() {
	local status=$?
	if [[ $status -ne 0 || $failures -ne 0 ]]; then
		echo "--- API logs"
		docker logs "$API" 2>&1 | tail -30 || true
	fi
	docker rm -f "$API" "$PG" >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT

log() { printf '\n== %s\n' "$*"; }

# wait_for <description> <timeout seconds> <command...>
wait_for() {
	local what=$1 timeout=$2
	shift 2
	for ((i = 0; i < timeout; i++)); do
		if "$@" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	echo "timeout waiting for $what" >&2
	return 1
}

# check <description> <expected status> <curl args...>
check() {
	local what=$1 want=$2
	shift 2
	local got
	got=$(curl -s -o /dev/null -w '%{http_code}' "$@") || got="curl-error"
	if [[ $got == "$want" ]]; then
		printf '  ok    %-45s %s\n' "$what" "$got"
	else
		printf '  FAIL  %-45s got %s, want %s\n' "$what" "$got" "$want"
		failures=$((failures + 1))
	fi
}

if [[ ${SKIP_BUILD:-0} != 1 ]]; then
	log "Building $IMAGE"
	docker build -t "$IMAGE" "$(dirname "$0")/.."
fi

log "Starting PostgreSQL"
docker network create "$NET" >/dev/null
docker run -d --name "$PG" --network "$NET" \
	-e POSTGRES_USER=claude -e POSTGRES_PASSWORD=claude -e POSTGRES_DB=claude \
	postgres:16-alpine >/dev/null
wait_for "PostgreSQL" 60 docker exec "$PG" pg_isready -U claude -d claude -h 127.0.0.1

log "API refuses to start without API_KEYS"
if out=$(docker run --rm --network "$NET" -e DATABASE_URL="$DB" "$IMAGE" 2>&1); then
	echo "  FAIL  container exited with status 0"
	failures=$((failures + 1))
elif grep -q "API_KEYS is empty" <<<"$out"; then
	echo "  ok    exits with an error mentioning API_KEYS"
else
	echo "  FAIL  unexpected output: $out"
	failures=$((failures + 1))
fi

log "Starting the API"
docker run -d --name "$API" --network "$NET" -p "127.0.0.1:$PORT:8080" \
	-e DATABASE_URL="$DB" -e API_KEYS="other-key,$KEY" "$IMAGE" >/dev/null
wait_for "API" 30 curl -sf "$URL/healthz"

log "HTTP checks"
check "GET /healthz without key" 200 "$URL/healthz"
check "GET /products without key" 401 "$URL/products"
check "GET /products with wrong key" 401 -H "X-API-Key: wrong" "$URL/products"
check "GET /products with X-API-Key" 200 -H "X-API-Key: $KEY" "$URL/products"
check "GET /products with Bearer token" 200 -H "Authorization: Bearer $KEY" "$URL/products"
check "POST /categories" 201 -H "X-API-Key: $KEY" -X POST -d '{"name":"Meubles"}' "$URL/categories"
check "POST /products" 201 -H "X-API-Key: $KEY" -X POST \
	-d '{"reference":"C1","name":"Chaise","price_cents":4999,"stock":5,"category_id":1}' "$URL/products"
check "POST /products duplicate reference" 409 -H "X-API-Key: $KEY" -X POST \
	-d '{"reference":"C1","name":"Autre"}' "$URL/products"
check "POST /products/1/stock -3" 200 -H "X-API-Key: $KEY" -X POST -d '{"delta":-3}' "$URL/products/1/stock"
check "POST /products/1/stock -3 (insufficient)" 409 -H "X-API-Key: $KEY" -X POST -d '{"delta":-3}' "$URL/products/1/stock"
check "GET /products/1" 200 -H "X-API-Key: $KEY" "$URL/products/1"

stock=$(curl -s -H "X-API-Key: $KEY" "$URL/products/1" | grep -o '"stock":[0-9]*' | cut -d: -f2)
if [[ $stock == 2 ]]; then
	echo "  ok    stock is 2 after the adjustments"
else
	echo "  FAIL  stock is '$stock', want 2"
	failures=$((failures + 1))
fi

log "Container checks"
user=$(docker inspect "$API" --format '{{.Config.User}}')
if [[ $user == nonroot* ]]; then
	echo "  ok    runs as $user"
else
	echo "  FAIL  runs as '$user', want nonroot"
	failures=$((failures + 1))
fi
if wait_for "healthy status" 40 bash -c "[[ \$(docker inspect $API --format '{{.State.Health.Status}}') == healthy ]]"; then
	echo "  ok    Docker reports the container as healthy"
else
	failures=$((failures + 1))
fi
docker stop -t 15 "$API" >/dev/null
if docker logs "$API" 2>&1 | grep -q "shutting down"; then
	echo "  ok    graceful shutdown on SIGTERM"
else
	echo "  FAIL  no graceful shutdown message"
	failures=$((failures + 1))
fi

echo
if [[ $failures -ne 0 ]]; then
	echo "Smoke test FAILED: $failures check(s) failed"
	exit 1
fi
echo "Smoke test passed"
