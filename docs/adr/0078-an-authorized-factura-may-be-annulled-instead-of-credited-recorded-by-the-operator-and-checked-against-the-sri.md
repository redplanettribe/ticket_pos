# An authorized factura may be annulled instead of credited, recorded by the operator and checked against the SRI

Charted as wayfinder map #710 (rulings #711-#726), specified with this ADR.
Amends [ADR 0060](./0060-a-house-organizations-tickets-are-the-platforms-sale-invoiced-after-checkout-and-credited-on-reversal.md), which chose the Credit Note over the portal annulment as the way to undo a Sale Invoice,
[ADR 0061](./0061-a-wrong-recipient-is-corrected-by-reissue-a-credit-note-then-a-fresh-sale-invoice-never-an-edit.md), whose Reissue admits only an authorized factura,
and [ADR 0068](./0068-a-number-the-authority-never-held-is-abandoned-not-annulled-and-its-sale-is-issued-again.md), whose `annulled` is reached only from `pending` and `needs_attention`.
`annulled` keeps ADR 0068's meaning exactly: the authority held the document, and a Platform Operator disowned it by hand at the portal.

## Context

Mark annulled refuses an authorized factura with `INVOICE_NOT_ANNULLABLE`.
So when an operator, or the platform entity's accountant, annuls an authorized factura at the SRI en línea portal, the platform cannot record it, and its books keep declaring a document the authority has voided.
Within the SRI's window, annulling is also the cleaner undo: the buyer is left with no Credit Note to file, and the sequence holds no credit for a mistake.

The SRI offers no machine route for annulment (#711): Res. NAC-DGERCGC25-00000014 Art. 2 limits online annulment to the SRI en línea portal, and the Ficha Técnica v2.34 has no annulment service.
A factura annuls immediately, with no buyer acceptance.
The window closes on day 7 of the month after the month of emission, rolling to the next business day when day 7 is a rest day or a national or local holiday (Art. 3, as amended by 25-00000017).
Consumidor final facturas, facturas supporting a tax refund and facturas comerciales negociables are never annullable; the SRI's guidance adds facturas already declared.
No primary source rules on a factura with a Credit Note against it.

The one thing the SRI does tell a machine is `ConsultaComprobante` / `consultarEstadoAutorizacionComprobante`, whose `estadoAutorizacion` is documented as AUTORIZADO, NO AUTORIZADO, PENDIENTE DE ANULAR or ANULADO.
Probing it from production (#716) showed it, and `autorizacionComprobante` beside it, giving wrong negatives at random (about 2 in 10 for one clave), HTTP 302s and resets.
Only a positive answer is evidence.
ANULADO itself has never been observed.

## Decision

### 1. One act: Mark annulled, extended to authorized facturas

Annulment is never welded into another act, and the platform never chooses it: the portal step is a human act the platform can neither perform nor observe (#712).
Mark annulled is admitted on a **factura** (a Sale Invoice or a manual Tax Invoice) from `pending`, `needs_attention` and now `authorized`.
Guided annulment and catch-up recording are the same act: nothing stored tells "annulled after our guidance" from "annulled earlier, recorded now", because the platform could verify neither.
Platform Operator only, as today.

The gates are uniform across the three states (#713):
- **The window.** The deadline is day 7 of the month after `issued_on`, computed in `America/Guayaquil`.
  The platform claims day 7 strictly and keeps no holiday calendar, so it never relies on the SRI's rollover day.
  Past the deadline, recording is still admitted, but only with a typed acknowledgement that the annulment was done at the portal, because a rollover-day annulment is real.
- **Refusals.** A live Credit Note against the factura (owed, pending, needs_attention, authorized) refuses; a dead one (withdrawn, abandoned, annulled, rejected, not_authorized) does not.
  Ruled when #745 was built: the Credit Note refuses on an authorized factura only.
  On a pending or needs_attention factura a Credit Note can only be owed and waiting for it, since the Drainer signs one only once its factura is authorized, and annulling the factura is what withdraws it; refusing there would strand that path.
  A live Reissue successor refuses.
  `INVOICE_ABANDON_INSTEAD` stays as ADR 0068 built it.
- **Consumidor final** has no code gate, because the platform cannot issue one: identification codes 07 and 08 are absent.
  This is an invariant: whoever adds them must add the gate.
- **Tax refund and already declared** are unknowable to the platform; the confirm names them and the operator answers for it.
- **The test environment** is admitted with the same act, so the flow can be walked against celcer.
- **The confirm** is the document's number typed in full, with an optional note of at most 500 characters, as Abandon has.
  No fresh-check requirement.

Mark annulled on a **Credit Note** stays exactly as ADR 0061 and #484 built it: from `pending` and `needs_attention` only, withdrawing a Reissue's unsigned corrected factura.
Annulling an authorized Credit Note is out of scope.

### 2. The Annulment Check

The recording stands on the operator's word, and the platform then asks the SRI whether it agrees (#713, #723).
The **Annulment Check** has four states: *checking*, *confirmed*, *mismatch* and *unconfirmed*.
The word "unknown" is avoided on purpose: it collides with the Query outcome `OutcomeUnknown` ("no record"), which is exactly the false negative the probe found.

- **It asks only `ConsultaComprobante`.** `autorizacionComprobante` returns the stored authorization record and may keep saying AUTORIZADO after annulment, which would be a permanent false mismatch.
- **One answer has three readings.** ANULADO and AUTORIZADO are `estadoAutorizacion` present and reading so, matched case- and whitespace-insensitively since ANULADO's exact form was never observed.
  Everything else is *no reading*: any `RECHAZADA` refusal, HTTP 302, a non-SOAP body, a reset, a timeout, an unrecognised `estadoAutorizacion` (logged).
- **The schedule.** Asks fall due at +1, +5, +15, +30 and +60 minutes after recording, ticked by the Sale Invoice Drainer's tick.
  One ask is up to 3 calls 5 seconds apart, stopping at the first ANULADO or AUTORIZADO.
- **Verdicts.** One ANULADO confirms at once, whatever came before, and is the only answer that ends the check for good.
  A mismatch needs AUTORIZADO from at least two different asks, the later one at or after +30 minutes.
  Unconfirmed is the +60 minute ask done with neither.
- **After a mismatch or unconfirmed,** asking continues every 6 hours until the later of 7 days after recording and the window's close; after that only Check status by hand asks.
  Unconfirmed becomes a mismatch on the same rule; a mismatch never falls back to unconfirmed.
- **Check status by hand** on an annulled factura is one more ask.
  It cannot settle a mismatch before the +30 minute floor and does not move the schedule.
- **No backfill.** A factura annulled before the check existed reads as unconfirmed and is not scheduled.

A mismatch never undoes the annulment (#713).
It stands as a warning, puts the factura in the needs-attention queue, holds the buyer's notice, and refuses Issue again and Reissue on it (#719, #720).
There is no override: the only release is a check reading ANULADO.
Unconfirmed admits everything on the operator's word.
An Operator Reversal after a mismatch proceeds and owes no Credit Note, because a reversal is never delayed by its paperwork (ADR 0060) and owing a Credit Note against a document we record as annulled would contradict our own record.

### 3. What follows an annulment

Every case is an existing act composed after Mark annulled (#712):
- **Sale standing:** Issue again (ADR 0068) owes the fresh factura, unchanged.
  An annulled current Sale Invoice of a standing Sale with no live replacement joins the needs-attention queue until Issue again clears it, as an abandoned one does.
- **Wrong Recipient:** Reissue is now admitted on an **annulled** current Sale Invoice of a standing Sale, and owes only the corrected factura.
  The boundary between the acts stays sharp: Reissue corrects the Recipient and owes a Credit Note only while the document still stands at the authority; Issue again corrects nothing.
- **Reverse the Sale:** an Operator Reversal finds the factura dead and owes no Credit Note, as `SettleReversedSale` already does.
  Every reversal still credits automatically: annulment is never a mode of the reversal, and customer reversals keep crediting.
- **A reversed Sale whose Credit Note was refused and then annulled** keeps an authorized, uncredited factura (flows doc U6).
  Annulling that factura is the cure, and nothing follows.
- **A manual Tax Invoice** is typed again by hand.

### 4. What the buyer hears

Annulling a Sale Invoice the buyer was sent tells the buyer, in an **Annulment Notice** of its own, because they hold a void XML and RIDE whether or not a follow-up ever comes (#714, #719).
It carries no attachments and goes to the factura's frozen Recipient email, in the Sale's locale with the Customer's as fallback, on the delivery mail's retry ladder, at most once.
It is sent when the Annulment Check is *confirmed* or *unconfirmed*, and held while *checking* or at *mismatch*.
It is worded by the Sale's state **when it is sent**: a standing Sale is told the purchase and tickets are unchanged and a new factura will follow; a reversed Sale is told nothing follows.
A follow-up factura's "replaces" line is keyed on how its predecessor died, and an annulled predecessor gets its own line.
The Customer Area keeps an annulled factura in the list, labelled annulled at the SRI, with no downloads.
Manual Tax Invoices send nothing, as their issuing sends nothing.

### 5. Reinstate: our record follows the SRI

An annulment the SRI never took leaves a factura that really stands (#724).
A Platform Operator **reinstates** it: `annulled` moves back to `authorized`.
Admitted only while the Annulment Check stands at mismatch and the window has closed, on both kinds of factura; confirmed by the typed number with an optional note.
Never automatic, because a reinstatement may owe a Credit Note, and a legal document is not issued with nobody's name on it.

One rule decides what it owes: **a Credit Note if the Sale is reversed or the factura has a live successor; otherwise it is the Sale's current factura again.**
The Credit Note is owed in the same transaction, its reason by cause (the reversal, or the supersede).
An Issue again successor turns the verbatim pair into a Reissue's shape; if it is still unsigned it now waits for that Credit Note as a Reissue's does.
A held Annulment Notice is dropped; one already sent is followed by the Credit Note's own mail, or, where nothing follows, a reinstatement notice carrying the RIDE and XML again.
The annulment's record is kept as history, and the Annulment Check stops.

This is not the undo #713 rejected: that one was inside the open window, where it would have made Issue again and Reissue unsafe.

### 6. The Closing Sweep and the Unrecorded Annulment

Anyone holding the issuing RUC's portal credentials can annul a factura without telling the platform, the accountant included (#725).
Once a month, from day 8 of the following month in Guayaquil and retrying through day 12, the **Closing Sweep** asks `ConsultaComprobante`, paced as the Annulment Check, about every production factura the platform holds as `authorized` with `issued_on` in the month that just closed, credited ones included.
Annulment is impossible after the window closes, so one reading per factura is final, and it lands before the month's IVA declaration.
No rolling sweep, and no check tied to any act.

ANULADO raises an **Unrecorded Annulment**: a warning on the factura and an entry in the needs-attention queue.
The platform never records the annulment itself, because `annulled` is an operator's act and the follow-up is a human choice.
The operator records it with Mark annulled, whose Annulment Check then starts confirmed.
Where a live Credit Note refuses Mark annulled, the flag is only acknowledged and the record stays authorized and credited; whether that Credit Note holds is the accountant's question.
Facturas the sweep could not read by day 12 are counted on the Operator Dashboard with a way to sweep them again.

### 7. The Tax Document Archive corrects itself

An archive taken before an annulment is stale, and the next archive corrects it (#717).
The readme and the pre-download summary list every factura emitted in the range that is now `annulled` (número, clave, date annulled; no XML), and, beside them, every Unrecorded Annulment (número, clave, date found).
Defined by the period alone, so the archive of a period stays the same whoever takes it; archives are still not stored.

### 8. No dashboard nudge before the window closes

The factura's own page shows "Annullable until <date>", in a warning tone from day 1 of the following month to the deadline (#726).
Nothing reaches an operator who is not on that page.

## Considered options

- **Drive the SRI portal with a headless browser.** Refused: no stable machine route exists (#711), and a scripted login with the issuing RUC's credentials is a larger risk than the act it saves.
- **Annulment as a mode of the Sale Reversal or the Reissue.** Refused (#712): welding asserts two claims at one instant when the portal step is often done, or found out about, later; and a reversal is never delayed by its paperwork.
- **A separate "guided annulment" act beside Mark annulled.** Refused: the platform could not tell the two apart, so a field distinguishing them would be a claim nobody verified.
- **Refuse past day 7 outright, or keep a holiday table.** Refused: a rollover-day annulment is real and must be recordable; a holiday table, local holidays included, is unmaintainable for a rare day.
- **Undo an annulment inside the window.** Refused (#713): it makes `annulled` reversible exactly when Issue again and Reissue may already have acted on it.
- **Trust one answer.** Refused (#716, #723): both services return wrong negatives, so a single AUTORIZADO would hold the buyer's mail and block the Sale on noise.
- **Ask `autorizacionComprobante` too.** Refused: it returns the stored authorization record and may keep saying AUTORIZADO after annulment.
- **Credit a factura we record as annulled, or acknowledge the mismatch as terminal.** Refused (#724): the first contradicts our own record and needs an exception in every "is it dead?" check; the second leaves the SRI holding a factura our books call dead.
- **Let the Closing Sweep record the annulment itself.** Refused (#725): `annulled` carries an author, and the follow-up is a human choice.
- **Mark earlier archives superseded.** Refused (#717): it needs archives stored, and the next archive correcting the last answers the accountant without it.
- **A dashboard nudge before the window closes.** Refused on volume (#726): 0 Recipient Warnings in 219 authorized Sale Invoices, and both Reissues came from buyers writing in, when the operator is already on the page.

## Consequences

- The platform can record an annulment done at the portal, and can annul instead of crediting within the window.
  ADR 0060's choice of the Credit Note stands as the automatic path; annulment is an operator's alternative, never the platform's.
- ADR 0061's Reissue gate widens to annulled facturas, and a Reissue on one owes no Credit Note.
- ADR 0068's `annulled` is no longer terminal in every case: Reinstate leaves it, only after the window and only on a mismatch.
  `TerminallyDead` keeps answering for `annulled`, because until a Reinstate it is dead; every reader of it must tolerate a document leaving that state.
- The needs-attention queue widens again: annulled-and-unreplaced Sale Invoices of standing Sales, Annulment Check mismatches, and unacknowledged Unrecorded Annulments.
  Its documentation must state the full union.
- The flows doc's U6 and U8 close.
- Every `ConsultaComprobante` call is an attempt on the document's ledger, so the platform calls the SRI about 100-200 more times a month for the Closing Sweep, paced.
- **ANULADO has never been observed.** Its reading, and what `autorizacionComprobante` answers after an annulment, are assumptions until the first real annulment is checked with `REPEAT=10 SPACING=5 scripts/probe-sri-status.sh <clave>` (branch `worktree-sri-status-probe`).
  If ANULADO reads differently, the reading is the one place to change.
- **Revisit trigger for the nudge:** reopen §8 if Recipient Warnings start occurring, or if a Reissue (with its Credit Note) happens past the window on a factura that annulling would have spared.
- **Invariant:** if consumidor final identification (codes 07, 08) is ever added, Mark annulled must refuse those facturas.
