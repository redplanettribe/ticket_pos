import { dirname } from "path";
import { fileURLToPath } from "url";
import { FlatCompat } from "@eslint/eslintrc";
import i18next from "eslint-plugin-i18next";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const compat = new FlatCompat({
  baseDirectory: __dirname,
});

/**
 * Attribute names whose values are never copy.
 *
 * Two things to know before adding to this list:
 *
 *  - On a **native DOM tag** the plugin already checks only `placeholder`,
 *    `alt`, `aria-label`, `value` and `title` — every other attribute of an
 *    `<input>` or a `<div>` is allowed without being named here. This list
 *    exists almost entirely for **our own components**, where the plugin has no
 *    idea which props are words and which are tokens, so it checks all of them.
 *  - Excluding a name excludes it everywhere, on every element, and excludes the
 *    whole expression inside it — `variant={x ? "a" : "b"}` goes quiet too.
 *    So exclude names that could not carry a sentence, not names that happen not
 *    to today.
 */
const NON_COPY_ATTRIBUTES = [
  // The plugin's own defaults, restated because supplying `jsx-attributes`
  // replaces them wholesale rather than merging.
  "className",
  "styleName",
  "style",
  "type",
  "key",
  "id",
  "width",
  "height",

  // Appearance and shape tokens on @ticket-pos/ui — `variant="destructive"`,
  // `size="sm"`, `shape="tile"`. Each is a closed union in that package, so a
  // wrong value is already a type error and a translated value is nonsense.
  "variant",
  "size",
  "shape",
  "tone",
  "align",
  "side",
  "orientation",
  // Which column a table sorts on, which series a chart draws: API field names,
  // not headings. The heading beside them comes from the catalog.
  "field",
  "measure",

  // Addresses and identity. `activePath` is which nav entry the shell should
  // light up; `name`/`htmlFor` wire a label to an input.
  "href",
  "activePath",
  "action",
  "method",
  "target",
  "rel",
  "name",
  "htmlFor",
  "src",
  "form",
  "slot",
  // `value` is a token on every use in this app: `<option value="active">`, and
  // radio values that a reducer compares against. The word a reader actually
  // sees is the option's child text. NOTE: this is the one exclusion that could
  // hide copy, because `<input type="submit" value="Save">` renders its value —
  // this app has no such input, and a submit button here is a <Button> with
  // children.
  "value",

  // How a keyboard or an autofiller should behave. `inputMode="decimal"`,
  // `autoComplete="one-time-code"` — instructions to the browser.
  "inputMode",
  "autoComplete",
  "autoCapitalize",
  "enterKeyHint",
  "accept",
  "pattern",
  "step",
  "min",
  "max",
  "minLength",
  "maxLength",
  "lang",
  "dir",
  "spellCheck",

  // ARIA attributes that take a token from a fixed vocabulary. `aria-label`,
  // `aria-valuetext` and friends are deliberately NOT here: those are read aloud
  // to a person and are exactly as much copy as visible text is.
  "role",
  "aria-(hidden|current|expanded|controls|labelledby|describedby|live|atomic|haspopup|pressed|selected|checked|disabled|busy|modal|orientation|sort)",

  // Test hooks and anything else hung off the element for a machine to find.
  "data-.*",
];

/**
 * Strings that are not sentences in any language.
 *
 * Also restating the plugin's defaults, for the same merge reason as above.
 */
const NON_COPY_WORDS = [
  // Nothing with a letter in it: digits, punctuation, currency marks, the
  // arrows a sort header and a disclosure triangle draw ("▲", "▾"), "·", "0.00".
  // A string a translator would hand straight back.
  /^[^\p{L}]+$/u,
  // SCREAMING_SNAKE tokens.
  "[A-Z_-]+",
  // HTML entities — "&nbsp;", "&mdash;".
  "&\\w+;",
  // A URL is an address, never a sentence. This covers the example URL used as
  // a format hint in the external-registration field the same way the numeric
  // "0.00" hints are covered above.
  "https?://\\S+",
];

const eslintConfig = [
  ...compat.extends("next/core-web-vitals", "next/typescript"),
  {
    /**
     * A literal string left in a staff component is a lint error (#293).
     *
     * This closes the one hole the other two guards cannot see. `global.d.ts`
     * types en.json, so the compiler catches a key that does not exist;
     * `lib/messages.test.ts` catches a key es.json is missing. Neither can see a
     * sentence typed straight into JSX: no key was ever asked for, so the
     * compiler is content, and both catalogs still agree, so the parity test is
     * content. Nobody reading English notices either. Only a Spanish reader
     * finds it, in production.
     *
     * `error`, not `warn`, deliberately: a warning in a tree this size is a line
     * of scrollback nobody reads, and `next lint` exits 0 on it. This has to
     * fail `pnpm turbo lint`, which is what CI runs.
     *
     * Scope is every `.tsx` in the app, which today is all of `app/` and nothing
     * else. Deliberately not `app/**` alone: a `components/` directory added
     * later is somewhere `next lint` already looks, and it should not be a place
     * the guard does not. `lib/` is `.ts` only and holds no copy by rule
     * (messages/README.md), and `mode` below means only JSX is inspected anyway.
     */
    files: ["**/*.tsx"],
    plugins: { i18next },
    rules: {
      "i18next/no-literal-string": [
        "error",
        {
          // JSX text AND JSX attributes, which is the whole of what a staff
          // screen renders. Not "all": in "all" mode every string anywhere in
          // the file is a violation — an import path, a fetch URL, a role token
          // compared in a condition — and the allowlist needed to quiet that
          // down would be larger than the rule.
          mode: "jsx-only",
          "jsx-attributes": { exclude: NON_COPY_ATTRIBUTES },
          words: { exclude: NON_COPY_WORDS },
          "jsx-components": { exclude: ["Trans"] },
          /**
           * An argument to a function call is not rendered text.
           *
           * Copy reaches a screen through JSX; a string handed to a function is
           * a token — `setFeeHandling("absorb")`, `t("payouts.title")`,
           * `formatDate(value, PLATFORM_TIME_ZONE, locale)`. Listing the safe
           * callees instead would mean adding every new `setX("token")` in an
           * onClick to this file forever, which is how a rule earns its way
           * back out of a config.
           *
           * `include` wins over `exclude`, so the exceptions are the calls that
           * genuinely do put a sentence in front of somebody. The plugin matches
           * these with an optional dotted prefix, so `window.alert` is covered
           * by `alert`.
           */
          callees: { include: ["alert", "confirm", "prompt"], exclude: [".*"] },
          "object-properties": {
            // A nav entry's address, sitting in an object literal.
            exclude: ["[A-Z_-]+", "href"],
          },
        },
      ],
    },
  },
];

export default eslintConfig;
