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
| `checkout`     | Ticket selection, and the success / failed / stub terminals        |
| `signin`       | The Customer Session sign-in flow                                  |
| `customerArea` | A Customer's own tickets and the undo of a purchase                |
| `myInfo`       | The Customer's own details                                         |
| `errors`       | Failures that belong to no single surface, keyed by the API's code |

Surface, rather than component, is the split that survives: a component moves
between pages and gets reused by a surface that words it differently, but a
Customer looking at the Event page always reads the Event page's words. It also
keeps the diff of a copy change inside one namespace instead of scattering it.

Two levels are the limit — `checkout.success.title`, not
`checkout.terminals.success.heading.text`. A namespace that wants a third level
is really two surfaces.

## `errors` is the one namespace keyed by machines

Every other key names meaning in English. `errors` names the API's own codes,
because the API decides which failure occurred and this app only chooses the
words for it (ADR 0023). Four groups, and lookup order matters:

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

## Rules

- **Keys name meaning, not English.** `customerArea.undo.confirmTitle`, not
  `undoThisPurchaseQuestion`. The Spanish must be free to say it differently.
- **Interpolate, never concatenate.** `"{organization} logo"`, not a label
  glued to a name in JSX: word order is not the same in both languages.
- **No empty values.** An untranslated string is the English one, never `""` —
  a blank renders as a blank and nothing catches it. `lib/messages.test.ts`
  fails on both an empty value and a key that exists in one catalog only.
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
