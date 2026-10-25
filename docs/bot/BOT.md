# Telegram Bot System Design

**Status:** normative  
**Repository:** backend  
**Owner:** bot lane  
**Transport:** Telegram Bot API  
**Does not own:** construction business logic, project authorization rules, progress calculations, RAB logic, approval rules, database schema ownership

This document defines the Telegram Bot as a dedicated engineering domain inside the backend repository. It is intentionally separate from `docs/backend/BACKEND.md` so a Claude session can work on the bot in parallel with backend-core work without treating the bot as a third repository.

## 1. Boundary

The bot is an **adapter**, not a second backend.

```text
Telegram
   |
   v
Telegram adapter / webhook handler
   |
   v
Backend application-service ports
   |
   +--> identity / tenancy / project / reports / approvals / notifications
   |
   v
PostgreSQL through backend-owned repositories
```

The bot must not duplicate rules that already exist in backend application services.

### Bot owns

- Telegram webhook verification and update deduplication
- parsing commands, callback queries, deep links, and message updates
- Telegram account-linking transport
- Telegram notification rendering and delivery
- opening/deep-linking the Telegram Mini App
- Telegram-specific rate-limit handling and retry behavior
- Telegram message/callback correlation
- provider-specific observability and error mapping
- bot-specific localization of transport messages

### Bot does not own

- membership and project-access truth
- report state machines
- approval authorization
- RAB or budget calculations
- progress calculations
- document publication rules
- subscription entitlements
- direct mutations of domain tables
- UI of the Telegram Mini App

The Mini App UI is owned by the frontend repository and specified in `TELEGRAM-MINI-APP.md` there.

## 2. Recommended package ownership

```text
internal/
  integrations/
    telegram/
      webhook.go
      verification.go
      updates.go
      commands.go
      callbacks.go
      linking.go
      miniapp.go
      render.go
      delivery.go
      errors.go
      metrics.go
      ports.go
      *_test.go
```

Bot code may depend on application-service interfaces. Domain/application packages must not import the Telegram adapter.

Use dependency inversion:

```text
telegram adapter -> application port -> domain/application service
```

Never:

```text
domain -> telegram adapter
```

## 3. Runtime model

For the initial modular-monolith deployment, the webhook handler may run inside the main Go API process and Telegram delivery may run in the worker process. This does not merge bot ownership into backend core; package boundaries remain explicit.

A separate bot process is only justified when measured traffic, deployment isolation, or operational ownership requires it.

## 4. Webhook security

Every webhook request must:

1. use HTTPS at the edge;
2. verify the configured Telegram webhook secret header;
3. reject malformed bodies before domain calls;
4. deduplicate Telegram update IDs;
5. use a bounded request body;
6. record correlation identifiers;
7. never log raw authentication payloads, tokens, or private file contents.

Webhook acknowledgement must be fast. Long-running work is queued through backend outbox/job infrastructure.

## 5. Idempotency

Telegram may redeliver updates. A duplicate update must not:

- create a second report;
- approve something twice;
- issue duplicate linking tokens;
- send the same durable notification repeatedly without an explicit retry reason.

Persist processed update identifiers or a stable idempotency key with an appropriate retention policy.

## 6. Account linking

Telegram identifiers are external identifiers, not authorization.

Recommended flow:

```text
Web app
  -> request one-time link token
  -> user opens bot deep link
Bot
  -> submits token + Telegram user identity to backend application service
Backend
  -> validates token expiry/use/company/user
  -> records telegram link
  -> invalidates token
```

Requirements:

- one-time use;
- short expiry;
- revocable;
- auditable;
- never infer application user only from display name or username;
- account unlinking must invalidate future bot actions immediately.

## 7. Telegram Mini App relationship

The bot may provide buttons/deep links that open the Mini App. The bot does not render the Mini App UI.

Mini App `initData` is verified server-side by backend integration code before an application session/capability response is returned. Frontend may pass the raw initialization payload only over the expected bootstrap endpoint and must not treat `initDataUnsafe` as trusted authorization data.

## 8. Commands and callbacks

Use a small, human command surface. Avoid turning every workflow into chat commands when a form is more appropriate.

Good bot responsibilities:

- `/start` and account-link status
- `Open field report`
- `Open approvals`
- `Open project`
- notification actions with safe callback identifiers
- help/status commands

Complex input such as measurements, WBS selection, evidence upload review, or long forms belongs in the Mini App.

Callback payloads must use opaque identifiers or signed short references. Do not place sensitive data or permission claims inside callback data.

## 9. Notification delivery

Backend core emits provider-neutral notification intents. Telegram adapter converts eligible intents to Telegram messages.

```text
Domain event
  -> notification intent
  -> routing/preferences
  -> Telegram delivery job
  -> Telegram API
  -> delivery status
```

The Telegram adapter must honor:

- user preferences;
- tenant/project access at send time where relevant;
- message localization;
- retry policy;
- Telegram rate limits;
- permanent vs transient error classification;
- revoked/blocked chat handling.

Do not create notification text in domain services.

## 10. Message design rules

Bot messages are operational, short, and human.

Use:

- clear project/report context;
- one obvious next action;
- status wording consistent with the web UI;
- deep links instead of long instructions when the action is better in the Mini App.

Avoid:

- emoji as status semantics;
- marketing copy;
- long AI-generated explanations;
- exposing internal IDs;
- raw backend error messages;
- ambiguous CTA text such as `Proceed` or `Continue` when a specific action exists.

Example actions: `Open report`, `Review approval`, `Fix report`, `Open project`.

## 11. Localization

Bot transport messages must use translation keys/catalogs, not hardcoded English strings scattered through handlers. English is the default product locale; Indonesian is supported.

User-generated project names and notes remain unchanged.

## 12. Failure handling

Classify delivery failures:

- transient network/server error -> retry with backoff;
- Telegram rate limit -> retry after provider-advised delay;
- blocked/deactivated chat -> mark channel unavailable and stop automatic retry;
- malformed request created by our code -> dead-letter + alert;
- domain permission/state conflict -> do not retry blindly; return current state or safe action.

The bot must never hide an authoritative backend rejection by pretending an action succeeded.

## 13. Observability

Track at minimum:

- webhook request count and latency;
- invalid-secret attempts;
- duplicate updates;
- update processing failures by type;
- Telegram API latency/error rate;
- retries and dead letters;
- blocked/deactivated chats;
- linking success/failure;
- notification delivery lag.

Logs use correlation IDs and safe external references. Never log bot token, session credentials, raw Mini App auth payloads, or private evidence.

## 14. Testing

Required bot tests:

- webhook secret verification;
- duplicate update replay;
- malformed update handling;
- command/callback routing;
- expired/used linking token;
- unlinked user;
- linked user without project permission;
- blocked bot/channel state;
- provider rate limit;
- notification retry/idempotency;
- Mini App bootstrap handoff;
- localization key coverage.

Use fake Telegram HTTP transport in tests. CI must not depend on the live Telegram API.

## 15. Claude ownership rule

A Claude session assigned to the bot lane may edit bot adapter packages, bot tests, `BOT.md`, and `BOT-TASKS.md`.

It must not modify core calculation/database/domain behavior just to make a bot workflow easier. If a backend capability is missing, create or request a backend-core task/contract change and integrate after that change is approved.
