# DESIGN.md — Product UI System & Frontend Rules

> **Product:** Construction SaaS — multi-company project control  
> **Document ID:** UX-SYS-001  
> **Version:** 2.0.0  
> **Status:** Normative UI implementation specification  
> **Last revised:** 2026-10-24  
> **Applies to:** Next.js web application, responsive views, Telegram Mini App, screen-facing PDF/XLSX previews  
> **Product UI language:** English (`en`) by default; Indonesian (`id`) available through i18n  
> **Backend:** Go + PostgreSQL, pgx/sqlc, MinIO/S3 — **no Supabase**

## 0. Purpose, authority, and definition of done

This document is **not a prompt** and is **not a mood board**. It is a set of implementable interface rules, tokens, wireframes, accessibility requirements, component contracts, and review gates. AI coding agents and human developers must use it when creating or changing the product interface.

**Read with:**

- `docs/product/PRD.md` — functional scope and roles.
- `docs/architecture/SYSTEM-DESIGN.md` — Go/Next.js boundary.
- `docs/architecture/DATABASE.md` and `DATA-DICTIONARY.md` — tenant/project structure.
- `docs/architecture/CALCULATION-RULES.md` — RAB, volume, progress, planned/actual, S-curve.
- `docs/specs/REPORT-TEMPLATES.md` — weekly physical/financial progress and time schedule.
- `docs/specs/STATE-MACHINES.md` — approvals and publication states.
- `docs/specs/TELEGRAM-INTEGRATION.md` — bot and Mini App security.

When there is a conflict, **approved domain rules and authorization rules take precedence**. UI may not invent, smooth, or alter official quantities, costs, progress, permissions, or approval status for visual convenience. Record product/design decisions through the project change process instead of silently overriding them.

**Acceptance for a screen:** it must work in light/dark themes, at desktop and supported mobile width, in English and Indonesian, with empty/loading/error/permission states, keyboard navigation, real authorization, real API types, and no fake business numbers presented as production data.

---

## 1. Product design direction

### 1.1 Basalt / Clay

**Design character:** precise, grounded, quiet, practical, slightly editorial. A construction site is busy; its management software should not be. The visual system should feel like a well-designed professional tool, not a landing page, a finance dashboard template, or AI-generated demo.

**Visual DNA:**

- Warm-neutral canvas, graphite typography, sparing *clay/oxide* accent. **No generic SaaS blue palette.**
- Straightforward geometry; thin separators; restrained radius; almost no shadows.
- Dense data tables and clear filters on desktop; guided tasks and large controls on mobile.
- Typographic hierarchy before color, spacing before containers, borders before shadows.
- First-class dark theme, not a filter or color inversion.
- Visuals communicate a function. Decorative artwork is not required to finish a page.

**UI density budget:** one page heading; one visually dominant primary action per view or task region; no more than 3–4 small top-level metrics in project overview by default; detailed information in tables and drill-down views rather than a screen full of large cards.

### 1.2 Hard anti-AI-slop rules

**Reject the implementation if it contains any of these without an approved exception:**

| Do not ship | Replace with |
|---|---|
| Gradient heroes, blobs, glows, aurora/mesh backgrounds, glassmorphism | Plain, deliberate surfaces with meaningful grouping |
| Default purple/blue brand + colored stat cards on every page | Neutral shell + one clay action color |
| Oversized headlines, giant whitespace, 12 decorative cards | Appropriate information density and clear hierarchy |
| Repetitive rounded cards for a dataset naturally suited to rows | Data table, tree table, list, or split view |
| Stock construction workers, AI illustration filler, cute mascots | Purpose-built SVG diagrams for the *few* empty states that need them |
| Emojis in headings, buttons, navigation, status, alerts, charts | Lucide icons, custom SVGs, descriptive labels |
| Floating pills everywhere, neon strokes, busy chips | Labels, compact text, borders, measured badge use |
| Shimmer loops and motion on every element | Real loading feedback; reduced-motion support |
| Random icon on each section title | Icon only when recognition/action improves |
| Fake values like `72.5%` in production components | Actual API result or a clearly flagged fixture in development only |
| Generic marketing copy such as “Unlock the power of…” | Concrete, task-oriented copy (see §8) |
| Mystery-meat icon-only action buttons | Verb + object label, or icon with accessible name for truly familiar utilities |
| “Smart”/“AI” feature names for basic CRUD | Direct descriptive names a project manager would use |
| Overlong empty-state paragraphs | One heading, one short explanation if needed, one relevant next action |

**Design test:** if removing a visual element has no meaningful effect on comprehension, state, or next action, remove it. The UI should look believable with real project data, incomplete records, and long contractor names.

### 1.3 Navigation is part of the visual system

Do not create one enormous sidebar of every feature. Use two scopes:

- **Company scope:** Overview, Projects, Reports, Documents, People, Templates, Settings.
- **Project scope:** Overview, Budget & WBS, Schedule, Daily Reports, Approvals, Documents, Team, Settings.

Display a company switcher and clearly identify the current project. Permissions determine available navigation. Do not show irrelevant pages just to fill the sidebar. Never leave stale project data visible after switching companies.

---

## 2. Theme system: Light + Dark from day one

### 2.1 Theme policy

- `system`, `light`, and `dark` are all supported. Default is `system` on first visit unless user preference exists.
- A visible theme control lives in the user menu and Appearance settings. Persist explicit user choice; do not reset it on navigation.
- No white flashes on dark page load. Resolve effective theme before paint where possible.
- CSS semantic variables are the source of truth. Components **must not use ad hoc hex colors**.
- Both themes require separate design reviews, not CSS inversion. Charts, diagrams, tables, overlays, export preview chrome, and SVGs must honor theme.
- Documents for print/export use an explicit **print theme** independent from the application's dark theme. White paper remains white in print previews.
- Telegram Mini App respects its host environment while preserving contrast and product semantics. No forced dark colors against Telegram host backgrounds.

### 2.2 Canonical palette

Values below are initial tokens; approved refinements require updating the palette **and** screenshot baselines. Brand accent is *clay*, not the default color of everything.

| Semantic token | Light | Dark | Usage |
|---|---|---|---|
| `--bg` | `#F6F6F2` | `#131816` | App canvas |
| `--surface` | `#FFFFFF` | `#1C2420` | Table, panel, dialogs |
| `--surface-raised` | `#FAFAF7` | `#252E28` | Secondary panel/popover |
| `--surface-hover` | `#F0F2EC` | `#2D3830` | Row hover and mild highlight |
| `--text` | `#202622` | `#F0F2EB` | Primary copy |
| `--text-muted` | `#526057` | `#B1BDB3` | Secondary copy, labels |
| `--border` | `#D5DBD5` | `#47544A` | Decorative separators; not sole control boundary |
| `--control-border` | `#6B766E` | `#7A837A` | Inputs and essential boundaries |
| `--primary` | `#A64226` | `#F3AC84` | Primary action and selection accent |
| `--on-primary` | `#FFFFFF` | `#1D241F` | Button text on primary |
| `--focus` | `#A64226` | `#F3AC84` | Keyboard focus ring |
| `--positive` | `#216A4B` | `#4FE0A0` | Positive semantic text/icon |
| `--warning` | `#8A4A07` | `#F0C074` | Review/warning text/icon |
| `--negative` | `#A73035` | `#FF999C` | Critical/error text/icon |

Do not use `--primary` for positive states. Brand color means *action/selection*, not *approved*. Explicit labels and icons accompany semantic colors. Pale contextual backgrounds, if necessary, must use light tints derived from their semantic foreground with adequate contrast; never invent arbitrary status shades inside a component.

**Contrast baseline (calculated, rounded):**

- Light: primary text on canvas ≈ **14.2:1**; muted text on canvas ≈ **6.1:1**; white on primary button ≈ **6.1:1**.
- Dark: primary text on canvas ≈ **15.9:1**; muted text on panel ≈ **8.2:1**; dark text on primary button ≈ **8.3:1**.
- Normal text must meet WCAG AA **4.5:1**; large text and meaningful graphical/control boundaries **3:1**. Decorative dividers need not have the same contrast as interactive input outlines.

### 2.3 Implementation contract

```css
/* apps/web/src/styles/tokens.css */
:root {
  color-scheme: light;
  --bg: #F6F6F2;
  --surface: #FFFFFF;
  --surface-raised: #FAFAF7;
  --surface-hover: #F0F2EC;
  --text: #202622;
  --text-muted: #526057;
  --border: #D5DBD5;
  --control-border: #6B766E;
  --primary: #A64226;
  --on-primary: #FFFFFF;
  --focus: #A64226;
  --positive: #216A4B;
  --warning: #8A4A07;
  --negative: #A73035;
}
:root[data-theme="dark"] {
  color-scheme: dark;
  --bg: #131816;
  --surface: #1C2420;
  --surface-raised: #252E28;
  --surface-hover: #2D3830;
  --text: #F0F2EB;
  --text-muted: #B1BDB3;
  --border: #47544A;
  --control-border: #7A837A;
  --primary: #F3AC84;
  --on-primary: #1D241F;
  --focus: #F3AC84;
  --positive: #4FE0A0;
  --warning: #F0C074;
  --negative: #FF999C;
}
body { background: var(--bg); color: var(--text); }
:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { scroll-behavior: auto !important; animation-duration: .01ms !important; transition-duration: .01ms !important; }
}
@media print {
  :root { color-scheme: light; }
  body { background: #fff !important; color: #111 !important; }
}
```

**Implementation detail:** `data-theme` reflects the *resolved* theme (`light`/`dark`), while the saved preference may be `system`; subscribe to system changes only for `system`. The example is an initial contract, not permission to add hardcoded UI colors elsewhere. Test form controls, select options, input autofill, charts, SVGs, and scrollbars in both themes.

### 2.4 Color and motion test cases

1. Theme switch changes all surfaces without remounting forms or losing draft input.
2. On dark mode, charts remain legible and focused controls have visible outlines.
3. Print preview has white paper and suitable print ink in either UI theme.
4. System preference change updates theme only if preference is `system`.
5. Selection/approval/error information is understandable in grayscale.
6. No chart series is distinguished **only** by red/green.
7. All meaningful text and controls pass contrast checks; no opacity hack weakens copy.

---

## 3. Layout, typography, and component grammar

### 3.1 Typography

- Interface font: **IBM Plex Sans**, with system sans fallback. Use one family for navigation, headers, tables, and forms.
- Use **IBM Plex Mono** sparingly for project identifiers, document IDs, and fixed-width data diagnostics; do not switch every numeric cell to monospace.
- Tabular numbers (`font-variant-numeric: tabular-nums`) for columns and KPI values.
- A page heading uses 24/32 px, section heading 17/24 px, body 14/21 px, table data 13/19 or 14/20 px, metadata 12/17 px. Never shrink primary data below 12 px.
- Sentence case for UI labels and CTAs: **“Import budget”**, not **“IMPORT YOUR BUDGET”** or title case everywhere.
- Maximum continuous explanatory paragraph in application UI: **two short sentences**. Prefer direct labels and progressive disclosure.
- No massive display typography in authenticated screens. Dense does not mean unreadable.

### 3.2 Dimensions and rhythm

| Element | Target |
|---|---|
| Spacing scale | `4, 8, 12, 16, 20, 24, 32` px |
| App sidebar | 224 px expanded / 56 px collapsed |
| Header | 56 px |
| Desktop page gutter | 24 px, 32 px on wide screens |
| Mobile page gutter | 16 px |
| Input & toolbar height | 36–40 px on desktop; >=44 px touch target on mobile |
| Desktop table row | 36–40 px compact; 44 px comfortable |
| Field label / hint | 12–13 px / 12 px |
| Default border radius | 6 px control; 8 px panel; 10 px dialog |
| Elevation | Border first; one light shadow for overlays only |
| Loading transitions | 120–180 ms unless `prefers-reduced-motion` |

**Responsive decisions:** `>=1200` wide desktop with persistent sidebar, `768–1199` compact desktop/tablet, `<768` mobile-first flows. Do not take these as the only test widths: inspect at 360, 390, 768, 1024, 1440 px and 200% browser zoom.

### 3.3 Component rules

- **Button:** one dominant primary CTA per task area. Primary is not automatically red. Destructive action uses a danger treatment and contextual confirmation.
- **Input:** real label above; visible required indicators; field-specific error below; do not use placeholder as label.
- **Tabs:** for parallel views; **stepper** for a sequential import or publish workflow. Never use horizontal tabs where two different datasets have different permissions without a clear label.
- **Badges:** only operational states, compact text; no arbitrary badges on every card/section.
- **Table:** default for repeated structured work; right-align amounts/quantities; separate the unit from its number; frozen identifier/description columns where useful.
- **Dialog:** only for irreversible changes, short forms, and confirmation. Complex RAB editors and long workflows get pages or drawers, not nested modals.
- **Toast:** supplementary, not the sole durable acknowledgement of a business operation.
- **Chart:** contextual, explicitly labeled axes, meaningful tooltip, matching accessible data table.
- **Empty state:** intentionally designed and contextual; illustrations never hide CTA or permission explanation.
- **Icons:** Lucide React, single consistent stroke; choose meaning before decoration.

### 3.4 Visual composition constraints

- No dashboards made entirely of cards. Mix summary strip, wide data table, and a focused exception queue.
- Sidebar and content should not both be dark blocks in light mode. Use separation and typography, not colored rectangles.
- Avoid 3 nested scroll containers. For RAB tables, horizontal scroll is allowed and sticky first columns are encouraged.
- Charts should have data hierarchy; a time-series chart must not be a shapeless colored mountain.
- In tables with 10+ columns, allow column visibility, saving views later, and export of selected range.
- UI must survive 80-character project names, 14-digit currency values, missing photo, empty attachments, and 1000+ work items.
- Do not create `Card` wrappers around single-line settings or every row.

---

## 4. Content system: English UI, localized properly

### 4.1 Language contract

- **Default locale:** `en` (English user interface). Secondary locale: `id` (Bahasa Indonesia).
- Use `next-intl` with the Next.js App Router for translated UI messages, formatting, and routing where appropriate.
- UI labels, buttons, menus, placeholders, validation errors, dialogs, states, table headings, accessible names, notification messages, and document-preview chrome **must be translation keys**.
- **Never hardcode user-visible English text in JSX/TSX** even though English is the default.
- Backend sends **stable error/status codes**, typed values, and parameters. The frontend translates them. Logs and internal API field names remain English.
- User-created content (project name, company name, WBS description, report notes) is persisted as entered and is **not automatically translated**.
- Exported official report template language is set explicitly per template/publication. It does **not** change retroactively when a viewer switches UI language.
- Missing translation keys fail CI or are flagged in development; don't silently ship raw IDs as labels.

### 4.2 Directory convention

```text
apps/web/
  src/
    app/[locale]/(app)/...
    app/[locale]/(miniapp)/...
    i18n/
      routing.ts
      request.ts
      navigation.ts
    messages/
      en/
        common.json
        navigation.json
        projects.json
        budget.json
        schedule.json
        reports.json
        approvals.json
        documents.json
        errors.json
      id/
        ...same namespaces...
    components/
      ui/
      layouts/
      domain/
      illustrations/
    assets/
      illustrations/       # original SVG assets
      marks/               # logo / approved brand assets
    styles/
      tokens.css
```

Prefer normal translation APIs (`useTranslations` for client UI; `getTranslations` in server components). Use `Intl.NumberFormat` and `Intl.DateTimeFormat` or the localization library's formatter for user-facing display only. Go remains the source of truth for financial/progress calculation.

### 4.3 Example translation contract (do not inline in a button)

For example, the `messages/en/reports.json` catalog contains:

```json
{
  "list": {"title": "Daily reports", "empty": "No reports yet"},
  "actions": {
    "new": "New report",
    "submit": "Submit for review",
    "saveDraft": "Save draft",
    "requestChanges": "Request changes",
    "approve": "Approve report"
  },
  "fields": {"workDate": "Work date", "quantity": "Quantity completed"},
  "status": {
    "draft": "Draft",
    "submitted": "Awaiting review",
    "revision_required": "Changes requested",
    "approved": "Approved",
    "rejected": "Rejected"
  }
}
```

```tsx
'use client';
import {useTranslations} from 'next-intl';

export function SubmitReportButton() {
  const t = useTranslations('reports.actions');
  return <button type="submit">{t('submit')}</button>;
}
```

```ts
// i18n/routing.ts — adapt to actual project routing decision.
import {defineRouting} from 'next-intl/routing';
export const routing = defineRouting({
  locales: ['en', 'id'],
  defaultLocale: 'en',
  localePrefix: 'as-needed'
});
```

All visual examples in this document show **English translations rendered in the interface**. They are never permission to hardcode that copy in components.

### 4.4 Terminology and formatting

| Domain concept | Default UI English | Indonesian catalog |
|---|---|---|
| RAB / BoQ | **Budget (RAB)** | **RAB** |
| WBS | **Work breakdown** | **Struktur pekerjaan** |
| Bobot | **Weight (%)** | **Bobot (%)** |
| Time schedule | **Schedule baseline** | **Jadwal rencana** |
| Laporan harian | **Daily report** | **Laporan harian** |
| Persetujuan | **Approvals** | **Persetujuan** |
| Realisasi | **Actual progress** | **Progres realisasi** |
| Deviasi | **Variance (pp)** | **Deviasi (pp)** |
| Minggu lalu | **Previous period** | **Periode sebelumnya** |
| Minggu ini | **Current period** | **Periode ini** |
| Kumulatif | **Cumulative** | **Kumulatif** |
| Nilai pekerjaan | **Value of work completed** | **Nilai pekerjaan selesai** |

**Distinguish** budgeted cost, value of completed work, and actual expense; do not label all three “Cost”. Use percentage points (`pp`) for actual minus planned progress. Preserve project timezone, contract units, and displayed precision. Zero is not missing: `0` is valid; em dash `—` means unavailable.

**Formatting:** `en` sample `82.04 m³`, `68.50%`; `id` sample `82,04 m³`, `68,50%`. Currency display depends on the project contract and selected locale. API authoritative decimals remain decimal-safe strings; do not compute final values in JS floating point.

### 4.5 Human copy and CTA naming

- Action labels use **verb + direct object:** `Create project`, `Import budget`, `Submit for review`, `Request changes`, `Publish report`, `Download Excel`.
- Don't use vague verbs: `Let's go`, `Continue journey`, `Get started` (unless onboarding context justifies it), `Optimize`, `Boost`, `Explore magic`.
- Label destructive actions honestly: `Delete draft`, `Reject report`, `Remove member`.
- Confirmation must name the record and consequence. No “Are you sure?” by itself.
- Page headings are nouns or clear task names: `Projects`, `Budget (RAB)`, `Approvals`, `Weekly progress report`.
- Toast copy indicates result: `Draft saved`; failure copy indicates fix: `Upload failed. Try the file again.`
- Avoid second-person praise, cheerleading, all caps, exclamation marks, decorative ellipsis, and emojis.
- No stock filler paragraphs. **UI copy budget:** headings 2–5 words, helper text ideally <=12 words, empty descriptions <=18 words, CTA 1–4 words unless a specific action needs clarity.
- For complex field help, link `How this is calculated` or a disclosure rather than placing formula paragraphs above every field.

**Examples:**

| Bad / AI-generated | Preferred English UX |
|---|---|
| “Revolutionize Your Construction Workflow” | `Projects` |
| “Embark on your project management journey” | `No projects yet` |
| “Unlock Seamless Efficiency” | `Import budget` |
| “Oops! Something went wrong!” | `Could not load reports` + `Try again` |
| “Proceed with Action” | `Approve report` |
| “Congratulations! You did it!” | `Report approved` |
| “Manage and orchestrate your team seamlessly” | `People` |

### 4.6 Validation copy is specific

For failed actions, use translation keys with named parameters:

- `errors.budget.unsupportedFormat` → `Use an .xlsx file.`
- `errors.budget.columnMissing` → `Map the {column} column before importing.`
- `errors.reports.quantityExceedsRemaining` → `Entered quantity exceeds the remaining contract quantity.`
- `errors.reports.conflict` → `This report changed. Reload it before reviewing.`
- `errors.permissions.approveDenied` → `You can't approve reports for this project.`
- `errors.network.tryAgain` → `Connection lost. Your draft hasn't been submitted.`

Messages live in catalogs, not in response handlers or shared components. Include field labels and row/column context for spreadsheet validation.

---

## 5. Iconography, illustration assets, and empty states

### 5.1 Icons, not emoji

Use **Lucide React** for functional UI icons. Icons are implementation assets, not Unicode emoji. Recommended base size: 16 px in tables and toolbar, 18–20 px in navigation and menu; stroke width consistent at roughly 1.75–2.

| Use | Icon name (Lucide) |
|---|---|
| Projects | `FolderKanban` |
| Budget (RAB) | `Table2` |
| Schedule | `CalendarRange` |
| Daily reports | `ClipboardList` |
| Approvals | `ClipboardCheck` |
| Documents | `FileText` |
| Import | `FileUp` |
| Download | `Download` |
| Search | `Search` |
| Filter | `ListFilter` |
| Settings | `Settings2` |
| Team | `UsersRound` |
| More actions | `Ellipsis` |
| Theme | `Sun`, `Moon`, `Monitor` |

Consistency rules: one meaning → one icon; do not put icons on every button; icons next to visible text may be `aria-hidden`; icon-only actions need `aria-label`, tooltip, and keyboard access. **Never use emoji as fallback** when an icon asset is missing. Use text until a suitable icon is available.

### 5.2 Original vector assets

Create **purpose-built SVGs**, not stock photos or AI-generated construction cartoons. The default is *no illustration* for data-dense screens. Use a small visual only when it helps orient the user on true first-use or a significant empty state.

Asset family: **technical line diagrams** using simple shapes that echo project plans, sheets, columns, check marks, and engineering lines. Keep geometry consistent and restrained; no hard hats with smiling faces, cranes in sunsets, oversized soft 3D characters, noisy texture, gradients, glossy blobs, or mascots.

| Asset / component | Trigger | Form | Primary next action |
|---|---|---|---|
| `empty-projects.svg` | A company has no projects | Stacked plan sheet / simple site blocks | `Create project` |
| `empty-budget.svg` | Project has no budget | Sheet grid with two WBS rows | `Import budget` |
| `empty-schedule.svg` | No approved baseline | Sparse timeline with checkpoints | `Set schedule` |
| `empty-reports.svg` | No reports yet | Two compact report sheets | `New report` |
| `empty-approvals.svg` | Queue is empty | Outline checklist with completed markers | **No CTA required** |
| `empty-documents.svg` | No published reports | Empty document outline | `Generate report` (if permitted) |
| `state-no-results.svg` | Filters yield no results | Minimal cropped grid + search | `Clear filters` |
| `state-no-access.svg` | User lacks permission | Locked grid diagram | `Return to projects` |
| `state-offline.svg` | Mini App network unavailable | Disconnected minimal link | `Try again` |

**Asset requirements:**

- Draw in vector; store editable source where possible and production SVG, e.g. `apps/web/src/assets/illustrations/`.
- Use `viewBox="0 0 240 144"` for standard landscape assets, visual artwork within ~192x112 safe area.
- Strokes 1.5–2 px; palette uses `currentColor` or theme-aware semantic CSS; no hardcoded fill that breaks dark mode.
- For purely decorative assets: `aria-hidden="true"` and empty alt. For explanatory diagrams: translated alt/title and accessible description.
- Keep SVG paths deliberate and human-edited; no embedded raster, base64, animation, external URLs, inline executable scripts, or excessive path nodes.
- Use CSS variables/currentColor when imported as React components; note `<img src>` does not inherit parent CSS variables into the external SVG. If using public SVG files, provide separate light/dark files or select CSS-safe embedded colors.
- Avoid shadows and colored backplates. Illustrations may use a very subtle fill and a single clay accent, never a rainbow.
- Limit empty-state images to ~160–220 px wide on desktop and ~120–176 px on mobile; icons smaller for inline states.

### 5.3 Reusable empty-state composition

```text
             [ original SVG, optional ]
                   No projects yet
       Create your first project to start tracking work.
                   [ Create project ]
```

Do not add a second paragraph about the platform's value. On a filtered empty state, **do not show the first-use illustration**: say `No projects match your filters` and provide `Clear filters`. On zero pending approvals, use a quiet sentence without a fake CTA.

### 5.4 Concrete SVG reference: `empty-projects.svg`

This is an example starting point for a small custom asset, not a random icon stretched to hero size. Refine by visual review; avoid copying it into every state.

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 144" fill="none" aria-hidden="true">
  <g stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" stroke-linecap="round">
    <path d="M46 103h149" opacity=".32"/>
    <path d="M65 91V48l22-12 22 12v43H65Z"/>
    <path d="M87 36v55M65 49h44" opacity=".55"/>
    <path d="M112 91V59l22-12 30 12v32h-52Z"/>
    <path d="M134 47v44M112 59h52" opacity=".55"/>
    <path d="M72 101h102M76 109h93" opacity=".3"/>
  </g>
  <path d="M181 40h17v17h-17z" fill="none" stroke="currentColor" stroke-width="1.8" opacity=".65"/>
  <path d="M185 49h9" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"/>
</svg>
```

The illustration intentionally works in one ink color; a future design pass can add a single semantic accent without compromising either theme. It does **not** represent a real contractor's site or claim precise engineering meaning.

---

## 6. Wireframes — real application screens

These are **structural wireframes**, not marketing mockups. Square-bracket boxes denote controls/containers and sample rendered English labels (all supplied by i18n in code). Use them to guide composition; respect responsive and permission behavior below.

### 6.1 Desktop app shell / company overview (1440 px target)

```text
┌──────────────────────┬──────────────────────────────────────────────────────────────┐
│  BRAND               │  Company / Overview                         Search   User    │
│  Company name  ▾     ├──────────────────────────────────────────────────────────────┤
│                      │                                                              │
│  Overview            │  Company overview                      [ Select period ▾ ]    │
│  Projects            │  Work that needs attention                                 │
│  Reports             │                                                              │
│  Documents           │  Active projects   Awaiting review   Behind schedule         │
│  ─────────────       │        12                4                 2                │
│  People              │                                                              │
│  Templates           │  PROJECTS                                      [ View all ] │
│  Settings            │  ┌────────────────────────────────────────────────────────┐  │
│                      │  │ Project            Actual  Planned  Variance  Updated │  │
│                      │  │ East Ring Road     68.5%   75.0%    -6.5 pp   Today   │  │
│                      │  │ Office Tower B     42.0%   40.0%    +2.0 pp   Today   │  │
│                      │  └────────────────────────────────────────────────────────┘  │
│                      │                                                              │
│                      │  NEEDS REVIEW                                               │
│                      │  4 daily reports are awaiting verification. [ Review ]     │
└──────────────────────┴──────────────────────────────────────────────────────────────┘
```

**Rules:** overview is an exception-finding tool, not a vanity dashboard. Top summary is a plain metric strip; no colored metric cards. Avoid AI-generated greetings. `Variance` is percentage points, **not** a relative percentage. Show last-sync/cutoff context near data that requires it.

### 6.2 Project list

```text
Projects                                                       [ Create project ]
Find a project [____________________]  Status [All ▾] Type [All ▾]  [Clear filters]
────────────────────────────────────────────────────────────────────────────────
Code      Project                          Owner          Actual      Status
PRJ-012   East Ring Road                   Project team   68.50%      Active
PRJ-013   Office Tower B                   Project team   42.00%      Active
PRJ-014   Housing Cluster C                Project team       —       Draft
────────────────────────────────────────────────────────────────────────────────
1–25 of 73                                              [Previous]  1  2  3  [Next]
```

Default to table view; sorting, search and pagination should be server-compatible. Do not use hero-sized project cards. A row can navigate, but destructive actions must be explicit and not accidentally triggered by row click.

### 6.3 Project overview

```text
Projects / East Ring Road                                      [ New daily report ]
East Ring Road           Active  |  Contract PRJ/2026/012  |  Jan–Dec 2026
Overview  Budget & WBS  Schedule  Daily reports  Approvals  Documents  Team
──────────────────────────────────────────────────────────────────────────
Actual progress        Planned progress      Variance       Awaiting review
    68.50%                 75.00%            -6.50 pp             3
As of 24 Oct 2026  |  Approved budget v2  |  Baseline v1
──────────────────────────────────────────────────────────────────────────
PROGRESS OVER TIME                                  NEEDS ATTENTION
┌─────────────────────────────────────┐             ┌────────────────────────┐
│  100%                               │             │ 3 reports to review    │
│   75%      Planned   ____________   │             │ 2 items over quantity  │
│   50%      Actual  __________       │             │ [ Review reports ]     │
│    0%____________________________   │             └────────────────────────┘
│        Week 1  2  3  4  5  6        │
└─────────────────────────────────────┘
WORK ITEMS
Code      Work item           Qty approved    Item progress    Contribution
A-001     Concrete works        40 / 100 m³        40.00%           8.00 pp
...
```

Do not draw a fake S-curve using CSS decorations; use the same authoritative series as reports. Future actual points are absent, not zeros. Charts must preserve visible legends and accessible numeric alternatives.

### 6.4 Budget (RAB) / WBS — dense desktop table

```text
East Ring Road / Budget (RAB)                         Version 2 • Approved
[ Search work item... ]  [ Filter ▾ ]     [ Import Excel ] [ New draft version ]
────────────────────────────────────────────────────────────────────────────────────
Code    Work item                 Unit  Contract qty    Unit price     Amount   Weight
▾ A     ROAD WORKS
  A.01  Earthworks                  m³     1,200.00       150,000    180,000,000  18.0%
  A.02  Aggregate                   m³       500.00       450,000    225,000,000  22.5%
▾ B     DRAINAGE
  B.01  Concrete drain              m        600.00       250,000    150,000,000  15.0%
...
────────────────────────────────────────────────────────────────────────────────────
TOTAL                                                          1,000,000,000  100.0%
Showing approved version • Read only                 [ Compare versions ] [ Export ]
```

- Sticky first columns; numbers right-aligned with tabular digits. No red/green row fills.
- Parent/category rows are not counted again as leaf amounts in totals.
- Approved version is **immutable**, not editable with a hidden pencil icon.
- Display storage/calculation precision according to contract rules; the visual numbers here are illustrative only.
- On narrow screens, provide read-only condensed details or a dedicated entry flow; do not force editing a 12-column grid on a phone.

### 6.5 Excel budget import — wizard

```text
Import budget                               Step 2 of 4: Match columns
[1 Upload] ────── [2 Match columns] ────── [3 Validate] ────── [4 Save draft]

Detected worksheet:  BOQ_Master          Header row: [ 5 ▾ ]

Spreadsheet column                  System field
"Uraian Pekerjaan"              ->  [ Work item description ▾ ]
"Sat"                          ->  [ Unit ▾ ]
"Volume"                       ->  [ Contract quantity ▾ ]
"Harga Satuan"                  ->  [ Unit price ▾ ]
"Jumlah"                       ->  [ Amount ▾ ]

[ Preview 10 mapped rows ]
[ Back ]                                                     [ Validate data ]
```

Errors always identify `sheet`, `row`, `column`, `reason`, and next correction. Never silently import an entire uploaded Excel workbook as approved budget. Allow the user to save reusable mapping for repeated formats later.

### 6.6 Schedule / S-curve

```text
Schedule                                          Baseline v1 • Approved
View: [ Weekly ▾ ]  [ 2026 ▾ ]         [ Compare baseline ] [ Export ]

Work item                 Weight    W1     W2     W3     W4     W5     ...
Earthworks                  18.0%   2.0    4.0    6.0    6.0     —
Aggregate                   22.5%     —      —    4.5    6.0    6.0
...
Plan this week                     2.0    4.0   10.5   12.0   11.0
Planned cumulative                 2.0    6.0   16.5   28.5   39.5
Actual cumulative                  1.5    5.8   14.0   26.0   35.4
Variance (pp)                     -0.5   -0.2   -2.5   -2.5   -4.1

S-CURVE [ line chart for plan vs actual, same data array as table ]
```

Separate editing plan from viewing actual. Weekly boundaries derive from real dates and project timezone; do not assume 4 weeks/month. A published baseline is read-only. Use an explicit revision flow.

### 6.7 Review / approval — split view

```text
Approvals / Daily report #DR-1024                     Awaiting review
┌────────────────────────────────────────┬────────────────────────────────────┐
│ REPORT DETAILS                         │ VERIFICATION                       │
│ Work date          24 Oct 2026         │ Current stage   Supervisor         │
│ Work item          Concrete works      │ Submitted by    Field Reporter     │
│ Location           Zone B              │                                    │
│ Quantity           19.04 m³            │ APPROVAL HISTORY                   │
│ Approved to date   63.00 m³            │ Submitted     24 Oct, 15:42        │
│ Contract quantity  82.04 m³            │                                    │
│ Evidence           [image] [image]     │ Comments [____________________]    │
│ Field note         ...                 │                                    │
│                                        │ [ Request changes ] [ Approve ]    │
└────────────────────────────────────────┴────────────────────────────────────┘
```

Only after final approval does Go append the progress ledger in one transaction. Rejections and change requests require reasons. No optimistic “Approved” UI before server confirmation. Warn about concurrent edits (`409 Conflict`) with a real reload path.

### 6.8 Documents — generation and print preview

```text
Documents / Generate weekly report

Document type       [ Weekly physical & financial progress ▾ ]
Reporting period    [ 12 Oct – 18 Oct 2026 ▾ ]
Budget version      v2       Baseline       v1
Ledger cutoff       [ 18 Oct 2026, 23:59 project time ]

[ Generate preview ]

┌─────────────────────────────────────────────────────────────────────┐
│ A4 landscape / white paper                         DRAFT            │
│ Company logo     Weekly physical & financial progress              │
│ Contract...   Week...   Period...                                 │
│ ─────────────────────────────────────────────────────────────────── │
│ WBS  Description  Unit  Qty  Rate  Weight  Previous Current Total   │
│ ...                                                                 │
│ Sign-off blocks...                                                  │
└─────────────────────────────────────────────────────────────────────┘
[ Back ]                              [ Download draft ] [ Publish report ]
```

Print/PDF appearance is intentionally separate from dark-mode UI. Preserve the output's budget/baseline/template versions and immutable snapshot. “Financial progress” means contractual value of measured completed work, not automatically actual spending.

### 6.9 Telegram Mini App — field home (390 px target)

```text
┌──────────────────────────────────┐
│ My projects                [User]│
│                                  │
│ East Ring Road            [ v ]  │
│ Saturday, 24 Oct 2026            │
│                                  │
│ [       New daily report      ]  │
│                                  │
│ REPORTS                          │
│ Drafts                         1 │
│ Awaiting review                2 │
│ Changes requested              1 │
│                                  │
├──────────────────────────────────┤
│  Projects       Reports    Account│
└──────────────────────────────────┘
```

### 6.10 Telegram Mini App — daily report form

```text
┌──────────────────────────────────┐
│ [Back]  New daily report         │
│ Work date                        │
│ [ 24 Oct 2026                 ]   │
│ Work item                        │
│ [ Search WBS item...         v ]  │
│ Contract 82.04 m³ · Approved 63.00│
│                                  │
│ Quantity completed               │
│ [ 19.04               ]  m³      │
│ Location                         │
│ [ Zone B                     v ]  │
│                                  │
│ Evidence (2)                     │
│ [ Photo ] [ Photo ] [ Add photo ] │
│ Notes (optional)                 │
│ [_____________________________]   │
│                                  │
│ [ Save draft ] [ Submit for review ]
└──────────────────────────────────┘
```

The actual form supports multiple report entries where the business flow requires it; this wireframe shows one entry for legibility. Use data from the Go API; quantity is not officially counted at submit time. File upload failures must preserve other fields. Disable duplicate submit and use API idempotency keys.

### 6.11 Mobile approval view

```text
┌──────────────────────────────────┐
│ Report #DR-1024                  │
│ Awaiting review                  │
│                                  │
│ Concrete works                  │
│ Reported           19.04 m³      │
│ Approved to date   63.00 m³      │
│ Contract qty       82.04 m³      │
│                                  │
│ Evidence                         │
│ [ photo gallery, swipe ]         │
│                                  │
│ Approval history  [ View ]       │
│                                  │
│ [ Request changes ] [ Approve ]  │
└──────────────────────────────────┘
```

Keep the review context visible before approval. If approval rights are absent, replace the action row with permission text; do not display a misleading disabled primary button without explanation.

---

## 7. Required states and error handling

Every fetch-driven screen must account for at least these states:

| State | Behavior | Example copy key |
|---|---|---|
| Initial loading | Stable frame; small skeletons in real content regions | None; ARIA loading status |
| First-use empty | Optional custom vector, concise explanation, permission-aware action | `projects.empty.title` |
| Filtered empty | No decorative illustration; offer reset filters | `projects.filters.noMatches` |
| Partial / no baseline | Preserve verified data; explain which comparison is unavailable | `schedule.baseline.missing` |
| Awaiting approval | Show submitted volume as **pending** | `reports.status.submitted` |
| Validation failed | Inline field and table cell errors with next corrective action | `errors.validation.*` |
| Network failed | Retry read; preserve draft; no fake success | `errors.network.*` |
| Unauthorized | Don't leak company/project details; offer safe navigation | `errors.permissions.*` |
| Conflict/stale | Show reload/version difference, never overwrite silently | `errors.conflict.*` |
| Submitted/saved | Durable result screen/row, not only disappearing toast | `reports.status.*` |
| Published/archived | Read-only state and version/audit trail | `documents.status.published` |

**Text cap:** an empty state should be 1 short title and at most 1 short sentence. Use one primary action if the user has permission; otherwise explain the blocker without a useless CTA. Inline validation should be terse, contextual, and actionable.

---

## 8. Data presentation rules unique to this product

### 8.1 RAB, quantity, and money

- WBS codes and work descriptions are separate columns; hierarchy appears through indentation + expand/collapse.
- Display `contract quantity`, `approved cumulative`, `current period`, and `remaining quantity` as different concepts. Never merge them into a vague `Progress` figure.
- Every quantity has a clear unit; currency code belongs in headings/labels where appropriate.
- Rates, amounts, weighted progress, and totals are computed by Go; frontend merely formats canonical values.
- Parent headings/subtotals never count as an additional work item.
- Difference between budgeted cost, measured work value, and actual expenses must be legible in labels and docs.
- If more precision is stored than displayed, tooltips/details may reveal exact value; rounding behavior must match approved calculation and reporting rules.
- In comparison views, use `+2.0 pp` / `−6.5 pp`, not `+2%` / `−6.5%` for differences between progress percentages.

### 8.2 Status semantics

| Internal state | English label | Meaning for user |
|---|---|---|
| `draft` | Draft | Editable by authorized author |
| `submitted` | Awaiting review | Submitted, not counted as official progress |
| `revision_required` | Changes requested | Author can amend and resubmit |
| `approved` | Approved | Final verification; ledger effect posted once |
| `rejected` | Rejected | Not counted; reason available |
| `published` | Published | Report snapshot locked |

Where workflow configuration has multiple stages, display the stage name and who is next, not just an ambiguous “Pending” chip. Status text is translated, machine-readable state values are not.

### 8.3 S-curve

- Planned vs actual lines have distinct shapes/dashes or point markers plus labels—not only color.
- Time axis is real weeks derived from the approved baseline; y-axis has a meaningful `0–100%` range unless explicitly justified otherwise.
- Show last approved actual date. Never fill future actuals with 0.
- Provide a numeric table (weekly + cumulative) and data source/version context.
- Use the same authoritative data series for dashboard, preview, Excel, and PDF, subject to snapshot cutoffs.

### 8.4 Generated reports

The two core project-control documents are:

1. **Weekly physical & financial progress** — previous/current/cumulative volume and value of work, weight, contract subtotal, configurable tax and sign-off.
2. **Time schedule / S-curve** — weekly plan, cumulative plan, cumulative actual, variance (`pp`), chart and print-friendly table.

Do not invent output columns from a generic billing app. Reflect the approved report template and contract terms. Published documents do not change when users switch light/dark mode or localization preferences in the browser.

---

## 9. Interaction accessibility and performance

- Keyboard operation for all controls; dialogs trap focus; focused rows in tree tables have discoverable keyboard behavior.
- Every interactive control has a visible label or translated accessible name.
- WCAG 2.2 AA contrast: 4.5:1 for standard text; 3:1 for meaningful non-text graphics/control boundaries; obvious focus indicator.
- Error messages tied to form fields (`aria-describedby`, `aria-invalid`); focus moves to error summary/first invalid field when appropriate.
- Charts get semantic descriptions and accessible tabular equivalents.
- No status communicated by hue alone; do not rely on icon alone for `Approved` vs `Rejected`.
- Support 200% text zoom and 360 px viewport without cutting off core actions. Large desktop financial tables may use intentionally labeled horizontal scroll.
- Any optional tooltip must be keyboard accessible; critical task instructions should not exist only in tooltips.
- Do not auto-focus dangerous actions. Provide confirmation for non-reversible business transitions.
- Loading placeholders should not cause large layout shifts or expose fake values.
- Server pagination/filtering for long project/report lists; WBS tables need tuned rendering for thousands of work items.
- Use real optimized image thumbnails and original-file access according to permissions; avoid shipping full-size site photos in list views.
- `prefers-reduced-motion` respected; avoid motion as a status indicator.

**Source references:**

- W3C WCAG 2.2 — https://www.w3.org/TR/wcag/
- Lucide accessibility guidance — https://github.com/lucide-icons/lucide/blob/main/docs/how-to/accessibility.md
- Next-intl routing and App Router — https://next-intl.dev/docs/routing/configuration

---

## 10. Frontend conventions and human naming

### 10.1 Product names and routes

Use recognizable, stable nouns. Avoid brand-sounding feature names or agent-generated metaphors.

| Good | Reject |
|---|---|
| `projects`, `budget`, `schedule`, `daily-reports`, `approvals`, `documents` | `command-center`, `growth-engine`, `magic-dashboard`, `insights-hub` |
| `New daily report` | `Smart reporting experience` |
| `Import budget` | `Seamless data ingestion` |
| `Review report` | `Action center experience` |
| `Approve report` | `Confirm success action` |

**Files/components:** `BudgetTable.tsx`, `ProjectSwitcher.tsx`, `ReportApprovalPanel.tsx`, `EmptyProjectsIllustration.tsx`, `QuantityCell.tsx`, `LocaleSwitcher.tsx`, `ThemeSwitcher.tsx`. Avoid `BeautifulCard.tsx`, `FancyPanel.tsx`, `MagicButton.tsx`, `AIWidget.tsx`, `ComponentFinal2.tsx`.

**Design tokens:** semantic names (`--surface`, `--text-muted`, `--primary`); no raw palette names sprinkled through components (`--orange-500` is not enough context), and no `bg-[#...]` in domain JSX.

**Translations:** keys describe meaning, not position or vague count: `reports.actions.submit`, not `button3`, `submitText`, or `heroSubtitle`.

### 10.2 Suggested frontend layering

```text
apps/web/src/
  app/[locale]/...
  components/
    ui/                 # minimal primitives (buttons, inputs, dialogs, table base)
    layout/             # shell, breadcrumbs, company switcher, project tabs
    data-table/         # sorting, sizing, sticky columns, pagination
    domain/
      projects/
      budget/
      schedule/
      reports/
      approvals/
      documents/
      telegram/
    illustrations/      # SVG React wrappers only
  assets/illustrations/ # original SVG source
  hooks/                # UI behavior only
  i18n/                 # routing + request + navigation
  messages/en/          # translation catalogs
  messages/id/
  lib/api/              # typed Go REST client
  lib/format/           # locale number/date formatting, no final accounting
  lib/permissions/      # UX hints only; Go is authoritative
  styles/tokens.css
```

Prefer composable, boring components over abstractions that only save 3 lines. Business state and monetary math stay in Go. Use domain names as defined by PRD and API contract.

---

## 11. AI coding agent operating rules (mandatory)

**Do not treat this section as a UI generation prompt. These are persistent review rules.**

### Before touching frontend code

1. Read this document and the feature's PRD/task/acceptance criteria.
2. Check corresponding Go API types, permissions, status machine, and calculations. Do not make up response objects to fill the mockup.
3. Inventory existing components, token usage, i18n namespaces, SVG assets, and screenshots before adding new abstractions.
4. Draw/inspect a low-fidelity wireframe for changes that alter layout or user flow.
5. Check whether the proposed component is redundant with an existing table, list, or primitive.

### While implementing

1. Use **semantic theme tokens**; implement and test both themes immediately.
2. Use **English translation keys** by default with `id` translations; do not commit visible hardcoded JSX strings.
3. Use recognizable **human labels** and **verb + object** CTA copy; follow copy limits above.
4. Use **Lucide icons** only for actionable/meaningful icons. No emoji or Unicode symbol substitutes for icons.
5. For illustration states, use existing original SVG asset or add one following §5; don't pull stock illustrations/remote placeholder artwork.
6. Use **real Go API contracts**; if backend is not implemented, isolate fixtures in dev/test-only modules and label preview environments unmistakably.
7. Keep UI concise. A 10-row table is better than ten giant cards for numeric records.
8. Implement empty, filtered empty, pending, error, permission, conflict, and loading variants for every data-driven feature.
9. Preserve accessibility, responsive behavior, and keyboard operation.
10. Never mutate state optimistically for approval/publish until Go confirms the authoritative transaction.

### Anti-slop skill compatibility

If the repository contains an `anti-ai-slop` / UI-quality `SKILL.md`, read and apply it **in addition to** this document; never assume it exists or claim it has been run without reading it. The **built-in quality rubric here remains mandatory** even when a separate skill is unavailable. If a skill's visual suggestion conflicts with security, i18n, accessibility, business correctness, or this design contract, those product constraints win.

**Visual review rubric (score 0/1 each, minimum 9/10 to accept):**

- [ ] Layout is task-first, not a decorative template.
- [ ] Clear hierarchy; no excessive cards, pills, or shadows.
- [ ] Light and dark are both deliberate and readable.
- [ ] Data density fits construction work and wide tables.
- [ ] Human labels and concise copy; no marketing filler.
- [ ] UI text comes from i18n, English default, Indonesian available.
- [ ] No emoji; icons consistent; custom vector empties when justified.
- [ ] Correct empty/error/loading/permissions and approval states.
- [ ] Real API data, correct units, period, precision and snapshot context.
- [ ] Mobile + keyboard + focus + contrast are usable.

**Hard blockers regardless of score:** wrong tenant/project data, false progress numbers, unverified approval shown as approved, hardcoded UI copy, dark mode broken, fake working CTA, inaccessible primary action.

### After implementing

- Capture/check screenshot of desktop **1440 px** and mobile **390 px** in **both** light and dark.
- Check a long project name, a large currency value, 1000-row table behavior, no-baseline state, no-permission state, and long Indonesian labels.
- Test keyboard through toolbar/forms/dialogs; verify visible focus and screen reader labels for icon-only controls.
- Run translation completeness validation (`en` / `id` key parity), lint/type-check, and appropriate automated tests.
- Compare result to §6 wireframe and report genuine deviations. Do not claim UI QA passed without performing it.
- If design rules changed, update this document, related component guidelines, and snapshots in the **same PR**. Rule synchronization is not permission for autonomous architecture changes.

---

## 12. Design review checklist / merge gate

Before a frontend PR is accepted, the reviewer must answer **yes** to each:

- [ ] The screen has a clear job-to-be-done and visually prioritizes it.
- [ ] The page looks like operational construction software, not a generic startup theme.
- [ ] Theme is functional in light/dark/system and initial paint is correct.
- [ ] All user-facing UI strings and accessible names use i18n, with English and Indonesian catalogs.
- [ ] Buttons and navigation are human-readable; no hype, emoji, fake actions, or vague CTAs.
- [ ] Icons are functional/consistent and original SVG assets are used sparingly for empty states.
- [ ] No filler card, decorative gradient, oversized hero, or unjustified illustration.
- [ ] Data values are accurately scoped to tenant/project/period/budget version and approved status.
- [ ] RAB/schedule/report tables are readable with keyboard, sticky structure, and sensible density.
- [ ] Submit/approve/publish transitions reflect server truth and handle retries/conflicts.
- [ ] Empty/loading/error/permission/partial data states have concise copy and valid actions.
- [ ] Mobile field workflow works at 360–390 px and Telegram Mini App safe area.
- [ ] Charts label their units and provide accessible numeric alternatives.
- [ ] Contrast, focus, resizing and reduced-motion behavior tested.
- [ ] Screenshots for both themes and two screen sizes reviewed.

**If any mandatory line fails: change the implementation, not the acceptance criterion.**

---

## 13. Product evolution without visual drift

New modules (inventory, actual expenses, subscription billing, client portals, offline drafts, enterprise permissions) must reuse the primitives and semantics above. Add navigation only after defining the user's job, required permissions, data source, empty/error states, i18n keys, and mobile treatment. A new feature is not a reason to create a new color palette, illustration style, typography scale, or generic card system.

### Change control

1. Proposed change includes reason, affected roles, wireframe, light/dark variants, copy keys, and screenshots or prototypes.
2. Review impact on tables, mobile, exports, accessibility, and existing business states.
3. Update `DESIGN.md` version and document semantic token changes; migrate affected components coherently.
4. Capture and approve new visual baselines for both locales/themes.
5. Update the AI-agent rule references and provider adapters through the repository's existing sync process.

**Do not automatically redesign existing pages based on a new visual trend or an AI agent's taste.** Consistency is a product feature.

---

## 14. Non-negotiable decisions (summary)

- **English-first UI**, translated through `next-intl`, with Indonesian support; **zero hardcoded visible JSX copy**.
- **Light, dark, system** themes from initial implementation, using the **Basalt / Clay** token system.
- **No emojis**; **Lucide icons** for controls; **original theme-aware SVGs** for intentional empty states.
- **No AI slop**: no template gradients, repetitive giant cards, stock characters, vague CTA text, or filler paragraphs.
- **Data-first tables** for RAB/WBS, schedule, reports, approvals and exports.
- **Short, human, task-based English labels**; native locale formatting; no misleading calculations.
- **Go is authoritative** for calculations, permissions, approval, and export snapshots.
- **Full state coverage** and visual QA across widths, locales, and themes.

*End of DESIGN.md — UX-SYS-001 v2.0.0.*
