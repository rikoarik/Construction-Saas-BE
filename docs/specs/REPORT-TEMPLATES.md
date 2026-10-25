# Report Output Contract — Core Official Templates

Based on **two reference screenshots** shared for a 2022 road-construction project in Kabupaten Majene. Screenshots are **layout and formula references**, not independent proof that all source numbers are correct. Use sanitized fixtures in tests; do not ship personal signatory data as demo defaults.

## A. Weekly Physical & Financial Progress (`weekly-progress-v1`)

**Header:** owner/agency, project and package name, contractor, contract number/date, MC no, week, start/end dates.

**WBS rows:** category/header, leaf work item, sub-total, grand total. Column grouping:

- `No`, `Uraian Pekerjaan`, `Sat`, `Volume Kontrak`, `Harga Satuan (Rp)`, `Jumlah Harga (Rp)`, `Bobot (%)`.
- `Minggu lalu`: volume, nilai pekerjaan, bobot kontribusi.
- `Minggu ini`: volume, nilai pekerjaan, bobot kontribusi.
- `s/d Minggu ini`: volume, nilai pekerjaan, bobot kumulatif.
- Contract subtotal, configurable tax row, final rounded contract value (if required).
- Signatures: dibuat, diperiksa, disetujui, mengetahui (configured per report template).

**Source:** approved `progress_ledger` fixed by report cutoff and approved RAB version; grouping never double-counts subtotal/header rows. All amount columns are **earned value of measured work**, not actual expenditures.

## B. Time Schedule / Kurva-S (`time-schedule-v1`)

**Header:** same contract/project identity + MC no/period; grouped weeks/months timeline with WBS details.

**Columns:** number, work item description, unit, contract quantity/price/amount, weight, planned contribution distribution weekly; sidebar/notes optional.

**Footer:** weekly plan, cumulative plan, weekly realization, cumulative realization, deviation (actual - plan) in percentage points. Graph S-curve must be generated from same data arrays used in table. Weekly dates and durations must be derived from baseline calendar, **not fixed to four weeks/month**.

## Versioning & publishing

- `template_id`,`template_version`,`budget_version_id`,`plan_baseline_id`,`report_period_id`,`ledger_cutoff`,`snapshot_hash` saved on publish.
- Reissues create a new revision. Keep originals; corrections are explicit with reason and author.
- UI preview and PDF/XLSX must use same snapshot and numeric policy. Excel formulas, if present, are presentation aid; backend calculates authoritative values.
- `status: draft → generated → approved/published` must be distinct from **daily report** approvals.

## Template extensibility

Allow known-safe branding, configurable signatory sets, column visibility/order, unit locale, page dimensions and label translations. Arbitrary execution or HTML injection in templates is forbidden. Validate user config against versioned JSON schema and render escaped content.

## Golden tests

- Fixture for 82.04m³ item (63.00 previous + 19.04 current = 100% item), category subtotals, LS items, tax/rounding, absent weeks.
- XLSX: defined sheet names, merged headers, consistent formulas/results, columns and number formats.
- PDF: logo/heading, legible table on landscape, repeat header rows, page numbers, signature placeholders, printable margins.
