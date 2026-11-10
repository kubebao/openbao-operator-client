#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
for manifest in go.mod go.sum; do
  if [[ -f "$manifest" ]]; then cp "$manifest" "$scratch/$manifest"; else : > "$scratch/$manifest"; fi
done
go mod tidy
for manifest in go.mod go.sum; do
  if [[ -f "$manifest" ]]; then diff -u "$scratch/$manifest" "$manifest"; else test ! -s "$scratch/$manifest"; fi
done
