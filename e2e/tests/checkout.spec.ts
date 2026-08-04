import { test, expect, type Page } from "@playwright/test";

// Storefront checkout journeys through the stub Payment Provider (issue #84),
// against the dev stack (`make dev`). These are thin cross-runtime journeys —
// browser → Storefront BFF → Go API → Postgres and back through the stub's
// redirect legs; the business rules behind them live in
// backend/integration/checkout_test.go and must not be re-asserted here.
//
// Data: the dev-seed migrations (backend/migrations/002, 008) provide the
// published, discoverable Event this spec buys from. Capacity is 200, so
// repeated runs never sell it out; declined Payments never consume capacity.

// Every Storefront page is served under a Locale carried in the URL, so these
// journeys walk the English Storefront by name rather than leaning on the
// redirect an address naming no language gets. The stub Payment Provider's own
// page is the exception: the API builds that URL from a fixed constant, so the
// buyer arrives at it unprefixed and is redirected — which is asserted below.
const LOCALE = "en";
const EVENT_PATH = `/${LOCALE}/demo-venue/events/midnight-synth-live`;
const EVENT_NAME = "Midnight Synth Live";

// One General Admission ticket, priced at $35.00 by the seed and quoted all in
// at $39.03 under the Event's default 'pass_on' Fee Handling: 3500¢ + 350¢
// Platform Fee + 53¢ Fee IVA (ADR 0014). The Customer sees this one number
// everywhere — picker, cart, and the provider's payment page.
const GA_TICKET = "General Admission";
const GA_PRICE = "$39.03";

// A cédula with a valid province prefix and check digit, shared by both
// journeys — the Tax ID is not an identity, so every run may assert the same one.
const GA_TAX_ID = "1712345675";

// Sale Confirmation references look like TP-J7K2QX9M (base32).
const CONFIRMATION_REF = /^TP-[A-Z2-7]+$/;

async function selectOneTicketAndOpenCheckout(page: Page) {
  await page.goto(EVENT_PATH);
  await expect(page.getByRole("heading", { level: 1, name: EVENT_NAME })).toBeVisible();

  await page.getByRole("button", { name: `Add one ${GA_TICKET} ticket` }).click();
  await expect(page.getByTestId("selection-total")).toHaveText(GA_PRICE);

  await page.getByRole("button", { name: "Get tickets" }).click();
  await expect(page.getByRole("dialog", { name: "Checkout" })).toBeVisible();
}

async function fillCheckoutForm(page: Page, email: string) {
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("First name").fill("Ada");
  await page.getByLabel("Last name").fill("Lovelace");
  // The Tax ID is as required as the email (#98, ADR 0016). A real cédula: the
  // API validates the check digit and refuses the checkout otherwise, so an
  // arbitrary ten digits would fail this journey at begin-checkout.
  await page.getByLabel("ID type").selectOption("cedula");
  await page.getByLabel("ID number").fill(GA_TAX_ID);
  await page.getByRole("button", { name: "Continue to payment" }).click();

  // The stub Payment Provider's interstitial: a top-level page showing the
  // amount, exactly where a real provider's hosted payment page would be.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/checkout/stub\\?`));
  await expect(page.getByTestId("stub-amount")).toHaveText(GA_PRICE);
}

test("a Customer buys a ticket through the stub provider and lands on the confirmation", async ({
  page,
}) => {
  await selectOneTicketAndOpenCheckout(page);
  // Unique per run: each journey is its own guest Customer.
  await fillCheckoutForm(page, `e2e-approve-${Date.now()}@example.com`);

  await page.getByRole("link", { name: "Approve payment" }).click();

  // Still in the Locale the buyer was reading in when they started.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/checkout/success\\?ref=`));
  await expect(page.getByRole("heading", { name: "You're going!" })).toBeVisible();

  // The confirmation reference is the artifact of the whole journey: it only
  // exists if the Payment was approved and the Ticket Sale was committed.
  const ref = (await page.getByTestId("confirmation-ref").innerText()).trim();
  expect(ref).toMatch(CONFIRMATION_REF);

  // The checkout-context cookie survived the provider round trip: the success
  // page can point back at the Event that was bought.
  await expect(page.getByRole("link", { name: "Back to the event" })).toHaveAttribute(
    "href",
    EVENT_PATH,
  );

  // Signing in from here must not cost the buyer their confirmation. The
  // reference lives in this URL and in no cookie, session or API the Storefront
  // could ask, so a `next` built from the path alone sent them back to a
  // confirmation with nothing to confirm — a 404 until the page learned to go
  // home instead. Asserted on the link rather than by signing in, because the
  // passcode needs a mailbox this suite does not have.
  // Exact, because the page offers two: this one, and the undo panel's "Sign in
  // to undo", which carries a `next` pointing at the ticket rather than at the
  // confirmation. A substring match resolves to both.
  const signIn = page.getByRole("link", { name: "Sign in", exact: true });
  const next = new URL(
    (await signIn.getAttribute("href")) ?? "",
    "http://localhost",
  ).searchParams.get("next");
  expect(next).toBe(`/checkout/success?ref=${ref}`);
});

test("a declined payment lands on the failure page and retry returns to the Event", async ({
  page,
}) => {
  await selectOneTicketAndOpenCheckout(page);
  await fillCheckoutForm(page, `e2e-decline-${Date.now()}@example.com`);

  await page.getByRole("link", { name: "Decline payment" }).click();

  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/checkout/failed`));
  await expect(page.getByRole("heading", { name: "Payment not completed" })).toBeVisible();
  await expect(page.getByText("You haven't been charged", { exact: false })).toBeVisible();

  // Retry goes back to the event page, where checking out again begins a
  // fresh Payment — steppers reset, ready to sell.
  await page.getByRole("link", { name: "Try again" }).click();
  await expect(page).toHaveURL(EVENT_PATH);
  await expect(page.getByRole("heading", { level: 1, name: EVENT_NAME })).toBeVisible();
  await expect(page.getByRole("button", { name: "Get tickets" })).toBeDisabled();
});

test("the confirmation page without a reference sends the visitor home, not to a 404", async ({
  page,
}) => {
  // The Sale Confirmation reference lives in this URL and nowhere else the page
  // can reach, so an address arriving without one has nothing to show. It used
  // to answer 404 — a dead end handed to somebody who has usually just paid.
  //
  // Asserted through the browser rather than the lib seam because there is no
  // lib seam: the decision is three lines inside a Server Component, and what
  // could break it is the wiring around it — a redirect helper that emits an
  // unprefixed path, or a middleware matcher that never admits the address.
  const response = await page.goto(`/${LOCALE}/checkout/success`);

  // The final answer is the home page, in the Locale that was asked for: the
  // redirect must not drop the language on the way.
  expect(response?.status()).toBe(200);
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}$`));
});
