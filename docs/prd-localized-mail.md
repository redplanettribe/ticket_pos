# Localized Mail

Mail to Customers is written in English or Spanish, chosen from the language the buyer actually
used, rather than always in English.

See ADR 0033 for the decision and the alternatives that were rejected. This document is the spec.

## Problem Statement

The Storefront serves two languages properly. Every page carries its language in its address
(`/{locale}/...`), a visitor picks between them in the footer, and the two catalogs in
`apps/storefront/messages/` are held in key-for-key parity by a test. A Customer can browse an
Event, check out and pay without reading a word of English.

Then we email them in English.

Nine of the ten messages the platform sends are English-only string literals in
`backend/internal/platform/email_content.go`. The tenth, the Follow Digest, is bilingual — it reads
`customers.digest_locale`, captured at sign-in, because ADR 0030 found that a Locale belongs to a
page's address and mail has no address.

That mechanism does not stretch to receipts. A `customers` row is upserted by every sale, including
box office sales and imports, so most rows belong to people who have never signed in and whose
`digest_locale` sits at its `'en'` default. A Customer reads a Spanish Event page, checks out in
Spanish as a guest, and receives an English receipt — the one email they are certain to open,
immediately after a checkout flow that spoke their language throughout. The fact that would have
answered correctly was discarded: the page they bought on knew what language it was in.

## Solution

Resolve the language of a mail at send time from three sources in order:

1. **Sale Locale** — the language of the Storefront page the sale was completed on, recorded on the
   Ticket Sale.
2. **Mail Locale** — the language of the Storefront the Customer last signed in on, remembered on
   the Customer row (today's `digest_locale`, renamed).
3. **English**, always, as the floor.

The sale outranks the memory because it is better evidence: collected at the moment of the act the
mail is about, from someone who had just read a whole page in that language. And because the sale
carries its own language, mail sent days later about that sale — a void notice raised by a Platform
Operator, from no page at all — still knows what to write in.

Six messages come into scope: Sale Confirmation, Sale Voided, Sale Reversal Refused, the Customer
One-time Passcode, and the Follow Digest (already bilingual, unchanged in behaviour). Staff mail
stays English.

## User Stories

### Buying in Spanish

A Customer browses `/es/...`, buys a ticket as a guest without ever signing in, and receives a Sale
Confirmation in Spanish. Nothing about their record, and no earlier visit, is consulted or needed.

### The sale remembers

Three days later that sale is reversed by a Platform Operator from the operator console. The Sale
Voided notice arrives in Spanish, because the sale recorded the language it was made in and the
notice reads it. No page was involved in sending it.

### Buying in English, signed in as a Spanish reader

A Customer whose remembered Mail Locale is Spanish buys on an English page. The receipt is in
English. The Sale Locale is the more recent and more specific evidence and it wins; the memory is
consulted only when the sale names no language.

### The box office

An Org Admin sells a ticket at the door. The sale records no language. If the buyer has signed in
before, the receipt follows their Mail Locale; otherwise it is English. A box office sale never
asserts that the buyer chose English.

### Asking for a passcode

A visitor on `/es/...` asks for a one-time passcode. The email arrives in Spanish. They never
complete the sign-in; their Mail Locale is unchanged, because only a completed sign-in writes it.

### Staff

A Member signs in and gets an English passcode. An organizer's payout request is approved and they
get an English notice. Neither is localized, deliberately — see ADR 0033.

### Language of record

Times stay in the Event's timezone, the Reversal Window's cutoff in Ecuador's, and money in the
Organization's currency. Spanish mail changes the words and the marks around the numbers, never the
numbers.

## Implementation Decisions

### Domain vocabulary

`CONTEXT.md` gains **Mail Locale** (replacing **Digest Locale**) and **Sale Locale**, and the final
paragraph of the **Locale** entry is amended to point at Mail Locale. Both are already written.

### The rename

`customers.digest_locale` → `customers.mail_locale`, in a new forward-only migration, with its
callers in the same change: `customerColumns` and the scan in
`backend/internal/customers/repository/session.go`, `VerifyCustomer`, `signInProvenEmail`, and the
digest's read at `backend/internal/digest/service/service.go`. `customers.digest_enabled` is not
renamed — it gates the digest and only the digest.

### The Sale Locale column

`ticket_sales.locale TEXT NULL CHECK (locale IN ('en','es'))`.

Nullable, which diverges from migration 051's `NOT NULL DEFAULT 'en'`. The column comment must carry
the reason, because the next reader will otherwise "fix" it into consistency: on `customers`, `en`
is the terminal answer of the chain; on `ticket_sales` it would be an assertion that a buyer chose
English, and it would beat the remembered Spanish it sits above.

Written by online checkout from a new optional `locale` on the checkout request body. Box office and
import write NULL. A checkout naming no language, or an unserved one, writes NULL — parsed with
`platform.ParseLocale` and ignored rather than refused, so a bad value never fails a purchase.

### Resolution

One helper, in `platform`, taking the sale's locale and the recipient's and returning the language
to write in. Every caller goes through it; nothing open-codes the chain.

### Copy

Extend the existing pattern. `digestCopy{en, es}` generalizes to a locale-agnostic copy type, and
every `Subject()` / `Text()` in `email_content.go` takes a `platform.Locale`. A missing translation
is a compile error, and `CaptureEmailSender` keeps asserting on exactly what a recipient reads.

`SaleConfirmation`, `SaleVoided` and `SaleReversalRefused` each gain a `Locale` field, set by the
caller from the helper. `FollowDigest` already has one.

The `spanishWeekdays` / `spanishMonths` maps move out of the digest's private corner into shared
use — receipts print Event dates too, and Go's stdlib has no localized calendar.

### The passcode

`SendOTP(ctx, to, code string, locale platform.Locale)`. `otpRequestBody` gains an optional
`locale`, read by the same ignore-don't-refuse rule as the verify doors. The staff caller in
`identity/service/service.go` passes `platform.DefaultLocale` explicitly at the call site.

The OTP subject and body move from `email_resend.go` into `email_content.go` — it is the only
message whose copy still lives in the provider, and that file's header says copy was extracted so
tests could assert on it.

The request-time locale words that one email and nothing else. It does not write the Mail Locale:
asking for a passcode is not proof you own the address.

### Spanish register

**Usted throughout.** The Storefront catalog already uses it; the digest's Spanish uses tú and is
rewritten to match. See ADR 0033 for why usted won.

### API surface

Two optional request fields — `locale` on the checkout body and on `otpRequestBody` — plus the
storefront passing them. `openapi/openapi.yaml` and the generated `packages/api-client` follow. No
read path takes an `Accept-Language`; ADR 0027 stands.

### Documentation

ADR 0033 (written). ADR 0030 gets a short amendment noting that the locale claim has been
generalized — it is **not** superseded, since its digest and split-sender reasoning is untouched.

## Testing Decisions

### Resolution is where the bugs are

The copy is static data the compiler checks. The chain is logic with branches and two sources, so
that is what gets the table-driven tests: sale wins over remembered, NULL falls through, unserved
values are ignored, English is the floor.

### One integration test

A Spanish online checkout, asserting the captured Sale Confirmation is Spanish. It is the only test
that proves the locale survives the whole journey — request body, checkout transaction, database
column, send time. Repeating it per message buys little; the resolution tests cover the rest more
directly and faster.

### Parity

A unit test walking every copy value asserting neither language is empty — the backend's counterpart
to `apps/storefront/lib/messages.test.ts`. Cheap, and it fails loudly for the right reason.

## Out of Scope

- **Localizing staff mail** — the staff passcode and all five payout notices stay English. Needs a
  Member locale and a localized `apps/staff` first.
- **An editable language preference in the Customer Area.** The Sale Locale removes most of the
  visible symptom. If revisited, the better answer is the language switcher writing the Mail Locale
  for a signed-in Customer, so site language and mail language stay one choice.
- **`Accept-Language` handling.** Not now, not later — ADR 0027.
- **Languages beyond English and Spanish.**
- **HTML email.** Everything stays plain text.
