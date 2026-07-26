#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../server"
bin="$(mktemp)"
trap 'rm -f "$bin"' EXIT
go build -o "$bin" .
exec "$bin" "$@"
