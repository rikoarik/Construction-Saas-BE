# Telegram Mini App Frontend Design

**Repository:** frontend  
**Owner:** frontend lane  
**Runtime:** Next.js/React UI opened inside Telegram  
**Not the Telegram Bot:** bot webhook, commands, callback delivery, and Telegram Bot API live in the backend repository under `BOT.md`.

## Boundary

The Mini App is a focused field interface for people on site. It uses the same backend API and domain rules as the normal web application.

It owns:

- mobile field-report UX;
- report draft/edit/revision views;
- project/work/location selectors;
- quantity/milestone/verified-percent input UI;
- evidence upload UI;
- submission and returned-for-revision state;
- theme adaptation appropriate for Telegram host context;
- localized UI through the same i18n catalog strategy as web.

It does not own:

- Telegram webhook processing;
- bot commands/messages;
- account authorization truth;
- calculation or approval rules;
- direct database access.

## Primary journeys

```text
Open from bot
  -> bootstrap session
  -> assigned projects
  -> choose project
  -> create/open report
  -> add work measurement
  -> attach evidence
  -> review
  -> submit
  -> see submitted / revision-required state
```

Do not reproduce the entire desktop dashboard inside Telegram. Field flows are task-specific and touch-first.

## Bootstrap

The frontend forwards the expected Telegram initialization payload only to the backend bootstrap endpoint. It does not trust `initDataUnsafe` as authorization and does not derive permissions locally.

After bootstrap, render from server-provided user/company/project capabilities.

## UI rules

All rules in `DESIGN.md` apply:

- no emoji as icons;
- no hardcoded UI copy;
- English default with i18n keys;
- light/dark/system-compatible tokens;
- human CTA names;
- concise text;
- original vector assets for empty states;
- 44px minimum touch targets;
- explicit units next to construction measurements;
- no decorative card wall.

Prefer one main action per screen. Use bottom action areas for mobile submission/review where appropriate.

## Network behavior

Field connectivity can be unreliable. The Mini App must distinguish:

- local editing state;
- upload in progress;
- server-saved draft;
- submission pending;
- authoritative submitted state.

Never show success before authoritative save/submit succeeds. Photo upload supports retry and per-file status. Full offline synchronization may be implemented later only with an explicit conflict model; do not fake offline support with local-only data that users may mistake as submitted.

## Testing

Test at minimum:

- Telegram host bootstrap success/failure;
- linked vs unlinked account;
- revoked project access;
- quantity/milestone/percent report variants;
- evidence upload retry;
- slow/failed connection;
- returned-for-revision flow;
- English/Indonesian messages;
- dark/light themes;
- compact phone widths;
- keyboard/focus where applicable.
