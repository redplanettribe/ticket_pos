# Past House sales are invoiced by an operator's backfill, dated the day of the act, never automatically

Amends [ADR 0060](./0060-a-house-organizations-tickets-are-the-platforms-sale-invoiced-after-checkout-and-credited-on-reversal.md),
whose consequence "Designation affects future sales only. Nothing is issued retroactively" this decision
narrows: nothing is issued retroactively *by the platform on its own*. Leans on
`docs/research-sri-facturacion-electronica.md` §2.8 for the transmission deadline.

## Context

The platform sold tickets to its own House Events before Sale Invoicing existed, and then before the
`SALE_INVOICING_ENABLED` flag opened. ADR 0060 owes a Sale Invoice only inside the transaction that
records a paid Online Sale, so every such sale transacted earlier — and every sale of an Organization
designated House after the fact — stands with no Tax Invoice at the SRI, though for tax purposes it was
the platform's sale. There is no operator surface listing sales at all, and no way to owe a document to
a sale that already exists.

Since 2026-01-01 the SRI requires transmission at the moment of generation and refuses a `fechaEmision`
in the past (error 65, "Fecha de emisión extemporánea"). A factura for a sale of three weeks ago can
only be dated the day it is generated.

## Decision

A Platform Operator may perform a **Sale Invoice Backfill**: from a platform-wide list of
**Uninvoiced House Sales**, select one or many and owe each a Sale Invoice, dated the day of the act.

An Uninvoiced House Sale is a paid Online Sale (`channel = online`, approved payment, amount above
zero) that is `active` with no Reversal Request in flight or parked, has **no** Sale Invoice row of any
status, and belongs to an Organization that is House **now**. When it was designated is irrelevant;
an Organization no longer House contributes nothing. Reversed sales, free sales, imported and Manually
Recorded sales, and sales whose Sale Invoice was annulled or withdrawn are not candidates.

The act owes the document through the same builder checkout uses (`OwePaidOnlineSale`), **one
transaction per sale**, locking the sale row so a second request finds it already invoiced; a sale the
builder refuses (lines not matching the payment) is reported on its own and the rest proceed. The
document records the operator who owed it and when. Then the Sale Invoice Drainer is kicked for the
owed sales and everything downstream is unchanged: signing, submission, polling, parking, the buyer's
delivery mail with XML and RIDE, the Credit Note on reversal, the Recipient Warning and the Reissue.

There is **no age cutoff**: the list shows the sale's own date beside every row, oldest first, and
which sales to declare late is the operator's — and their accountant's — call. Before the act a
confirmation states the count, the total, that the documents will be dated today, and that each buyer
will be mailed. The buyer mail is the standard delivery mail; no "late factura" variant.

The surface is `/operator/invoicing/uninvoiced` with a count beside the invoicing list's other counts;
Organization staff surfaces are untouched. The endpoint and page live under `SALE_INVOICING_ENABLED`
exactly as the Drainer does — closed, the page is gone and the act answers `SALE_INVOICING_UNAVAILABLE`.

## Considered options

**Whether to issue at all.** Operator-selected backfill (chosen); never (ADR 0060 as written); the
platform owing every candidate automatically once the flag opens. Automatic issuance would declare weeks
of income on one scheduler tick, mail every past buyer at once, and remove the accountant's judgement
about which period to declare in. Never leaves the platform's own sales undeclared with no remedy.

**Which date.** The day of the act (chosen); the sale's date. The SRI refuses backdating; every
backdated document would park `needs_attention` having consumed a sequence number.

**Which sales.** Paid Online Sales of Organizations House now (chosen); only sales after
`house_designated_at`; also imported and Manually Recorded sales. The designation date is bookkeeping,
not the tax fact. Imported sales may already be invoiced elsewhere, their money never touched the
platform, and a row without a Tax ID would be consumidor final — irreversible; that gap stays visible
and separate. Annulled documents are #480's dead end, not a missing invoice.

**Transaction shape.** Per sale (chosen); all-or-nothing. A refused sale cannot be fixed from this
screen, so one bad row would block every good one; per-sale also makes a repeated request harmless.

**Where.** A platform-wide invoicing page (chosen); a per-Organization list; a filter on the invoicing
list. The operator sweeps one backlog; the invoicing list is a list of documents and these sales have
none.

**Flag.** The existing flag (chosen); a second flag. Same feature, same incident switch.

## Consequences

- ADR 0060's consequence now reads: designation affects future sales automatically, and past sales
  only through a Sale Invoice Backfill; undesignation still withdraws nothing.
- A backfilled Sale Invoice's `issued_on` is later than its Ticket Sale's `sold_at`, sometimes by
  months. Every list that shows both must show both.
- Buyers receive a factura mail long after a Sale Confirmation that promised none. Accepted.
- The "one Sale Invoice per Ticket Sale" invariant gains a second call site; it is still control flow
  and a row lock, not schema (flows doc U3), because the reissue keeps two authorized documents per sale.
- The first operator surface listing Ticket Sales exists, read-only and House-scoped.
