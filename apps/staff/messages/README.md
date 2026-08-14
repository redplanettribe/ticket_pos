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
| `events`  | The events list, the create-event page, and the **Event status** vocabulary — every surface that draws a status badge reads it from here rather than coining a second word for "Published" |
| `event`   | One Event: its own side panel, the header bar, and the Details form — venue, registration, service fee, description, cover image and cover video |
| `ticketTypes` | An Event's Ticket Types: the list, the cards, the add and edit dialogs, and the Promotion dialog |
| `tags`    | The Tag editor inside Details. **Tag names are not here** — a Preset Tag's copy is the Storefront's, keyed on canonical key, and a Custom Tag reads as coined in every Locale (ADR 0027) |
| `affiliateLinks` | An Event's Affiliate Links: the list, the create form, and the rename and delete dialogs |
| `pos`     | The point of sale: selling at the door, on a phone, often by Event Staff          |
| `sales`   | The Event's Sales tab — the list, its filters and export, the Net Proceeds strip, and the Sale Import tool that sits under them |
| `trends`  | Sales Trends: the pair of day-by-day charts and what they are counting            |
| `team`    | Who belongs to the Organization and what they may touch: the Members card and the Event access card that assigns them to Events |
| `organization` | The Organization being administered: the Settings page header, the profile, the Logo, and the danger zone |
| `onboarding` | Creating an Organization — the gate a brand-new organizer lands on straight after signing in, and the in-app create page that shares its form |
| `payouts` | Getting paid: both balances, the Payout Profile, the ask, the request history, and the **Payout Request status** vocabulary — one key per state, read by every screen that draws one |
| `operator` | The Operator Dashboard: the platform-revenue overview, the organizations roll and one Organization's detail, the Payout Request queue and one request, the sale lookup and one sale, and the Consent Withdrawal surface |
| `errors`  | Failures, keyed on the API's error code — belongs to no single surface            |

`sales` covers the **Sale Import** tool as well as the list, and there is
deliberately no `imports` namespace: importing is a section of the Sales tab
rather than a screen of its own, a Member reading it is reading the Sales tab,
and its keys are prefixed `import…` inside the one namespace. `trends` is
separate because it IS its own screen, with its own route and its own nav entry.

`events` and `event` are two surfaces and not one namespace split in half: the
events list is where an organizer chooses which Event to work on, and everything
under `/events/[id]` is where they work on it. The one thing that crosses the
line is the **status vocabulary** — `events.statusDraft` and its two siblings —
which lives with the list that coined it and is read from the Event's header bar
and side panel too, because a role or a status translated per screen is how there
come to be two Spanish words for "Published".

`team` and `organization` are two surfaces sharing one route, `/settings`, and
that is deliberate rather than an oversight: `/team` redirects there, the Members
and Event access cards are what a person means by the team screen, and the
profile, Logo and danger zone are the Organization itself. Splitting them by
surface keeps the diff of a copy change inside the thing it is about, and it is
why `app/settings/settings-page-client.tsx` reads two namespaces.

The Event's own side panel gets its words the way the app shell does:
`eventNavItems` in `@ticket-pos/ui` returns **keys**, and `app/events/[id]/layout.tsx`
is the single place that turns them into `EventShellLabels`. A nav entry added
there is a compile error at that one file.

**The table grew one surface at a time, and that was the point.** #286 landed the
scaffolding and translated the login page; #287 translated the shell and landed
the two shared mechanisms; every ticket after them migrated one surface and added
its namespace to this table in the same commit it added the keys. **#292 added
the last one.** The table is now the whole application, so a screen with an
English literal in it is a mistake rather than a surface awaiting its turn —
which is exactly the state #293's lint rule needs to be switched on against.

`shell` covers **both** side panels, the Operator Dashboard's included: the
Operator Dashboard has screens of its own that `operator` speaks for, but the
panel around them is chrome, and one switcher and one session serve both surfaces.

`operator` is ONE namespace for six routes because a Platform Operator looking at
any of them is looking at the Operator Dashboard — the surface, not the page, is
the unit (see above). It is also the namespace that borrows the most, and
deliberately: it reads the Payout Request statuses and the whole bank-detail
vocabulary from `payouts`, the Event statuses from `events`, and the Sales
Channel, source, Payment Method and reversal-actor words from `sales`. An
organizer and an operator on the phone about one payout must be saying one word
for its state, and the operator's screen is the second reader of every term the
organizer's screen coined — never a second coiner of it.
`shell` also owns the **role names** (`roleOrgAdmin`, `roleEventOwner`,
`roleEventStaff`) even though the team screen renders them too — they are coined
once in `CONTEXT.md` and named once here, because a role translated per screen is
how there come to be three Spanish words for Event Staff.

**No surface maps a Payout Request status to a word itself either.**
`app/payout-request-status.ts` is written the way `app/role-name.ts` is and for
the same reason: the organizer's outstanding card and request history, the
operator's queue, an Organization's request history and the request detail all
read `usePayoutRequestStatusName`, and the words live in `payouts` because the
vocabulary belongs to the domain rather than to a surface. #290 left an English
shim in `lib/payout-requests.ts` for the untranslated Operator Dashboard, tested
against the English catalog so the six states were written down twice rather than
twice-decided; #292 translated that surface and deleted the shim, along with
`waitingLabel`, `transferSentLabel`, the sentence-returning reason validators,
`fulfilmentDivergence`'s prose and `formatPaidAtDate`. **`lib/` now holds no
English at all.**

**No surface maps a role token to a word itself.** `app/role-name.ts` is the one
place that does, and every screen showing a role — the membership list, the
organization switcher, the Members card, the Event access card, and the two
`<select>`s that set a role — reads it through `useRoleName`. The role `<option>`
lists come from the same module (`MEMBER_ROLES`, `ASSIGNMENT_ROLES`), so a role
cannot be offered under a label the rest of the app does not use. A role the API
adds that nobody has translated falls back to its token with the underscore
rubbed out: an unfamiliar role reads oddly, never blank.

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
bare `Intl.*` anywhere under `app/` — the qualification "on a migrated surface"
retired with #292, which migrated the last one and deleted the two bare-`Intl`
wrappers `lib/events-api.ts` had kept alive for it (`formatPriceCents`,
`formatEventStartDate`).

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

### The payout vocabulary is the mail's

The five Payout Request notices were translated first (#285), so the Spanish for
this domain was settled in Go before it was settled here, and `payouts` follows
it rather than coining a second set of words. A notice and the screen it sends an
organizer to must read as one voice — the reader has both open.

| English         | Spanish, as `backend/internal/platform/email_content.go` says it |
| --------------- | ---------------------------------------------------------------- |
| Payout Request  | **solicitud de pago**                                            |
| Payout Profile  | **Perfil de Pagos** — which is what `payouts.profileTitle` says in Spanish, where the English says "Bank details" |
| Operator Dashboard | **Panel de Operador**                                         |
| Reason:         | **Motivo:** — `payouts.resolutionDeclined` and `resolutionFailed` |
| Note:           | **Nota:**                                                        |
| Asked by:       | **Solicitado por:**                                              |

Two balance terms had no Spanish anywhere and are coined here: **Saldo por
retirar** for the Withdrawable Balance and **Saldo pagable** for the Payable
Balance. _Saldo disponible_ is barred for the second one, for the reason
`CONTEXT.md` bars "available balance" for it in English.

The Operator Dashboard (#292) takes every one of those words rather than coining
its own, and adds only what is genuinely operator-side: **reversión** for a Sale
Reversal, **revocatoria** for a Consent Withdrawal — the word the Ecuadorian
_Formulario de Revocatoria_ an operator is holding actually uses — and **Panel de
Operador** for the dashboard itself, which is `shell.platformDescription`'s
spelling too since #292 aligned it with the mail's.

A **Payout** is _pago_ and a **Payout Request** is _solicitud de pago_, which
means the operator's "Record payout" is _Registrar el pago_ and never
_Registrar la solicitud_: recording money that already moved and answering an ask
are two different acts on the same screen, and one word for both is how an
operator records the wrong one.

## What the tests do and do not check

`lib/messages.test.ts` asserts that every catalog holds exactly the keys
`en.json` holds — both directions, so a missing key and a stray key both surface
— and that no message is empty. Every offending path is reported in one run.

It does **not** assert that the Spanish differs from the English, and cannot: a
message that is only placeholders and punctuation is correctly identical in both
catalogs, and so is a word like `Google`.
