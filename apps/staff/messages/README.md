# Staff message catalogs

`en.json` is the source of truth. `es.json` mirrors it key for key.

**These are not the Storefront's catalogs, and must never be merged with them.**
The two audiences are given different words for the same entity on purpose: the
Storefront says _Organizer_ to a Customer where a staff screen says
_Organization_, and in Spanish _Organizador_ to a Customer where a staff screen
says _Organización_. Sharing one catalog would import the public vocabulary into
the private surface and collapse a distinction `CONTEXT.md` exists to preserve
(ADR 0041).

## Namespaces are surfaces

A top-level key is one surface a Member is looking at, not one component and not
one page file. Surface, rather than component, is the split that survives: a
component moves between pages and gets reused by a surface that words it
differently, but a Member looking at the payouts screen always reads the payouts
screen's words. It also keeps the diff of a copy change inside one namespace
instead of scattering it.

| Namespace | The surface it speaks for                                            |
| --------- | -------------------------------------------------------------------- |
| `shell`   | Chrome around every page: the organization switcher, logout, language |
| `login`   | The sign-in page, the one surface reachable without a session         |

**Only these two exist yet, and that is the point.** #286 lands the scaffolding
and translates the login page; the rest of the application is still English and
is migrated one surface at a time by the tickets after it. A migration adds a
namespace to this table in the same commit it adds the keys.

The names to expect, so that two tickets do not coin two names for one surface:
`events`, `event`, `ticketTypes`, `tags`, `affiliateLinks`, `pos`, `sales`,
`trends`, `team`, `payouts`, `onboarding`, `organization`, `operator`, and
`errors` for the failures that belong to no single surface.

Two levels are the limit — `login.createTitle`, not
`login.card.create.title.text`. A namespace that wants a third level is really
two surfaces.

## Rules

- **Keys name meaning, not English.** `login.useDifferentEmail`, not
  `useADifferentEmailAddress`. The Spanish must be free to say it differently.
- **Interpolate, never concatenate.** `"...sent to {email}."`, not a label glued
  to a value in JSX: word order is not the same in both languages. Interpolation
  is ICU message syntax, so Spanish word order is the translator's problem rather
  than the code's.
- **No empty values.** An untranslated string is the English one, never `""` — a
  blank renders as a blank and nothing catches it. `lib/messages.test.ts` fails
  on both an empty value and a key that exists in one catalog only.
- **Sentences live here; `lib/` returns tokens.** A pure module decides *which*
  thing is true and returns a token and its data — `lib/login-copy.ts` returns
  `"create"` or `"default"` — and this catalog turns that into words. `lib/` never
  imports the catalog and is never handed a `t`. That keeps the fast test runner
  free of React and of the i18n runtime, and it means a copy edit does not break
  a logic test.
- **Names inside content are not copy.** Organization names, Event names, Ticket
  Type names and tags coined by an Organization are data, and read as coined in
  both languages.
- **The locale decides words, and the marks around numbers and dates. It decides
  nothing about time or money.** An Event's times are drawn in the Event's own
  timezone and amounts in the Organization's currency, whichever language the
  reader is in.

## Typed keys

`global.d.ts` points next-intl's `AppConfig["Messages"]` at `en.json`, so
`t("login.nope")` is a compile error rather than a blank space on a page. Add the
key to `en.json` first and the call site typechecks; add it to `es.json` in the
same commit and `pnpm test` stays green.

## Spanish

`es.json` holds Ecuadorian Spanish, **usted** throughout — the same register
ADR 0033 chose for Customer mail, so the product has one voice across its screens
and its email.

The role vocabulary is coined once, in `CONTEXT.md`, and this catalog follows it
rather than inventing per screen:

| English     | Spanish                            |
| ----------- | ---------------------------------- |
| Organization | **Organización** — never _Organizador_ |
| Member      | **Miembro**                        |
| Org Admin   | **Administrador de la organización** |
| Event Owner | **Responsable del evento**         |
| Event Staff | **Personal del evento**            |

_Organizador_ is barred on every staff surface. It is the same entity's
**public** word, given to Customers on the Storefront, and a staff screen using
it would name the thing being administered with the word for the thing being
advertised.

## What the tests do and do not check

`lib/messages.test.ts` asserts that every catalog holds exactly the keys
`en.json` holds — both directions, so a missing key and a stray key both surface
— and that no message is empty. Every offending path is reported in one run.

It does **not** assert that the Spanish differs from the English, and cannot: a
message that is only placeholders and punctuation is correctly identical in both
catalogs, and so is a word like `Google`.
