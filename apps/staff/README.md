# @ticket-pos/staff

Staff-facing Next.js app for Ticket POS.

Acts as a BFF for authenticated staff workflows: catalog management, in-person POS mode, and sale imports.
Auth is email OTP with server-side sessions (not yet implemented).

## Development

```bash
pnpm install
pnpm dev
```

Runs on port 3001 by default.
