#!/usr/bin/env bash
set -euo pipefail

FRONTEND_REPO="${FRONTEND_REPO:-../construction-frontend}"
BACKEND_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [[ ! -d "$FRONTEND_REPO" ]]; then
  echo "Frontend repo not found: $FRONTEND_REPO" >&2
  exit 1
fi

{
  echo '# API Contract Mirror — Frontend Consumer Copy'
  echo
  echo '> **GENERATED/READ ONLY.** Canonical source: backend repository `docs/API-CONTRACT.md`. Do not edit this mirror directly.'
  echo
  tail -n +2 "$BACKEND_ROOT/docs/specs/API-CONTRACT.md"
} > "$TMP/API-CONTRACT-MIRROR.md"

cmp "$TMP/API-CONTRACT-MIRROR.md" "$FRONTEND_REPO/docs/contracts/API-CONTRACT-MIRROR.md"
cmp "$BACKEND_ROOT/docs/product/PRD.md" "$FRONTEND_REPO/docs/product/PRD.md"

echo "Shared frontend docs are synchronized."
