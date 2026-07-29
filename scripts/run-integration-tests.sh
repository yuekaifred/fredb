#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export FREDB_DATA_ROOT="/tmp/fredb-test-data"
export FREDB_API_ADDR=":8090"
export FREDB_WEBSITE_ADDR=":8091"
export FREDB_MAX_STORAGE_BYTES="262144" # 256KB, small so storage-limit tests dont need to write real gigabytes
export FREDB_RATE_LIMIT_CAPACITY_BYTES="400000"
export FREDB_RATE_LIMIT_REFILL_BYTES_PER_SEC="1"    # near-frozen, tests run in well under a second
export FREDB_RATE_LIMIT_FLAT_OVERHEAD_BYTES="500"
export FREDB_ADMIN_KEY="test-admin-key"
export FREDB_REAP_INTERVAL_SECONDS="1"

echo "building fredb engine..."
cmake -S "$ROOT/engine" -B "$ROOT/engine/build" -DCMAKE_BUILD_TYPE=Release -Wno-dev >/dev/null
cmake --build "$ROOT/engine/build" --target fredb_core --parallel >/dev/null

echo "building fredb-server..."
(cd "$ROOT/server" && CGO_ENABLED=1 CXX=clang++ go build -o fredb-server .)

pkill -f "$ROOT/server/fredb-server" 2>/dev/null || true
rm -rf "$FREDB_DATA_ROOT"

cleanup() {
	kill "$SERVER_PID" 2>/dev/null || true
	wait "$SERVER_PID" 2>/dev/null || true
	rm -rf "$FREDB_DATA_ROOT"
}
trap cleanup EXIT

"$ROOT/server/fredb-server" &
SERVER_PID=$!

echo "waiting for server..."
for _ in $(seq 1 50); do
	curl -s -o /dev/null "http://localhost:8091/" && break
	sleep 0.1
done

export FREDB_TEST_BASE_URL="http://localhost:8090"
export FREDB_TEST_ADMIN_URL="http://localhost:8091"
export FREDB_TEST_ADMIN_KEY="test-admin-key"

export HOME="${HOME:-/tmp}"
(cd "$ROOT/integration-tests" && CGO_ENABLED=0 go test ./... -v "$@")
