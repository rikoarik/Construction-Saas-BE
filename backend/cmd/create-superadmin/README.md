# Provision a platform administrator

Run this command only from a trusted operator environment with access to the intended database. It creates a **new** active user and a platform-wide administrator grant in one transaction; it will not promote or replace an existing account. Start the API first so migrations are applied.

Configure `DATABASE_URL` securely in the environment. From `backend/`, run the following in a private terminal; input is hidden and is passed on stdin, not in command arguments:

```sh
stty -echo
read -r PASSWORD
stty echo
printf '%s\n' "$PASSWORD" | go run ./cmd/create-superadmin -email 'admin@example.com'
unset PASSWORD
```

Replace the email with the intended account and enter a unique password of at least 8 characters and at most 1024 bytes when prompted. Do not share it in chat or commit it to a file. Protect access to the database and this command: anyone able to run it can create an administrator. Remove a grant from `app.platform_admins` to revoke platform access; authorization checks the current grant on every request. This command does not provide audit logging or MFA.

The frontend requires `BACKEND_ORIGIN` pointing to the backend. Configure `CSRF_SECRET` and `TRUSTED_ORIGINS` on the API (including the exact frontend origin) for browser login and logout; production requires a secret of at least 32 bytes. Platform access requires TOTP enrollment and a verified session; creating an account alone does not grant access without MFA. Platform company management has not been implemented.
