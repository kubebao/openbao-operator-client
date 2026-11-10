#!/usr/bin/env bash
# Fetch and compile a published commit without credentials or local replacements.
set -euo pipefail
exec python3 "$(dirname "$0")/check-consumer.py" "${1:?provide a published revision}"
