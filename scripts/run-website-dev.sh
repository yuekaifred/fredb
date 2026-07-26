#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
exec templ generate -path website -watch -cmd "$root/scripts/run-server.sh $*"
