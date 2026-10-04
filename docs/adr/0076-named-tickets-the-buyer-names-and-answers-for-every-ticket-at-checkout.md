# Named Tickets: the buyer names and answers for every Ticket at checkout

Supersedes in part [ADR 0044](./0044-the-holder-answers-by-link-and-the-platform-never-writes-to-them.md)'s "required means outstanding, not blocking", [ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md)'s "after the purchase and never at checkout", and [ADR 0049](./0049-only-the-holder-answers-and-the-answer-link-is-retired.md)'s "only the Holder answers" - each only on an Event that requires Named Tickets.
Everything else those ADRs decided stands, and on an Event that does not require Named Tickets nothing changes except the own-address acceptance below, which applies everywhere.
Builds on [ADR 0048](./0048-the-buyer-holds-one-ticket-by-paying.md), [ADR 0054](./0054-checkout-begins-signed-in-and-the-sale-is-addressed-to-the-session.md) and [ADR 0070](./0070-a-ticket-type-closes-on-the-storefronts-clock-alone-judged-once-at-begin-checkout.md).

## Context

Ticket Assignment and Ticket Questions were built so that nothing about a person is guessed and nothing ever blocks a sale.
A buyer of four names Holders when they like, each Holder accepts and answers for themself, and a required question that nobody answers is an Outstanding Answer the Organization can see and chase.

That gives an Organization a truthful roster and a poor one to plan with.
An organizer ordering t-shirts a week before the Event needs every size, and a debt that many Holders never pay - because they never accepted, or the buyer never named them - leaves the order to be guessed anyway.
The organizer would rather add friction to the purchase than run the Event on blanks.

The previous ADRs rejected each half of this for reasons that still hold in general: a buyer does not know four people's sizes (0044, 0049), and an abandoned checkout must not leave a third party's address behind (0046).
What changes is that the Organization, which is answerable for what it collects, chooses the trade per Event.

## Decision

**An Event may require Named Tickets, and a new Event does.**
With it on, a Storefront checkout is refused until the buyer has named a Holder by email address for every Ticket beyond their Self-held Ticket, and answered every required, approved Ticket Question on every Ticket, their own included.
The buyer keeps the Self-held Ticket exactly as ADR 0048 and ADR 0074 seat it; a buyer who is not attending reassigns it afterwards, as today.
Any address will do, including the buyer's own and the same one more than once - a parent buying for small children has nobody else to name.
The requirement is on a named and answered Ticket, not on a different person per Ticket.

**The buyer's Answers on another person's Ticket are provisional until that person accepts.**
The buyer may read and correct them while the Ticket is `assigned`.
Once the Holder accepts, the Answers are theirs: they find them already given and may change them, and the buyer sees only the assignment state again, as ADR 0049 has it.
Who gave an Answer is not recorded, as before.

**Reassigning keeps the rule.**
On such an Event, a buyer reassigning a Ticket gives the new Holder's required Answers with the address, since reassignment still clears the old Holder's Answers (ADR 0046) and the requirement must not be undone by a change of plans.

**A Ticket the buyer assigns to their own address is `accepted` at once, with no Assignment Link, on every Event.**
Checkout requires a Customer Session (ADR 0054), and a Confirmation Link session is reached through the buyer's inbox, so the address has already been proven by the act that names it.
Mailing the buyer a link to prove it again would leave a fully known family reading "assigned, no name" on the Holder List.

**Nothing is mailed before the Ticket Sale exists.**
Addresses typed at checkout wait on the Payment beside its Answers and are purged on the same terms - 30 days after a Payment that is not approved, never on `expired` alone.
This replaces ADR 0046's guarantee that an abandoned checkout holds no third party's address: it may hold one for 30 days, and it never writes to it.
Assignments named at checkout are written in the transaction that records the Ticket Sale and are never refused by the Assignment mail rationing, since the money has already moved; their mails go out through a paced queue with retries rather than in a burst.

**It binds the Storefront alone, is judged once, and never reaches back.**
A Sale Import, a Manually Recorded Sale, a Sale Correction's replacement and a door sale are never refused by it, as with a Sales Cutoff.
An Org Admin may switch it at any time; begin-checkout reads it once, so a Payment under way settles on the terms it started on.
Tickets already sold keep their state when it is switched on, and a question approved later is an Outstanding Answer on them.
It falls silent once the Event has started, when an assignment can no longer be accepted nor an Answer changed.

**Existing Events are migrated with it off.**
Their organizers authored their questions under "nothing ever blocks a checkout", and some of their buyers are mid-checkout on the day this deploys.

## Considered Options

- **Each Holder answers after accepting; checkout only requires the addresses.** Rejected: it adds the friction and still leaves the sizes late or missing, which is the problem the setting exists to solve.
- **The buyer of four types four addresses, one of them their own.** Rejected: the Self-held Ticket is load-bearing for the Upgrade, the Assignment Reminder and the Sale Commit Terms, and making it conditional to serve the rare buyer who is not attending costs more than reassigning afterwards.
- **Require distinct addresses that are not the buyer's.** Rejected: it refuses families and pushes buyers into invented addresses, which are worse data than an honest repeat.
- **Bind imports and Manually Recorded Sales too.** Rejected: they record transactions that already happened, and refusing a row of history does not make it true.
- **Switch it on for existing Events.** Rejected: it would change live checkouts under organizers who never chose it, and turn questions approved as "nice to know" into gates.
- **Record who gave each Answer.** Rejected: an organizer acts on the size whoever typed it, and ADR 0044 and 0049 already chose not to keep an author.
- **Ship behind a deploy flag pending a legal review of the Privacy Policy.** Rejected by the product owner.
  The Storefront states, beside the address fields, that the addresses will be mailed and shown to the Organization, as the assignment route already requires.

## Consequences

- "Required" now has two meanings depending on the Event: a gate at a Named Tickets checkout, a debt everywhere else.
  The glossary's Ticket Question, Answer and Outstanding Answer entries say so.
- The platform collects third parties' contact details and, through Ticket Questions, possibly their health data, before payment and by default.
  The Question Review's acknowledgement that a buyer may be supplying data about somebody else is now the normal case on most Events, not an edge.
- A checkout that names nine people produces nine Assignment mails at once; the paced queue is what keeps that from tripping the mail provider's rate limit, as the Assignment Reminder's first run did.
- The Assignment Reminder has almost nothing to chase on a Named Tickets Event, since its online Tickets are all named; it keeps working for imported sales and for Events with the setting off.
- Event Owners cannot switch it, because they cannot edit the Event; if that gate is ever widened, this setting moves with the rest of the form.
