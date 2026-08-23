import { test, expect, type Page } from "@playwright/test";

import { readPasscode } from "./support/passcode";
import { signInAsCustomer } from "./support/sign-in";

// Storefront checkout journeys through the stub Payment Provider (issue #84),
// against the dev stack (`make dev`). These are thin cross-runtime journeys —
// browser → Storefront BFF → Go API → Postgres and back through the stub's
// redirect legs; the business rules behind them live in
// backend/integration/checkout_test.go and must not be re-asserted here.
//
// EVERY JOURNEY HERE BEGINS SIGNED IN, because checkout does (ADR 0054, #385).
// They were guest journeys until that ticket and were rewritten rather than
// deleted: what they cover — a full purchase, a declined Payment, which consent
// boxes a dialog draws — is unchanged, and only who is doing it has. The wall
// itself, and the round trip it sends a buyer on, is the first spec below.
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
const EVENT_SLUG_PATH = "/demo-venue/events/midnight-synth-live";
const EVENT_PATH = `/${LOCALE}${EVENT_SLUG_PATH}`;
const EVENT_NAME = "Midnight Synth Live";

// One General Admission ticket, priced at $35.00 by the seed and quoted all in
// at $39.03 under the Event's default 'pass_on' Fee Handling: 3500¢ + 350¢
// Platform Fee + 53¢ Fee IVA (ADR 0014). The Customer sees this one number
// everywhere — picker, cart, and the provider's payment page.
const GA_TICKET = "General Admission";
const GA_PRICE = "$39.03";
// Its id, as the seed fixes it (backend/migrations/008). Named here because the
// basket travels in the ADDRESS BAR and this spec asserts the address itself —
// which is the point of putting a selection in a URL rather than in storage: it
// is readable, and readable is testable.
const GA_TICKET_TYPE_ID = "d0000000-0000-4000-8000-000000000001";

// A cédula with a valid province prefix and check digit, shared by every
// journey — the Tax ID is not an identity, so every run may assert the same one.
const GA_TAX_ID = "1712345675";

// Sale Confirmation references look like TP-J7K2QX9M (base32).
const CONFIRMATION_REF = /^TP-[A-Z2-7]+$/;

async function selectOneTicket(page: Page) {
  await page.goto(EVENT_PATH);
  await expect(page.getByRole("heading", { level: 1, name: EVENT_NAME })).toBeVisible();

  await page.getByRole("button", { name: `Add one ${GA_TICKET} ticket` }).click();
  await expect(page.getByTestId("selection-total")).toHaveText(GA_PRICE);
}

async function selectOneTicketAndOpenCheckout(page: Page) {
  await selectOneTicket(page);
  await page.getByRole("button", { name: "Get tickets" }).click();
  await expect(page.getByRole("dialog", { name: "Checkout" })).toBeVisible();
}

/**
 * The dialog as a signed-in buyer meets it: no email field, the address stated
 * back at them, and the name asked for because signing in mints a Customer with
 * none.
 */
async function fillCheckoutFormAndPay(page: Page, email: string) {
  const dialog = page.getByRole("dialog", { name: "Checkout" });

  // The load-bearing absence of ADR 0054. While the field exists the mistake —
  // a sale addressed to a typo or to somebody else's inbox — is expressible.
  await expect(dialog.getByLabel("Email", { exact: true })).toHaveCount(0);

  // And the address in its place, prominent rather than fine print: this is the
  // whole mitigation for a shared machine, since a Customer Session satisfies
  // the wall for its full life with no re-proof at the till.
  await expect(page.getByTestId("checkout-identity")).toContainText(email);

  await dialog.getByLabel("First name").fill("Ada");
  await dialog.getByLabel("Last name").fill("Lovelace");
  // The Tax ID is required on this Sales Channel (#98, ADR 0016). A real
  // cédula: the API validates the check digit and refuses the checkout
  // otherwise, so an arbitrary ten digits would fail this journey at
  // begin-checkout.
  await dialog.getByLabel("ID type").selectOption("cedula");
  await dialog.getByLabel("ID number").fill(GA_TAX_ID);

  // No consent boxes and no pay button held behind one. The buyer answered
  // everything at sign-in, which is where consent is now asked — once, in one
  // place (#254, ADR 0054). A dialog that re-asked would be the bug.
  const pay = page.getByRole("button", { name: "Continue to payment" });
  await expect(pay).toBeEnabled();
  await pay.click();

  // The stub Payment Provider's interstitial: a top-level page showing the
  // amount, exactly where a real provider's hosted payment page would be.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/checkout/stub\\?`));
  await expect(page.getByTestId("stub-amount")).toHaveText(GA_PRICE);
}

test("browsing an Event stays anonymous, and the wall stands at Buy", async ({ page }) => {
  // The whole of #385 in one journey, and it can only be seen from a browser:
  // every hop of it is a redirect, a query parameter or a cookie.
  const email = `e2e-wall-${Date.now()}@example.com`;

  // Signed out, the Event page, its Ticket Types, its prices and its steppers
  // all work. Nothing here gates discovery (ADR 0002, ADR 0037).
  await selectOneTicket(page);

  // Buy is a LINK for a visitor with no session, not a button that opens a
  // dialog: the wall stands at one control and crosses a page boundary, so the
  // bad news arrives before the buyer has typed anything.
  const buy = page.getByRole("link", { name: "Get tickets" });
  await expect(buy).toBeVisible();
  await buy.click();

  // On the sign-in page, carrying the basket in the open where the server can
  // see it — had it been stashed in browser storage there would be nothing here
  // to look at.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/signin\\?`));
  const next = new URL(page.url()).searchParams.get("next");
  expect(next).toBe(`${EVENT_SLUG_PATH}?sel=${GA_TICKET_TYPE_ID}:1&checkout=1`);

  // And it says WHY, before a single field.
  await expect(page.getByText("Sign in to buy your tickets", { exact: false })).toBeVisible();

  // Finish by passcode, from the form they were sent to.
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send passcode" }).click();
  await expect(page.getByLabel("Passcode")).toBeVisible();
  const code = readPasscode(email);
  test.skip(
    code === null,
    "no passcode in the API log — this journey needs the dev stack (`make dev`)",
  );
  await page.getByLabel("Passcode").fill(code as string);
  await page.getByRole("button", { name: "Sign in" }).click();
  // A brand-new address always meets the consent step, which is exactly where
  // a first-time buyer answers the boxes — and why the dialog below draws none.
  await page.getByLabel(/I have read and accept the Privacy Policy/).check();
  await page.getByRole("button", { name: "Agree and sign in" }).click();

  // Back on the EVENT page — not the Customer Area — with the quantity restored
  // and the dialog already open.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}${EVENT_SLUG_PATH}\\?`));
  await expect(page.getByLabel(`${GA_TICKET} quantity`)).toHaveText("1");
  const dialog = page.getByRole("dialog", { name: "Checkout" });
  await expect(dialog).toBeVisible();
  await expect(page.getByTestId("checkout-identity")).toContainText(email);

  // Asked for a name, because signing in minted a Customer with none — and
  // asked for no consent, because it was given at sign-in.
  await expect(dialog.getByLabel("First name")).toHaveValue("");
  await expect(page.locator("#consent-policy-acceptance")).toHaveCount(0);
});

test("a Customer buys a ticket through the stub provider and lands on the confirmation", async ({
  page,
}) => {
  // Unique per run: each journey is its own Customer, signed in before it can
  // buy anything at all.
  const email = `e2e-approve-${Date.now()}@example.com`;
  await signInAsCustomer(page, email);

  await selectOneTicketAndOpenCheckout(page);
  await fillCheckoutFormAndPay(page, email);

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

  // And the buyer is signed in on the way out, which is now true by
  // construction — so the page offers their Customer Area rather than the
  // sign-in it used to offer a guest.
  await expect(page.getByRole("link", { name: "See your tickets" })).toBeVisible();
});

// The buyer whose session did not survive the Payment Provider (#387, ADR 0054).
//
// This is the most consequential path in the Storefront, because the money has
// already moved by the time it runs. The buyer is off-origin at the provider,
// possibly for minutes: cookies get cleared, a provider webview can drop them,
// a session can be revoked, and they can come back in a different browser. Under
// every one of those, a person who HAS ALREADY PAID lands on this page.
//
// It is worth stating plainly because the branch this exercises looks exactly
// like guest-checkout residue now that a buyer is signed in by construction, and
// deleting it would be invisible until it happened to somebody who had paid. So
// the session cookie is destroyed mid-journey — after the Payment is under way
// and before the return leg runs — and the checkout context cookie is left
// alone, which is precisely the state a dropped session produces.
test("a buyer whose session dies at the provider still lands on their confirmation", async ({
  page,
  context,
}) => {
  const email = `e2e-lostsession-${Date.now()}@example.com`;
  await signInAsCustomer(page, email);

  await selectOneTicketAndOpenCheckout(page);
  await fillCheckoutFormAndPay(page, email);

  // At the provider, holding an unconfirmed Payment. Drop ONLY the Customer
  // Session: the checkout context is a separate cookie and is what carries this
  // buyer home.
  const surviving = (await context.cookies()).filter(
    (cookie) => cookie.name !== "ticket_pos_customer_session",
  );
  await context.clearCookies();
  await context.addCookies(surviving);

  await page.getByRole("link", { name: "Approve payment" }).click();

  // Confirm is public and reads the checkout context rather than a session, so
  // the Ticket Sale still commits and the buyer still gets their reference.
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/checkout/success\\?ref=`));
  await expect(page.getByRole("heading", { name: "You're going!" })).toBeVisible();
  const ref = (await page.getByTestId("confirmation-ref").innerText()).trim();
  expect(ref).toMatch(CONFIRMATION_REF);

  // Not a dead end: they are told where they stand and offered a way back in,
  // with the address they bought under already in the field, so a buyer who
  // mistyped nothing cannot be locked out of what they just paid for.
  await expect(page.getByText("You're signed out on this device", { exact: false })).toBeVisible();
  const signIn = page.getByRole("link", { name: "Sign in to see your tickets" });
  await expect(signIn).toBeVisible();
  const href = await signIn.getAttribute("href");
  expect(href).toContain(encodeURIComponent(email));
  expect(href).toContain(`next=${encodeURIComponent("/tickets")}`);

  // And the locale carried through the return leg still decides the language:
  // null is not English.
  await expect(page.locator("html")).toHaveAttribute("lang", LOCALE);

  // Re-loading the confirmation shows the same Ticket Sale rather than a second
  // one. This is the terminal page and not the return route, so it is the weaker
  // of the two idempotency claims; the return route's own is pinned in the Go
  // integration suite, against a real database.
  await page.reload();
  await expect(page.getByTestId("confirmation-ref")).toHaveText(ref);
});

test("a declined payment lands on the failure page and retry returns the selection", async ({
  page,
}) => {
  const email = `e2e-decline-${Date.now()}@example.com`;
  await signInAsCustomer(page, email);

  await selectOneTicketAndOpenCheckout(page);
  await fillCheckoutFormAndPay(page, email);

  await page.getByRole("link", { name: "Decline payment" }).click();

  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/checkout/failed`));
  await expect(page.getByRole("heading", { name: "Payment not completed" })).toBeVisible();
  await expect(page.getByText("You haven't been charged", { exact: false })).toBeVisible();

  // Retry goes back to the event page WITH THE BASKET the buyer was paying for
  // (ADR 0054): a refused card must not also cost them their selection. The
  // quantities are re-judged on arrival against live availability, and the
  // dialog deliberately does NOT reopen — pressing Buy again is theirs to do,
  // and a fresh press begins a fresh Payment with a new client transaction id.
  await page.getByRole("link", { name: "Try again" }).click();
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}${EVENT_SLUG_PATH}\\?sel=`));
  await expect(page.getByRole("heading", { level: 1, name: EVENT_NAME })).toBeVisible();
  await expect(page.getByLabel(`${GA_TICKET} quantity`)).toHaveText("1");
  await expect(page.getByRole("dialog", { name: "Checkout" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Get tickets" })).toBeEnabled();
});

// Which consent boxes the dialog draws, and for whom (#254, parent #249; ADR
// 0054).
//
// The rule is the API's and its matrix is pinned in
// backend/integration/checkout_consent_visibility_test.go; what a browser adds
// is the wiring between them. Since #385 there is one case left to wire, and it
// is the ordinary one: a buyer reaches this dialog having already answered
// everything at SIGN-IN, so "nothing outstanding" must reach the screen as no
// consent UI at all rather than as three boxes nobody can see the point of.
//
// The guest case is gone with the guests. There is no address to type here any
// more, so there is no way to turn a signed-in checkout back into somebody
// else's — which was the other half of the journey this replaces.
const CONSENT_BOX_IDS = ["#consent-policy-acceptance", "#consent-marketing", "#consent-networking"];

test("a Customer who answered consent at sign-in is asked nothing at checkout", async ({
  page,
}) => {
  // A fresh address, signed in through the consent step with the required box
  // ticked and the optional two deliberately left unticked — an unticked box at
  // a capture moment is an explicit No, and a No is an ANSWER (ADR 0034). That
  // is what makes this Customer "fully answered" a moment later.
  const email = `e2e-consent-checkout-${Date.now()}@example.com`;
  await signInAsCustomer(page, email);
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/tickets$`));

  await selectOneTicketAndOpenCheckout(page);

  // The address the purchase will be written to is stated, off the same session
  // read the consent set came from — so seeing it is also evidence the set
  // arrived.
  await expect(page.getByTestId("checkout-identity")).toContainText(email);

  // No notice, no link, no boxes: the checkout is exactly what it was before
  // this feature existed (parent #249, user story 10).
  await expect(page.getByText("How we handle your data", { exact: false })).toHaveCount(0);
  for (const id of CONSENT_BOX_IDS) {
    await expect(page.locator(id)).toHaveCount(0);
  }
  // And the pay button is not held behind a box that is not there.
  await expect(page.getByRole("button", { name: "Continue to payment" })).toBeEnabled();
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
