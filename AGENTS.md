# Frontend Agent Rules

This repository owns **web frontend and Telegram Mini App UI only**.

Before implementation read:

1. `docs/product/PRD.md`
2. `docs/frontend/FRONTEND.md`
3. `docs/frontend/DESIGN.md`
4. `docs/delivery/FRONTEND-TASKS.md`
5. `docs/contracts/API-CONTRACT-MIRROR.md`
6. `docs/frontend/TELEGRAM-MINI-APP.md` for Telegram UI work
7. `docs/frontend/FRONTEND-ACCEPTANCE.md` for done criteria
8. `docs/delivery/PARALLEL-WORKFLOW.md` for parallel execution

Hard rules:

- no backend/domain/business calculation logic in the browser;
- no hardcoded user-visible UI copy; use i18n;
- English is default locale; Indonesian is supported;
- light/dark/system are first-class;
- follow `DESIGN.md`; avoid AI-slop/card-wall UI;
- no emoji as interface icons; use approved icons/assets;
- use human labels and CTAs;
- API types come from the canonical backend contract, not handwritten guesses;
- Telegram Bot API/webhook code does not belong in this repository;
- do not mark tasks done without repository evidence/tests.
