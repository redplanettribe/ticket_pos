import assert from "node:assert/strict";
import test from "node:test";

import { pathWithQuery } from "./current-path.ts";

test("the link points at the page the visitor is on, not at the explorer", () => {
  assert.equal(pathWithQuery("/acme/events/gala"), "/acme/events/gala");
});

test("the query survives the switch", () => {
  // The explorer's filters live in the query, so dropping it would answer
  // "read this in English" with an unfiltered page.
  assert.equal(pathWithQuery("/", "q=gala&city=quito"), "/?q=gala&city=quito");
  assert.equal(pathWithQuery("/tickets", "page=2"), "/tickets?page=2");
});

test("a query is accepted with or without its leading '?'", () => {
  // `useSearchParams().toString()` gives one form and `location.search` the
  // other; neither may produce "/tickets??page=2".
  assert.equal(pathWithQuery("/tickets", "?page=2"), "/tickets?page=2");
});

test("an empty query leaves no dangling '?'", () => {
  for (const search of ["", "?", null, undefined]) {
    assert.equal(pathWithQuery("/tickets", search), "/tickets");
  }
});

test("the root path keeps its slash for the prefixer to collapse", () => {
  // next-intl's prefixer turns "/" into "/es" and "/?q=x" into "/es?q=x"; it
  // needs the slash to be there to do either.
  assert.equal(pathWithQuery("/"), "/");
  assert.equal(pathWithQuery("/", "q=x"), "/?q=x");
});

test("a path that lost its leading slash is made root-relative again", () => {
  // A relative href would resolve against the current directory and quietly
  // produce /es/acme/en/acme rather than /en/acme.
  assert.equal(pathWithQuery("acme"), "/acme");
  assert.equal(pathWithQuery(""), "/");
});

test("the Sale Confirmation reference survives, because it is the page", () => {
  // The checkout success page renders from ?ref= and holds it nowhere else, so
  // a sign-in link that dropped the query would send the buyer back to a
  // confirmation with nothing to confirm.
  assert.equal(
    pathWithQuery("/checkout/success", "ref=TP-J7K2QX9M"),
    "/checkout/success?ref=TP-J7K2QX9M",
  );
});
