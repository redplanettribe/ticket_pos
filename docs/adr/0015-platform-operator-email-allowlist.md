# Platform Operator authority is an email allowlist riding Staff Sessions

The platform needs an actor whose authority spans every Organization: the Platform Operator,
who oversees all Organizations, their Events, and the money owed to each, and records Payouts.
We grant that authority by listing the person's email in a `platform_operators` table. Signing
in is the ordinary staff Proof of Email Ownership (OTP or Google Sign-In); a Staff Session
whose email is on the list may use the `/api/v1/operator/*` surface and the Operator Dashboard
inside the staff app. Operator authority is orthogonal to Membership — an operator need not be
a Member of any Organization, no active Member is required on operator routes, and being an
Org Admin grants nothing platform-wide.

## Considered Options

- **A reserved "platform" Organization whose Org Admins are operators** — reuses the Member
  model, but plants a magic Organization that every org-scoped query, invariant, and listing
  must remember to exclude, and contradicts the glossary: an Org Admin's authority stops at
  their Organization.
- **Env-var allowlist** — no schema, but changing operators means a redeploy and there is
  nowhere to hang audit metadata.
- **A separate credential system for operators** — strongest isolation, but a second auth
  surface to build and maintain for one or two people, when Proof of Email Ownership is
  already the platform's whole theory of signing in.

## Consequences

- Rows in `platform_operators` are inserted by direct database write (plus a dev-seed
  migration). There is deliberately no UI for managing operators: the first row could never
  be added through one, and the action is rare enough that none is warranted.
- Recording a Payout moves from a direct database write into the Operator Dashboard,
  partially superseding ADR 0014's stance that Payouts are recorded "directly in the
  database." The Payout remains a bare fact — amount, paid date, note — now stamped with
  `recorded_by`, the recording operator's email (plain text, not a foreign key, so history
  survives an operator's removal from the allowlist).
- The API never rejects a Payout for exceeding the Withdrawable Balance: by recording time
  the money has already moved, and refusing to record reality would corrupt the ledger. The
  signed balance absorbs it; the dashboard warns before submission instead.
- Platform-wide money totals (accumulated Platform Fees and Fee IVA, total owed) are grouped
  by currency with no FX conversion — one USD row in practice while PayPhone is the only
  Payment Provider.
