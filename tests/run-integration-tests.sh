#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export FREDB_DATA_ROOT="/tmp/fredb-test-data"
export FREDB_SOCK_ROOT="/tmp/fredb-test-socks"
export FREDB_API_ADDR=":8090"
export FREDB_ADMIN_ADDR=":8091"

echo "building engine-server..."
cmake -S "$ROOT/engine-server" -B "$ROOT/engine-server/build" -DCMAKE_BUILD_TYPE=Release -Wno-dev >/dev/null
cmake --build "$ROOT/engine-server/build" --parallel >/dev/null

echo "building fredb-server..."
(cd "$ROOT/server" && CGO_ENABLED=0 go build -o fredb-server .)

pkill -f engine-server 2>/dev/null || true
pkill -f "$ROOT/server/fredb-server" 2>/dev/null || true
rm -rf "$FREDB_DATA_ROOT" "$FREDB_SOCK_ROOT"

cleanup() {
	kill "$SERVER_PID" 2>/dev/null || true
	wait "$SERVER_PID" 2>/dev/null || true
	pkill -f engine-server 2>/dev/null || true
	rm -rf "$FREDB_DATA_ROOT" "$FREDB_SOCK_ROOT"
}
trap cleanup EXIT

export PATH="$ROOT/engine-server/build:$PATH"
"$ROOT/server/fredb-server" &
SERVER_PID=$!

echo "waiting for admin port..."
for _ in $(seq 1 50); do
	curl -s -o /dev/null -X POST "http://localhost:8091/keys" -d '' && break
	sleep 0.1
done

export FREDB_TEST_BASE_URL="http://localhost:8090"
export FREDB_TEST_ADMIN_URL="http://localhost:8091"

export HOME="${HOME:-/tmp}"
(cd "$ROOT/tests" && CGO_ENABLED=0 go test ./... -v "$@")
