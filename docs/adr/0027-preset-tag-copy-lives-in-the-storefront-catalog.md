# Preset Tag copy lives in the Storefront catalog, keyed on canonical key

## Status

accepted

## Context and decision

The `tags` table holds one `display_name` per Tag, in English, seeded by
[009_event_tags.sql](../../backend/migrations/009_event_tags.sql). The Storefront serves two
Locales. So a Customer reading `/es` filtered the Timeline with a chip bar reading "Music",
"Nightlife", "Arts & Theatre" — twelve English words in the middle of a Spanish page, and the
first thing a Spanish reader touches on the explorer.

We have decided that **a Preset Tag is rendered in the page's Locale, and a Custom Tag is
rendered exactly as it was typed**:

- The twelve Preset Tags' copy lives in `apps/storefront/messages/{en,es}.json` under a new
  `tags` namespace, keyed on `canonical_key`. This is the second machine-keyed namespace, after
  `errors`, and for the same reason: the database decides which Tags exist, and this app only
  chooses the words for them.
- A Preset Tag the catalog does not know **falls back to the API's `display_name`, verbatim** —
  English, never a blank chip and never the lowercase key.
- A Custom Tag is never looked up at all. "Techno", "Cumbia", the name of a scene or a
  promoter's night: an Organization's own word, rendered as coined in every Locale.
- **The API does not change shape.** It stays English and Locale-unaware — no
  `Accept-Language`, no locale parameter, no per-language column or translations table. It
  gains one field: `canonical_key` on `service.TagView`, beside the `name` and `curated` it
  already carried.

Resolution lives in `apps/storefront/lib/tag-name.ts` — pure, framework-free, and unit-tested.

## Why this way

- **It is [ADR 0023](0023-storefront-error-copy-keyed-on-api-error-code.md) again, and the
  argument transfers without amendment.** That decision put error copy in the catalog keyed on
  the API's code, because "the API decides which failure occurred and this app only chooses the
  words for it". Swap *which failure occurred* for *which Tags exist* and it is the same
  sentence, with the same shape of key and the same fallback to the API's English. Deciding
  this one differently would mean two rules for the same problem.
- **Localizing in the database was the other real option, and it is worse for the same reasons
  it was worse there.** Per-Locale columns (`display_name_es`) or a `tag_translations` table
  would each end the API's Locale-unawareness: an `Accept-Language` on the public read paths, or
  a locale parameter threaded from a cookie the Go side deliberately does not read
  (`lib/checkout-context-cookie.ts` — *"payment contract — the Go API stays Locale-unaware"*).
  It would also make one copy surface shared by the Staff app, which is English-only, and the
  Storefront, which is not — the entanglement ADR 0010 exists to prevent. And it buys
  extensibility that has no buyer: only a migration can mint a Preset Tag, so the set changes at
  the same rate the catalog does.
- **Keyed on `canonical_key` rather than on the display name, because the display name is the
  thing being replaced.** The chip bar previously rebuilt the machine key by lowercasing the
  label (`explorer-filters.tsx`), which worked only while the label *was* the English key. Once
  the chip reads "Artes y teatro" it lowercases to a token the API matches nothing against. The
  key was already on the row and already `UNIQUE`; putting it on the wire removes a guess rather
  than adding a coupling, and it is not the Tag's ID — Tags stay addressed by name across the
  API, and `canonical_key` adds no way to address one that `display_name` did not already give.
- **The fallback is what keeps the coupling one-way, and it is not a theoretical branch.**
  [ADR 0004](0004-event-tags-shared-pool.md) grows the Preset tier by flipping `curated` on a
  hot Custom Tag — *"a one-flag change, not a data migration"*. That is an `UPDATE` in
  production with no commit anywhere to carry the Spanish, so a Preset Tag can reach the chip
  bar that this catalog has never heard of. It degrades to English, which is what the whole
  chip bar read yesterday.
- **Custom Tags stay as typed because nobody owes them a translation and nobody could supply
  one.** They are coined by an Org Admin mid-edit, in one language, with no second field to
  fill and no reviewer between them and the Storefront. Most are proper nouns or scene words
  that do not translate anyway — "Techno" is "Techno" in Quito. The alternative is a second
  required input on every coined Tag, which would tax the common case to serve a rare one.
- **Badges are worded as well as chips, not chips alone.** A Preset Tag renders on the
  explorer, the Organization page and the Event page, and translating only the filter bar would
  put the *same* Tag on screen twice in two spellings — a Spanish chip reading "Arte y teatro"
  over a card badged "Arts & Theatre". One rule at every render site is both simpler to state
  and the only version without that artifact.

## Consequences

- **A Spanish card can read "Música" beside "Techno", and that is the intended result.** The
  asymmetry is legible rather than broken: a category this product offers next to a name
  somebody chose. It is stated on both terms in `CONTEXT.md` so a future reader does not
  "fix" it.
- **The backend can never render a Tag's name in a Customer's language.** Emails are English
  and Locale-unaware today (`internal/platform/email_content.go`), so nothing is lost now — but
  the day a Customer's confirmation is localized, a Tag cannot appear in it without revisiting
  this. That is the real cost of the decision, and the one to weigh if that day comes.
- **The catalog is a second, softer contract with the seed.** A `display_name` restyled in a
  migration is harmless — the key is what matches. A *new* Preset Tag, or one promoted by
  `UPDATE`, silently renders English until copy is written. `lib/messages.test.ts` holds a
  fixture of the twelve seeded keys and fails when copy for one is missing, which catches the
  migration half; nothing catches the `UPDATE` half, which is why the fallback is not optional.
- **The Staff app is unaffected and stays English.** It receives `canonical_key` on every
  `TagView` and ignores it; its Preset chips, its "Preset" badge, and its typeahead all keep
  reading `display_name`. The `display_name` column remains the Tag's one name for every
  non-Storefront reader — Staff, logs, and the database itself.
- **`tags=` in the URL stays English in both Locales.** It carries `canonical_key`, which is
  what the API filters on, so a filtered link survives being read in the other Locale and the
  reciprocal hreflang pairs `lib/alternates.ts` emits keep naming the same result set. The cost
  is an English token in a Spanish reader's address bar, alongside the Organization and Event
  slugs that were never localized either.
- **A Custom Tag must never be given a key in the `tags` namespace.** Copy under one would
  contradict "rendered as typed" the day that Tag is promoted, and the lookup order — `curated`
  first, catalog second — is what makes the rule hold rather than merely happen to be true.
