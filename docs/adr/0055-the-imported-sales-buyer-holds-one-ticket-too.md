# The imported Sale's buyer holds one Ticket too

**Supersedes [ADR 0048](./0048-the-buyer-holds-one-ticket-by-paying.md)'s "Online Sales only" clause,
and nothing else of it.** The Ticket that is chosen, the state it is written in, what it does to the
buyer's surfaces and the fact that it makes nobody Verified are all 0048's and are unchanged here.

Amends [ADR 0050](./0050-an-imported-sale-is-corrected-by-replacement-never-edited.md)'s
"the replacement's Tickets start `unassigned`" and widens
[ADR 0051](./0051-the-buyer-is-reminded-to-assign-and-the-first-sweep-is-the-announcement.md)'s
audience past `online`. Stays inside
[ADR 0047](./0047-the-organization-sees-the-holders-address.md) and
[ADR 0049](./0049-only-the-holder-answers-and-the-answer-link-is-retired.md), which are untouched.

## Context

ADR 0048 gave the buyer of an Online Sale one Ticket of their own and backfilled every Sale that
predated it. It excluded the `import` and `in_person` channels in a single clause: *"A door sale's or
Sale Import's buyer is a name somebody else typed, and often is not attending."*

On the one Event where this matters, that exclusion leaves **184 of 190 imported Tickets held by
nobody**. The Holder List — the Organization's answer to "who is coming" — renders each as nobody
named, so the Organizer cannot print a roster, cannot check anyone in against it, and cannot tell an
attendee from a gap. Every one of those 184 is a real person with a name and an address already
recorded on their Ticket Sale.

The cost is not only what is displayed. ADR 0049 made an Answer the Holder's to give, so a Ticket
nobody holds is **structurally incapable of accruing one**: nobody but Event Staff can ever answer
for it, and ADR 0051's reminder chases nobody about it, because there is nobody to chase. The
roster's silence is therefore permanent rather than merely current.

The premise behind 0048's clause is what fails. A **Sale Import transcribes a transaction that
already happened** — the file's email column is the person who bought, from a system where they
bought it themselves. Treating that address as a clerk's guess is what produced the hole. The clause
is right about a door sale and wrong about an import, and it was written as one sentence covering
both.

## Decision

**One Ticket of every imported Ticket Sale is a Self-held Ticket, exactly as it is on an Online
Sale.** It is assigned to the buyer and `accepted` in the transaction that records the Sale, on all
three routes onto the channel: a file Sale Import, a Manually Recorded Sale, and a Sale Correction's
replacement. The Ticket is chosen by ADR 0048's rule unchanged — the lowest-ordinal Ticket of the
line whose Ticket Type sorts first — which on a quantity of one degenerates to "the Ticket". **The
forward rule and 0048's are the same sentence**, which is what makes this an amendment and not a
second rule to keep in step.

**The grounds for `accepted` are the transcription, not a proof.** A Sale Import records a real prior
transaction, a Member is accountable for the file, and two correction paths exist. This is a weaker
warrant than 0048's payment and far weaker than 0046's click, and it is stated plainly rather than
dressed up: the platform is **presuming** the buyer attends. It makes nobody Verified — `verified_at`
is untouched and the sign-in module keeps that authority — and it grants no consent.

**A fourth assignment state was rejected again.** "Presumed attendee, unproven" would give the roster
a value meaning "we think so", and ADR 0048 already refused a fourth state on the grounds that *"the
roster's question is who is coming, not how they came to hold the Ticket."* Reversing that for a
weaker case would be backwards.

**`in_person` stays out — pending a buyer surface, not on principle.** This is a different statement
from 0048's and the reason is correctability, not entitlement. Ticket Assignment refuses the channel
outright because a door sale has no buyer page to assign from, and **no staff-side reassignment route
exists at all**: the only two writers of a Holder are the buyer's own Customer Session and the
Assignment Link click. A Holder written onto a door sale could therefore be removed by nobody, and a
presumption with no undo is not one this platform should make. When a POS exists, the rule follows
the buyer surface onto the channel.

**No opt-out, on either route.** No column on the import template and no checkbox on the Manually
Recorded Sale form. The remedy for a wrong presumption is the same one an online buyer has —
reassignment — plus Sale Correction, which staff have and buyers do not.

**Existing imported Sales are backfilled**, on the same terms as 0048's: the Sale's own timestamp on
both holder columns so the row is indistinguishable from one the flow would have written, no mail, no
assignment-mail ledger row, no reminder, `verified_at` untouched, idempotent by construction, and
ungated by the feature flag — a backfill is a one-shot act, and a gated one running while the flag
was closed would mean the data never existed at all. It skips a reversed Sale, a Sale whose Ticket 1
is already assigned, and a Sale where the buyer already holds a Ticket.

**It skips a *live* reversal only, which narrows 0048's backfill.** Migration 084 disqualified any
Sale carrying a Reversal Request *"whatever became of it"*, so a buyer who asked for their money back,
was **refused**, and is still coming would hold nothing. That is the wrong answer and it is corrected
here rather than inherited. The predicate is the schema's own — the partial unique index on
`sale_reversals` already defines a live reversal as one whose status is not `refused`.

**The Assignment Reminder widens to `import`.** Its rationing is untouched: more than one Ticket, one
still unassigned, a 24-hour floor, a 7-day cooldown, a lifetime cap, silence once the Event has
started, and per-sweep pacing. This is a deliberate exception to ADR 0050's "an imported buyer is
mailed nothing by default", which was about transactional mail the buyer did not ask about; asking
somebody holding Tickets to name who is coming is the one thing only they can do, and it is the only
way a roster hole is filled truthfully rather than by presumption.

**The No Longer Holding notice follows the buyer's notification policy for a self-held import
Holder.** #327 made that mail unconditional — deliberately not gated by the Sale Import undo's notify
toggle — and gave a reason: *"A Holder is not in that position: they came here, proved their address,
accepted a ticket."* **This ADR makes that premise false.** A presumed Holder did none of those
things, and left alone the change would turn a batch undo into 184 mails saying "you no longer hold a
ticket", bypassing the very toggle built to stop an import writing to buyers — and would make every
Sale Correction tell a buyer they had lost a ticket that the replacement re-seats them on in the same
act. So for an import Sale's *self-held* Holder the notice follows the buyer's policy (the undo's
toggle, the correction's checkbox), and for a Holder who **accepted by Assignment Link** it stays
unconditional. This restores #327's rule rather than carving an exception out of it: the warrant was
"this person came here and accepted", and where a self-held import Holder is concerned that person
*is* the buyer — precisely who the toggle exists to protect.

## Considered options

- *A one-off data fix for this import batch, leaving the channel rule alone.* Recommended at first,
  declined: it treats a fact about how import files are produced as a fact about one file, and the
  next import would recreate the hole.
- *An optional `holder_email` column on the import template.* Rejected, and it is worse than it
  looks: a holder differing from the buyer must be written `assigned`, and ADR 0047 shows no name for
  an unaccepted assignment — staff would fill in an attendee and watch the roster refuse to name
  them. The right version of this feature mails an Assignment Link, and is its own decision.
- *Extending to `in_person` in the same act.* Rejected above: no buyer surface, no reassignment route,
  no rows, and therefore no undo.
- *Writing the backfilled Tickets `assigned` rather than `accepted`, as the honest weaker claim.*
  Rejected because it does not solve the problem: ADR 0047 discloses nothing before acceptance, so
  the roster would trade "nobody named yet" for "assigned" and still print no name.
- *Suppressing the No Longer Holding notice on the whole `import` channel.* Rejected: it would
  silence a Holder who genuinely clicked an Assignment Link and proved their address, which is the
  case #327 was written for.

## Consequences

- The affected Event's roster goes from **81% to 97% named** — 1,106 Tickets naming a person, up from
  922. The existing Sales Export, whose per-Ticket sheet already carries assignment state, Holder name
  and Holder address, becomes a printable check-in roster with no new surface.
- **The residual 3% stays honestly unnamed** and is not in scope: units 2+ of Online Sales nobody
  assigned, and addresses typed but never accepted, which ADR 0047 forbids naming. The roster keeps
  distinguishing "nobody named" from "named and never claimed", which are different facts to an
  Organizer deciding whether to chase.
- **The backfill sends no mail.** Every path was checked: the Answer Reminder needs an outstanding
  debt and the backfilled Tickets are all of a Ticket Type asking no questions; the Assignment
  Reminder needs more than one Ticket and every affected Sale is quantity 1; the Assignment Link mail
  is written only by a buyer's own act; No Longer Holding fires only on a reversal.
- **Adding a required Ticket Question to a Ticket Type carrying backfilled Holders chases every one
  of them.** On the affected Event that is a 184-recipient action performed by editing a Ticket Type,
  on a live and enabled sweep. This is correct behaviour and is recorded here so that nobody discovers
  it by doing it.
- ADR 0050's *"the replacement's Tickets all start `unassigned`"* is no longer true of Ticket 1. A
  Sale Correction now re-seats the buyer on the roster instead of dropping them off it, which is the
  better behaviour and was not available when 0050 was written.
- The Assignment Reminder's widening **fires on nothing today**: every active imported Sale is
  quantity 1 and becomes fully held. It ships dark and proves itself on the next multi-Ticket import.
- Nothing about a figure moves. Tickets Sold, Takings, capacity, Purchase Limit and Sales Trends all
  read off Ticket Sale Line quantities, and a Ticket Assignment has never changed one.
- The `import` channel now behaves like `online` in every respect that touches the roster, which
  leaves `in_person` as the only channel with no buyer surface — and the only remaining reader of the
  clause this ADR supersedes.
