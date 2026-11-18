import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { LOCALES } from "@ticket-pos/locale";

/**
 * The catalogs are held to the same shape, because nothing else is holding them.
 *
 * en.json is typed — global.d.ts makes every key part of `AppConfig`, so a call
 * site that asks for a key it does not have fails to compile. es.json is typed
 * by nothing at all: it is loaded by filename at request time (i18n/request.ts),
 * and a key missing from it is not a build error, it is a Spanish page with a
 * hole in it that only a Spanish-reading Member ever sees — and on this app,
 * unlike the Storefront, nobody can reach that page by editing a URL to check.
 *
 * So the compiler guards English and this file guards the rest. It asserts the
 * two things a translation can get wrong on its own — a key that exists in one
 * catalog only, and a value that is present but empty — and it reports the
 * offending paths rather than a boolean, because "es.json does not match" is not
 * something anyone can act on at 6pm.
 *
 * It deliberately does NOT assert that the Spanish differs from the English. A
 * message that is only placeholders and punctuation, or a name like "Google", is
 * correctly identical in both catalogs, so an assertion like that would fail on
 * the translations that are right.
 *
 * A direct port of the Storefront's lib/messages.test.ts, minus its Preset Tag
 * check: Preset Tags are a Customer-facing surface and no staff screen renders
 * one. Two files rather than one shared helper, because the two catalogs are
 * deliberately separate (ADR 0041) and a shared test would be the first step
 * back towards a shared catalog.
 */

type Catalog = { [key: string]: string | Catalog };

function load(locale: string): Catalog {
  return JSON.parse(
    readFileSync(new URL(`../messages/${locale}.json`, import.meta.url), "utf8"),
  ) as Catalog;
}

/**
 * Every message in a catalog as "namespace.key", nesting and all.
 *
 * Flattening is what makes a namespace that turned into a string (or the other
 * way round) show up as an ordinary key mismatch instead of a silently mistyped
 * comparison.
 */
function flatten(catalog: Catalog, prefix = ""): Map<string, string> {
  const messages = new Map<string, string>();
  for (const [key, value] of Object.entries(catalog)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") {
      messages.set(path, value);
    } else {
      for (const [nested, message] of flatten(value, path)) {
        messages.set(nested, message);
      }
    }
  }
  return messages;
}

const catalogs = new Map(LOCALES.map((locale) => [locale, flatten(load(locale))]));
const [reference, ...others] = LOCALES;
const referenceMessages = catalogs.get(reference)!;

test("every catalog holds exactly the keys en.json holds", () => {
  // Every offending path is collected before anything is asserted, so one run
  // names all of them. Asserting per locale as they are found would report the
  // first missing key and hide the other nine behind it — which matters most in
  // exactly the change this test exists for: a whole surface being migrated at
  // once.
  const problems: string[] = [];
  for (const locale of others) {
    const messages = catalogs.get(locale)!;
    for (const key of referenceMessages.keys()) {
      if (!messages.has(key)) problems.push(`${locale}.json is missing ${key}`);
    }
    for (const key of messages.keys()) {
      // A key only the translation has is copy nothing renders: en.json is the
      // typed source of truth, so no call site can ask for it.
      if (!referenceMessages.has(key)) problems.push(`${locale}.json has a stray ${key}`);
    }
  }

  assert.deepEqual(problems, [], `\n${problems.join("\n")}\n`);
});

test("no message is empty", () => {
  const problems: string[] = [];
  for (const locale of LOCALES) {
    for (const [key, message] of catalogs.get(locale)!) {
      // Trimmed, because a value of " " is a blank label with extra steps.
      if (message.trim() === "") problems.push(`${locale}.json is empty at ${key}`);
    }
  }

  assert.deepEqual(
    problems,
    [],
    `\n${problems.join("\n")}\nAn untranslated message is the English one, never "".\n`,
  );
});

/**
 * Platform Trends (#821) is a section of the Operator Dashboard's Overview, so
 * its copy is the `operator.trends...` keys. Two things are asked of it that
 * the catalogs at large do not yet meet: no em dash, and the full terms Active
 * Customer and Active Organization, since Active Event and Active Member mean
 * something else.
 */
function platformTrendsCopy(locale: (typeof LOCALES)[number]): [string, string][] {
  return [...catalogs.get(locale)!].filter(([key]) => key.startsWith("operator.trends"));
}

test("the Platform Trends copy holds no em dash", () => {
  for (const locale of LOCALES) {
    const copy = platformTrendsCopy(locale);
    assert.ok(copy.length > 0, `${locale}.json has no Platform Trends copy`);
    for (const [key, message] of copy) {
      assert.ok(!message.includes("\u2014"), `${locale}.json ${key}`);
    }
  }
});

/**
 * The full term in each language, and the place a bare "active" would stand.
 * English puts the adjective before its noun and Spanish after it, so the rule
 * is the language's own. Keyed by locale, so a new catalog does not compile
 * until it says what its full terms are.
 */
const ACTIVE_TERMS: Record<
  (typeof LOCALES)[number],
  { customers: string; organizations: string; bare: RegExp }
> = {
  en: {
    customers: "Active Customers",
    organizations: "Active Organizations",
    // "Active" is always followed by the thing that is active.
    bare: /\bactive\b(?!\s+(?:customers?|organizations?))/i,
  },
  es: {
    customers: "Clientes activos",
    organizations: "Organizaciones activas",
    // "activo" always follows the thing that is active, and agrees with it.
    // The word's end is spelled out because \b does not know an accented
    // letter for a letter.
    bare: /(?<!\bclientes?\s+)\bactivos?(?![\p{L}\p{N}])|(?<!\borganizaci(?:ón|ones)\s+)\bactivas?(?![\p{L}\p{N}])/iu,
  },
};

/** The bare "active" in a message, or null when every use names what is active. */
function bareActive(locale: (typeof LOCALES)[number], message: string): string | null {
  return ACTIVE_TERMS[locale].bare.exec(message)?.[0] ?? null;
}

test("a bare active is found in each language, and the full terms are not", () => {
  for (const full of [
    "Active Customers",
    "An Active Organization sold or had a Member working.",
    "Activity figures are frozen.",
  ]) {
    assert.equal(bareActive("en", full), null, full);
  }
  for (const bare of ["Days active", "Active that day", "Active Events", "Customers active"]) {
    assert.notEqual(bareActive("en", bare), null, bare);
  }

  for (const full of [
    "Clientes activos",
    "Los clientes activos aún no se registran.",
    "Organizaciones activas",
    "Una organización activa vendió.",
    "Un cliente activo",
    "Las cifras de actividad quedan fijas.",
  ]) {
    assert.equal(bareActive("es", full), null, full);
  }
  for (const bare of [
    "Días activos",
    "Activos ese día",
    "Eventos activos",
    "Activa ese día",
    "Miembros activos",
    // The adjective of the other term: it does not agree, so it is not the term.
    "Organizaciones activos",
    "Clientes activas",
  ]) {
    assert.notEqual(bareActive("es", bare), null, bare);
  }
});

test("the Platform Trends copy says Active Customer and Active Organization in full", () => {
  for (const locale of LOCALES) {
    const copy = new Map(platformTrendsCopy(locale));
    const terms = ACTIVE_TERMS[locale];
    assert.equal(copy.get("operator.trendsActiveCustomers"), terms.customers);
    assert.equal(copy.get("operator.trendsActiveOrganizations"), terms.organizations);
    for (const [key, message] of copy) {
      assert.equal(
        bareActive(locale, message),
        null,
        `${locale}.json ${key} says "active" without Customer or Organization: ${message}`,
      );
    }
  }
});

/**
 * House Organization is "Organización propia" wherever the Spanish catalog
 * names one (operator.houseTitle and the rest). Platform Trends reads that
 * term and does not coin a second one (messages/README.md).
 */
test("the Spanish Platform Trends copy names a House Organization as the rest of the catalog does", () => {
  const copy = new Map(platformTrendsCopy("es"));
  assert.match(copy.get("operator.trendsDescription") ?? "", /organizaciones propias/);
  for (const [key, message] of copy) {
    assert.ok(!/de la casa/i.test(message), `es.json ${key}: ${message}`);
  }
});
