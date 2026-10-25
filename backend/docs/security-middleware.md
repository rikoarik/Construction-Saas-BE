## Security middleware configuration

`APP_ENV=production` enables Secure session cookies and HSTS response headers; deployments must enforce TLS at the edge. `CSRF_SECRET` (at least 32 bytes, stable across API replicas) enables session-bound CSRF protection; configure exact origins in comma-separated `TRUSTED_ORIGINS`. Authenticated browser clients call `GET /api/v1/auth/csrf` and submit the returned `csrf_token` in `X-CSRF-Token` on unsafe cookie-authenticated methods. Login requests require a matching Origin/Referer and reject `Sec-Fetch-Site: cross-site`.

Login and API rate limits are configurable with `LOGIN_RATE_LIMIT_CAPACITY`, `LOGIN_RATE_LIMIT_WINDOW`, `LOGIN_RATE_LIMIT_MAX_KEYS`, and equivalent `API_RATE_LIMIT_*` variables. Current defaults are 10 login requests/minute and 120 general API requests/minute per socket peer. Forwarded IP headers are ignored. The built-in limiter is process-local; use shared atomic state for multi-replica deployment.

Baseline response headers include `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and restrictive `Content-Security-Policy`; HSTS is enabled only in production mode.

Run the negative middleware tests with `go test ./internal/httpapi -run 'CSRF|RateLimiter|SecurityHeaders|RouterSecurity' -count=1`.
