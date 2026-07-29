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

`es.json` currently holds the English values verbatim. That is deliberate and
temporary — the keys are what the app depends on, and the Spanish is written in
one pass later. The parity test therefore never asserts that the two differ.
