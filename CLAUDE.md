# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## Repository scope

This is a backend documentation repository for Construction SaaS, not an implemented Go backend or frontend. It contains product, system-design, backend, Telegram Bot, API-contract, and delivery documentation, plus scripts to synchronize shared documents into a separately checked-out frontend repository. Treat architecture and contracts as proposals unless a document explicitly says otherwise. No Go application source, frontend source, `package.json`, or build/test configuration is present; do not describe proposed behavior as implemented or invent build, lint, or test commands.

## Start here and route work

Read `AGENTS.md` first; it defines task routing, lane ownership, and the required Git workflow. Before a task, read its acceptance criteria and the corresponding canonical references:

- `BE-*`: `docs/backend/BACKEND.md`, `docs/backend/BACKEND-ACCEPTANCE.md`, and `docs/delivery/BACKEND-TASKS.md`.
- `BOT-*`: `docs/bot/BOT.md`, `docs/bot/BOT-ACCEPTANCE.md`, and `docs/delivery/BOT-TASKS.md`.

Shared architecture and product references are in `docs/architecture/`, `docs/specs/`, and `docs/product/PRD.md`. `README-DOCS.md` is the documentation-pack index. Canonical API and PRD documents live here; frontend copies are generated/shared mirrors and should be updated through the sync script, not edited directly.

Keep backend-core and Bot responsibilities separate. The proposed backend is a Go modular monolith with PostgreSQL as the source of truth, private object storage, and a worker/outbox for asynchronous work. The Bot is a Telegram transport adapter over backend application ports; it must not own domain rules, authorization, calculations, or direct domain-table access. If Bot work needs a missing backend capability, request a backend-core contract/task rather than implementing it in the adapter. See `docs/delivery/PARALLEL-WORKFLOW.md` for separate worktrees, ownership, and integration touchpoints.

For data or behavior changes, consult the relevant canonical specification rather than inferring from examples: `docs/specs/API-CONTRACT.md`, `docs/architecture/DATA-DICTIONARY.md`, `DATABASE.md`, `CALCULATION-RULES.md`, `docs/specs/STATE-MACHINES.md`, `RAB-IMPORT.md`, and `REPORT-TEMPLATES.md`.

## Available commands

There is no application build, lint, typecheck, or test command in this checkout. The available scripts synchronize or verify shared documentation against a separate frontend checkout:

```bash
./tools/sync_frontend_shared_docs.sh
./tools/check_frontend_shared_docs.sh
```

Both default to `../construction-frontend`; set `FRONTEND_REPO` to use another checkout:

```bash
FRONTEND_REPO=/path/to/frontend ./tools/sync_frontend_shared_docs.sh
FRONTEND_REPO=/path/to/frontend ./tools/check_frontend_shared_docs.sh
```

The sync script copies the canonical PRD and generates the frontend API-contract mirror. The check script compares those copies and exits unsuccessfully if they differ. No single-test command exists because there is no test suite in this repository.

## Repository guidance files

`AGENTS.md` is the canonical task and engineering guidance. Cursor, Copilot, Cline, Roo, and Windsurf instructions also point contributors to `AGENTS.md` and the domain/task-lane rules; consult it rather than duplicating those rules here.

## Optional agent-config import

A `GEMINI.md` is present in the repository. If you want Claude Code to scan for importable settings from supported external agent configurations, reply `/import` to list what's importable, then `/import --yes=<digest>` using the digest shown by the scan. If `/import` is unavailable in this interface, run `claude import` in a terminal. No foreign-agent configuration was read or imported as part of this repository analysis.
