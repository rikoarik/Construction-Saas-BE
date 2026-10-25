# Parallel Claude CLI Workflow — Backend Repository

This repository has **two parallel engineering lanes**:

1. `backend-core` — Go domain/API/database/workers.
2. `bot` — Telegram Bot adapter and delivery.

They share one repository but must use separate branches/worktrees and file ownership.

## Worktrees

From the backend repository:

```bash
git worktree add ../construction-backend-core -b lane/backend-core
git worktree add ../construction-backend-bot -b lane/bot
```

Run one Claude session in each worktree.

## Ownership

### backend-core lane may edit

- domain/application packages;
- repositories/database/migrations;
- API endpoints;
- calculation/reporting code;
- generic notification/outbox infrastructure;
- `BACKEND.md`, `BACKEND-TASKS.md`, database/calculation/API docs.

### bot lane may edit

- `internal/integrations/telegram/**` or equivalent Telegram adapter packages;
- Telegram delivery adapter/tests;
- `BOT.md` and `BOT-TASKS.md`.

### shared touch points

Route registration, generated interfaces, dependency wiring, and canonical API contracts are integration touch points. Avoid editing them concurrently. Assign one explicit integration task after both lane branches are ready.

## Dependency rule

Bot work depends on backend application ports/contracts, not backend implementation details. If the bot lane needs a new core capability:

1. define the required interface/contract;
2. create a backend-core task;
3. backend-core implements and tests it;
4. bot integrates against it.

The bot lane must not bypass this by reading/writing domain tables directly.

## Merge order

For a capability that affects both lanes:

```text
contract/application port
        -> backend-core implementation
        -> bot adapter implementation
        -> integration tests
        -> merge
```

Independent bot transport work may proceed before backend implementation when it uses fakes for the agreed port.
