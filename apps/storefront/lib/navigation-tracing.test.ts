import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import path from "node:path";
import test from "node:test";

/**
 * Each client navigation starts a trace of its own, with its own one in ten
 * decision (#792, ADR 0079), whichever way Next started it: a <Link> click or
 * router.push (Next's onRouterTransitionStart, "push"), router.replace
 * ("replace"), or back and forward (its popstate listener, "traverse").
 *
 * The browser SDK runs for real, in lib/navigation-tracing.fixture.ts, a
 * process of its own (see there why); each step's Math.random is the step's
 * sampling draw, 0.05 sampled and 0.9 not, at the rate of 0.1.
 */

const SAMPLED = 0.05;
const NOT_SAMPLED = 0.9;

type Step = ({ push: string } | { replace: string } | { back: true }) & { random: number };
type Navigation = { traceId: string; sampled: boolean; type: string };

function navigate(steps: Step[]): { navigations: Navigation[]; headers: string[] } {
  const fixture = path.join(import.meta.dirname, "navigation-tracing.fixture.ts");
  const out = execFileSync(
    process.execPath,
    [
      "--conditions=browser",
      "--experimental-strip-types",
      "--disable-warning=ExperimentalWarning",
      "--disable-warning=MODULE_TYPELESS_PACKAGE_JSON",
      fixture,
      JSON.stringify(steps),
    ],
    { encoding: "utf8" },
  );
  return JSON.parse(out);
}

/** The trace id and the decision a request made then carries in sentry-trace. */
function header(value: string): { traceId: string; sampled: boolean } {
  const [traceId, , sampled] = value.split("-");
  return { traceId, sampled: sampled === "1" };
}

test("navigations in a row each start a trace with its own decision", () => {
  const { navigations, headers } = navigate([
    { push: "/es/acme/events/fiesta", random: SAMPLED },
    { push: "/es", random: NOT_SAMPLED },
    { replace: "/es?q=fiesta", random: SAMPLED },
  ]);
  assert.deepEqual(
    navigations.map(({ sampled, type }) => ({ sampled, type })),
    [
      { sampled: true, type: "router.push" },
      { sampled: false, type: "router.push" },
      { sampled: true, type: "router.replace" },
    ],
  );
  assert.equal(new Set(navigations.map(({ traceId }) => traceId)).size, navigations.length);
  assert.deepEqual(headers.map(header), navigations.map(({ traceId, sampled }) => ({ traceId, sampled })));
});

test("back is one navigation, and the navigation after it starts its own trace and decision", () => {
  const { navigations, headers } = navigate([
    { push: "/es/acme/events/fiesta", random: NOT_SAMPLED },
    { back: true, random: SAMPLED },
    { push: "/es/acme/events/otra", random: NOT_SAMPLED },
    { back: true, random: NOT_SAMPLED },
    { push: "/es/acme", random: SAMPLED },
  ]);
  assert.deepEqual(
    navigations.map(({ sampled, type }) => ({ sampled, type })),
    [
      { sampled: false, type: "router.push" },
      { sampled: true, type: "router.traverse" },
      { sampled: false, type: "router.push" },
      { sampled: false, type: "router.traverse" },
      { sampled: true, type: "router.push" },
    ],
  );
  assert.equal(new Set(navigations.map(({ traceId }) => traceId)).size, navigations.length);
  // What the requests each navigation makes (its RSC fetch, the API calls it
  // causes) carry to the server: that navigation's trace and decision.
  assert.deepEqual(headers.map(header), navigations.map(({ traceId, sampled }) => ({ traceId, sampled })));
});
