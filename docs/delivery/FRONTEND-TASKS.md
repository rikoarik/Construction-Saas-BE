# Frontend Task Backlog — Full Product

**Owner:** frontend lane · **Status:** planned unless repository evidence says otherwise

Frontend work may begin against contract-ready OpenAPI mocks without waiting for backend implementation. UI must follow `docs/frontend/DESIGN.md`, `docs/frontend/FRONTEND.md`, and for Telegram UI `docs/frontend/TELEGRAM-MINI-APP.md`. Telegram Bot API/webhook work is not a frontend task and lives in the backend repository as `docs/delivery/BOT-TASKS.md` in the backend repository.

## F0 — Application shell, design system, i18n

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-001 | Next.js app scaffold, lint/test/build | — | CI passes |
| FE-002 | semantic design tokens light/dark/system | FE-001 | visual checks both themes |
| FE-003 | next-intl English/Indonesian foundation | FE-001 | no hardcoded UI-copy lint/check |
| FE-004 | icon/asset conventions + SVG empty-state framework | FE-002 | asset review |
| FE-005 | accessible primitives: buttons/forms/dialogs/table chrome | FE-002,FE-003 | component/a11y tests |
| FE-006 | project/company application shell | FE-005 | responsive shell tests |
| FE-007 | generated OpenAPI client + mock server | FE-001 | contract fixture tests |
| FE-008 | error/capability handling layer | FE-003,FE-007 | error-code UI tests |

## F1 — Identity, company, project navigation

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-010 | login/session/recovery UI | FE-005,FE-007 | e2e mock tests |
| FE-011 | invitation acceptance and company switcher | FE-006,FE-007 | multi-company states |
| FE-012 | company member/role management | FE-006,FE-007 | permission UI states |
| FE-013 | project list/search/filter/create | FE-006,FE-007 | responsive route tests |
| FE-014 | project overview shell | FE-013 | empty/full/long-data visuals |
| FE-015 | project team and access page | FE-012,FE-014 | role-aware tests |

## F2 — WBS, locations, contracts

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-020 | project contract/settings forms | FE-014 | validation/localization tests |
| FE-021 | WBS tree/table editor | FE-014 | 1000+ item performance test |
| FE-022 | location/zone/STA hierarchy UI | FE-014 | building/road fixtures |
| FE-023 | project template selection/customization | FE-021,FE-022 | template states |

## F3 — RAB / BoQ

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-030 | budget versions page | FE-014,FE-007 | version-state tests |
| FE-031 | XLSX upload/import stepper | FE-030 | loading/error/retry states |
| FE-032 | sheet/header/column mapping UI | FE-031 | keyboard/table tests |
| FE-033 | row validation/preview UI | FE-032 | large/error fixtures |
| FE-034 | dense budget/WBS value table | FE-030 | responsive + numeric alignment |
| FE-035 | budget approval/read-only history | FE-030 | capability tests |
| FE-036 | addendum/version comparison UI | FE-035 | historical diff fixtures |
| FE-037 | saved import mapping management | FE-032 | scoped settings tests |

## F4 — Planning / Time Schedule

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-040 | baseline versions and status page | FE-014 | state tests |
| FE-041 | weekly allocation grid | FE-034,FE-040 | keyboard/large-grid tests |
| FE-042 | S-curve planned preview | FE-041 | chart + data table |
| FE-043 | baseline approval/history/rebaseline comparison | FE-040 | version tests |

## F5 — Field reporting

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-050 | daily/field report list/filter | FE-014 | state/filter tests |
| FE-051 | report create/edit form | FE-050 | quantity/milestone/percent variants |
| FE-052 | work/location picker optimized for field use | FE-051 | mobile tests |
| FE-053 | evidence photo/file upload UX | FE-051 | progress/retry/error states |
| FE-054 | submit/revision/resubmit UX | FE-051 | state-machine tests |
| FE-055 | typed custom fields renderer | FE-051 | schema fixtures |

## F6 — Approval and progress controls

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-060 | approval queue | FE-050 | dense desktop/mobile states |
| FE-061 | report review/evidence/measurement view | FE-060 | keyboard/mobile tests |
| FE-062 | approve/reject/request-revision actions | FE-061 | conflict/version handling |
| FE-063 | project progress dashboard | FE-014 | role-aware views |
| FE-064 | actual vs planned S-curve | FE-063 | accessible chart/table |
| FE-065 | work-item progress drilldown | FE-063 | large-WBS tests |
| FE-066 | delay/overrun/exception surfaces | FE-063 | human CTA and status tests |

## F7 — Official documents

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-070 | report-period/document list | FE-014 | status/version tests |
| FE-071 | weekly report preview chrome | FE-070 | print-theme visual tests |
| FE-072 | time-schedule/S-curve preview | FE-070 | print-theme tests |
| FE-073 | publish/reissue/download UX | FE-071,FE-072 | capability/audit states |
| FE-074 | company branding/signatory settings | FE-070 | theme-independent print preview |
| FE-075 | report-template configuration UI | FE-074 | validation/preview tests |

## F8 — Telegram Mini App and notification UI

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-080 | Telegram linking UI from web | FE-011 | expired/revoked states |
| FE-081 | Mini App shell/session bootstrap | FE-007 | host/theme states |
| FE-082 | Mini App project/report list | FE-081 | mobile tests |
| FE-083 | Mini App field report form | FE-082,FE-051 | touch/mobile tests |
| FE-084 | Mini App evidence/upload/submission | FE-083 | poor-network states |
| FE-085 | notification center/preferences | FE-006 | in-app/Telegram preference states |

## F9 — Costs and materials

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-090 | expense/commitment list and forms | FE-014 | accounting-neutral labels/tests |
| FE-091 | cost vs budget/progress view | FE-090,FE-063 | reconciliation display tests |
| FE-092 | vendor/cost-code management UI | FE-090 | CRUD states |
| FE-093 | material catalog | FE-014 | table/form tests |
| FE-094 | stock movement forms | FE-093 | unit/validation tests |
| FE-095 | project stock/consumption view | FE-094 | movement history states |

## F10 — SaaS administration and billing

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-100 | plan/usage/entitlement page | FE-011 | limit/read-only states |
| FE-101 | subscription lifecycle UI | FE-100 | grace/cancel states |
| FE-102 | platform tenant admin console | FE-006 | support-access boundaries |
| FE-103 | support access request/indicator | FE-102 | explicit consent/audit UI |

## F11 — Enterprise, scale, quality

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| FE-110 | SSO organization setup UI | FE-011 | provider/error states |
| FE-111 | audit log explorer/export UI | FE-014 | filter/pagination tests |
| FE-112 | delegated approval settings | FE-060 | role/time-bound states |
| FE-113 | accessibility regression suite | all major surfaces | WCAG checks |
| FE-114 | light/dark visual regression suite | all major surfaces | screenshot baselines |
| FE-115 | localization completeness gate | all major surfaces | en/id parity check |
| FE-116 | large-data/performance hardening | budget/schedule/report views | measured benchmark |
| FE-117 | responsive/mobile hardening | all field workflows | 360/390/768/1024/1440 checks |
