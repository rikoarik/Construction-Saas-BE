# Parallel Claude CLI Workflow — Frontend Repository

Frontend is a separate repository and can run concurrently with both backend-core and bot lanes.

## Branch/worktree

```bash
git worktree add ../construction-frontend-lane -b lane/frontend
```

Run Claude in that worktree.

## Frontend can proceed without waiting for backend

Use the reviewed API/OpenAPI contract and conforming mock fixtures. Frontend must not invent response fields because a backend endpoint is unfinished.

```text
Backend contract ready
       |
       +--> backend-core implementation
       |
       +--> frontend generated types + mocks + UI
       |
       +--> bot adapter when relevant
                   |
                   v
              integration/E2E
```

## Ownership

Frontend session owns:

- Next.js application;
- design system;
- i18n catalogs;
- web UI;
- Telegram Mini App UI;
- client API layer generated from contract;
- frontend tests.

It does not modify backend behavior to satisfy UI assumptions.

When a contract is insufficient, record the required contract change instead of adding frontend-only fake semantics that will diverge from production.
