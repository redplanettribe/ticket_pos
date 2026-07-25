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

const EVENT_PATH = "/demo-venue/events/midnight-synth-live";
const EVENT_NAME = "Midnight Synth Live";

// One General Admission ticket at $35 (3500 cents, whole-dollar → "$35").
const GA_TICKET = "General Admission";
const GA_PRICE = "$35";

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
  await page.getByRole("button", { name: "Continue to payment" }).click();

  // The stub Payment Provider's interstitial: a top-level page showing the
  // amount, exactly where a real provider's hosted payment page would be.
  await expect(page).toHaveURL(/\/checkout\/stub\?/);
  await expect(page.getByTestId("stub-amount")).toHaveText(GA_PRICE);
}

test("a Customer buys a ticket through the stub provider and lands on the confirmation", async ({
  page,
}) => {
  await selectOneTicketAndOpenCheckout(page);
  // Unique per run: each journey is its own guest Customer.
  await fillCheckoutForm(page, `e2e-approve-${Date.now()}@example.com`);

  await page.getByRole("link", { name: "Approve payment" }).click();

  await expect(page).toHaveURL(/\/checkout\/success\?ref=/);
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
});

test("a declined payment lands on the failure page and retry returns to the Event", async ({
  page,
}) => {
  await selectOneTicketAndOpenCheckout(page);
  await fillCheckoutForm(page, `e2e-decline-${Date.now()}@example.com`);

  await page.getByRole("link", { name: "Decline payment" }).click();

  await expect(page).toHaveURL(/\/checkout\/failed/);
  await expect(page.getByRole("heading", { name: "Payment not completed" })).toBeVisible();
  await expect(page.getByText("You haven't been charged", { exact: false })).toBeVisible();

  // Retry goes back to the event page, where checking out again begins a
  // fresh Payment — steppers reset, ready to sell.
  await page.getByRole("link", { name: "Try again" }).click();
  await expect(page).toHaveURL(EVENT_PATH);
  await expect(page.getByRole("heading", { level: 1, name: EVENT_NAME })).toBeVisible();
  await expect(page.getByRole("button", { name: "Get tickets" })).toBeDisabled();
});
