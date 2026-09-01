# A number the authority never held is abandoned, not annulled, and its Sale is issued again

Specified as issue #575, from the triage of #574. Builds on
[ADR 0059](./0059-the-platform-is-the-sole-issuer-and-its-signing-certificate-lives-encrypted-in-the-database.md),
which made every issued document a legal artifact with a consumed sequence number that nothing deletes,
on [ADR 0060](./0060-a-house-organizations-tickets-are-the-platforms-sale-invoiced-after-checkout-and-credited-on-reversal.md),
which made every paid Online Sale of a House Event owe a Sale Invoice drained by the Drainer, and on
[ADR 0061](./0061-a-wrong-recipient-is-corrected-by-reissue-a-credit-note-then-a-fresh-sale-invoice-never-an-edit.md),
whose supersede chain this reuses and whose account of Mark annulled this amends. Absorbs #480.

## Context

On 2026-08-30 two production facturas, 001-001-000000025 and 26, were refused by recepción with
`45 ERROR SECUENCIAL REGISTRADO`, on their first Submit and again on an operator Resend two days later.
The operator checked the SRI portal: neither number is registered there. The authority is refusing a
number it does not hold, and the refusal is stable over days. Why it does so is #573's question, not
this one.

The platform has no answer. Resend re-signs and resubmits under the same clave de acceso and secuencial
— correct for every other refusal, and required by S1 §5.10 — which is exactly what error 45 complains
about, so each Resend can only earn another rejection. Both invoice tables are append-only: nothing
issued is deleted and there is no down path for an issued document. The remedy cannot be to repair the
row; it has to be a recorded act.

The vocabulary we have does not fit the act. ADR 0061 said in passing that "Mark annulled is for
documents the authority refused", and `annullable` admits any signed, parked document — so an operator
may press Mark annulled on 25 today. But that action's contract is to record an annulment the operator
performed *by hand at the SRI portal*, and for a number the portal does not show there is nothing to
annul. The only escape the platform offers is a true-looking record of an act that never happened.

Meanwhile a Sale whose factura is annulled has been a dead end since #477: nothing owes it another one.
#480 was raised for that, closed `completed` on 2026-08-31, and never built.

## Decision

A document's death has **three** terminal states, distinguished by what a reader may conclude about the
authority, not merely by why the state was entered:

- **`withdrawn`** — never sent to the authority, and never will be. Its Sale was reversed, or the
  document it depended on died first.
- **`abandoned`** — sent, the authority never took it, and never will. **It was never a legal
  document.** Nothing is owed at the portal and nothing is ever declared for it.
- **`annulled`** — the authority held it, and a Platform Operator disowned it by hand at the portal.
  An obligation was discharged there, and a declaration as anulado follows.

A Platform Operator **abandons** a document the authority refuses by number. The act is gated to the
SRI's secuencial-registrado refusal alone, on a document of any kind, and **requires a fresh Check
status immediately beforehand**, so that the decision rests on the authority's own current answer and
the attempts ledger carries that "nothing known", timestamped, immediately before the act. The
abandoned document keeps its number, clave, signed bytes and every attempt forever. **The secuencial
stays consumed**: the sequence only moves forward, and an abandoned number is never handed out again.
Resend is refused on a document in this state, with its reason; Mark annulled is refused on a document
that qualifies for Abandon, which narrows an existing action deliberately.

A Platform Operator **issues again** a Ticket Sale whose Sale Invoice is terminally dead — `abandoned`
or `annulled` — where the Sale still stands and has no live replacement. It owes a fresh Sale Invoice
with the original lines, amounts and Recipient, linked to the document it replaces through ADR 0061's
supersede chain, and the Drainer signs it in a later round under a freshly allocated secuencial by the
ordinary owed path. **Reissue is not a signing route**: it re-owes, and the queue does its work.
Issue again is refused on a manual Tax Invoice, which is typed again by hand as it always was, and on
a Credit Note.

The two are **separate acts**, never welded: abandoning without reissuing is legal — a manual document,
or a Sale the operator does not want reinvoiced — and the two claims may honestly be made days apart.

Because abandoning removes a document from `needs_attention`, the needs-attention queue and its badge
widen to mean what their name promises: documents parked `needs_attention`, **or** abandoned documents
whose Sale still stands and has no live replacement. The entry clears itself when Issue again owes one.

Whether a document parked for any *other* reason may be abandoned is **deliberately not decided**, until
a second case needs it.

## Considered options

- **Reuse `annulled` with a reason column.** One column instead of a status, no enum churn. Refused:
  the two states differ in what a reader may conclude, so every future consumer would have to remember
  to read the discriminator, and the first that forgets treats a document that never existed as one
  requiring portal action. The annulled refusal copy would have to branch on the reason anyway — this
  option is the new status with extra steps. A status that must be qualified to be understood is two
  statuses wearing one name.
- **One compound "abandon and reissue" press.** Matches the operator's usual intent and leaves no gap
  state. Refused: abandon-without-reissue must remain legal, and welding makes the ledger assert both
  claims at one instant when the reissue is often a later decision.
- **Two separate acts, abandon-and-reissue beside annul-and-issue-again.** What #574 proposed, leaving
  #480 unbuilt. Refused: the re-owe half is identical, and building it twice means two paths that must
  both get the live-successor rule, the queue and the delivery exclusion right.
- **A stored boolean for the refusal code**, as the Recipient Warning has. Refused: that flag earns its
  column because the list filters and the dashboard counts it across rows; this is a single-row question
  on one detail page. Deriving it from the messages already stored means the two production documents
  become abandonable on deploy with no backfill, and a correction to the detection re-answers every
  historical row at once.
- **A parallel `replaced_by` link beside ADR 0061's supersede chain.** Refused: "what became of this
  document" is one question, and two chains would have to be zipped together by every reader.
- **Re-owe a manual Tax Invoice too.** Refused: it would drag manual documents into the owed → Drainer
  → delivery path they are kept out of in three separate places, to fix a case whose answer is to type
  it again.
- **A new "Sales with no current factura" queue and dashboard tile**, #480's open ruling. Refused: two
  badges both meaning "unfinished invoicing work" is how an operator learns to ignore one. Widening the
  queue that already exists costs one query.
- **Keep polling an abandoned clave** and alarm if it ever authorizes. Refused: a permanent background
  obligation to detect an event for which there is no remedy.
- **Let the Drainer abandon automatically** after N refusals. Refused: abandoning declares a document
  was never legal, which is a human act with a human author.

## Consequences

- The platform can, for the first time, reach a Sale whose factura is terminally dead. #480's dead end
  closes as a consequence of this rather than as a project of its own.
- Mark annulled loses ground it should never have held. An operator who has been reaching for it on a
  refused-by-number document is told to abandon instead. ADR 0061's line that "Mark annulled is for
  documents the authority refused" is amended: it is for documents the authority *held* and the
  operator disowned.
- ADR 0061's live-successor rule changes for every reissue, not only these: the index excluded only
  `withdrawn`, so a dead successor permanently blocked its Sale — the defect recorded on #480. It now
  excludes every terminal-dead status. This is a fix, and it changes behaviour built before it.
- A sequence acquires visible gaps: numbers issued, refused, abandoned and never declared. The SRI's own
  guidance is that a document which can never be authorized is annulled rather than reused, so the gap
  is the correct shape; but the platform must be able to account for each one, and the abandonment's
  author, instant, note and preceding Check are that account.
- The needs-attention queue is no longer a status equality. Its documentation must state the union, or
  the next reader will "simplify" it back.
- An authorization landing between the mandatory Check and the Abandon is still possible. The existing
  guard — an outcome applies only to the status the row was read in — keeps the data consistent, leaving
  a reconcilable ledger rather than a corrupt row. The window is the one Mark annulled already accepts.
- Abandoning is available on manual Tax Invoices, so a hand-typed document refused by number stops
  showing a futile Resend; but its replacement is the operator's to type, and nothing tracks that it is
  owed.
- Nothing here addresses why the authority refused a number it does not hold. If #573's pacing fix is
  not deployed first, a reissued document is submitted in the same unpaced burst and may earn error 45
  in its turn, burning another number.
