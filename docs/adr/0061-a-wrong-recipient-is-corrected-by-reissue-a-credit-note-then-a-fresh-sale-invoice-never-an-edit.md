# A wrong Recipient is corrected by reissue: a Credit Note, then a fresh Sale Invoice, never an edit

> Amended by [ADR 0068](./0068-a-number-the-authority-never-held-is-abandoned-not-annulled-and-its-sale-is-issued-again.md)
> (#575, built as #578): the line below that "Mark annulled is for documents the authority refused" is
> narrowed. Mark annulled is for a document the authority **held**, which a Platform Operator then
> disowned by hand at its portal — an obligation discharged there, and a declaration as anulado. A
> document the authority refused *by number* (SRI 45, secuencial registrado) it never held, so there is
> nothing at the portal to annul; that document is **abandoned**, a terminal state of its own, and Mark
> annulled is refused on it. The rest of this decision stands, and Issue again reuses its supersede
> chain; ADR 0068 also widens the live-successor rule below to exclude every terminal-dead status, not
> only `withdrawn`.

Specified as issue #478. Builds on [ADR 0060](./0060-a-house-organizations-tickets-are-the-platforms-sale-invoiced-after-checkout-and-credited-on-reversal.md),
which made every paid Online Sale of a House Event owe a Sale Invoice to the Sale's buyer as transacted,
and on [ADR 0059](./0059-the-platform-is-the-sole-issuer-and-its-signing-certificate-lives-encrypted-in-the-database.md),
which made every issued document a legal artifact with a consumed sequence number that nothing deletes.

## Context

`ValidateTaxID` catches a cédula or RUC that fails its check digit and nothing else. Somebody else's
number, a transposition that still checks, a never-issued number and any passport string pass checkout,
are snapshotted on the Sale, copied into the Sale Invoice's Recipient and submitted. The SRI answers a
non-existent or incorrect identification with *advertencias* only (59, 62) and authorizes the factura,
so the platform has declared income to the wrong taxpayer, the buyer holds an XML in the wrong name,
and nothing in the system can say otherwise: the Recipient is fixed once issued, Mark annulled is for
documents the authority refused, and the only path to a Credit Note was a Sale Reversal inside the
Reversal Window — reverse and buy again, which is no path once the window closes or the Payment
Provider declines, and forces a buyer whose tickets are fine to pay twice.

## Decision

A Platform Operator corrects an authorized Sale Invoice's Recipient by a **Sale Invoice Reissue**: in one
transaction the platform owes a **Credit Note for the full amount against the authorized factura, and a
fresh Sale Invoice to the corrected Recipient** — Tax ID Type, number and legal name as entered, an
address if given, the email the Ticket Sale carries at that moment — with the original lines. The Sale,
its money, its Tickets, its Reversal Window, its stored buyer and the Customer's stored Tax ID are
untouched. The reissued factura is **superseded**: still authorized, still on file, still the buyer's,
but no longer the Sale's current Sale Invoice. Old factura, Credit Note and new factura point at each
other; a later Sale Reversal credits the current factura alone; a second reissue corrects the current
one, so a chain has no fixed length.

The Drainer issues the new factura **only once the Credit Note is authorized**. A Sale has one
authorized current factura at a time: if the Credit Note dies — refused, then marked annulled at the
portal — the new factura is withdrawn unsigned and the old factura stands, and the operator may
reissue again. A Sale Reversal during a reissue is neither refused nor delayed; it credits whichever
factura is current and withdraws one not yet sent, exactly as ADR 0060 has it.

Reissue is allowed on a current, authorized Sale Invoice of a Sale that still stands, one reissue at a
time per Sale; refused on manual Tax Invoices, Credit Notes, superseded facturas, reversed Sales and
`needs_attention` documents, whose Resend and Mark annulled paths are unchanged. It is **operator-only**:
the buyer writes in. The Credit Note states a fixed motivo — *"Corrección de los datos del receptor"* —
and carries `reissue` as its reason beside the five reversal routes; the operator may leave an optional
note, bounded as an Operator Reversal's is, shown on the operator surfaces and never mailed.

The authority's advertencias 59 and 62 on an authorized document become a **Recipient Warning**: a marker
the invoicing list filters on and the Operator Dashboard counts beside the `needs_attention` queue,
cleared when the document is credited — by the reissue's Credit Note, or a reversal's. The status stays `authorized` — the document is valid and the
Drainer settled it. A Tax ID that exists and belongs to somebody else raises nothing; only the buyer can
notice that one.

The buyer receives **both documents by mail**, the Credit Note with copy that says the earlier factura is
cancelled for a correction and a corrected one follows — never that the sale was reversed — and the
Customer Area shows the whole chain, current factura first, the superseded one still downloadable and
labelled.

## Considered options

- **Edit the Recipient on the authorized document.** The signed XML is the artifact; rewriting the row
  makes the platform's record disagree with what the SRI holds and the buyer received.
- **Annul at the SRI portal, then issue anew.** A manual portal act with a deadline and no web
  service; the Credit Note flows through the same reception and authorization services as the factura
  and needs nothing by hand. Mark annulled stays what it was: the record of a portal annulment of a
  document the authority never authorized.
- **Reverse and buy again.** Exists already; dies with the Reversal Window and the Payment Provider,
  and charges the buyer twice for a mistake in a form field.
- **Validate the Tax ID against the SRI at checkout.** No official web service exists; the unofficial
  lookup is rate-limited, would make checkout depend on the SRI as ADR 0060 refused to, and cannot catch
  a valid number that is somebody else's. May be added later as prevention; it does not replace the
  cure.
- **Drain the Credit Note and the new factura independently.** Faster for the buyer, but a refused
  Credit Note leaves two authorized facturas for one Sale — double-declared income with no automatic way
  out. The gate costs the buyer at most the SRI's own 24 hours.
- **Park a Recipient Warning `needs_attention`.** Contradicts what `needs_attention` means — a document
  the Drainer could not settle — and would block the Credit Note, which credits only `authorized`.
- **Let the Customer request the correction from the Customer Area.** A self-service way to move
  declared income between taxpayers; refused for now, the buyer writes in.

## Consequences

- Every reissue consumes two more sequence numbers and adds two rows nothing deletes; a superseded
  factura keeps the wrong Tax ID in its signed XML for the seven years the SRI requires, as any issued
  document does.
- A corrected factura the SRI refuses outright and the operator then annuls leaves the Sale with no
  current factura — the same terminal state an annulled first factura leaves today (#477). Left as a
  visible gap; "issue a Sale Invoice again after annulment" is #480, not this one.
- The Credit Note's reason is no longer a reversal route by definition; every reader of it — the
  motivo, the delivery copy, the operator surfaces — keys on the reason, not on the existence of a
  reversal.
- The Recipient Warning is derived from the authority's messages the Drainer already stores, so
  documents authorized before it existed can be marked from their attempts ledger.
- Ships behind `SALE_INVOICING_ENABLED` with the rest of ADR 0060.
