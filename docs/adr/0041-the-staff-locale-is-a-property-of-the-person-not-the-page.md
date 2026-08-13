# The Staff Locale is a property of the person, not the page

## Context

The Storefront speaks two languages. The staff app speaks one, and the seam between them is now a
public one: ADR 0037 put a "Create an event" button on every Storefront page, so an Ecuadorian
promoter can read a Spanish explorer, press a Spanish invitation, and land on a login screen written
entirely in English. Every screen behind it is English too, and so is the one-time passcode they need
in order to reach any of them.

This was a documented boundary rather than an oversight. ADR 0033 localized ten messages and stopped
deliberately at the staff door, on two supports: no Member had a locale field, and `apps/staff` had
no internationalization at all. It also recorded why doing the mail first would be wrong — Spanish
email linking into an English application. Both supports are removed by this work, and the
consequence ADR 0033 predicted ("untranslated mail is now conspicuous") gets worse rather than
better, because a fully Spanish application that only ever emails in English is stranger than an
English application that emails in English.

The obvious move is to copy the Storefront: a `/{locale}/` URL segment, a `Locale` on every page, a
switcher in the footer. That would be the wrong shape, and the reason is the sentence this document
exists to make legible:

> **The Storefront needs two locale terms because a page cannot reach an email. Staff needs one
> because a person can.**

The Storefront's Locale is a property of a *page*, and it is a page property for good reasons that
are entirely about being public: Storefront pages are shareable, indexable and linked to by
strangers, and an address that does not say which language it serves is a real problem there. But a
page property cannot reach outside a page, which is exactly why mail — having no address — needed a
second term invented for it, first as ADR 0030's Digest Locale and then as ADR 0033's Mail Locale.

None of that is true of the staff app. Every one of its pages is behind authentication. It knows
precisely who is reading, on every render, because it already resolves the Active Member from the
Staff Session to draw anything at all. It has an identity to hang a preference on, and that identity
reaches the inbox natively — because it *is* an email address.

## Decision

**The Staff Locale is the language the staff app is written in, and the language staff mail is
written in, for the person signed in.** One term, one stored row, one switcher that moves both
together.

**It is keyed on the email address.** Email is already the key staff identity runs on: the Staff
Session is tied to an email, the one-time passcode challenge is issued against one, and the Platform
Operator allowlist is a list of them. Storage is a person-level row, not a column on `members`, and
it is nullable and absent by default with no backfill. Absence stays meaningful — a person invited
but never signed in, or a payout recipient address belonging to no session, genuinely has not stated
a language — and the floor beneath absence is English.

**The whole staff application is translated, Operator Dashboard included**, in English and
Ecuadorian Spanish only, in usted throughout, matching ADR 0033's register decision for Customer
mail. The locale is resolved from the Staff Session rather than from a URL segment: there is no
`[locale]` segment and no existing staff route changes.

**A person changes it from a switcher in the app shell**, beside the organization switcher and
logout — the neighbourhood of controls that are about *you* rather than about the Organization. A
second switcher sits on the login page, for the case where detection guessed wrong before anyone
signed in. The two deliberately write different things: the login switcher writes the cookie only,
because there is nobody yet to write a preference for, while the shell switcher writes the stored
Staff Locale and the cookie. They should not later be "simplified" into one.

**Before sign-in the language is detected — cookie, then `Accept-Language`, then English — and at
sign-in the detected value is persisted if the person has none.** This is a narrow, deliberate
divergence from ADR 0033's reasoning about the Sale Locale, which keeps a box office sale NULL
because there is genuinely no evidence of a buyer's language. A staff sign-in always carries
evidence: the browser stated a preference, and the person read a login page rendered in it and
proceeded. Weak evidence, but not absent evidence — and it is what makes somebody's app and their
mail agree without their ever visiting a setting. After sign-in the stored value wins and rewrites
the cookie; an explicit choice in a switcher overwrites the stored value.

### ADR 0033's staff-mail boundary is retired, not shrunk

The staff one-time passcode and all five Payout Request notices become bilingual, through the
existing per-sentence copy registry that already holds the Customer mail.

The obstacle ADR 0033 named was specific and it dissolves here rather than being overruled. That ADR
could not localize the payout notices because they are addressed to `request.RequestedBy` — a
recorded email string, kept deliberately untied to a Member id so it still resolves after that
person's Membership ends, and therefore "attached to no record that could hold a language". Key the
Staff Locale on the email address and that string becomes exactly such a record. The backend reads
the same row by recipient email at send time, with English as the floor, which is the same shape as
the existing Customer resolution.

This is why the mail decision lives in this document rather than in one of its own. It is not an
independent trade-off: once the key is the email address, localizing the notices stops being a
choice about direction and becomes a choice about whether to cash in something already paid for.

**Everything else in ADR 0033 stands.** The Sale Locale still outranks the Mail Locale, a sale's
language still governs every mail about it, and the Customer side of the chain is untouched. That
ADR is annotated at its staff paragraph, not superseded.

### This does not violate the Locale-unaware API principle

The staff "me" endpoint gains a `locale` field, and a small write endpoint backs the shell switcher.
ADR 0027's rule survives intact, because the rule forbids the API from *speaking* a language —
choosing its own sentences according to who is reading. **Reporting which language a person prefers
is a fact about that person, like their role.** The API states the fact; the application picks the
words.

Concretely: no `Accept-Language` handling is added to any read path, no locale parameter appears on
any existing endpoint, no per-language columns and no translations table arrive, and error sentences
are still chosen client-side from the error code per ADR 0023. ADR 0036 already established that the
API may serve locale-addressed content where the locale is explicit rather than negotiated; this is
weaker still, since nothing localized is being served at all. The `locale` field also costs nothing
on the render path — the staff app already calls "me" on every server render to resolve the Active
Member, so the value arrives on a request that was happening anyway.

### The role vocabulary is coined in the glossary, once

Spanish for Organization, Member, Org Admin, Event Owner and Event Staff is recorded in `CONTEXT.md`
and the catalog follows it. This is not decoration: these role names have no Spanish anywhere yet,
and translated ad hoc as each screen is migrated there would be three words for Event Staff by the
third page.

The load-bearing constraint is that **staff surfaces say *Organización* and never *Organizador***.
The glossary keeps Organization and Organizer apart on purpose — they are one entity seen from two
sides, and Organizer is the word the public has been given. A staff screen calling the entity being
administered *Organizador* would collapse a distinction the glossary exists to preserve. For the same
reason the staff catalog is deliberately separate from the Storefront's rather than shared: sharing
it would import the public vocabulary into the private surface.

## Considered options

- **A `locale` column on `members`** — the obvious place, no new table, and it is already loaded
  wherever the Active Member is. Rejected because `members` is unique on (organization, email), so
  one human belonging to two Organizations would get two languages and the app would change language
  when they switched context. It also has nowhere to put a Platform Operator, who need not be a
  Member of anything, which would have left the Operator Dashboard structurally unable to hold a
  preference at all. The language belongs to the person, and the person is not the membership.
- **A `/{locale}/` URL segment, as the Storefront has** — symmetry across the two apps, one mental
  model, and the middleware already exists to copy. Rejected because the segment buys things staff
  pages cannot use — a shareable address that declares its own language, indexable by language — and
  charges for them in a rewrite of every route in the application. Worse, it would make the language
  a property of the URL a person happened to arrive at rather than of the person, so it could not
  word their mail, and staff would have needed a second term for exactly the reason the Storefront
  did. The asymmetry between the two apps is the decision, not an accident of implementation.
- **Two staff terms, one for the app and one for staff mail** — mirrors the Storefront's Locale /
  Mail Locale pair, and it would let the two be set independently. Rejected because the pair exists
  only to bridge a gap that does not exist here: the Storefront's second term is compensation for
  the first being unable to reach an inbox. A person's row reaches both. Two terms would permit a
  state nobody wants — Spanish mail linking to an English screen — and invite a Member to hold a
  distinction they should never have to think about.
- **An English-only Operator Dashboard, translating the organizer app alone** — the Operator
  Dashboard has few readers and they are all reachable, so it is the cheapest thing to cut.
  Rejected because it leaves a Spanish shell around English pages: an English island rather than an
  English section, since the two surfaces share one application shell, one switcher and one session.
  It would draw a fresh boundary of exactly the kind this work exists to retire, and the saving is
  small against a translation of the whole app.
- **Localizing staff mail without translating the app**, as ADR 0033 contemplated and declined —
  still declined, and for the reason that ADR gave: Spanish email linking into an English
  application. What changed is that the application is being translated, not that the objection
  weakened.
- **A personal profile or account page to hold the setting** — a conventional home for a preference,
  and room for the next one. Rejected as a route invented to hold a single control. The shell is
  where a person's own controls already live, and it is reachable from every page including the
  Operator Dashboard.
- **Defaulting every existing Member to English by backfill** — no absent values to handle, one code
  path at send time. Rejected because it records a decision nobody made. Absence is a fact worth
  keeping: it distinguishes a person who chose English from a person who has never been asked, and
  the behaviour is identical either way since English is the floor.

## Consequences

- **Staff pages become per-reader and cannot be statically rendered.** Accepted without cost: every
  staff page is already behind authentication and scoped to an Organization, and none was static.
- **The two apps model the same-looking thing differently, permanently.** A reader arriving later
  will see a `[locale]` segment in one application and a session-resolved locale in the other, and
  it will look like drift. It is not: it is two answers to two different questions, public
  addressability versus known identity. This document is the answer to that reader.
- **A person's language is now written by an act that is not a choice.** Signing in with a Spanish
  browser persists Spanish. This is intended — it is what makes mail agree with the screen without
  anyone visiting a setting — but it means the stored value is not always a stated preference, and
  the switcher is what makes it one.
- **Staff mail can no longer be read by whoever happens to open it.** A Payout Request notice now
  arrives in the recipient's language, so a shared or forwarded finance inbox may receive Spanish
  where it previously received English. The recipient's language is the right one to write in, and
  this is the cost of doing so.
- **Formatting changes for existing English readers**, because staff dates and numbers currently
  follow the *browser* locale rather than any chosen language — a Spanish-speaking organizer on an
  English laptop sees `en-US` dates today. Formatting now follows the Staff Locale, mapping to the
  same regional tags the Storefront uses. The glossary's rule is inherited verbatim: the locale
  decides words and the marks around numbers and dates, and decides nothing about time or money. An
  Event's times stay in the Event's timezone and amounts stay in the Organization's currency.
- **A third language is not made cheap by this.** The locale type stays a closed union of two in both
  applications, and nothing here is generalized speculatively.
