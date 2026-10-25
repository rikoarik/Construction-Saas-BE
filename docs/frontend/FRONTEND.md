# Frontend System Design — Construction SaaS

**Status:** normative · **Owner:** frontend lane · **Stack:** Next.js App Router, TypeScript, next-intl, TanStack Query, React Hook Form + Zod, Tailwind/CSS variables, Lucide, Recharts

This document is the source of truth for web and Telegram Mini App implementation. The frontend is designed for the complete product. Delivery can be phased, but navigation, information architecture, localization, theme tokens, API boundaries, and component behavior must support growth without a later rewrite.

Read `docs/frontend/DESIGN.md` before changing visual UI.

## 1. Frontend responsibilities

The frontend owns navigation, information architecture, interaction design, client-side validation for usability, localized presentation, optimistic UX where safe, accessible tables/forms/charts, theme and display preferences, report preview chrome, Telegram Mini App UX, and API state management.

The frontend does not own authoritative permissions, calculations, approval rules, budget totals, report publication truth, tenant isolation, or subscription enforcement. It may present server-provided capabilities but must expect the server to reject unauthorized actions.

## 2. Application surfaces

```text
apps/web/src/app/[locale]/
  (auth)/                login, invitation, recovery
  (app)/
    companies/
    projects/
      [projectId]/
        overview/
        wbs/
        budget/
        schedule/
        reports/
        approvals/
        documents/
        costs/
        materials/
        team/
        settings/
    administration/
    billing/
  telegram/              Mini App routes
```

A project shell provides consistent project switcher, project state, role-aware navigation, theme control, locale control, global command/search entry where useful, and contextual actions.

## 3. Contract-first API consumption

`backend/api/openapi.yaml` is the contract. Generate TypeScript API types/client helpers from it. Do not manually duplicate request/response interfaces. Frontend can work in parallel with backend using versioned mock fixtures that conform to the same OpenAPI schemas.

Stable backend error codes map to i18n keys. Never render raw backend error strings as final UI copy. Preserve field-error paths for inline validation.

## 4. i18n

Default UI locale is English; Indonesian is supported. All user-visible app strings use `next-intl` message keys: navigation, labels, buttons, tooltips, dialogs, validation, status names, empty states, aria labels, notifications, and table headings.

Do not hardcode English in JSX/TSX. User-generated names/notes remain as entered. Official document language is tied to a report-template/publication setting, not the current viewer locale.

Message namespaces should follow domain/surface, for example `projects`, `budget`, `reports`, `approvals`, `documents`, `costs`, `materials`, `billing`, `common`.

## 5. Light/dark/system themes

Theme preference is `light | dark | system`; resolved theme is applied before paint. Use semantic CSS variables only. UI components cannot invent ad-hoc hex colors. Dark mode is separately reviewed; it is not a simple filter or inversion. Print preview always uses explicit paper/print tokens.

The canonical palette, contrast rules, spacing, typography, icons, and asset policy live in `docs/frontend/DESIGN.md`.

## 6. Information density

This product is data-heavy. Primary screens favor well-designed tables, split panes, filters, summaries, exception queues, and contextual charts rather than grids of decorative cards.

Desktop budget/WBS/schedule screens may be dense and horizontally scrollable. Field-report and approval screens prioritize touch targets and one-handed mobile use. Do not force a desktop table into a mobile viewport; design a task-specific mobile representation.

## 7. State and data fetching

Use TanStack Query for server state and cache invalidation. Keep local UI state local. Avoid a global client store for server data. URL search params own shareable filters, sorting, tabs, date ranges, and selected views where sensible.

After mutations, use server-returned canonical data. Optimistic updates are allowed for reversible, low-risk operations; never optimistic-finalize budget approval, report approval, document publication, subscription changes, or other legally/financially meaningful operations.

## 8. Forms

Use React Hook Form and Zod at the UI boundary where it improves developer ergonomics. Backend validation remains authoritative. Inputs have visible labels, contextual units, clear error placement, keyboard flow, and preserved drafts where a user reasonably expects it.

Large workflows such as RAB import, budget editing, baseline planning, and publication use dedicated pages/steppers—not nested dialogs.

## 9. Data tables

Use virtualization where row counts justify it. Support sticky identifier columns, numeric alignment, locale-aware formatting, column visibility, filters, sorting, and safe bulk actions. Do not cram action icons into every cell; prefer row menu or contextual toolbar.

Amounts and quantities use tabular numbers. Unit is explicit. Edited cells show dirty/saving/error states. Selection and keyboard interaction must remain accessible.

## 10. Charts and progress visualization

Charts are secondary to the numbers they explain. S-curve uses planned and actual series from the backend, with deviation visible in labels/tooltip. Provide a table equivalent or accessible summary. Never recompute authoritative progress from raw reports in the browser.

Do not rely only on color to distinguish series or status.

## 11. Official report preview

Report previews show white-paper pages regardless of app theme, with surrounding app chrome respecting theme. Preview metadata must show period, snapshot/version references, publication state, and revision. Download/share controls follow backend capabilities.

## 12. Telegram Mini App

The Mini App is a focused field surface, not a squeezed copy of the full dashboard. Detailed rules live in `docs/frontend/TELEGRAM-MINI-APP.md`. Core journeys: choose assigned project, create/edit draft field report, select work/location, enter measurement, add evidence/photos, review, submit, and inspect returned/revision status.

The backend verifies Telegram init data and provides a normal app session/capability response. The frontend never assumes Telegram identity equals application authorization.

## 13. Empty/loading/error states

Use skeletons only when they preserve layout understanding; otherwise show a compact progress state. Empty states use original vector assets from the repository, not emoji or generic AI illustrations. Each state explains the condition in one short sentence and offers a human CTA only when the user can resolve it.

Errors distinguish validation, permission, conflict/version, connectivity, and unexpected server failure. Avoid long paragraphs in product UI.

## 14. Assets and icons

Use Lucide for common interface icons. No emoji as UI icons. Product-specific empty-state/illustrative assets live under `apps/web/public/illustrations` as owned SVGs with light/dark-compatible styling. Do not pull random stock illustrations or generated gradients into authenticated product screens.

## 15. Naming and CTA rules

Use human labels: `Import budget`, `Create report`, `Request revision`, `Approve report`, `Publish document`, `Add member`. Avoid vague AI-ish language such as `Proceed`, `Execute`, `Generate magic`, `Unlock insights`, `Optimize now`, or excessive “smart/AI” naming.

One primary CTA per task area. Destructive actions state the object/action explicitly.

## 16. Accessibility

Target WCAG 2.2 AA. All workflows are keyboard accessible on web. Focus is visible. Dialogs trap/return focus correctly. Forms expose labels/errors programmatically. Tables/charts have accessible alternatives. Respect reduced motion and browser zoom. Touch targets are at least 44 px on mobile flows.

## 17. Frontend testing

Use unit/component tests for formatters and complex interactions, integration tests with contract-compliant mock server, and Playwright for critical user journeys. Visual regression covers light/dark at representative desktop/mobile widths. Accessibility checks run on major routes.

Critical end-to-end paths include onboarding, project creation, RAB import mapping, baseline planning, field report submit, approval/revision, S-curve view, document preview/publication, cost/material entry, billing entitlement states, and Telegram Mini App reporting.

## 18. Performance

Server-render shells where useful, lazy-load heavy charts/editors, virtualize large data grids, keep translation bundles scoped, avoid shipping Excel/PDF generation libraries to the browser, and measure Core Web Vitals on real data volumes. Performance optimizations must not break accessibility or consistency.

## 19. Frontend/backend parallel rule

Frontend tasks can start once the related OpenAPI schema and example fixtures are marked `contract-ready`, even if the backend endpoint is not implemented. Implement against generated types and mock handlers. When backend becomes available, the integration lane switches the same tests from mock to real API. Do not edit backend code from the frontend lane to “make the UI work”; open/assign a contract or backend task instead.
