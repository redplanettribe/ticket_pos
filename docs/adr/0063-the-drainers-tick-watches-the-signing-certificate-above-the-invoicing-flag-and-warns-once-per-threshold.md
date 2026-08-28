# The Drainer's tick watches the signing certificate, above the invoicing flag, and warns once per threshold

Specified as issue #490, gap U9 of `docs/sri-invoicing-flows.md` (row 59). Builds on
[ADR 0059](./0059-the-platform-is-the-sole-issuer-and-its-signing-certificate-lives-encrypted-in-the-database.md),
which put the Issuer's `.p12` in the database with its `NotAfter` beside it, and on
[ADR 0060](./0060-a-house-organizations-tickets-are-the-platforms-sale-invoiced-after-checkout-and-credited-on-reversal.md),
whose Sale Invoice Drainer parks a document `needs_attention` unsigned the minute that date passes.

## Context

The certificate lasts one to five years, set by the entidad certificadora, and the platform's only statement of
its `NotAfter` is a date on the Issuer page. When it lapses every owed Sale Invoice parks unsigned — no
secuencial, no SRI call — while the SRI's 24 h window and the "immediate transmission" rule keep running, and
the only signal is a rising `needs_attention` count somebody may or may not look at that day. The remedy is a
re-upload, which takes a person with the new `.p12` and its password; the warning has to reach that person
before the date, not after.

Two things about the surrounding machinery shape the answer. Every periodic job here is a Cloud Scheduler job
calling an internal endpoint, created paused by Terraform. And `SALE_INVOICING_ENABLED` ships closed: the
Drainer's endpoint answers 404 on its first line while it is closed, which is precisely the state production is
in, and the state this warning must be alive in — the certificate uploaded ahead of the flag opening can lapse
while nobody is looking.

## Decision

1. **The Sale Invoice Drainer's tick carries the Certificate Expiry Warning, and does so above the invoicing
   flag.** Every drain first reads the Issuer's certificate metadata and works the ladder, then — only if
   `SALE_INVOICING_ENABLED` is open — signs and submits as before. A closed flag still answers
   `SALE_INVOICING_UNAVAILABLE` to the caller and signs nothing; it no longer means the certificate goes
   unwatched. No new endpoint and no new scheduler job: the Drainer already opens this certificate and already
   has the clock.
2. **The ladder is 30, 7, 1 and 0 days, counted in Ecuadorian calendar days,** between today in
   `America/Guayaquil` and the Ecuadorian date `NotAfter` falls on — never elapsed hours. A tick fires **the
   highest threshold not yet fired that the day count has reached, and only that one**: a certificate uploaded
   with five days left fires 30 and nothing else; one uploaded already expired fires 0 alone. The rungs the
   day count has reached *below* the one fired are recorded as covered by that same mail — otherwise the tick
   five minutes later would send "seven days" about the same date, the three-mails outcome spread out. Past
   `NotAfter` only the 0 rung is a candidate; a missed countdown is never sent late. Expiry fires
   once; there is no daily nag afterwards, because the banner and the parked documents' own message carry the
   ongoing state.
3. **Once per threshold per certificate, kept in a ledger of sends keyed on the certificate's SHA-256
   fingerprint and the threshold** — `certificate_expiry_notices`, in the family of `answer_reminders` and
   `ticket_assignment_mails`: a table that exists to say no. A different certificate has no rows, so a
   re-upload restarts the ladder against the new `NotAfter` by construction; re-uploading the same file changes
   nothing and re-fires nothing. The row is written **after** the fan-out, once at least one address accepted the
   mail; a partial failure is logged and never fails the tick, and a total failure leaves no row so the next
   tick tries again. At-least-once, because there is no second chance at "seven days".
4. **The mail goes to every address on the operator allowlist, from the transactional sender, in each
   recipient's Staff Locale** — exactly as the Payout Request and Question Review notices do (ADR 0015: the
   allowlist is the whole of operator authority). No new configuration and no "ops address": the people told
   are the people who can re-upload.
5. **One derivation serves every surface.** The Issuer read carries a `certificate_expiry` block — state
   (`none`, `valid`, `expiring`, `expired`), `NotAfter`, and the day count — computed by the same rule the
   ladder uses, and the Operator Dashboard, the invoicing list and the Issuer page render their banner from it.
   The Issuer read is not behind the flag, so the banner is alive whenever the mail is. No surface re-derives
   days from a date.
6. **A missing certificate is not a warning.** `none` fires no threshold and mails nobody; it stays what it is
   today, a state the Issuer page shows. Expired and absent are different facts with different remedies and
   are never conflated.

## Considered options

- **A daily sweep of its own** (`POST /internal/certificate-expiry/sweep`, a fifth paused Cloud Scheduler job).
  The reminders' shape, and the ladder's natural clock. Rejected as a second job to unpause and forget, doing
  work the Drainer's tick already does; the ticking-in-minutes concern it answers is answered by the ledger
  instead.
- **The check inside the gated round.** Silent in production until the flag opens — the one window the ticket
  says this must cover. Rejected.
- **A fixed address in config.** One mail, one knob that can be empty, and a reader who may not be an operator.
  Rejected in favour of the precedent.
- **Fire every threshold the day count has passed.** Sends two or three mails at once to somebody who just
  uploaded a short-lived certificate; reads as a bug. Rejected for highest-unfired-only.
- **Ledger row before the send (at-most-once).** Loses a whole threshold to a five-minute provider outage.
  Rejected.
- **Naming the expiry date in the parked message from the frontend, off the live certificate.** Lies after a
  re-upload. The Drainer writes the date into the park's `additional_info` at park time instead — the row says
  what was true when it parked, like every other park.

## Consequences

- The Cloud Scheduler job driving the Drainer must be **unpaused in production before the flag opens** — and,
  for this warning to mean anything, before the certificate's first threshold. Today it ships paused (ADR 0060);
  the deploy step is Terraform's, and the ticket's brief names it.
- The Drainer's tick now does one thing while the flag is closed. Its 404 to the caller is unchanged; a log line
  states the ladder's outcome so a closed-flag deployment can be seen watching.
- `certificate_expiry_notices` is the third ledger-of-sends, with the same warning the other two carry: it
  rations, it is never read for a count of mail, and it is never confused with its siblings.
- Row 59 / U9 of `docs/sri-invoicing-flows.md` moves from **Partial** to built when this lands.
