# The Organization sees the Holder's address

## Context

[ADR 0046](./0046-a-ticket-is-assigned-to-a-holder-who-accepts-by-mail.md) gives the platform, for
the first time, a name and a proven email address for the person a Ticket was handed to. That raises
a question 0046 does not answer, and which is separable from it: how much of that does the
Organization get to see?

Today an Organization's view of its own Event is a headcount. Its Outstanding Answers list shows
Tickets, not people, and its Sales Export carries the buyer's name repeated once per Ticket. An
Organizer running a dinner for eighty knows it sold eighty tickets and can name perhaps thirty
people. Everything it wants to do with the other fifty — tell them the venue moved, ask the one
person whose dietary Answer is blank, check a name at the door — currently routes through whichever
buyer it happens to know, by hand.

The competing consideration is that a Holder's address arrived here by an unusual route. They did not
seek the Organization out, did not buy from it, and gave the platform their address only because a
friend typed it and they clicked a link to claim a ticket. Disclosing it hands an Organization a
contact detail for a person who chose to hold a ticket, not to be on a list. Once disclosed, the
address leaves the platform's reach entirely: it goes into a Sales Export sheet that gets forwarded
and kept, and whatever is done with it afterwards happens outside anything this platform can govern,
log or revoke.

Withholding the address — showing the Organization the Holder's name only — was recommended. It was
declined. This ADR exists so a future reader sees that as a decision rather than an oversight, and so
it can be revisited without unpicking the rest of assignment.

## Decision

**An Organization sees an accepted Holder's name and email address**, on the staff surface that
already carries Outstanding Answers and in the Sales Export, beside that Ticket's Answers. The
grounds are plain and were weighed against the cost below: an Organizer needs to be able to reach the
people attending its Event, and "who is coming and what size are they" should be one sheet rather
than a name list the Organizer then has to match against a buyer they must ask for addresses anyway.
A name without a way to reach the person leaves the Organizer exactly where it is today, routing
through buyers by hand, which is the problem assignment was built to solve.

**The Holder is told before they accept, and ignoring the mail is how they decline.** The Assignment
mail states that accepting discloses the address to the Organization, and the accept page says it
again. That notice is load-bearing: it is the entire difference between a disclosure the person chose
and one done to them, and it is the reason the accept click can be treated as the capture moment at
all. Weakening or burying that sentence reverses this decision without editing this file.

**Nothing is disclosed before acceptance.** An address that was typed by a buyer and never accepted
is never shown to the Organization, never reaches the Sales Export, and is purged when the Event
starts per ADR 0046. The Organization sees that such a Ticket is `assigned` and not who it was
assigned to. The unconsented address stays inside the platform for its whole short life.

**The disclosure is not gated on Marketing Consent, and grants none.** Marketing Consent governs what
the platform sends a Customer on an Organization's behalf; it is not the switch that decides what an
Organization can see about a person holding a ticket to its own Event. Correspondingly, a Customer
minted by accepting has consented to nothing: they are not in a Follow Digest, not on a marketing
list, and the disclosure buys the Organization no platform-mediated sending.

**The platform builds no surface for an Organization to mail Holders.** The address is disclosed; no
send button, no bulk mailer, no templated announcement. This is not a residual gap to be filled
later — it is the boundary that keeps the platform out of the business of sending Organization mail
to people who never consented to receive it. What an Organization does with an exported address is
outside the platform, and this ADR says so rather than pretending otherwise.

## Considered options

- **Show the Holder's name and withhold the address.** The recommended option, and the one declined.
  It reads well — the Organization learns who is in the room without gaining a mailing list — but it
  does not survive the first practical question. An Organizer with a name and no address is back to
  asking the buyer, which is the manual routing assignment was supposed to end; and since the
  platform deliberately builds no sending surface, withholding the address means an Event whose venue
  changes has no route to the people attending it. Withholding also has a false tidiness: the
  Organizer can still reach the Holder through the buyer, so the address is often only one message
  away, and the platform gets the appearance of protection rather than the fact of it.
- **Ask the Holder at accept time whether to share the address, and disclose only on a tick.** The
  most defensible option on paper. Rejected on the shape of what it produces: uptake on an optional
  tick in a flow the person is trying to finish is predictably low, and a guest list where some rows
  have addresses and some do not is worse for the Organizer than either extreme, because it cannot be
  used as a contact list at all and cannot be reasoned about at a glance. It also converts one clear
  notice into a consent decision taken in three seconds, which is not obviously better for the Holder.
- **Platform-mediated mailing: the Organization composes, the platform sends, the address is never
  disclosed.** The genuinely governable design, and the right long-term answer if this is ever
  revisited. Rejected on scope, not principle. It is a whole sending product — composition, approval,
  rate limits, unsubscribe handling, bounce and complaint handling, and an abuse surface pointed at
  the transactional sending domain that every Sale Confirmation depends on. That is a larger and
  riskier thing to build than assignment itself.
- **Disclose on the staff screen but keep the address out of the Sales Export.** Rejected as theatre.
  Screen and file are the same disclosure to the same people, and the export is where the Organizer
  actually works — it is the artifact behind "how many larges do I order". Splitting them would buy no
  protection and would guarantee somebody re-types the addresses into a spreadsheet by hand.
- **Disclose assigned-but-not-accepted addresses too.** Rejected outright. Those addresses have no
  consent moment behind them at all; the person may not even know a ticket was bought for them. This
  is the line the whole design is drawn around. Put to the product owner explicitly on 2026-08-22 and
  confirmed: the Organization sees that such a Ticket is `assigned`, and nothing more.

## Consequences

**Every Organization gains a list of email addresses belonging to people who never chose to give it
to them.** Their consent was to hold a ticket and to have that fact known to the Organization running
the Event, and reasonable people will read that consent more narrowly than an Organization with a
spreadsheet will. Once exported the addresses are mailable outside anything the platform governs,
with no unsubscribe the platform honours and no log it keeps. This is the cost. It is accepted
deliberately, and it is recorded here rather than left implicit so that revisiting it is a matter of
reversing a decision rather than discovering one.

**The Sales Export becomes a larger concentration of personal data than it already was**, and it was
already described in the glossary as the largest the platform emits. Its existing restriction to Org
Admins and Event Owners now protects third parties' contact details rather than only buyers', which
raises the stakes on any future proposal to widen who may download it.

**Erasure and deletion requests widen again.** A person asking to be forgotten now has to be told that
their address may sit in spreadsheets held by Organizations the platform cannot reach. Those requests
escalate rather than being handled inline, and this is one more reason why.

**Retraction is harder than withholding would have been.** If this is revisited, an Organizer will be
losing something it can already see, and every address disclosed before the reversal is already gone.
That asymmetry is the strongest argument the declined option had, and it is the one to weigh first if
the question is reopened.

**The Privacy Policy clause grows a sentence Ticket Questions does not need.** ADR 0045 blocks
collection until a Policy Version describes it; this decision adds disclosure to a third party to
what that clause must cover, and it is the part counsel is most likely to argue about. If that
drafting stalls, the escape hatch is to split the clause and ship Ticket Questions first, at the cost
of re-gating every Customer twice.
