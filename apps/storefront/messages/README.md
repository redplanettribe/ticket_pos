# Storefront message catalogs

`en.json` is the source of truth. `es.json` mirrors it key for key.

## Namespaces are surfaces

A top-level key is one surface a Customer is looking at, not one component and
not one page file. Surfaces, in the order a Customer meets them:

| Namespace      | The surface it speaks for                                          |
| -------------- | ------------------------------------------------------------------ |
| `shell`        | Chrome around every page: header, footer, breadcrumb, close buttons |
| `explorer`     | The global event explorer at `/{locale}`                           |
| `organization` | An Organization's page, `/{locale}/{orgSlug}`                      |
| `event`        | An Event's page and its Ticket Type list                           |
| `checkout`     | Ticket selection, the answer section, and the success / failed / stub terminals |
| `signin`       | The Customer Session sign-in flow                                  |
| `customerArea` | A Customer's own tickets and the undo of a purchase                |
| `myInfo`       | The Customer's own details                                         |
| `privacySettings` | A Customer's own consents, at `/{locale}/privacy`               |
| `privacy`      | The Privacy Policy page's chrome — never the policy itself         |
| `answerLink`   | The Answer Link's page at `/{locale}/answer` — its chrome only     |
| `errors`       | Failures that belong to no single surface, keyed by the API's code |
| `tags`         | Preset Tag names, which belong to no single surface either         |

The two privacy namespaces are crossed over and it is worth reading twice:
`privacy` is the **public** Privacy Policy page at `/{locale}/privacy-policy`,
and `privacySettings` is a **signed-in** Customer's own consents at
`/{locale}/privacy`. The names predate the routes; neither was renamed, because
`privacy` is referenced by the page, the footer link and the sitemap and a
rename would be a diff about nothing.

Surface, rather than component, is the split that survives: a component moves
between pages and gets reused by a surface that words it differently, but a
Customer looking at the Event page always reads the Event page's words. It also
keeps the diff of a copy change inside one namespace instead of scattering it.

Two levels are the limit — `checkout.success.title`, not
`checkout.terminals.success.heading.text`. A namespace that wants a third level
is really two surfaces.

## `errors` and `tags` are the namespaces keyed by machines

Every other key names meaning in English. These two name what the backend
decides, because it decides *which* and this app only chooses the words for it.

### `errors`

`errors` names the API's own codes (ADR 0023). Four groups, and lookup order
matters:

| Group           | Keyed by                    | Holds                                                        |
| --------------- | --------------------------- | ------------------------------------------------------------ |
| `errors.envelope` | `error.code`              | Failures of a whole request                                   |
| `errors.field`    | a `FieldError`'s `code`   | Fragments that render under one input ("is required")         |
| `errors.undo`     | `error.code`              | Codes the undo dialog words differently                       |
| `errors.myInfo`   | `error.code`              | Codes "My info" words differently                             |

A surface group is consulted first, then `errors.envelope`, then the API's own
`message`. That last step is the point of the whole arrangement: a backend that
ships a code this catalog has never heard of degrades to the English sentence
the API sent, never to a blank.

Two rules that do not apply anywhere else in these files:

- **Add a code only when a Storefront surface can actually receive it.** Copy
  nothing renders is copy a translator maintains for nothing. Codes already
  intercepted elsewhere (the Payment Provider return leg, Confirmation Links,
  Google Sign-In) own their copy on their own surfaces and are deliberately
  absent here.
- **A code with two meanings gets no `envelope` entry.**
  `CUSTOMER_SESSION_SCOPE_INSUFFICIENT` is worded by the operation refused, so
  it lives in the surface groups alone: a third surface that starts receiving it
  falls through to the API's message rather than to another surface's sentence.

### `tags`

`tags` names Preset Tags by `canonical_key` — the twelve seeded by
`backend/migrations/009_event_tags.sql`, plus any a later migration adds
(`047_technology_preset_tag.sql`) — because the database decides which
Tags exist (ADR 0027). Flat, one level, keys lowercased exactly as the column
holds them. A key can never contain a `.`: the Tag charset is letters, digits,
spaces and hyphens, and the two seeds carrying an `&` predate it by being raw
SQL.

It is the **second** namespace keyed this way, and the only other one. It
degrades the same way `errors` does: a `curated` Tag with no entry here renders
the API's own English `name`, which matters because ADR 0004 grows
the Preset tier by flipping a flag in a single `UPDATE` — a Tag can reach the
chip bar with no commit that could have carried its Spanish.

Two rules, mirroring the ones above:

- **A Custom Tag gets no entry, ever.** Custom Tags render as the Organization
  typed them, in every Locale. Copy under a Custom Tag's key would contradict
  that the day the Tag is promoted to a Preset Tag.
- **Add a key when the seed does.** `lib/messages.test.ts` holds the seeded
  keys and fails when copy for one is missing.

## Rules

- **Keys name meaning, not English.** `customerArea.undo.confirmTitle`, not
  `undoThisPurchaseQuestion`. The Spanish must be free to say it differently.
- **Interpolate, never concatenate.** `"{organization} logo"`, not a label
  glued to a name in JSX: word order is not the same in both languages.
- **No empty values.** An untranslated string is the English one, never `""` —
  a blank renders as a blank and nothing catches it. `lib/messages.test.ts`
  fails on both an empty value and a key that exists in one catalog only.
- **The Privacy Policy's text is not in here, and must never be.** The
  `privacy` namespace holds the page's heading, its effective-date label and the
  footer link's words — chrome. The policy body, the Short Notice and the three
  consent checkbox labels are served by the API from
  `backend/internal/consent/policy`, because a Policy Version records the
  SHA-256 of the exact text a person was shown and a hash taken over a file in
  this directory could not be checked against what the page rendered (#250).
  Legal text pasted in here would be text no test can tie to the fingerprint.
- **A Ticket Question's words are not in here, and must never be.** The
  `answerLink` and `checkout.answers` keys are chrome only — headings, the line
  saying the section can be skipped, the per-ticket heading, the "Optional" chip,
  the empty entry of a choice select, the Save button. The QUESTIONS themselves,
  their Options' labels, and an Answer's own text come from the API as the
  Organization coined them and are read identically in every Locale, exactly as a
  Custom Tag is (ADR 0027). Translating "T-shirt size" into the reader's language
  would be the platform putting words in an Organization's mouth, and translating
  a reply would be putting them in a Customer's — and a key here under an
  Organization's own wording would contradict that the first time two
  Organizations coin the same question.
- **"Multiticketing" is not in here.** It is a brand name and reads the same in
  every language, so it lives in `lib/brand.ts` and is interpolated in
  (`"Powered by {brand}"`) rather than being copied into each catalog where a
  translator would eventually "fix" it.

## Typed keys

`global.d.ts` points next-intl's `AppConfig["Messages"]` at `en.json`, so
`t("shell.nope")` is a compile error rather than a blank space on a page. Add
the key to `en.json` first and the call site typechecks; add it to `es.json` in
the same commit and `pnpm test` stays green.

`es.json` holds Ecuadorian Spanish: **usted** throughout, because this is a
product where someone is entering a Tax ID or being told a payment failed, and
**entradas** for the thing being bought — never _boletos_, and never both.

The parity test does not assert that the two catalogs differ, and cannot: a
message that is only placeholders and punctuation (`"{count} × {ticketType}"`,
`"{event} · {organization}"`) is correctly identical in both, and so is `Total`.
