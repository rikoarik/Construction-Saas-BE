# Calculation Rules — RAB, Progres, Kurva-S

Source of truth for implementations and tests. Formula definitions aligned to `docs/product/BLUEPRINT.md` §5.

## Quantity-based work item

`amount_i = contracted_qty_i × unit_price_i`

`budget_total = Σ amount_i` over eligible **leaf items** of approved budget version (exclude category headers/subtotal).

`weight_i = amount_i / budget_total × 100`

`approved_cumulative_qty_i(t) = Σ ledger.qty_delta` for approved postings effective on/before cutoff `t`, including explicit corrections.

`item_pct_i(t) = approved_cumulative_qty_i(t) / contracted_qty_i × 100`

`project_pct(t) = Σ [weight_i × item_pct_i(t) / 100]` for eligible items.

**Undefined cases:** `budget_total = 0`, `contracted_qty = 0` → validation business rule, not divide-by-zero fallback. Overage requires review; do not silently clamp raw data.

## Report period P = [start_date, end_date]

- `previous_qty = Σ ledger.delta where effective_date < start_date`.
- `current_qty = Σ ledger.delta where start_date <= effective_date <= end_date`.
- `to_date_qty = previous_qty + current_qty`.
- For each, value = quantity × approved version's price; weight = value / same version's budget total × 100.
- Report IDs must fix `budget_version_id`, `baseline_id`, timezone/week boundary, cutoff, and template_version.

## Time schedule

- `plan_week_i,t = planned_contribution_pct_i,t` from approved baseline.
- `plan_week_t = Σ_i plan_week_i,t` and `plan_cumulative_t = Σ_{week≤t} plan_week`.
- `actual_week_t = actual_cumulative_t - actual_cumulative_(t-1)`.
- `deviation_pp_t = actual_cumulative_t - plan_cumulative_t` (percentage points).
- For template output, percentage formatting should be clearly labeled; 6.5 pp ahead is **not** +6.5% relative growth.

## Snapshot compatibility

Never recompute previously published reports against newly approved addendum without explicit reissue/revision. Effective price/version policy must be recorded for cross-version quantities; changes require ADR and fixture tests.

## Other measurement methods

- **Milestone:** item internal stage weights total 100%; only approved stages count.
- **Verified percentage:** cumulative approved percentage with evidence and review, monotone except explicit correction. Conversion between methods requires versioning and data reconciliation.

## Edge tests

1. Source example: approved 63.00 m³ + 19.04 m³ = 82.04 m³, contract 82.04 m³ → item 100%.
2. Retry approved report does not create extra ledger entry.
3. 0 qty/0 budget, LS, negative correction, cross-week cutoff, leap date/timezone, duplicate work_item, approval race, overcontract.
4. Sum displayed per-row roundings may differ from true total; reconcile by explicit policy in export, not by altering stored ledger.
