# A Ticket Sale is recorded by hand as one batchless import row

## Context

An Organization has exactly one way to get an off-platform sale into the platform: download the Sale
Import template, fill in a spreadsheet, upload it, read the preview, commit a batch. That is the
right tool for a season's history or an External Platform's export. For one cash payment taken over
the phone it is absurd, and the friction is not free — a sale that is annoying to record gets
recorded late, in a lump, or never, and every unrecorded sale is a seat the platform still believes
is for sale.

The obvious shape is a form. What is not obvious is what the form produces. The product already
holds two different ideas that both look like "an Organizer types a sale into a modal", and they are
different domain objects with different consequences:

- A **Direct Sale** — the Organization's own off-platform cash or transfer sale — lives on the
  `import` Sales Channel with Sales Source `direct`. It has a buyer surface: a Sale Confirmation, a
  Confirmation Link, Ticket Assignment, Answers. It can be reversed on its own and corrected by
  replacement.
- An **In-Person Sale** lives on the `in_person` channel: a live sale at a physical POS. It requires
  a Tax ID (ADR 0016), has no buyer surface to assign or answer from, is refused by Ticket
  Assignment outright, and has no Sale Correction route. It is roadmap section E, unstarted, and it
  is the slice the product is named after.

Choosing wrongly here is expensive in both directions. Building the form on `in_person` would ship
the POS slice by accident, without its card capture, its tablet-first surface or its receipt, and
would hand organizers a sale their buyers cannot act on. Building it on `import` and calling it a
POS would mean the real POS, when it lands, finds its name taken.

There is a second question underneath. A Sale Import is a **batch**, and the batch is load-bearing:
it is the unit of idempotency, the row in the Import history, and the thing batch undo undoes —
latest-batch-only, a guard that exists so an undo cannot reach behind a later import. ADR 0050 has
already established that a batchless imported sale is a coherent object: a Sale Correction's
replacement belongs to no batch precisely so a later batch undo cannot sweep it away.

## Decision

**A Manually Recorded Sale is one Sale Import row, typed instead of uploaded: the `import` Sales
Channel, Sales Source `direct`, one Ticket Type, and no batch.** It is held to every rule an import
row is held to, and it is not a Sale Import — it never appears in the Import history and no batch
undo can reach it.

Four rulings follow, each of which departs from one of the two paths it is otherwise identical to.

**It is committed one sale at a time, as you go.** The form carries a Keep adding toggle: with it
on, a save records the sale and hands back an empty form for the next name. Each save is a complete,
final sale before the next is typed. There is no session, no draft and no accumulated list — those
would be a batch under another name, and would need all-or-nothing semantics, an abandonment story
and an answer to what the eleventh row failing does to the ten good ones.

**It always mails the buyer their Sale Confirmation, with no toggle**, as a Sale Import does and
unlike a Sale Correction. ADR 0050 made the correction's Confirmation off-by-default because the
buyer already holds one from the original import and a reverse-then-reissue pair would confuse. No
prior mail exists here. Suppressing it would ship a sale whose buyer holds no Confirmation Link and
therefore cannot assign a Ticket or answer a Ticket Question.

**It enforces the Purchase Limit**, as the file commit and the correction do and unlike the JSON
commit. The JSON commit's exemption is documented and its stated reason is that the route "carries
no per-row complaint channel to report a refusal through". This form has one — a preview endpoint
that blames a named field — so the reason does not transfer, and shipping the exemption would make
the manual form the one way to quietly punch through a limit an Organization set for itself.

**Its origin is derived, never stored.** A Manually Recorded Sale is
`channel = 'import' AND import_batch_id IS NULL AND replaces_sale_id IS NULL`. Nothing is written to
say so. This follows the platform's existing habit in this exact area — the Sales Export already
tells a batch undo from a single-sale reversal by deriving it from `undone_at` matching `reversed_at`
rather than storing a route — and a derived value cannot drift out of agreement with the facts it
describes, which for an immutable Ticket Sale is the whole game.

Everything else is inherited without variation: the import row's validation, capacity refused at
preview time on the quantity (ADR 0050's departure from the file import, which defers capacity to
batch commit), the non-blocking duplicate warning, an optional Tax ID (ADR 0016), a blank amount
meaning the Ticket Type's catalog price, Tickets minted one per unit and all `unassigned` with none
self-held (ADRs 0043, 0048), no Answers, no consent record, an unverified Customer, a NULL Sale
Locale so mail falls to the Customer's remembered language (ADR 0033), zero fee snapshots so the
sale contributes its full price to Takings (ADR 0040) and nothing to Net Proceeds (ADR 0032), and an
external-registration Event refused before the body is judged (ADR 0028). It needs no migration:
every column it writes already exists.

## Considered options

- **Build it on the `in_person` channel — the honest name.** The Organizer is, after all, recording a
  sale by hand, and the product is a POS. Rejected because the channel is not about who typed the
  sale, it is about what the sale is: `in_person` means the buyer was standing there, which is why it
  demands a Tax ID and offers no buyer surface. A phone sale recorded on Tuesday is not that. The
  cost of getting this wrong is not cosmetic — an `in_person` sale is refused by Ticket Assignment
  and has no Sale Correction route, so the form would produce sales that cannot be fixed and buyers
  who cannot act.
- **Mint a one-row Sale Import batch per typed sale.** Uniform, needs no new object, and gives the
  Import history a complete account. Rejected on a consequence that only shows up later: batch undo
  is latest-only. Typing one sale by hand would silently make yesterday's spreadsheet import no
  longer the latest, and block undoing it. A feature that quietly disarms an unrelated safety valve
  is worse than one that needs a paragraph of explanation.
- **One batch per Keep adding session.** The appealing version of the same idea, and it buys a real
  thing the chosen design lacks: undo the whole sitting. Rejected because a session has no clean
  moment to close — the batch would be open while the organizer types, and a closed tab, a lost
  connection or a lunch break each need an answer — and because it inherits the latest-only
  interference above for as long as it stays open.
- **Accumulate rows in the modal and commit once at the end.** All-or-nothing, and it reuses the
  existing array-shaped commit exactly. Rejected as the same batch wearing different clothes, plus a
  worse failure mode: the twentieth row failing on capacity throws away nineteen correct ones, or
  does not, and either answer is a surprise.
- **Store a marker column saying the sale was typed.** A positive fact beats a triple negative, and
  it would survive a future batchless writer. Rejected for now because it is a migration on
  `ticket_sales` to record something the row already states, and because the derived predicate cannot
  disagree with reality while a flag can. This is the option to revisit first if the negative starts
  to hurt — see the consequence below.
- **Allow several Ticket Types in one sale.** Genuinely better for the family buying two Adult and
  two Child in one cash transaction, which becomes two records with two Confirmation references and
  two mails to the same person. Rejected because no `import`-channel sale has ever carried more than
  one Ticket Sale Line, and this form would become the sole producer of a shape the amount override,
  the correction path and the export have never seen. The cost is acknowledged rather than denied.
- **Reuse the Sale Import commit endpoint with a no-batch flag.** Rejected because that route returns
  a batch id, so the flag would make a documented response field conditionally meaningless, and
  because the route names a resource this act does not create.

## Consequences

**There is no undo for a sitting.** With commit-as-you-go, sale seven is recorded and mailed the
moment it is saved. An Organizer who notices the mistake at sale twenty fixes that one row with a
Sale Correction, exactly as they would for any imported sale. This is the price of refusing the
batch and it is the first thing anyone will ask for. The right answer, if it ever hurts enough, is
multi-select on the Sales list — not a retroactive batch.

**The batchless predicate is a negative, and negatives accumulate silently.** Two writers now produce
an imported sale with no batch: a Sale Correction's replacement, and this. They are told apart only
because the replacement carries `replaces_sale_id`. A third batchless writer will be classified as
"manually recorded" by default, without a line of code changing and without a test failing. Anyone
adding one must either give it a positive marker or accept the label deliberately. The predicate is
single-sourced so there is exactly one place to look.

**A duplicate is possible and is not prevented.** The create carries no idempotency key — there is no
batch to hang one on, and Sale Correction, the closest precedent, has none either. A request that
times out *after* its write succeeds shows the Organizer an error for a sale that exists; retyping it
records it twice and mails the buyer twice. The mitigations are that the in-flight button is
disabled, the session receipt shows what was recorded, and the duplicate warning flags it on the very
next record. Accepted, with the note that this is the same hole Sale Correction has today and both
should be closed together if either is.

**Two routes now record the same object, and their surfaces must not drift.** A sale typed into the
form and a sale in the uploaded file must be validated identically, or the same act will succeed one
way and fail the other for reasons nobody can see. This is kept true by construction — both run the
same import row validator — and anything added to one path's validation belongs in that validator
rather than beside it.

**The sales page section is no longer about importing.** It presents two routes to one act, and its
copy is renamed accordingly. The Sales list gains no write action: it is read by every Member of the
Event, while recording a sale stays gated to whoever can manage the Event's sales.

**The In-Person Sale is still unbuilt, and is now easier to mistake for built.** An Organizer typing
a door sale into this form on Sunday morning is recording a Direct Sale, which is correct — the
money came to the Organization, not through the platform — but "we already have a POS" is now a
plausible thing for someone to believe. Roadmap section E stays where it is, and the day the
`in_person` channel starts writing fee snapshots, ADR 0040 warns that Takings changes meaning
silently for it. That warning does not touch this feature, which never writes a snapshot.
