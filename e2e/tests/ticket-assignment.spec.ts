import { test, expect, type Page } from "@playwright/test";

import { readAssignmentLink } from "./support/assignment-link";
import { readPasscode } from "./support/passcode";
import {
  ensureFixtureEvent,
  FIXTURE_TICKET_TYPE,
  holderList,
  organizerSession,
} from "./support/staff-fixtures";

// Ticket Assignment, end to end (#322, ADRs 0046–0047), against the dev stack
// (`make dev`) with TICKET_QUESTIONS_ENABLED and TICKET_ASSIGNMENT_ENABLED on.
//
// ONE journey, because the promise is one sentence: a Customer who bought more
// than one ticket gives one of them to somebody by email, and that somebody —
// from the link the platform mails them, with no account and no sign-in — can
// answer the Organization's Ticket Questions for the ticket that is now theirs.
//
// What a browser proves here and nothing else can is the WIRING between four
// runtimes and three people: the Organizer's question reaching the buyer's
// page, the buyer's press reaching the logging mail sender, the link in that
// mail opening on the Storefront and minting the Holder on a press, and the
// Holder's acceptance reaching back to the buyer's page and the Organizer's
// Holder List. Everything this journey walks past — what assigning clears,
// what a reassignment does to a stale link, who may be named — is pinned in
// backend/integration/ticket_assignment_test.go and is not re-asserted here
// (docs/testing.md, E2E boundary).

const LOCALE = "en";

// The Organization's own words, as a Ticket Question's label is (ADR 0027).
// Prefixed so the idempotent fixture can find its own question on a rerun.
const QUESTION = "E2E: dietary requirements";
// A required question's label carries the chase-mark on every answering surface.
const QUESTION_LABEL = new RegExp(`^${QUESTION} \\(the organizer is waiting on this one\\)$`);

// A cédula with a valid check digit: the API validates it and refuses the
// checkout otherwise (#98, ADR 0016). Not an identity, so every run may reuse it.
const BUYER_TAX_ID = "1712345675";

// Every amount the Customer sees is one formatted dollar figure.
const MONEY = /^\$\d+\.\d{2}$/;

async function signInFromPasscode(page: Page, email: string) {
  await page.goto(`/${LOCALE}/signin?next=/tickets`);
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

  // The consent step (#251) appears only for an address with something still
  // unanswered. This buyer ticked the required box at checkout and left the
  // optional two unticked — and an unticked box at a capture moment is a No,
  // which is an ANSWER (ADR 0034) — so ordinarily there is nothing to ask and
  // the session is minted straight away. Tolerated either way: whether the
  // step is shown is a consent rule, not this journey's.
  const consentBox = page.getByLabel(/I have read and accept the Privacy Policy/);
  await Promise.race([
    consentBox.waitFor({ state: "visible" }),
    page.waitForURL(new RegExp(`/${LOCALE}/tickets$`)),
  ]);
  if (await consentBox.isVisible()) {
    await consentBox.check();
    await page.getByRole("button", { name: "Agree and sign in" }).click();
  }
  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/tickets$`));
}

test("a buyer of two tickets gives one away, and the Holder answers its questions from the link", async ({
  page,
  browser,
  request,
}) => {
  // Step 0 — the Organizer, through the staff API: a published Event whose
  // General Admission asks one required question. Created on the first run and
  // found on the next, so this spec needs no seed rows at all.
  const organizer = await organizerSession(request);
  test.skip("skipped" in organizer, "skipped" in organizer ? organizer.skipped : "");
  const token = (organizer as { token: string }).token;
  const fixture = await ensureFixtureEvent(request, token, QUESTION);

  const stamp = Date.now();
  const buyer = `e2e-buyer-${stamp}@example.com`;
  const holder = `e2e-holder-${stamp}@example.com`;

  // Step 1 — the buyer takes TWO General Admission tickets through the stub
  // Payment Provider. The total is read off the page rather than asserted to a
  // number: what this journey needs is that the same figure follows the buyer
  // onto the provider's page, not what the Fee Handling made it.
  await page.goto(fixture.path(LOCALE));
  await expect(page.getByRole("heading", { level: 1, name: fixture.name })).toBeVisible();
  const addOne = page.getByRole("button", { name: `Add one ${FIXTURE_TICKET_TYPE} ticket` });
  await addOne.click();
  const oneTicket = await page.getByTestId("selection-total").innerText();
  await addOne.click();
  const total = page.getByTestId("selection-total");
  await expect(total).toHaveText(MONEY);
  await expect(total).not.toHaveText(oneTicket);
  const quoted = await total.innerText();

  await page.getByRole("button", { name: "Get tickets" }).click();
  const dialog = page.getByRole("dialog", { name: "Checkout" });
  await expect(dialog).toBeVisible();

  // The dialog asks the question ONCE, for the buyer's own ticket, and is
  // explicit that it may be skipped (#311, ADR 0044, ADR 0048): the second
  // ticket is not mentioned, because its Holder answers for it, below. Left
  // blank here, which is the premise of the whole feature.
  await expect(dialog.getByText("Your ticket · General Admission")).toBeVisible();
  await expect(dialog.getByLabel(QUESTION, { exact: false })).toHaveCount(1);

  await dialog.getByLabel("Email", { exact: true }).fill(buyer);
  await dialog.getByLabel("First name").fill("Ada");
  await dialog.getByLabel("Last name").fill("Lovelace");
  await dialog.getByLabel("ID type").selectOption("cedula");
  await dialog.getByLabel("ID number").fill(BUYER_TAX_ID);
  // Addressed by id rather than by words the Policy Version owns (ADR 0036).
  await page.locator("#consent-policy-acceptance").check();
  await page.getByRole("button", { name: "Continue to payment" }).click();

  await expect(page).toHaveURL(new RegExp(`/${LOCALE}/checkout/stub\\?`));
  await expect(page.getByTestId("stub-amount")).toHaveText(quoted);
  await page.getByRole("link", { name: "Approve payment" }).click();
  await expect(page.getByRole("heading", { name: "You're going!" })).toBeVisible();

  // Step 2 — the buyer signs in. The first ticket is already THEIRS (ADR
  // 0048); the second is one collapsed row, opened to give it an address. The
  // notice about what that discloses sits on the field; the row's state is
  // what tells the two tickets apart once saved.
  await signInFromPasscode(page, buyer);
  await expect(page.getByText("Your ticket", { exact: true })).toBeVisible();
  await page.getByText("Ticket 2 of 2").click();
  const addressField = page.getByLabel("Email address of whoever will use this ticket");
  await expect(addressField).toHaveCount(1);
  await addressField.fill(holder);
  await page.getByRole("button", { name: "Save address" }).click();
  await expect(page.getByText(`For ${holder}`, { exact: true })).toBeVisible();

  // Step 3 — the Holder. A DIFFERENT PERSON: a fresh context with none of the
  // buyer's cookies, arriving from the mail with nothing but the link. The link
  // is written locale-free and against whatever origin the API was configured
  // with, so only its origin is swapped for this suite's, never its path.
  const mailed = readAssignmentLink(holder);
  expect(mailed, "an Assignment mail for the Holder in the API log").not.toBeNull();
  const link = new URL(mailed as string);
  const base = new URL(page.url());
  link.protocol = base.protocol;
  link.host = base.host;

  const holderContext = await browser.newContext();
  const holderPage = await holderContext.newPage();
  try {
    await holderPage.goto(link.toString());
    await expect(holderPage.getByRole("heading", { name: "You have a ticket" })).toBeVisible();

    // Opening the page has done nothing; the press is the act (ADR 0046). Before
    // it the reader is told what accepting discloses, and after it the ticket
    // is theirs.
    await expect(holderPage.getByText(/your email address is shared with the organizer/)).toBeVisible();
    await holderPage.getByRole("button", { name: "Accept my ticket" }).click();
    await expect(holderPage.getByText("The ticket is yours")).toBeVisible();

    await holderPage.getByLabel("First name").fill("Grace");
    await holderPage.getByLabel("Last name").fill("Hopper");
    await holderPage.getByRole("button", { name: "Save my name" }).click();
    await expect(holderPage.getByText("Saved", { exact: true })).toBeVisible();

    // The promise itself: the Holder meets the Organization's question on the
    // page the link opened, with no account and no sign-in, and can answer it.
    const answer = holderPage.getByLabel(QUESTION_LABEL);
    await expect(answer).toBeVisible();
    await answer.fill("Vegetarian");
    await holderPage.getByRole("button", { name: "Save", exact: true }).click();
    // Two "Saved"s on the page now: the name's, and the answer's beside its button.
    await expect(holderPage.getByText("Saved", { exact: true })).toHaveCount(2);

    // Step 3b — the Holder corrects themself from their OWN Customer Area
    // (#345, ADR 0049), with no link and no buyer involved. The ticket is
    // there because they hold it; its panel is the same one the buyer has.
    // It owes nothing (the question was answered from the link), so it sits
    // folded behind "Review or edit"; opened, the field holds what they said
    // and saves on blur with no button to press. A save from a panel that
    // owed nothing does NOT fold it — only the last owed Answer does.
    await signInFromPasscode(holderPage, holder);
    await expect(holderPage.getByText("Tickets someone gave you")).toBeVisible();
    const reviewOrEdit = holderPage.getByText("Answered · Review or edit", { exact: true });
    await expect(reviewOrEdit).toBeVisible();
    await reviewOrEdit.click();
    const correction = holderPage.getByLabel(QUESTION_LABEL);
    await expect(correction).toHaveValue("Vegetarian");
    await correction.fill("Vegan");
    await correction.blur();
    await expect(holderPage.getByText("Saved", { exact: true })).toBeVisible();
    await expect(holderPage.getByRole("button", { name: "Save", exact: true })).toHaveCount(0);
    // The correction stuck, and the panel the reader opened stayed open.
    await holderPage.reload();
    await holderPage.getByText("Answered · Review or edit", { exact: true }).click();
    await expect(holderPage.getByLabel(QUESTION_LABEL)).toHaveValue("Vegan");
  } finally {
    await holderContext.close();
  }

  // Step 4 — the loop closes on both sides. The buyer's page reads the pickup,
  // and the Organizer's Holder List has that ticket accepted by that address
  // with nothing left outstanding — one row, one assertion; the roster's own
  // rules are integration-tested.
  await page.reload();
  await expect(page.getByText(`${holder} has it`, { exact: true })).toBeVisible();

  // Step 5 — the fold (#345). The buyer's OWN ticket is the same panel the
  // Holder just used, and it still owes the question they left blank at
  // checkout, so it is open on arrival with no button to press. Answering it
  // is the last owed Answer, and the panel folds the moment it saves — the
  // page now shows what remains to be done, which is nothing.
  const own = page.getByLabel(QUESTION_LABEL);
  await expect(own).toBeVisible();
  await own.fill("Omnivore");
  await own.blur();
  await expect(page.getByText("Answered · Review or edit", { exact: true })).toBeVisible();
  await expect(own).toBeHidden();

  const roster = await holderList(request, token, fixture.eventId);
  const accepted = roster.find((entry) => entry.holder_email === holder);
  expect(accepted).toMatchObject({ assignment_state: "accepted", outstanding: [] });
});
