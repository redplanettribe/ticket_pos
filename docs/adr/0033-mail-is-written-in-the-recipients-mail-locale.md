# Mail is written in the recipient's Mail Locale

## Context

The Storefront has served two languages for some time. Every page carries its language in its own
address (`/{locale}/...`), a visitor picks between them in the footer, and 361 lines of catalog in
each of `messages/en.json` and `messages/es.json` are held in key-for-key parity by a test. A
Customer can browse, choose an Event, and pay without reading a word of English.

Then the platform emails them, and the email is in English.

Nine of the ten messages this platform sends are English-only string literals assembled with
`fmt.Sprintf`. The tenth, the Follow Digest, is bilingual — and the reasoning that got it there is
the reason this ADR exists. ADR 0030 had to answer "what language is a weekly email written in?"
and found that the answer the Storefront uses does not carry: **a Locale is a property of a page's
address, and mail has no address**. So it invented a **Digest Locale**, captured from the
Storefront a Customer last signed in on, stored on the Customer row, and read back at send time.

That was the right shape for one email. It is the wrong shape for ten, in two distinct ways.

The first is naming. A column called `digest_locale`, read inside `sendSaleConfirmation`, is a lie
about its own scope, and the next reader has no way to tell whether the reuse was considered or
careless. The glossary is worse than silent — the Digest Locale entry lists *"email language,
preferred language, user locale"* under `_Avoid_`, which is precisely the concept this work needs.

The second is that a remembered sign-in language is a **poor answer for a receipt**, and receipts
are most of the mail this platform sends. A `customers` row is upserted by every sale, including
box office sales and imports, so the great majority of rows belong to people who have never signed
in and whose `digest_locale` therefore sits at its `'en'` default, untouched. A Customer can read a
Spanish Event page, check out in Spanish as a guest, and get an English receipt — the one email
they are certain to open, in a language they did not choose, immediately after the platform spent a
whole checkout flow proving it could speak theirs. Meanwhile the fact that would have answered
correctly was right there and was thrown away: the page they bought on knew its own language.

## Decision

**Mail is written in the recipient's Mail Locale**, resolved at send time from a chain of three:

1. **The Sale Locale** — the language of the Storefront page the sale was completed on, recorded
   on the Ticket Sale.
2. **The Customer's Mail Locale** — the language of the Storefront they last signed in on,
   remembered on the Customer row.
3. **English**, always, as the floor.

Each step is consulted only when the one above it has nothing to say.

**The Sale Locale outranks the Mail Locale, and this ordering is the substance of the decision.**
Both are evidence of a language a person chose; the sale's is simply better evidence, because it
was collected at the moment of the act the mail is about, from someone who had just read a whole
page in that language and pressed the button at the bottom of it. The remembered one is the same
evidence, older, and usually absent.

**A sale's language governs every mail about that sale, however long afterwards.** Sale
Confirmation, Sale Voided and Sale Reversal Refused all read it. This is what makes the ordering
work rather than merely sound right: a void notice is sent days later, sometimes by a Platform
Operator, from no page at all, and without the sale carrying its own language there would be
nothing at that moment to read but a default. The sale remembers, so the later mail does not have
to ask.

**`customers.digest_locale` is renamed to `customers.mail_locale`**, and the glossary term Digest
Locale is retired in favour of **Mail Locale**. The Follow Digest keeps reading it and behaves
exactly as before. The rename is not tidiness: it is the difference between a column whose name
constrains its readers and one that describes what it holds.

**The name is Mail Locale, not Customer Locale.** What a Customer sees in the Storefront is
determined by the URL segment and by nothing else (ADR 0027), and a property that sounded like it
governed both would invite exactly the wrong reading.

### The Sale Locale is absent, not English, where no page produced the sale

**`ticket_sales.locale` is nullable, and box office sales and imports record NULL.** This
deliberately breaks the precedent of migration 051, which chose `NOT NULL DEFAULT 'en'` and argued
for it at length, so the divergence is recorded here rather than left to look like an oversight.

On `customers`, `en` is a genuine answer: it is the terminal value of the chain, the language the
platform would write in regardless, and defaulting there removes a case with an obvious resolution.
On `ticket_sales`, `en` would be something else entirely — an **assertion that a buyer chose
English** — and because Sale Locale sits at the top of the chain, that assertion wins. A box office
sale to a Spanish-speaking regular would pin their receipt to English and shadow the very memory
built to catch that case. NULL carries real information here: *no page produced this sale, ask the
recipient instead*.

An online checkout whose request names no language, or names one the platform does not serve,
records NULL by the same rule. A missing or malformed locale must never fail a purchase.

### Staff and Operator mail stays English

**The Staff One-time Passcode and all five Payout Request notices are not localized, and this is a
boundary rather than an omission.** No Member has a locale field, `apps/staff` has no i18n at all,
and the payout notices are addressed to `request.RequestedBy` — a recorded email string, kept
deliberately untied to a Member id so it still resolves after that person's Membership ends, and
therefore attached to no record that could hold a language.

Sending Spanish payout mail to people whose entire working surface is English would be worse than
consistency. `SendOTP` takes a locale because both doors send an identical message and one argument
states the policy more plainly than two duplicated copies of the text would; the staff caller
passes `platform.DefaultLocale` explicitly, at the call site, where a reader can see it.

### This does not weaken ADR 0027

**No read path takes an `Accept-Language`, and none is added.** The locale reaching the backend is
an explicit field in a request body, set by a page that already knows its own language because that
language is in its address. It is a fact being reported, not a preference being negotiated. A
future reader who sees locales in the Go code should not conclude that content negotiation arrived;
it did not, and it should not.

Relatedly, **the passcode request's locale words that one email and nothing else — it does not
write the Mail Locale.** Only a completed sign-in does that, as it always has. Asking for a passcode
is not proof you own the address, and letting an unauthenticated request rewrite a stored property
of a stranger's record is a small but real way to vandalize their receipts.

### Spanish is written in usted

The two bodies of Spanish copy in this repo disagreed, and had never been in the same place to
notice. The Storefront catalog uses **usted** (*"compruebe su conexión e inténtelo de nuevo"*); the
digest copy uses **tú** (*"Novedades de lo que sigues"*, *"Consigue entradas"*). After this change a
Customer buys on a page worded one way and is written to by a system worded the other.

**Usted is the standard, and the digest's Spanish is rewritten to match.** The Storefront catalog is
larger by an order of magnitude and is the cheaper side to converge on; mail is the more formal
register regardless, and *"Consigue entradas"* on a payment reversal notice reads oddly familiar;
and usted is never rude to anyone, which tú is not. The cost is a slightly colder digest, on the one
email where warmth had value, and it is accepted.

### What still does not follow the reader's language

Times remain in the Event's own timezone, the Reversal Window's cutoff in Ecuador's, and money in
the Organization's currency. Localizing mail changes the words and the marks around the numbers,
and changes nothing about the numbers. This is the same line the Locale entry in `CONTEXT.md` has
always drawn, restated because a Spanish receipt showing an Ecuadorian time is the sort of thing
that gets reported as a bug.

## Considered options

- **Read the remembered locale and add nothing per sale** — nearly free, since the Customer row is
  already loaded at send time, and shippable in a day. Rejected because it gets guest checkout
  wrong, and guest checkout is the common case: most `customers` rows have never signed in, so the
  column answers `'en'` for the very people whose language the checkout page just demonstrated it
  knew. It would also have meant the platform storing a language for someone and still writing to
  them in another.
- **Record a locale on the sale and drop the remembered one entirely** — one source, no chain, no
  precedence to explain. Rejected because it strands every mail that is about no sale. The Customer
  passcode has no sale, and the Follow Digest — already shipped and already bilingual — is a weekly
  email about Events nobody has bought. Removing the memory would have meant regressing the one
  email that was already right.
- **Take `Accept-Language` on the API** — the conventional answer, and it would need no new field
  anywhere. Rejected because it contradicts ADR 0027 directly, and because it is a worse signal
  than the one available: a browser header states what a device was configured with, while the URL
  segment states what a person chose, possibly by pressing the language switcher a moment earlier.
  The platform should honour the choice, not the configuration.
- **Localize the payout notices too** — consistency, and organizers are as likely to be Spanish
  speakers as buyers are. Rejected as out of scope rather than wrong: it requires inventing a
  Member locale, and it is only worth having once `apps/staff` is localized, which is a much larger
  piece of work. Doing the mail first would produce Spanish emails linking into an English
  application.
- **Expose the Mail Locale as an editable preference in the Customer Area** — self-serve, honest,
  and it closes the gap where switching the site to Spanish does not change what the mail says.
  Rejected for now because the Sale Locale removes most of the visible symptom: the mail a Customer
  actually notices is the receipt, and the receipt follows the page they bought on. A second
  control would also ask the Customer to hold a distinction — the language of the site versus the
  language of my email — that they should never have to think about. If this is revisited, the
  better answer is the language switcher writing the Mail Locale for a signed-in Customer, so the
  two remain one choice.
- **A JSON message catalog in the backend, mirroring `messages/{en,es}.json`** — symmetric with the
  Storefront and friendlier to a translator. Rejected because it creates a second translation
  system that must be held in parity with the first, and moves a missing translation from a compile
  error to a runtime one. The existing per-sentence struct is the pattern this repo chose for
  exactly this problem one release ago, and the translator is us.
- **Go `text/template` files per locale** — clean separation of copy from code. Rejected as
  machinery for its own sake: these are plain-text messages assembled with `fmt.Sprintf`, there is
  no templating anywhere in the mail path, and six messages do not justify introducing one.
- **Standardize Spanish on tú and rewrite the Storefront catalog** — tú is the ordinary register
  for events and ticketing in Ecuador and reads warmer. Rejected on cost and blast radius: it means
  rewriting ~361 lines of catalog on a surface this work otherwise does not touch, to make the
  smaller body of copy win.

## Consequences

**A sale's language is frozen at the moment of sale and cannot be corrected.** If a buyer picks the
wrong language, or a friend completes the checkout on their behalf, every mail about that sale is
written in it forever. This is the deliberate cost of ranking the sale above the memory, and it is
the right cost: the alternative is mail whose language changes retroactively when someone signs in
elsewhere.

**A Customer can receive mail in two languages at once.** A Spanish receipt for a sale made in
Spanish, and an English digest, if that is what the memory holds. Both are individually correct and
the pair will look like a bug to whoever notices it first. It is the honest consequence of two
sources of evidence disagreeing, and the answer is not to collapse them.

**The rename touches every reader of `digest_locale`.** It is a forward-only migration in a scheme
with no down migrations, so the rename and its callers ship together or not at all.

**`digest_enabled` is deliberately untouched.** It gates the Follow Digest and nothing else, and
the Unsubscribe entry in `CONTEXT.md` is emphatic that it reaches no transactional mail. The
similarity of the two column names now cuts the other way — one generalized and one did not — and
anyone tempted to rename the second for symmetry would be changing what it means.

**Untranslated mail is now conspicuous.** Payout notices and the staff passcode stay English beside
mail that no longer is, and this will be read as unfinished work rather than as a boundary. That is
what the staff section above is for.
