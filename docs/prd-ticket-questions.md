# Ticket Questions

Spec synthesized from domain grilling session.
Canonical vocabulary: [CONTEXT.md](../CONTEXT.md).
Design records: [ADR 0043](./adr/0043-a-ticket-becomes-a-row-and-quantities-stay-the-truth.md),
[ADR 0044](./adr/0044-the-holder-answers-by-link-and-the-platform-never-writes-to-them.md),
[ADR 0045](./adr/0045-ticket-questions-ship-dark-until-the-privacy-policy-describes-them.md).

> **Status note.** The Answer Link this spec describes is retired by
> [ADR 0049](./adr/0049-only-the-holder-answers-and-the-answer-link-is-retired.md): an Answer is given only by a Ticket's Holder — the buyer
> for their Self-held Ticket — or by Event Staff, and a Ticket's questions are reached through the
> Ticket Assignment (see [prd-ticket-assignment.md](./prd-ticket-assignment.md)). References to the
> Answer Link below are kept as the record of what was decided at the time.

## Problem Statement

An Organization often needs to know something about the people coming, and the platform gives it
nowhere to ask. A workshop that hands out t-shirts needs sizes. A dinner needs dietary
requirements. A conference needs to know which track somebody picked. Today an Organization
collects this in a Google Form linked from the Event description, matches the responses to buyers
by hand, and lives with the ones who never fill it in.

The model cannot hold the answers even if they were collected. A Ticket Sale Line is a Ticket Type
and a quantity; nothing represents one ticket, so there is no place for a fact about one person.
And the person who knows the answer is frequently not the buyer: someone buying four tickets for
friends knows one size out of four.

## Solution

An Organization defines **Ticket Questions** on a Ticket Type. Each of that type's **Tickets** —
one per unit sold — carries an **Answer** to each.

The buyer answers what they know at checkout, on a skippable form. Afterwards, every Ticket has an
**Answer Link** the buyer passes to whoever will hold it, and that person answers for themselves.
Event Staff can fill in or correct any Answer. Nothing is ever refused for a missing Answer: a
required Ticket Question that has not been answered is an **Outstanding Answer**, which the
Organization can see and chase, and which a swept **Answer Reminder** asks the buyer about.

The Sales Export gains a second sheet, one row per Ticket, so "how many larges do I order" is a
pivot table.

## Scope

### Ticket

- One row per unit of every Ticket Sale Line's quantity, on `online`, `in_person` and `import`,
  independent of whether any Ticket Question exists.
- Minted inside the sale-commit transaction. No Ticket for a Payment that is not approved.
- No status column; liveness is read from the Ticket Sale.
- Backfill migration over every existing Ticket Sale, reversed ones included.
- **Tickets Sold is untouched.** It keeps summing quantities. A test asserts Ticket rows equal the
  summed quantities.

### Ticket Question

Defined on a Ticket Type, alongside name, description, price, capacity and sort order.

| Type | Buyer/holder sees | Export cell |
|---|---|---|
| `short_text` | one-line input | the text |
| `long_text` | textarea | the text |
| `single_choice` | radio or select | the chosen Option's current label |
| `multi_choice` | checkboxes | one `TRUE`/`FALSE` column per Option |
| `number` | numeric input | a number, typed as a number |
| `date` | date picker | a date, typed as a date |
| `checkbox` | single tick box | `TRUE`/`FALSE` |

- No file upload. No purpose-built email or phone type — `short_text` covers those without the
  platform inviting a second contact channel for an unconsented person.
- Choice questions carry at most **20 Options**.
- `required` is a flag whose only effect is producing Outstanding Answers.
- Carries a **timing** field (`at_checkout` / `after_purchase`). v1 writes `at_checkout` and always
  honours it; the field exists so the Organization's choice can be honoured later without migrating
  Answers.
- Labels and Option labels are read **as coined** in every Locale, like a Custom Tag. Only page
  chrome follows the Locale.

**Editing rules once any Answer exists:**

- Options may be **added** freely.
- Options and questions may be **renamed**; the current label is what the export header shows.
- Options are **retired, never deleted** — gone from new lists, kept on the Tickets that chose them,
  and still given an export column.
- A question's **type is frozen**. Changing it means retiring the question and adding another.

### Answering

**At checkout.** A skippable form section per ticket. Answers are held on the Payment keyed by
`(payment_line, index)`, mirroring `payment_lines`, and written onto the minted Tickets in order
when the sale commits. Nothing about them can refuse or delay a checkout.

**By Answer Link.** A signed, stateless, per-Ticket link.

- Shows: Event name, Ticket Type name, the Ticket Questions.
- Never shows: buyer name or email, price, Tax ID, confirmation reference, the Sale's other Tickets.
- No identity proof. Overwrites what the buyer entered.
- Expires at Event start (Event timezone). Stops opening on a reversed Sale.

**By buyer, afterwards.** The Confirmation Link's page and the Customer Area list the Sale's
Tickets with their questions and a copy-link button per Ticket.

**By Event Staff.** Any Answer on any Ticket of their Event.

**Edit window.** Until Event start; never on a reversed Sale. `updated_at` is kept; no version
history and no record of who changed it.

### Sale Confirmation

One conditional sentence when the Sale has Outstanding Answers, pointing at the existing
Confirmation Link. No per-Ticket links in the mail body — forwarding one would forward the receipt.
Sales with nothing outstanding are unchanged.

### Answer Reminder

- **Swept, not edit-triggered.** A job over active Sales with Outstanding Answers.
- At most once per Ticket Sale per 7 days, capped at 2 per Sale, silent once the Event has started.
- Transactional: not gated by Marketing Consent. Written in the recipient's Mail Locale.
- **Ships paused.**

### Sales Export

- The `Ticket Sales` data sheet is unchanged — still one row per Ticket Sale, still summable.
- A new sheet, one row per Ticket: `confirmation_ref`, Ticket Type name, then one column per Ticket
  Question, and one column per Option for `multi_choice`.
- **Not named "Sales"**, for the reason `exportfile/export.go` gives: the Sale Import parser selects
  its sheet by that name.
- The sheet appears only when the Event has at least one Ticket Question.

### Retention

Answers held on Payments that never reach `approved` are purged **30 days** after the Payment. Never
on `expired` alone — an expired Payment can flip to approved.

### Shipping

- Own branch off `storefront-tickets-sold`.
- Question authoring is **flagged off**. The Answer Reminder job **ships paused**.
- The flag flips only after a Policy Version describing this collection publishes (ADR 0045).
- An Organization sees a warning at authoring time about what it is asking for.

## Out of scope

- **Check-in, QR codes, ticket transfer, named holders.** A Ticket exists and carries Answers; it
  does nothing else.
- **Emailing holders.** The platform holds no holder addresses and asks for none.
- **Per-Locale question authoring.** Questions read as coined.
- **Event-level questions shared across Ticket Types.** A question belongs to one Ticket Type;
  promoting one to the Event is additive later.
- **File upload questions.**
- **Blocking a sale on a required question**, on any channel.
- **Version history of Answers.**

## Open

- **The Privacy Policy clause.** Drafted for counsel, not authored in the repo. Blocks the flag,
  not the merge.
- **Staff app placement** of question authoring and of the Outstanding Answers view.
