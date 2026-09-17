# The Customer Dossier shows the Organization the phone given on its own Event's checkouts

Ratified 2026-09-16. Amends the single-purpose rule written into migration 029; nothing is built
yet.

## Context

A phone number exists in this system because PayPhone's Prepare call accepts one to prefill the
hosted card form. Migration 029 says so in as many words — "a phone number exists in this system for
exactly one reason" — and warns that anything wanting to treat it as "the Customer's phone number"
in general needs a deliberate decision first. It is collected only through online checkout and "My
info", stored on the Payment as what that checkout was told and on the Customer as their current
assertion, and never on the staff-recorded, imported or manually recorded channels, nor from a
Holder.

The Customer Dossier (see CONTEXT.md) is where an Org Admin or Event Owner goes to reach one person
about one Event: an Answer still owed, something wrong with their ticket. Email reaches every
Customer, but the request that produced the Dossier asked for the phone too, because "your ticket
has a problem, call me" is the conversation an organizer actually needs to have.

## Decision

**The Dossier shows the phone number given on a checkout for this Event**, beside the Ticket Sale
that checkout became, read from that Payment. Never the number on the Customer record, which a
later checkout with another Organization may have overwritten — the Dossier is scoped to what this
Event was told, on the same terms as the name and Tax ID it shows per Sale.

**Numbers already collected are shown**, not only those collected after this decision. The purpose
widens from "prefill the card form" to "prefill the card form, and let the Organization selling
this Event contact the buyer about it."

**Read only where the Dossier is read**: Org Admins and Event Owners, never Event Staff. The number
is not added to the Sales list, the Sales Export or the Holder Export, and nothing looks a Customer
up by it — email remains the sole Customer identity.

## Considered options

- **Leave the phone off and contact by email.** Rejected: email is universal but slow, and the use
  cases are the ones where a call is the point.
- **Show only numbers collected after the checkout wording says organizers will see them.**
  Rejected by the Platform Operator in favour of making the existing numbers useful now.

## Consequences

- This is a change of purpose for personal data already held. The privacy notice, and the checkout
  wording beside the phone field, must say that the organizer of the Event receives the number.
  That text change is owned outside the code and is not blocked by, nor blocking, the build.
- Coverage is partial by construction: an In-Person, imported or manually recorded Sale has no
  phone, and a Holder who never bought has none either. The Dossier shows the absence, not a gap to
  fill.
- A future bulk use — SMS reminders, an export column — is not licensed by this ADR and needs its
  own decision.
