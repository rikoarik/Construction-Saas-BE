# ADR: Exact Numeric Arithmetic

**Status:** Accepted for implementation semantics; storage precision and report rounding remain open product decisions.

## Context

Quantities, money, weights, rates, and percentages affect approved RAB values, progress, and published documents. Binary floating-point can introduce representation and accumulation errors. The canonical calculation rules require exact decimal semantics and explicitly leave storage precision, export rounding, and residual assignment to documented policy/golden fixtures.

## Decision

- Go calculation code uses `internal/decimal.Decimal`, backed by exact rational arithmetic. It does not convert through `float32` or `float64`.
- Parsing accepts base-10 integer and decimal strings only. Division remains exact; non-terminating results retain a rational representation until an explicit boundary formats them.
- PostgreSQL stores authoritative numeric values using `NUMERIC`, never `REAL`/`DOUBLE PRECISION`. Each domain migration must choose explicit precision/scale only after the required domain range and rounding policy are approved; do not guess a universal scale.
- `Decimal.StringFixed` implements round-half-away-from-zero as an available explicit operation. It is not the default product/export policy. Callers may use it only where a contract or approved report policy specifies that rounding rule.
- Zero denominators are validation/business-rule errors. Overage is preserved and surfaced for review; arithmetic does not clamp it.
- Calculations retain exact intermediates. Display/export rounding and row-residual assignment are separate policies and must not alter ledger or stored source values.

## Consequences

Callers must handle division errors and choose formatting at an explicit boundary. Database schema tasks must document each selected `NUMERIC(p,s)` and provide boundary/golden tests. The unresolved decisions listed in `docs/architecture/CALCULATION-RULES.md` remain open; this ADR does not settle them.

## Verification

Golden tests in `backend/internal/decimal/decimal_test.go` cover the 82.04 quantity example, exact amount multiplication, zero values, division by zero, repeating division, parsing rejection, and explicit rounding behavior.
