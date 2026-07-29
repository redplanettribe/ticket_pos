import type messages from "./messages/en.json";

/**
 * What next-intl is allowed to be asked for.
 *
 * Without this, `t("shell.nope")` typechecks, builds, deploys, and renders as
 * nothing at all — a missing message is a runtime miss that next-intl reports
 * in a console nobody is reading and a gap in the page everybody is. Pointing
 * `Messages` at en.json turns every key into part of the type: a typo, a
 * renamed namespace, or a call site left behind by a deleted key all fail
 * `pnpm turbo typecheck` at the line that made the mistake.
 *
 * en.json is the source of truth on purpose. Only one catalog can define the
 * shape, and it must be the one the copy is written in; es.json is held to the
 * same keys by lib/messages.test.ts rather than by the compiler, which is what
 * lets a translation land in a later commit without breaking the build in the
 * meantime.
 *
 * `AppConfig` is next-intl v4's spelling of what v3 called the global
 * `IntlMessages` interface. Its sibling slot, `Locale`, is deliberately left
 * alone: narrowing next-intl's locale to "en" | "es" would make every page's
 * raw `params.locale` — a string off the URL, unvalidated until the layout
 * checks it — a type error at `setRequestLocale`. lib/locale.ts already owns
 * that narrowing, through `toAppLocale`, at the points that have validated it.
 */
declare module "next-intl" {
  interface AppConfig {
    Messages: typeof messages;
  }
}
