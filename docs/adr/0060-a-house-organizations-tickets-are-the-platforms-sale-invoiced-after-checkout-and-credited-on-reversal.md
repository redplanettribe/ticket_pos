# A House Organization's tickets are the platform's sale, invoiced after checkout and credited on reversal

Specified as issue #471.

Builds on [ADR 0059](./0059-the-platform-is-the-sole-issuer-and-its-signing-certificate-lives-encrypted-in-the-database.md),
which made the platform the only Issuer and issued Tax Invoices by hand for the Platform Fee alone.
Carves one exception out of [ADR 0014](./0014-platform-fee-charged-to-organization.md)'s stance — restated in `CONTEXT.md`
under Platform Fee and Tax Invoice — that tickets are the Organization's sale and their taxation stays
outside the system. Leans on `docs/research-sri-facturacion-electronica.md` for what the SRI requires.

## Context

The platform's own legal entity runs events of its own on the platform, under Organizations like any
other. Every ticket sold there is, for tax purposes, the platform's sale: the SRI obliges the emisor to
issue an electronic factura to the buyer at the moment of sale and to deliver the XML and RIDE to the
buyer's email, and since 2026-01-01 the transmission must be immediate. ADR 0059 built the Issuer, the
certificate custody and a manual factura form; nothing connects a Ticket Sale to a Tax Invoice, and an
operator typing a factura per ticket buyer does not scale past the first evening.

The SRI is also periodically unavailable, answers "within 24 hours", forbids resubmitting a document it
is still processing, offers no web service for annulment, and can never credit a "consumidor final"
factura. The platform's Reversal Window means a buyer may undo a paid Online Sale the same day.

## Decision

A Platform Operator may designate an Organization a **House Organization**: one the platform's own
entity runs. A House Organization's tickets are the platform's sale, so **every paid Online Sale of a
House Event owes a Sale Invoice** — a Tax Invoice from the platform's Issuer to the Sale's buyer for the
full amount paid — and **every Sale Reversal of such a Sale owes a Credit Note** for the whole amount.

The Sale Invoice is **owed in the same transaction that records the Sale, and issued afterwards**: an
immediate attempt right after checkout, then a Sale Invoice Drainer — the Reversal Reconciler's pattern
of a per-feature table, an internal endpoint and a scheduler — signs, submits and polls on a backoff
ladder (1, 5, 15 minutes, then hourly), parks a document `needs_attention` on a definite refusal, after
24 hours without a definite answer, or when the Issuer or its certificate is unusable, and never deletes
one. A buyer's checkout never waits on the Tax Authority, and never fails because of it.

Its Recipient is the Sale's own snapshot — "First Last", the Tax ID the Sale was transacted under, the
Sale's email, no address — and one line per Ticket Sale Line priced as the buyer paid it, with IVA
inside the price at 15% and the rate stored on the document. The Platform Fee is not a line: the platform
sold the ticket, and the fee is its own money whichever way Fee Handling went.

On reversal, an authorized Sale Invoice is credited; one never sent is withdrawn, because the Tax
Authority is told nothing about a sale that no longer stands; one still unanswered waits for its answer
first. A Sale Reversal is never refused or delayed by the state of its paperwork.

Delivery is a second mail once authorized, carrying the XML and a link to the Sale in the Customer
Area, retried independently of authorization; the Sale Confirmation of a House sale says a factura will
follow. Operators see every Sale Invoice and Credit Note in the invoicing list beside the manual Tax
Invoices, and a `needs_attention` queue on the Operator Dashboard. Organization staff surfaces are
untouched.

## Considered options

**Whose sale a House ticket is.** A platform-owned Organization (chosen); a third-party Organization the
platform invoices on its behalf. The second is Ficha Anexo 26 territory — the platform's RUC in every
factura as a billing provider, or two facturas per ticket — and an accounting arrangement counsel has to
bless. The first needs no new legal relationship: the platform invoices what it sells.

**Which sales.** Paid Online Sales only (chosen); also imported sales with a Tax ID; also free sales. An
imported sale's money never touched the platform, its sold-at date may be long past, the external
platform may already have invoiced, and a row without a Tax ID would have to be "consumidor final" —
irreversible at the SRI. A free sale has nothing to invoice, and whether the SRI accepts a zero factura
is unverified. The import gap is deliberate and visible, to be closed from the Manually Recorded Sale
form later.

**When to issue.** Owed-then-drained (chosen); in-line at checkout confirm; drainer only. In-line makes
an SRI outage a checkout outage for every House Event. Drainer-only makes every buyer wait a scheduler
tick for a document that usually authorizes in seconds. Owing in the sale's transaction is what makes
the invariant hold: nothing can be sold and forgotten.

**What the Recipient is.** The Sale's snapshot (chosen); a billing address and razón social asked at
checkout. The SRI accepts a factura to a RUC without an address, a company buyer is rare, and every
checkout field costs conversion. A Sale Re-addressing moves the addressee and not what was transacted,
so it does not rewrite a Recipient either.

**How to undo.** Credit Note (chosen); portal annulment. The SRI's annulment is a manual portal act with
a deadline, not a web service; the Credit Note flows through the same reception and authorization
services and needs nothing by hand. Because every online buyer has a Tax ID, no Sale Invoice is ever
"consumidor final", and every one can be credited.

**Whether designation needs a working Issuer.** No gate (chosen); refuse designation without a
production Issuer. The platform's compliance is the Operator's to see and fix, never the buyer's to wait
for; an Issuer certified in `test` produces test facturas for House sales, which is exactly how the
platform certifies.

## Consequences

- The signed XML of a Sale Invoice carries the buyer's name, Tax ID and email for the seven years the
  SRI requires, in a row nothing deletes. A deletion request touching a House buyer meets a legal
  retention, and goes to counsel as every deletion request does.
- A House Organization's Platform Fee, Fee IVA, Net Proceeds, Withdrawable Balance and Payouts still
  compute; they describe money that never leaves the house, and a manual fee Tax Invoice to a House
  Organization would be the platform billing itself.
- Designation affects future sales only. Nothing is issued retroactively and nothing owed is withdrawn
  when an Organization is undesignated.
- The whole feature ships behind `SALE_INVOICING_ENABLED`, closed, on ADR 0045's terms for a flag:
  closed, no Organization can be designated House, no sale owes a document, no reversal owes a Credit
  Note, the Drainer answers 404 and the operator's detail hides the designation, while manual Tax
  Invoices serve as before. It opens once the Issuer stands in `production` with a live certificate and
  the flow has been walked on the parity stack; it is also the incident switch, distinct from the
  scheduler's pause, which only paces the Drainer. A designation recorded while open survives a close
  and does nothing until the flag opens again.
- Until the RIDE exists (#456) the buyer's mail carries the XML alone; when it does, the same mail and
  the same Customer Area page carry both.
- Imported and free House sales, non-House Organizations, and the 0% RUAC rate for cultural shows are
  out of scope, each a visible gap rather than a silent one.
