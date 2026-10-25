# Frontend Acceptance Criteria

These criteria validate web and Telegram Mini App presentation/interaction only. Backend business correctness is validated in the backend repository.

| ID | Scenario | Pass condition |
|---|---|---|
| FE-AC-001 | English/Indonesian UI | all visible system copy comes from i18n catalogs; no hardcoded JSX labels |
| FE-AC-002 | light/dark/system | no unreadable controls, flashes, or token bypass on critical pages |
| FE-AC-003 | project/RAB tables | dense data remains readable, keyboard usable, numeric columns aligned, sticky identifiers work |
| FE-AC-004 | RAB import | upload/mapping/preview/error states are understandable without exposing raw server errors |
| FE-AC-005 | report form | quantity, milestone, and verified-percent variants render correct controls and units |
| FE-AC-006 | approval UI | server permissions/state determine available actions; stale conflict is shown safely |
| FE-AC-007 | S-curve | planned/actual/deviation are readable, not color-only, and have accessible numeric equivalent |
| FE-AC-008 | official document preview | paper surface remains print-correct in both app themes |
| FE-AC-009 | mobile field flow | critical report flow works at 360–390 px with >=44 px touch targets |
| FE-AC-010 | empty/error states | concise copy, original vector assets where useful, no emoji/AI-slop filler |
| FE-AC-011 | Telegram Mini App bootstrap | invalid/expired bootstrap shows safe recovery; valid user receives server capabilities |
| FE-AC-012 | evidence upload | per-file progress, failure, retry, and authoritative completion are visible |
| FE-AC-013 | network failure | UI never shows report submitted/approved before backend confirms it |
| FE-AC-014 | API contract | generated/client types and fixtures conform to canonical backend contract |
| FE-AC-015 | accessibility | keyboard/focus/zoom/reduced-motion and accessible labels pass critical-flow review |
