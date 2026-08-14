# Staff message catalogs

`en.json` is the source of truth. `es.json` mirrors it key for key.

**These are not the Storefront's catalogs, and must never be merged with them.**
The two audiences are given different words for the same entity on purpose: the
Storefront says _Organizer_ to a Customer where a staff screen says
_Organization_, and in Spanish _Organizador_ to a Customer where a staff screen
says _Organización_. Sharing one catalog would import the public vocabulary into
the private surface and collapse a distinction `CONTEXT.md` exists to preserve
(ADR 0041).

## Where the language comes from

A call site never has to know. `useTranslations` / `getTranslations` and
`useLocale` / `getLocale` already answer in whatever `i18n/request.ts` resolved
for the request, which is one ladder with three rungs (ADR 0041):

1. the **Staff Locale** stored against the signed-in person's email address;
2. the `NEXT_LOCALE` cookie, written by a switcher;
3. `Accept-Language`, and then English.

The stored value wins once somebody is signed in, which is what makes the choice
follow a person across devices and across Organizations. It costs no extra
request: it rides in on `/api/v1/auth/session`, the call `lib/staff-session.ts`
already makes once per render and React-caches, so asking for the locale and
asking for the Active Member is one HTTP call.

Two switchers exist and **must not be merged** (the ADR says so outright):

- `app/language-switcher.tsx` — the login page. Writes the cookie only; there is
  nobody signed in to store a preference for.
- `app/shell-language-switcher.tsx` — the app shell, the organization picker and
  the onboarding gate. Writes the stored Staff Locale through
  `/api/staff/locale` **and** the cookie.

Components in `@ticket-pos/ui` cannot reach this catalog — that package is shared
with the Storefront, whose catalog is deliberately a different one. They take
their words as props (`StaffShellLabels`, `OperatorShellLabels`,
`SidebarLabels`), and the nav builders return **keys** rather than labels, so a
nav entry added there is a type error at the one place that can translate it.

## Namespaces are surfaces

A top-level key is one surface a Member is looking at, not one component and not
one page file. Surface, rather than component, is the split that survives: a
component moves between pages and gets reused by a surface that words it
differently, but a Member looking at the payouts screen always reads the payouts
screen's words. It also keeps the diff of a copy change inside one namespace
instead of scattering it.

| Namespace | The surface it speaks for                                                        |
| --------- | -------------------------------------------------------------------------------- |
| `shell`   | Chrome around every page: both side panels, the organization switcher, logout, language |
| `login`   | The sign-in page, the one surface reachable without a session                     |
| `errors`  | Failures, keyed on the API's error code — belongs to no single surface            |

**Only these three exist yet, and that is the point.** #286 lands the
scaffolding and translates the login page; #287 translates the shell and lands
the two shared mechanisms; the rest of the application is still English and is
migrated one surface at a time by the tickets after it. A migration adds a
namespace to this table in the same commit it adds the keys.

The names to expect, so that two tickets do not coin two names for one surface:
`events`, `event`, `ticketTypes`, `tags`, `affiliateLinks`, `pos`, `sales`,
`trends`, `team`, `payouts`, `onboarding`, `organization`, `operator`.

`shell` covers **both** side panels, the Operator Dashboard's included: the
Operator Dashboard has screens of its own that `operator` will speak for, but the
panel around them is chrome, and one switcher and one session serve both surfaces.
`shell` also owns the **role names** (`roleOrgAdmin`, `roleEventOwner`,
`roleEventStaff`) even though the team screen renders them too — they are coined
once in `CONTEXT.md` and named once here, because a role translated per screen is
how there come to be three Spanish words for Event Staff.

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
  reader is in. `lib/format.ts` is what enforces this — see below.

## Formatting: `lib/format.ts`

Every number, amount, date and time a staff screen draws goes through
`lib/format.ts`. Nothing calls `toLocaleDateString()`, `toLocaleString()` or a
bare `Intl.*` on a migrated surface.

The reason is a live bug rather than tidiness: a bare `Intl` call follows the
**browser's** locale, so a Spanish-speaking organizer on an English laptop reads
`en-US` dates today and an English-speaking one on a Spanish laptop reads Spanish
ones. Nothing in the application chose either.

```ts
import { toAppLocale } from "@ticket-pos/locale";
import { useLocale } from "next-intl";          // or getLocale() on the server
import { formatMoney, formatDateTime } from "@/lib/format";

const locale = toAppLocale(useLocale());

formatMoney(ticketType.price_cents, organization.currency, locale);
formatDateTime(event.starts_at, event.timezone, locale);
```

| Function                                        | Draws                                   |
| ----------------------------------------------- | --------------------------------------- |
| `formatNumber(value, locale, options?)`          | A count — tickets, requests, people      |
| `formatMoney(cents, currency, locale)`           | An amount in a **stated** currency       |
| `formatDate / formatTime / formatDateTime(value, timeZone, locale)` | A moment in a **stated** zone |
| `formatInstant(value, timeZone, locale, options)` | A moment in a shape of your own          |
| `formatCalendarDay(day, locale)`                 | A `"YYYY-MM-DD"` day, which has no zone  |
| `staffIntlLocale(locale)`                        | The `Intl` tag, for a case not wrapped   |
| `PLATFORM_TIME_ZONE`                             | Ecuador's clock, when nothing else names one |

**`currency` and `timeZone` are required and have no defaults, deliberately.**
That is how the rule above is enforced by the compiler rather than by anyone
remembering it: a locale cannot reach either, because neither is derived from it.
A moment with nothing to draw — null, empty, unparseable — comes back `null`, so
a call site renders nothing rather than "Invalid Date".

`formatCalendarDay` is separate from `formatDate` because a calendar day is not a
moment: `new Date("2026-03-01")` is UTC midnight, which is the 28th of February
everywhere west of Greenwich, which is where this platform sells.

## Error copy: `lib/api-errors.ts`

A failure's sentence is chosen from the API's error **code**, with the API's own
English `message` as the floor beneath any code the catalog has not heard of (ADR
0023). The API stays English and Locale-unaware; the application picks the words.

```ts
import { useMessages } from "next-intl";       // or getMessages() on the server
import { apiErrorMessage } from "@/lib/api-errors";

const errorCopy = useMessages().errors;
setError(apiErrorMessage(errorCopy, envelope.error) ?? t("switchFailed"));
```

`apiErrorMessage` answers `null` only when there was nothing to read at all — a
request that never reached the API. That `?? t(...)` is therefore not a
redundancy: it is the difference between "the API refused, and here is why" and
"we could not reach the API", and its sentence comes from the **surface's own**
namespace, not from `errors`.

**To add a code:**

1. Find the `code` the API sends (browser network tab; `backend/internal/...`).
2. Add it under `errors.envelope` in `en.json` **and** `es.json`, same commit.
3. There is no step three. Resolution is a lookup, so a key in the catalog is a
   translated failure.

A field-level code — the `code` on an entry of `details.fields[]` — goes under
`errors.field` and is resolved by `fieldErrorMessages(catalog, details)`.

Catalog only the codes a surface deliberately shows: copy nothing renders is copy
that rots, and the floor already covers everything else. A sentence may name facts
the API supplied — `"…limited to {limit} per person"` — and a `details` payload
missing one of them falls back to the API's English rather than printing a literal
`{limit}`.

There is no `surface` parameter, unlike the Storefront's resolver. If one API code
ever comes to mean two things on two staff surfaces, the answer is a sentence in
each surface's own namespace passed as the `??` fallback — not a second key space
in `errors`.

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

| English     | Spanish                            | Key                |
| ----------- | ---------------------------------- | ------------------ |
| Organization | **Organización** — never _Organizador_ | —              |
| Member      | **Miembro**                        | —                  |
| Org Admin   | **Administrador de la organización** | `shell.roleOrgAdmin`   |
| Event Owner | **Responsable del evento**         | `shell.roleEventOwner` |
| Event Staff | **Personal del evento**            | `shell.roleEventStaff` |

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
