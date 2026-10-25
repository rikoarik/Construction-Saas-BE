#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ruby "$ROOT/scripts/validate-openapi.rb" "$ROOT/api/openapi.yaml"
