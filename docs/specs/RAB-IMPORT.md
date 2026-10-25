# RAB / BoQ Excel Import Specification

**Status:** Proposed · **Owner:** budget module (Go) · **Renderer:** Excelize for exports; ingestion parser may use Excelize too.

## Purpose

Allow different Excel RAB workbooks to map into one data model without custom development per contractor. Workbook can include multiple sheets, merged headers, headers/subtotals, formatted figures, different thousand/decimal separators, and blank rows.

## Mapping workflow

1. Upload `.xlsx` via private object storage; validate extension, actual content signature, size, sheet limits, formula/zip-bomb constraints.
2. Choose sheet, header row, data start/end; map `work_code`, `description`, `unit`, `contract_qty`, `unit_price`, `amount` (optional computed), and parent/group hints.
3. Infer candidate mapping only as UI suggestion; admin confirms explicitly. Saved mapping is **versioned** per company.
4. Parse staging rows with locale-aware numeric handling; disambiguate `1.234,56` vs `1,234.56` using explicit user preview, no guessing when ambiguous.
5. Detect headers, leaf items, subtotals, tax/footer rows; leaf-only eligible amount contributes to total and weight.
6. Show validation errors by sheet, row, column and code. Allow correction then re-parse.
7. Preview canonical WBS, qty, unit, price, amount, computed weight and total; compare optional workbook totals and flag discrepancy.
8. Finalize produces **draft** budget. Only authorized approval freezes budget version and amount/weights.

## Required validations

- Unique work item code per appropriate scope; same-project parent-child; no cycles; unknown unit mapping confirmed.
- Qty/price decimal strict; negative/zero policies defined and approved; no zero budget total if progress weighted by amount.
- Warn suspicious subtotal double counting; reject blank mandatory leaf item fields.
- Cell text beginning formula markers must be sanitized when exported to protect spreadsheet consumers.
- Do not execute arbitrary Excel formulas/macros; user-uploaded workbook is untrusted.
- Audit original file hash, mapping version, parse outcomes, approving user and budget version.

## Golden fixture strategy

Two workbooks with equivalent data but different labels and column order should yield identical canonical totals. Fixture includes qty `82.04`, prior posted qty `63.00` and current `19.04` (period reports test), LS item, subtotal row, tax row, localized number format, negative check and duplicate work code.

**Explicitly unsupported unless separately specified:** arbitrary macros, auto-repair of corrupt files, AI-only mapping approval, vendor-specific formulas interpreted as business truth.
