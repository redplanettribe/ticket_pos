# Transactional email via Resend

Transactional email — staff OTP passcodes, Sale Confirmations, and Sale void
notices — is delivered through **Resend**, called with a raw `net/http` POST
behind the existing `platform.EmailSender` interface. Mail is sent from a
verified **subdomain**, `send.multiticketing.com`, as
`Multiticketing <noreply@send.multiticketing.com>`. The provider is selected at
runtime by the presence of `RESEND_API_KEY`: set (from Secret Manager in prod) it
uses Resend, unset (local dev, tests) it falls back to the logging sender.

Resend was chosen over AWS SES and Postmark for cost and setup at this stage: its
free tier covers current volume at $0, and it is the least code and configuration.
The choice is cheap to reverse — a new implementation of an interface the code
already depends on — which is why it did not warrant a heavier evaluation.

## Consequences

- **SMTP is not an option.** GCP blocks outbound port 25 permanently, so delivery
  is over HTTPS. This also rules out any future "just point net/smtp at a relay"
  shortcut from Cloud Run.
- **A subdomain, not the apex.** Sending reputation is isolated to
  `send.multiticketing.com`, so anything later attached to the apex cannot poison
  OTP deliverability — and the OTP is a login credential. The cost is a slightly
  longer From address.
- **DNS is an operator step.** Resend domain verification requires DKIM/SPF
  records, plus a `DMARC p=none` record, added at Namecheap by hand — DNS is not
  delegated to Cloud DNS (see ADR 0007, decision 7).
- **`noreply@` for now.** There is no inbox to receive replies. A `Reply-To`
  pointing at a real support address is a one-line addition when one exists, with
  no re-verification.
- **Bulk Sale Confirmations are not yet safe.** Confirmations are sent one
  synchronous request per sale, inline in the import commit, and Resend's default
  rate limit is ~2 requests/second. A large Sale Import would be slow and
  rate-limited. OTP (one request) is unaffected. Batching or an async path is a
  tracked follow-up, not solved here.
