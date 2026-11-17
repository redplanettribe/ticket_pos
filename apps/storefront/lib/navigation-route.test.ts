import assert from "node:assert/strict";
import { readdirSync } from "node:fs";
import test from "node:test";

import { LOCALE_PAGES, nameNavigation, navigationRoute } from "./navigation-route.ts";

test("a navigation is named by its page, never by an Organization or Event slug", () => {
  const cases: Array<[string, string]> = [
    ["/es", "/[locale]"],
    ["/en/", "/[locale]"],
    ["/es/acme", "/[locale]/[orgSlug]"],
    ["/es/acme/events/fiesta-2026", "/[locale]/[orgSlug]/events/[eventSlug]"],
    ["/en/acme/events/fiesta-2026/register", "/[locale]/[orgSlug]/events/[eventSlug]/register"],
    ["/es/checkout/success", "/[locale]/checkout/success"],
    ["/es/tickets", "/[locale]/tickets"],
    ["/en/signin", "/[locale]/signin"],
  ];
  for (const [pathname, route] of cases) assert.equal(navigationRoute(pathname), route, pathname);
});

test("a path that is not under a locale is left as it is", () => {
  assert.equal(navigationRoute("/"), "/");
  assert.equal(navigationRoute("/checkout/return"), "/checkout/return");
});

test("every static page under app/[locale] is named, and nothing else", () => {
  const pages = readdirSync(new URL("../app/[locale]/", import.meta.url), { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !entry.name.startsWith("["))
    .map((entry) => entry.name)
    .sort();
  assert.deepEqual([...LOCALE_PAGES].sort(), pages);
});

test("a navigation transaction is renamed by its page as it is sent, and nothing else is", () => {
  const navigation = {
    transaction: "/es/acme/events/fiesta-2026",
    transaction_info: { source: "url" },
    contexts: { trace: { op: "navigation", trace_id: "t" } },
  };
  assert.deepEqual(nameNavigation(navigation), {
    ...navigation,
    transaction: "/[locale]/[orgSlug]/events/[eventSlug]",
    transaction_info: { source: "route" },
  });
  assert.equal(navigation.transaction, "/es/acme/events/fiesta-2026");

  const other = { transaction: "/es/acme", contexts: { trace: { op: "http.client" } } };
  assert.equal(nameNavigation(other), other);
  assert.deepEqual(nameNavigation({ contexts: { trace: { op: "navigation" } } }), { contexts: { trace: { op: "navigation" } } });
});
