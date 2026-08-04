# Support WhatsApp: on the Organization, published publicly

## Context

A Customer on an Event page who has a question has nowhere to ask it.

Everything the Storefront tells them is one-directional — the Event description the organizer
wrote, the venue, the Ticket Types, the prices. If the question is "is this all-ages?", "where
do I park?", "my payment bounced, did I just buy twice?", or "I was at the show and my ticket
would not scan", the page offers no answer and no way to reach a person. Their only recourse is
to find the Organization somewhere else — a social profile, a Google listing — or to abandon the
purchase.

Organizations on this platform already run support over WhatsApp. It is how their customers
expect to reach them and how they already work; ADR 0026 was written about the operator side of
exactly the same habit. The number simply is not published anywhere the platform controls, so
the Storefront drops every Customer who needs a human.

Three questions had to be answered together, because each constrains the others: whose attribute
the number is, how widely it is published, and what counts as a valid one.

## Decision

**An Organization has one optional Support WhatsApp number**, set by an Org Admin in
Organization Settings, published on its public Event pages as a link a Customer can message.

### It belongs to the Organization, not the Event

Support is a property of who answers the phone, not of a single night's show. One field
inherited by every Event means an organizer types it once and every Event they have ever
published — and every one they publish later — carries it. A per-Event column would start blank
on each new Event and in practice go unfilled, which is the worst outcome: a support feature
most Events do not use.

The Organization already has an optional attribute shaped exactly like this — the Logo, set by
an Org Admin and shown wherever the Organization is represented to Customers. Support WhatsApp
sits beside it in the glossary for that reason.

A **per-Event override is deliberately deferred**, not rejected. It remains a strictly additive
change: a nullable Event column plus a resolution rule falling back to the Organization's. The
day a festival with its own dedicated line asks for it, nothing in this decision obstructs it.

### It is published on the public API, and narrowly

The number is served on the public Event detail's Organization block. This is a phone number in
an unauthenticated JSON response, permanently and irrevocably — anything scraped cannot be
unscraped.

That exposure is **inherent to the feature rather than a flaw in it**. The organizer typed the
number into a field labelled as public, whose helper text says in as many words that it appears
on every Event page and that Customers will message it. Obfuscation — server-only rendering, an
email-style anti-scrape encoding — would be theatre that also breaks the Integration Partner who
legitimately wants to show the number in their own listing. The real mitigation is an informed
organizer, and that is what the form provides.

**The one part of the exposure that was optional is excluded.** The public Organization summary
is a single structure shared by three payloads: the Event detail, every Event card in the global
explorer, and the Organization page. Adding the field to it would publish the number in all
three. Instead it is populated *only* on the Event detail path, and the two listing queries do
not select the column.

The reason is that the global Event listing is paginated and unauthenticated. A number riding on
the shared summary would let one crawl of the platform harvest every Organization's support
number at once, rather than requiring each Event detail to be discovered and fetched
individually. Those are materially different exposures, and only the second is one an organizer
could reasonably have anticipated when they filled in the field.

The cost is a field that is always absent in two of the three payloads its structure serves,
which is a trap for the next reader. It is held by a comment on the structure and by an
integration test asserting the listings do not carry it. The alternative — a near-twin structure
for the Event detail, duplicating name, slug and logo — trades the trap for duplication, and was
judged the heavier of the two. **At a second detail-only Organization field, the separate
structure becomes clearly right and this should be revisited.**

### It is judged by the existing phone rule, strict tier and all

The number goes through `platform.ValidatePhone`, the platform's single phone rule, unchanged.
No second rule is introduced. The number of definitions of "a valid phone number" a system can
hold without them drifting is one.

That rule is two-tier: Ecuadorian numbers must be a **mobile** (nine digits beginning with 9);
every other country gets permissive E.164. Its stated justification is that PayPhone's hosted
card form wants a cardholder's mobile — a rationale that does not transfer to a support line.

The strict tier is kept anyway, on its own merits: an organizer who types their landline by
mistake publishes a number that never answers a message, and a Customer discovering that
mid-purchase is a worse failure than a rejection at the settings form.

The **knowing cost** is that WhatsApp Business can be verified on a landline by voice call, so
an Ecuadorian Organization running support from an 02… number cannot enter it. This is an
accepted exclusion, recorded here so it is not later mistaken for a bug. If such an Organization
ever appears, the fix is a deliberate widening of the tier, not a second rule.

Because the rule's file previously stated that a phone number existed in this system "for
exactly one reason", that comment was rewritten as part of this change. The accuracy of that
file's reasoning is load-bearing: it is what stops someone reintroducing a second phone rule.

### The Staff app does not mirror the rule

The Storefront mirrors the phone rule in TypeScript so a buyer mid-checkout does not lose a
round trip to a typo. The Staff app does not, and should not start. An Org Admin editing
Settings is nowhere near that bar, and a second mirror would double the drift surface the rule's
own documentation warns about — where a mirror and the server disagree, a person is locked out
of something the platform would have accepted. The server's verdict is the only verdict.

## Consequences

An Organization's support number is entered once and appears on every Event page it has, without
per-Event work and without a migration when new Events are created. Clearing the field withdraws
it everywhere at once.

Because the field can be cleared, its update is presence-keyed: an absent key leaves the stored
number alone and a null or blank one clears it. A plain pointer cannot express that distinction,
so the three-state decoder the Customer profile already used was hoisted into the shared platform
package as its second caller.

A Customer on an ended Event still sees the link. That page is where a Customer is most likely
to have a concrete grievance rather than a pre-sale question — a refund, a ticket that would not
scan — and it is the page that has just removed every other interactive element.

The drafted message the link opens with is in the **Customer's** locale, so a Spanish-speaking
organizer may receive an English opener. Accepted: the organizer recognizes their own Event's
name regardless of the words around it, and the alternative requires an Organization language
preference that does not exist.

Support surfaces beyond the Event page — the failed-checkout page especially, where a Customer
is most likely to want a human — read the same value and are deferred to their own pass.
