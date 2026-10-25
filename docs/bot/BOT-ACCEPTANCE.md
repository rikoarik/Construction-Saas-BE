# Telegram Bot Acceptance Criteria

| ID | Scenario | Pass condition |
|---|---|---|
| BOT-AC-001 | webhook secret | invalid/missing secret is rejected before update processing |
| BOT-AC-002 | duplicate update | replayed Telegram update produces no duplicate durable side effect |
| BOT-AC-003 | malformed update | parser rejects safely without panic or raw payload leakage |
| BOT-AC-004 | account linking | expired/used/revoked link token cannot link an account |
| BOT-AC-005 | authorization | linked Telegram identity without project permission cannot perform privileged action |
| BOT-AC-006 | Mini App handoff | invalid/expired init data is rejected; valid payload receives backend bootstrap response |
| BOT-AC-007 | callback tampering | modified/replayed callback reference cannot bypass expected state/permission checks |
| BOT-AC-008 | notification idempotency | retried delivery job does not create unintended duplicate logical notification |
| BOT-AC-009 | provider rate limit | retry honors Telegram rate-limit semantics and does not busy-loop |
| BOT-AC-010 | blocked chat | permanent delivery failure disables/marks channel appropriately instead of retrying forever |
| BOT-AC-011 | localization | supported bot system messages resolve through catalogs with English default and Indonesian support |
| BOT-AC-012 | no business duplication | bot tests use backend ports; no progress/RAB/approval rules are reimplemented in Telegram adapter |
| BOT-AC-013 | secret redaction | logs never contain bot token, raw Mini App auth payload, session secret, or private evidence content |
| BOT-AC-014 | live-provider independence | CI bot suite passes with fake Telegram transport and no live API dependency |
