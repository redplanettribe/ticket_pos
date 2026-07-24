import { expect, test } from "@playwright/test";

// The Staff production image is a Next standalone bundle, and Staff owns the
// session itself: its middleware reads the httpOnly `ticket_pos_session` cookie
// to decide whether to redirect to sign-in, and its route handlers proxy to the
// Go API over the Compose network. Both of those behave differently in a
// production build than under `next dev` - middleware runs from the traced
// bundle, and the cookie is flagged `secure`. So the assertions here follow the
// signed-out redirect rather than merely checking that a page renders.
//
// The `secure` flag does not block the parity stack: browsers treat
// http://localhost as a trustworthy origin, so a real sign-in against this
// image stores and replays the cookie (see README, "Frontend production
// images"). Sign-in itself is not asserted here because it needs the emailed
// passcode, which only exists in the API container's logs.
//
// Requires the parity stack: `make prod` (see README).

test("Staff redirects a signed-out visitor to sign-in from the parity stack", async ({ page }) => {
  await page.goto("/");

  // Middleware, running inside the standalone server, saw no session cookie.
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: /sign in/i })).toBeVisible();
  await expect(page.getByLabel(/email/i)).toBeVisible();
});

test("Staff redirects a signed-out visitor away from a deep link", async ({ page }) => {
  await page.goto("/events");

  await expect(page).toHaveURL(/\/login$/);
});

test("Staff rejects a stale session cookie and clears it", async ({ page, context }) => {
  // A cookie that is well-formed but not a session the API knows. Middleware
  // has to call its own /api/auth/session route, which proxies to the Go API
  // using the runtime API_URL - the whole server-side path in one request.
  await context.addCookies([
    {
      name: "ticket_pos_session",
      value: "not-a-real-session-token",
      domain: "localhost",
      path: "/",
      httpOnly: true,
    },
  ]);

  await page.goto("/");

  await expect(page).toHaveURL(/\/login$/);
  const cookies = await context.cookies();
  expect(
    cookies.find((cookie) => cookie.name === "ticket_pos_session")?.value ?? "",
    "middleware should clear a session the API rejected",
  ).toBe("");
});

test("Staff reaches the API from inside the parity network", async ({ request }) => {
  // The BFF route handler proxies to the Go API over the Compose network. A
  // structured API answer (rather than a 502 or a 500 from an unreachable
  // host) is the evidence that the standalone server can call it.
  const response = await request.get("/api/auth/session", {
    headers: { cookie: "ticket_pos_session=not-a-real-session-token" },
  });

  const envelope = (await response.json()) as {
    data: unknown;
    error: { code: string } | null;
  };
  // 401 from the Go API, not 500/502 from a handler that could not reach it.
  expect(response.status()).toBe(401);
  expect(envelope.error?.code).not.toBe("INTERNAL_ERROR");
  expect(envelope.data).toBeNull();
});

test("Staff shows the Multiticketing brand on the sign-in page", async ({ page }) => {
  // The authed sidebar lockup needs a real session (an emailed passcode the
  // parity stack cannot supply), so the reachable branded surface is the
  // sign-in page, where AuthCard renders the Multiticketing wordmark.
  await page.goto("/login");

  await expect(page).toHaveTitle(/Multiticketing/);
  await expect(page.getByText("Multiticketing", { exact: true }).first()).toBeVisible();
  expect(await page.locator('link[rel="icon"]').count()).toBeGreaterThan(0);
});

test("Staff serves its client bundle in the parity stack", async ({ page }) => {
  const failures: string[] = [];
  page.on("response", (response) => {
    if (response.url().includes("/_next/static/") && !response.ok()) {
      failures.push(`${response.status()} ${response.url()}`);
    }
  });

  await page.goto("/login");

  // The sign-in form is a client component built on @ticket-pos/ui; it only
  // reacts if the traced client chunks were served and hydrated.
  await page.getByLabel(/email/i).fill("parity-smoke@example.com");
  await expect(page.getByLabel(/email/i)).toHaveValue("parity-smoke@example.com");

  expect(failures, "static chunks missing from the standalone image").toEqual([]);
});
