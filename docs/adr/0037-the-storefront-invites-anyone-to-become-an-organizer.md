# The Storefront invites anyone to become an Organizer

## Context

The door has been open since launch. Signing in to the staff app takes no invitation and no
allowlist: any proven email mints a Staff Session. A Staff Session holding no Membership is walked
straight into creating an Organization, and that Organization is granted on the spot — no vetting,
no approval, no application, no `status` column anywhere for one to sit in. From there, publishing
an Event needs only content, and listing it publicly is a self-serve toggle. No Platform Operator
is involved at any point between a stranger's email address and a public Event selling tickets.

None of this was visible from the Storefront. The Storefront is the platform's entire public face
and the only surface a member of the public ever sees, and it carried no link to the staff app and
no words telling a reader that the Events they were browsing had been put there by people like
them. The supply side therefore ran on word of mouth: a visitor who runs a venue or promotes club
nights could browse the Timeline, buy a ticket, follow an Organizer and leave without ever learning
that publishing was on offer.

So the open door was real, and unmarked. That is a strange combination to leave in place — and a
stranger one to find in a codebase that stores Payout Profiles and withholds a Platform Fee, where
a future reader would reasonably assume the absence of a gate was an oversight nobody had got round
to fixing.

## Decision

The Storefront invites anyone, publicly, with no gate. An outlined "Create an event" button sits in
the header of the global explorer and a quiet link sits in the footer of every page, both leading
to the staff app's sign-in page, where the existing self-serve onboarding takes over unchanged. The
invitation is shown to everyone: there is no branch on Customer Session state, because buyers and
Organizers are not separate populations.

The onboarding behind it is not narrowed to match. Gated onboarding was the real alternative — an
invitation code, an operator allowlist like the Platform Operator one, or an application an
operator approves — and it was rejected. So was the softer version: a vetting or approval step that
holds a new Organization in a pending state until somebody looks at it. Both buy their safety by
inventing a queue that a Platform Operator must staff, at a moment when the platform's problem is
that it has too few Organizers rather than too many, and both would have turned "publish your Event
today" into "hear back from us". The controls that matter — the Payout Profile, and the Platform
Operator who reads it and moves the money — sit at the far end, on the payout side, where a bad
actor's Organization can be stopped before anyone is paid rather than before anyone is heard.

A localized `/organizers` marketing page on the Storefront was also considered and dropped. The
staff app's own create-organization screen already explains the offer, and a new static route under
the Storefront's Locale segment would have become an eighth sibling shadowing the `[orgSlug]`
dynamic segment.

## Consequences

- **The invitation cannot be untold.** A button can be deleted; an audience that has learned this
  platform is self-serve cannot be made to unlearn it. Introducing a gate later is therefore a
  withdrawal of a public offer, not a configuration change, and should be costed as one.
- **The risk is accepted, not absent.** Anyone can mint an Organization, attach a Payout Profile
  and publish an Event, and the invitation raises the volume of people doing so. The platform is
  betting that abuse arriving through a public front door is cheaper to catch at payout than
  supply starvation is to cure, and that bet is the decision. If it is lost, the fix is a control
  at the Payout Request, not a lock on Organization creation.
- **There is no Storefront landing page between the invitation and the staff app.** The button is a
  cross-origin link, so the first thing a would-be Organizer reads about the offer is written in the
  staff app, in the staff app's voice, and any later change to how the offer is pitched is a change
  to two applications rather than one.
- **A Spanish-reading visitor is not warned that the staff app is English.** They cross from a
  Spanish Storefront page into an entirely untranslated application, and nothing in this change
  mitigates or discloses that. Translating only the sign-in and create-organization screens was
  rejected: Spanish onboarding followed by an English event editor reads as a broken app rather
  than an English one. This is accepted on the basis that staff translation is planned shortly —
  and that work will reopen ADR 0033, whose "mail is written in the recipient's Mail Locale" rule
  stops at staff mail precisely because no Member has a Locale and the staff app has no i18n at all.

  **Overtaken by ADR 0041.** The staff translation named above landed: the whole staff application
  and all six staff messages are bilingual, and a Spanish-reading visitor now crosses from a Spanish
  Storefront page into a Spanish staff app. The consequence this bullet accepted no longer exists;
  it is left in place because the decision was genuinely taken without the mitigation.
- **The staff sign-in page now has a second audience.** It answers strangers as well as Members, so
  its copy is no longer free to assume the reader already has an Organization.
