#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   FRONTEND_REPO=../construction-frontend ./tools/sync_frontend_shared_docs.sh

FRONTEND_REPO="${FRONTEND_REPO:-../construction-frontend}"
BACKEND_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -d "$FRONTEND_REPO" ]]; then
  echo "Frontend repo not found: $FRONTEND_REPO" >&2
  exit 1
fi

mkdir -p "$FRONTEND_REPO/docs/contracts" "$FRONTEND_REPO/docs/product"

{
  echo '# API Contract Mirror — Frontend Consumer Copy'
  echo
  echo '> **GENERATED/READ ONLY.** Canonical source: backend repository `docs/API-CONTRACT.md`. Do not edit this mirror directly.'
  echo
  tail -n +2 "$BACKEND_ROOT/docs/specs/API-CONTRACT.md"
} > "$FRONTEND_REPO/docs/contracts/API-CONTRACT-MIRROR.md"

cp "$BACKEND_ROOT/docs/product/PRD.md" "$FRONTEND_REPO/docs/product/PRD.md"

echo "Synchronized PRD and API contract mirror into $FRONTEND_REPO/docs/contracts and docs/product"
