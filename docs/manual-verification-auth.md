# Manual verification: staff authentication

Use these steps when Playwright E2E against the full Compose stack is not available.

## Prerequisites

- Postgres, Go API, and Staff app running (`make dev` or equivalent)
- Migrations applied (includes seed org `demo-venue` with `preseeded@example.com`)

## Happy path: login → create org → dashboard

1. Open `http://localhost:3001/login`
2. Enter a new email (e.g. `new-venue@example.com`) and request a passcode
3. Copy the 6-digit code from the Go API terminal logs
4. Enter the code and submit
5. Confirm redirect to `/onboarding/create-organization`
6. Enter organization name and slug, submit
7. Confirm redirect to `/` (dashboard)
8. Confirm the Session section shows your email and the new organization name

## Multi-org picker

1. In Postgres, add a second organization and member for the same email used above
2. Sign out, sign in again with that email
3. Confirm redirect to `/select-organization`
4. Pick an organization and confirm landing on `/` with the chosen org in Session

## Org switching

1. While signed in with 2+ memberships and an active org on `/`
2. Use the **Switch organization** dropdown
3. Select a different organization
4. Confirm the active organization updates without signing out

## Auth fork (no active member)

1. Sign in with an email that has 2+ memberships (do not pick an org)
2. Confirm `/` redirects to `/select-organization`
3. Confirm onboarding routes remain accessible

## API checks (optional)

```bash
# After OTP verify, without selecting org:
curl -s -H "Authorization: Bearer $SESSION_ID" http://localhost:8080/api/v1/staff/me
# Expect 403 FORBIDDEN

# Select org:
curl -s -X POST -H "Authorization: Bearer $SESSION_ID" -H "Content-Type: application/json" \
  -d '{"member_id":"MEMBER_UUID"}' http://localhost:8080/api/v1/staff/session/organization

# Staff me succeeds:
curl -s -H "Authorization: Bearer $SESSION_ID" http://localhost:8080/api/v1/staff/me
```
