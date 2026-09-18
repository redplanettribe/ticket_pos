"use client";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Input,
  Markdown,
  cn,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useMemo, useState, type FormEvent } from "react";

import { CheckoutAnswers } from "@/components/checkout-answers";
import { ConsentCheckbox } from "@/components/consent-checkbox";
import { SignInOtherAddressButton } from "@/components/sign-in-other-address-button";
import { UpgradePrompt } from "@/components/upgrade-prompt";
import { useFormatLocale } from "@/i18n/format-locale";
import { Link, usePathname } from "@/i18n/navigation";
import type { BeginCheckoutResult, PrivacyPolicy, PublicTicketType, Terms } from "@/lib/api";
import {
  checkoutAnswerBodies,
  ownTicketSlot,
  upgradeOffer,
  type AnswerValues,
} from "@/lib/checkout-answers";
import {
  apiErrorMessage,
  fieldCodeMessage,
  fieldErrorMessages,
  type ErrorCatalog,
} from "@/lib/api-errors";
import { anyConsentBox } from "@/lib/checkout-consent";
import type { CheckoutIdentity } from "@/lib/checkout-identity";
import { checkoutReturnPath, checkoutSignInHref } from "@/lib/checkout-signin";
import {
  allowanceSpent,
  checkoutDestination,
  clampQuantity,
  offerableQuantity,
  selectionLines,
  totalCents,
  totalQuantity,
} from "@/lib/checkout";
import type { RestoredSelection, SelectionAdjustment } from "@/lib/selection-url";
import { formatPrice } from "@/lib/format";
import { localizedPath, toAppLocale } from "@/lib/locale";
import {
  ECUADOR_DIALLING_CODE,
  composePhone,
  countries,
  normalizePhone,
  splitPhone,
  validatePhone,
} from "@/lib/phone";
import { PRIVACY_POLICY_PATH } from "@/lib/privacy-policy";
import {
  TAX_ID_TYPES,
  TAX_ID_TYPE_LABELS,
  isTaxIdType,
  normalizeTaxIdNumber,
  validateTaxId,
  type TaxIdType,
} from "@/lib/tax-id";
import { TERMS_PATH } from "@/lib/terms";

import { PromotionBadge, PromotionDeadline, TicketTypePrice } from "./promotion";
import { SalesClosedLine, TicketTypeStateBadge } from "./sales-cutoff";

/**
 * Ticket selection and the one checkout step, inline on the event page
 * (docs/design/storefront.md): quantity steppers per Ticket Type, a sticky
 * running total, and — for a buyer with a Customer Session — a Dialog collecting
 * first/last name and Tax ID, plus an optional phone number.
 *
 * BROWSING IS ANONYMOUS AND BUYING IS NOT (ADR 0054, #385). Everything above the
 * sticky bar is drawn identically for everybody: the Ticket Types, their prices,
 * their remaining counts and their steppers all work signed out, because the
 * Storefront is a discovery surface and nothing may gate discovery (ADR 0002,
 * ADR 0037). The wall stands at ONE control — Buy — where for a visitor with no
 * identity it is not a button that opens this dialog but a LINK to sign in,
 * carrying the basket in its destination (lib/checkout-signin.ts). Crossing a
 * page boundary is what lets the bad news arrive before the buyer has typed
 * anything, instead of after they have filled a form in.
 *
 * THERE IS NO EMAIL FIELD. The address the purchase is written to comes from the
 * session and is stated back, loudly, at the top of the dialog — because a
 * Customer Session satisfies the wall for its whole life with no re-proof at the
 * till, and on a shared machine the mitigation is that whose purchase this is
 * cannot be missed. Beside it sits the way out: sign in as somebody else, which
 * returns to this same Event with this same basket and this dialog open.
 *
 * IT STILL ASKS FOR A NAME, because signing in mints a Customer with none — the
 * sign-in form asks for an address, a code and consent and nothing else. The Tax
 * ID and the phone stay optional-shaped as they were: the phone prefills only
 * from the Customer's own stored number (#108), never from a default, a
 * placeholder or filler, because anything else would be the static cardholder
 * data PayPhone's rules prohibit.
 *
 * Below the fields the consent section is ORDINARILY ABSENT. A first-time buyer
 * meets the boxes at sign-in, which holds the sign-in at a consent step while
 * Policy Acceptance is outstanding (#251), so by the time anybody reaches this
 * dialog there is nothing left to ask (#254). What remains here is the case
 * where a Policy Version was published mid-session: the boxes are drawn
 * unticked, with the required one gating the pay button, and their words come
 * from the API rather than from the message catalogs because a Policy Version is
 * the fingerprint of exactly the text a person was shown (ADR 0036).
 *
 * Submitting asks this app's own /api/checkout route to begin the Payment
 * (the browser never addresses the Go API, ADR 0008) and then performs a
 * full-page navigation to the provider's payment URL: the payment page must be
 * top-level, never an iframe.
 */

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

type TicketSelectionProps = {
  orgSlug: string;
  eventSlug: string;
  eventName: string;
  ticketTypes: PublicTicketType[];
  /**
   * The basket the buyer arrived with, already re-judged against the Ticket
   * Types above (ADR 0054, lib/selection-url.ts).
   *
   * It arrives JUDGED, not claimed: the page decoded the address and clamped
   * every quantity to what capacity and the Purchase Limit currently allow
   * (ADR 0025) before handing it over, so nothing here can seed a stepper with
   * a number a finger could not have produced. Its `adjustments` are what could
   * NOT be restored, and they are drawn above the ticket list — a basket that
   * silently differs from the one the buyer pressed Buy on is the one outcome
   * this whole mechanism exists to prevent.
   *
   * Absent for every visitor who arrived without an encoded selection, which is
   * everyone who did not come back through the wall at Buy.
   */
  restoredSelection?: RestoredSelection;
  /**
   * Who this purchase would be addressed to, or null when nobody is signed in
   * (ADR 0054, lib/checkout-identity.ts).
   *
   * IT IS THE WALL. Null turns the Buy button into a link to sign-in and makes
   * this dialog unreachable; non-null is a proven address, the prefill that
   * comes with it, and the boxes that Customer still owes.
   *
   * Read on the SERVER with the page rather than fetched when the dialog opens,
   * which is what session prefill stopped being speculative about: the address
   * is on screen the instant the dialog is, there is no moment where an open
   * dialog cannot say whose purchase this is, and the decision the Buy button
   * makes is settled before the buyer can press it.
   */
  identity: CheckoutIdentity | null;
  /**
   * Whether the address asked for the checkout dialog — `?checkout=1`, written
   * by the wall onto the destination it sends a buyer back to.
   *
   * A REQUEST AND NOT A GRANT. It is honoured only when there is somebody to
   * sell to and something in the basket to sell, so a hand-typed one on an
   * anonymous visit, or on a basket that nothing survived, opens nothing.
   */
  openCheckoutOnArrival?: boolean;
  /**
   * Whether an Online Sale hands the buyer a Ticket of their own (ADR 0048) —
   * the platform's assignment flag, read off the event payload so the dialog
   * calls a Ticket "yours" only when the sale will make it so. WHICH Ticket is
   * the dearest one in the cart (ADR 0074) and is ownTicketSlot's business; the
   * name is the published field's and is left alone.
   */
  buyerHoldsFirstTicket: boolean;
  /**
   * How many free Tickets this buyer could give up on this Event through an
   * Upgrade, as the Event payload published it (ADR 0074, #648) — the server's
   * half of whether the Upgrade Prompt is drawn.
   *
   * NULL IS THE ANONYMOUS READ and never 0: the page was fetched without a
   * Customer Session, so nobody's earlier Sales were counted. upgradeOffer
   * draws nothing on a null, which is also the right answer for a visitor who
   * has not met the sign-in wall yet — they are sent through it before they can
   * buy, and the page they come back to is read with their session.
   */
  surrenderableFreeTickets: number | null;
  /**
   * The current Policy Version's Short Notice and checkbox labels, as the API
   * serves them — null when it could not be reached (#253).
   *
   * THE WORDS OF THE NOTICE AND THE LABELS ARE NOT IN THE MESSAGE CATALOGS.
   * They are evidence: the Policy Version records the SHA-256 of exactly these
   * strings, so what a buyer is shown here is byte-for-byte what the platform
   * will later claim they accepted (ADR 0036). The section's own chrome — the
   * summary line that opens the notice, the word "Optional", the link's words —
   * is ordinary copy and lives in the catalogs.
   *
   * Null hides the whole checkout form. A dialog that cannot show what is being
   * accepted must not collect an acceptance of it, and the API refuses such a
   * checkout anyway — a form that led somewhere refused would only waste the
   * buyer's typing.
   */
  policy: PrivacyPolicy | null;
  /**
   * The current Terms Version's checkbox label, read from the API for the same
   * evidential reason as the policy's labels above (#537, ADR 0066): what was
   * accepted is exactly what was served, and the UI never rewords it. Null when
   * the API could not be reached, which — like a null policy — becomes an
   * honest refusal to collect the answer rather than a checkout blocked for a
   * buyer who owes nothing.
   */
  terms: Terms | null;
  /**
   * Whether the quoted prices carry the platform's service fee, which is the
   * only thing that decides the muted note below. Prices themselves are always
   * shown exactly as the API quotes them: this app never does fee arithmetic
   * (ADR 0014).
   */
  priceIncludesFee: boolean;
  /**
   * The Event's timezone, which a Promotion's deadline is read in (ADR 0021) and
   * which a Sales Cutoff is both set and counted down on (ADR 0070).
   */
  timezone: string | null;
  /**
   * Whether every one of this Event's Ticket Types has closed — the server's
   * verdict, straight off the Event payload (ADR 0070).
   *
   * It replaces the sticky Buy bar with a sentence. A page of dimmed cards and no
   * explanation reads as a loading failure, and a Buy control that can never
   * complete is an offer this page cannot honour.
   *
   * Taken as a field and NEVER folded over the cards below, for the same reason
   * `closed` is: the server owns the clock, and the two apps must rank these
   * states identically. It is also not the same question as "is anything
   * buyable" — an Event that is merely sold out, or one where a single reader has
   * spent their Purchase Limit, has nothing to select either, and the sentence
   * for each of those is a different sentence this feature does not write.
   */
  allClosed: boolean;
  /**
   * The instant the countdown counts from — the SERVER's clock, read with the
   * page and handed down (ADR 0070).
   *
   * Not `new Date()` at render. This is a Client Component that is still
   * server-rendered, so a clock read in both places is read twice: across the
   * Event's midnight the two answers differ, and React meets a hydration
   * mismatch on text it has already streamed. Reading it once, above, means SSR
   * and hydration draw the same rung by construction.
   *
   * It is also the more honest clock. `closed` is the server's verdict, and a
   * countdown derived from the same clock that made it can never disagree with
   * it — where a browser set to next week would have counted down to a door the
   * server knows is already shut. The countdown does not tick, so a value fixed
   * at request time is exactly as fresh as the rest of the page.
   */
  now: Date;
};

/**
 * The API's begin-checkout field names, mapped onto the form's inputs.
 *
 * No `customer_email`: the session-gated route takes none, so it can report no
 * field error about one and there is no input here for such an error to sit
 * under (ADR 0054).
 */
const FORM_FIELDS = [
  "customer_first_name",
  "customer_last_name",
  "customer_tax_id_type",
  "customer_tax_id_number",
  "customer_phone",
] as const;

type FieldErrors = Partial<Record<(typeof FORM_FIELDS)[number], string>>;

function isFormField(field: string): field is (typeof FORM_FIELDS)[number] {
  return (FORM_FIELDS as readonly string[]).includes(field);
}

/**
 * The API's field errors narrowed to the inputs this dialog actually drew.
 *
 * The copy is chosen by fieldErrorMessages on each FieldError's stable `code`,
 * falling back to its message (ADR 0023); the only thing added here is the
 * filter. `lines[0].quantity` has no control on this form to sit under, so it is
 * dropped rather than shown somewhere it means nothing.
 */
function fieldErrorsFromDetails(catalog: ErrorCatalog, details: unknown): FieldErrors {
  const errors: FieldErrors = {};
  for (const [field, message] of Object.entries(fieldErrorMessages(catalog, details))) {
    if (isFormField(field)) {
      errors[field] = message;
    }
  }
  return errors;
}

/**
 * What went wrong, as a fact rather than as a sentence.
 *
 * `code` is the API's verdict about WHICH failure this is, and it is what the
 * sentence gets chosen by; `message` is the API's own words, kept as the
 * fallback for a code the catalog has never heard of (ADR 0023). `fallback`
 * names which of this app's own two failures to say instead when the API said
 * nothing at all. Nothing here is a sentence: the words are looked up at render,
 * so state never holds copy that a language switch would strand.
 *
 * `details` is carried for the same reason and under the same rule — they are the
 * facts the API refused over, never words. A Purchase Limit refusal is only
 * meaningful with them: the sentence has to state the limit and how many the
 * Customer already holds, or it reads as the Event being full (ADR 0025).
 */
type CheckoutError = {
  code: string | null;
  message: string | null;
  details: unknown;
  fallback: "startFailed" | "networkFailed";
};

/** Matches the Input component's height, border and mobile font size so the select reads as a peer. */
const SELECT_CLASS =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-base sm:text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50";

/**
 * The Customer's stored phone split back into the two controls the form draws
 * it with, or an empty pair when they have none (#108, #103).
 *
 * A number whose dialling code is in no row at all — only possible if the
 * country table shrinks under a number already stored — keeps Ecuador on the
 * selector and shows the whole value in the field, mirroring what "My info"
 * does. Showing a buyer their own number intact and letting the mirror check
 * complain beats mangling it to fit a control.
 */
function prefilledPhone(phone: string | null): {
  diallingCode: string;
  nationalNumber: string;
} {
  if (!phone) {
    return { diallingCode: ECUADOR_DIALLING_CODE, nationalNumber: "" };
  }
  const { diallingCode, nationalNumber } = splitPhone(phone);
  return {
    diallingCode: diallingCode === "" ? ECUADOR_DIALLING_CODE : diallingCode,
    nationalNumber,
  };
}

/** No boxes at all, which is what a dialog with no identity would draw. */
const NO_CONSENT_BOXES = {
  policy_acceptance: false,
  marketing_consent: false,
  networking_consent: false,
  terms_acceptance: false,
  adulthood_declaration: false,
};

export function TicketSelection({
  orgSlug,
  eventSlug,
  eventName,
  ticketTypes,
  restoredSelection,
  identity,
  openCheckoutOnArrival = false,
  buyerHoldsFirstTicket,
  surrenderableFreeTickets,
  priceIncludesFee,
  timezone,
  allClosed,
  now,
  policy,
  terms,
}: TicketSelectionProps) {
  // next/navigation's router, deliberately: the only thing asked of it here is
  // refresh(), which has no address to localize.
  const router = useRouter();
  // This Event page's own address, without a language prefix, which is what the
  // wall at Buy builds its round trip out of. Locale-free on the way out and
  // locale-bearing on the way back through `Link`, so a buyer who switches
  // language mid sign-in still lands on the page they were buying from.
  const pathname = usePathname();
  const locale = toAppLocale(useLocale());
  const t = useTranslations("checkout");
  // The error catalog as plain data rather than through `t`: its keys are API
  // codes, which arrive as strings at runtime and cannot be typed message keys.
  const errorCopy = useMessages().errors;
  // A Ticket Type's own statements about itself — sold out, how many are left,
  // whether the fee is inside the price — are the Event page's words, and the
  // read-only list on an ended Event says them from the same keys. Two lists of
  // the same Ticket Types must not be able to word the same fact differently.
  const eventCopy = useTranslations("event");
  // Seeded from the restored basket, and from an empty object for everybody
  // else. The seed is an initial value and not a subscription: once the buyer
  // touches a stepper the address has had its say, and a later re-render must
  // never push their own choice back to what a link suggested.
  const [quantities, setQuantities] = useState<Record<string, number>>(
    () => restoredSelection?.selection ?? {},
  );
  // Open on arrival when the buyer came back through the wall (?checkout=1) and
  // there is both somebody to sell to and something to sell them. Otherwise the
  // ordinary closed dialog that Buy opens.
  //
  // Judged once, as an initial value: `checkout=1` describes an ARRIVAL, and a
  // later re-render must not reopen a dialog the buyer has since closed.
  const [checkoutOpen, setCheckoutOpen] = useState(
    () =>
      openCheckoutOnArrival &&
      identity !== null &&
      totalQuantity(restoredSelection?.selection ?? {}) > 0,
  );
  // The buyer's name, prefilled from their Customer record — and ROUTINELY
  // EMPTY, which is why these two fields still exist. Signing in mints a
  // Customer with no name: the sign-in form asks for an address, a code and
  // consent, and this is the first place anybody is asked what to call them.
  const [firstName, setFirstName] = useState(identity?.firstName ?? "");
  const [lastName, setLastName] = useState(identity?.lastName ?? "");
  // The stored Tax ID is a prefill, not a lock: a signed-in Customer may
  // override it for this purchase — buying under a company RUC instead of their
  // cédula — and the override becomes their new stored default (ADR 0016). A
  // Customer who has never supplied one gets the empty form, with cédula
  // preselected because it is what the overwhelming majority of buyers hold;
  // the select is still required, so nothing is submitted under a type the buyer
  // never looked at without them also typing its number.
  //
  // The pair moves together or not at all — lib/checkout-identity.ts already
  // refuses to hand over half of one — so there is no state where a stored
  // number sits under a type nobody stored.
  const storedTaxIdType =
    identity?.taxIdType && isTaxIdType(identity.taxIdType) ? identity.taxIdType : null;
  const [taxIdType, setTaxIdType] = useState<TaxIdType>(storedTaxIdType ?? "cedula");
  const [taxIdNumber, setTaxIdNumber] = useState(
    storedTaxIdType ? (identity?.taxIdNumber ?? "") : "",
  );
  // The phone number, split across a country selector and a text field purely
  // for entry: what is submitted is the single canonical E.164 string the two
  // assemble into (#103). Ecuador is selected by default because it is the home
  // market and the overwhelming majority of buyers — the common case should need
  // no interaction at all.
  //
  // It is filled in only from the Customer's OWN stored number (#108). Nothing
  // else may write it: PayPhone's rules prohibit static or filler cardholder
  // data, so a number in this field is always one the person themselves entered
  // — here or, earlier, on their profile — and an untouched empty field submits
  // nothing at all.
  //
  // Seeded rather than filled in later, which is what retires the "touched"
  // flags this used to need: the prefill is the initial value, so there is no
  // moment where a buyer's own typing could be overwritten by a read landing
  // behind it.
  const [phoneDiallingCode, setPhoneDiallingCode] = useState(
    () => prefilledPhone(identity?.phone ?? null).diallingCode,
  );
  const [phoneNationalNumber, setPhoneNationalNumber] = useState(
    () => prefilledPhone(identity?.phone ?? null).nationalNumber,
  );
  // The three consent boxes, all starting unticked and never pre-ticked from
  // anything (parent #249): consent has to be something the person actively
  // gave, so there is no prefill here even for a signed-in Customer whose
  // standing answer this app could read. What is stored decides whether to ASK
  // (#254), never what to show as already agreed.
  const [policyAccepted, setPolicyAccepted] = useState(false);
  const [marketingConsent, setMarketingConsent] = useState(false);
  const [networkingConsent, setNetworkingConsent] = useState(false);
  // The Terms box (#537, ADR 0066): contractual, separate, and under the same
  // no-prefill rule as the three above.
  const [termsAccepted, setTermsAccepted] = useState(false);
  // The Adulthood Declaration (#588, ADR 0069): its own box, its own state, its
  // own answer, and never folded into termsAccepted. A combined tick would
  // evidence only that somebody accepted a document containing an age sentence,
  // which is the inference this feature exists to replace — and the two
  // refusals mean different things: declining the Terms is "I do not agree",
  // declining this is "I am a child", and one control cannot say both.
  const [adulthoodDeclared, setAdulthoodDeclared] = useState(false);
  // The Upgrade Prompt's answer (#652, ADR 0074), starting UNTICKED for
  // everybody and never prefilled from anything. The two answers are not equally
  // recoverable — keep-both is fixable at leisure, an Upgrade destroys a Ticket
  // silently inside the payment's own transaction — so the default falls to the
  // reversible side and a buyer who scrolls past this loses nothing by it.
  //
  // It is deliberately NOT cleared when the cart changes under it. A stale tick
  // cannot travel: the send below is gated on the prompt still being drawn for
  // the cart as it now stands, and clearing here as well would silently undo a
  // buyer's own answer when they added and then removed a ticket.
  const [upgradeElected, setUpgradeElected] = useState(false);
  // What the buyer has typed into the answer section, keyed by (Ticket Type,
  // ticket number, question) (#311).
  //
  // IT IS KEPT WHEN THE DIALOG CLOSES AND CLEARED WHEN THE CART DOES. A buyer
  // who steps back to change a quantity and returns should not have to retype
  // three t-shirt sizes; a buyer whose cart was refused starts over, and
  // backToTickets empties this beside the quantities. Values whose ticket is no
  // longer in the cart never travel anyway — the slots decide that, not this map
  // (checkoutAnswerBodies).
  const [answers, setAnswers] = useState<AnswerValues>({});
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [error, setError] = useState<CheckoutError | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Which boxes this dialog draws: the ones the Customer on the session still
  // owes, and ORDINARILY NONE. A first-time buyer met them at sign-in, so what
  // is left here is a Policy Version published mid-session (#254,
  // lib/checkout-consent.ts). No identity draws none, because there is nobody
  // to ask.
  const consentBoxes = identity?.consentBoxes ?? NO_CONSENT_BOXES;
  const showConsent = anyConsentBox(consentBoxes);

  // ONE set of questions, for the buyer's own Ticket, and none at all when
  // that Ticket Type asks nothing or the sale hands the buyer nothing (ADR
  // 0048). The other tickets in the cart are not asked about: their Answers are
  // their Holders' to give, from the sale page, after the purchase. Recomputed
  // with the quantities, so emptying the cart removes the section.
  const own = ownTicketSlot(ticketTypes, quantities, buyerHoldsFirstTicket);
  const slots = own === null ? [] : [own];

  // Whether this checkout offers an Upgrade, and about which free Ticket (ADR
  // 0074). Recomputed with the quantities, so changing the cart can withdraw an
  // offer that no longer makes sense — and withdrawing it is also what stops a
  // tick made against an older cart from travelling.
  const upgrade = upgradeOffer(
    ticketTypes,
    quantities,
    surrenderableFreeTickets,
    buyerHoldsFirstTicket,
  );

  const count = totalQuantity(quantities);
  const total = totalCents(ticketTypes, quantities);
  const currency = ticketTypes[0]?.currency ?? "USD";
  const formatLocale = useFormatLocale();
  // Two hundred region names and a collation sort, held across the keystrokes
  // that re-render the form around the selector.
  const countryRows = useMemo(() => countries(formatLocale), [formatLocale]);

  // What the address asked for and could not have. Empty for everybody who
  // arrived without an encoded selection, which is everybody today.
  const adjustments = restoredSelection?.adjustments ?? [];

  /**
   * The sentence for one thing that could not be restored.
   *
   * Each branch names a literal message key rather than composing one, so the
   * catalog can be checked and a missing sentence is a build failure instead of
   * a blank line in front of a buyer. The Ticket Type is named by ITS OWN name
   * off the Event payload — an adjustment only ever refers to a Ticket Type this
   * Event has, because lib/selection-url.ts reports on nothing else.
   */
  function adjustmentSentence(adjustment: SelectionAdjustment): string {
    const ticketType =
      ticketTypes.find((candidate) => candidate.id === adjustment.ticketTypeId)?.name ?? "";
    const { requested, restored } = adjustment;
    if (adjustment.reason === "sold_out") {
      return t("restored.soldOut", { ticketType });
    }
    if (adjustment.reason === "closed") {
      // Its Sales Cutoff passed during the round trip through the sign-in wall
      // (ADR 0070). A sentence of its own and never the sold-out one, since the
      // Event may have plenty of these left. A closed Ticket Type is always
      // dropped whole, so there is no reduced form to word.
      return t("restored.closed", { ticketType });
    }
    if (adjustment.reason === "purchase_limit") {
      // Their own allowance, not the Event's stock. The two must not share a
      // sentence (ADR 0025).
      return restored === 0
        ? t("restored.limitReached", { ticketType })
        : t("restored.limitReduced", { ticketType, requested, restored });
    }
    return restored === 0
      ? t("restored.unavailable", { ticketType })
      : t("restored.capacityReduced", { ticketType, requested, restored });
  }

  function adjust(ticketType: PublicTicketType, delta: number) {
    setQuantities((current) => ({
      ...current,
      [ticketType.id]: clampQuantity((current[ticketType.id] ?? 0) + delta, ticketType),
    }));
  }

  /**
   * The address this purchase would be sent back to after signing in as
   * somebody else: this same Event, this same basket, this dialog open again.
   *
   * Recomputed from the live quantities rather than from the address that was
   * arrived on, so a buyer who edited their basket before switching accounts
   * gets the basket they edited (lib/checkout-signin.ts).
   */
  const checkoutReturn = checkoutReturnPath(pathname, quantities);

  /**
   * Buy, for a buyer who has an identity. There is nothing to fetch: who they
   * are, what they still owe and what prefills their fields all came down with
   * the page, so the dialog opens fully formed rather than filling in a moment
   * later.
   *
   * A visitor with no identity never reaches this — their Buy control is a link
   * to sign-in, drawn in its place.
   */
  function openCheckout() {
    setError(null);
    setFieldErrors({});
    setCheckoutOpen(true);
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    // The mirror check: a mistyped cédula is caught here so the buyer is told
    // before a round trip. The API validates the same rules and its verdict is
    // the one that decides whether the sale happens (ADR 0016).
    //
    // The mirror names the rule that broke and the catalog says it in the
    // language this page is being read in — the same entry the API's own field
    // error would have resolved through, so the field says one thing rather than
    // the same thing twice in two languages (ADR 0023).
    const taxIdProblem = validateTaxId(taxIdType, taxIdNumber);
    if (taxIdProblem) {
      setError(null);
      setFieldErrors({ customer_tax_id_number: fieldCodeMessage(errorCopy, taxIdProblem) });
      return;
    }

    // The phone, if there is one at all. An empty field is not a problem to
    // report: the number is optional, and a buyer who skips it simply types it
    // on the payment page as they do today (#103). A number that IS typed is
    // held to the same mirror check as the Tax ID above, so a slipped digit is
    // caught before the round trip rather than after it.
    const phone = composePhone(phoneDiallingCode, phoneNationalNumber);
    const phoneProblem = validatePhone(phone);
    if (phoneProblem) {
      setError(null);
      setFieldErrors({ customer_phone: fieldCodeMessage(errorCopy, phoneProblem) });
      return;
    }
    // Non-null exactly when the buyer gave a number that passed, which is the
    // one condition under which the field is sent at all.
    const canonicalPhone = phone === "" ? null : normalizePhone(phone);

    // The answer section, reduced to what the buyer actually said. Computed
    // AFTER the two mirror checks above and never gating them: it cannot fail,
    // and there is no third check here for it to be part of.
    const answerBodies = checkoutAnswerBodies(slots, answers);

    setSubmitting(true);
    setError(null);
    setFieldErrors({});

    try {
      const response = await fetch("/api/checkout", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          org_slug: orgSlug,
          event_slug: eventSlug,
          event_name: eventName,
          // NO ADDRESS. The route reads it off the Customer Session and the API
          // reads it off the same session behind that, so the sale can only be
          // written to an inbox somebody proved they own (ADR 0054, #384). There
          // is no field here to send one from and nothing downstream would read
          // it if there were.
          customer_first_name: firstName,
          customer_last_name: lastName,
          customer_tax_id_type: taxIdType,
          customer_tax_id_number: normalizeTaxIdNumber(taxIdType, taxIdNumber),
          // Present only when the buyer gave a number, and canonical when it is:
          // the key is dropped rather than sent blank, all the way down to the
          // Prepare payload, so a skipped field reaches PayPhone as an absence
          // rather than as a value nobody entered.
          ...(canonicalPhone ? { customer_phone: canonicalPhone } : {}),
          // The consent answers, as the buyer left the boxes THAT WERE DRAWN.
          // A box that was shown and left unticked is an explicit No and is
          // recorded as one (ADR 0034); a box that was never drawn is omitted
          // entirely, all the way down to a NULL in the evidence, because "not
          // asked" is not a refusal (#254). The required one is sent as it
          // stands rather than assumed — the API refuses the checkout without it
          // from anybody who is still owed it, and the disabled button below is
          // the courtesy, not the rule.
          ...(consentBoxes.policy_acceptance ? { policy_acceptance: policyAccepted } : {}),
          ...(consentBoxes.marketing_consent ? { marketing_consent: marketingConsent } : {}),
          ...(consentBoxes.networking_consent ? { networking_consent: networkingConsent } : {}),
          ...(consentBoxes.terms_acceptance ? { terms_acceptance: termsAccepted } : {}),
          // The declaration, sent only where it was drawn (#588). Where it was
          // owed and left unticked this sends `false` and the API refuses the
          // checkout with ADULTHOOD_DECLARATION_REQUIRED, creating no Payment
          // and writing nothing at all — the disabled button below is the
          // courtesy, that refusal is the guarantee.
          ...(consentBoxes.adulthood_declaration
            ? { adulthood_declaration: adulthoodDeclared }
            : {}),
          // The answer section, as far as the buyer filled it in. The key is
          // dropped entirely when they skipped it — which is the ordinary case
          // and an explicitly supported way to check out — so a cart with
          // nothing to say posts the body it posted before this feature existed.
          //
          // NOTHING IS CHECKED BEFORE SENDING and nothing above this line can
          // stop the send over an answer: the two mirror checks in this handler
          // are the Tax ID and the phone, and an answer deliberately joins
          // neither of them (ADR 0044).
          ...(answerBodies.length > 0 ? { answers: answerBodies } : {}),
          // The Upgrade Prompt's answer (#652, ADR 0074), sent ONLY WHERE THE
          // PROMPT IS DRAWN for the cart as it stands right now — which is the
          // whole of how a tick made against an older cart is prevented from
          // travelling, and why the state above is never reset behind the
          // buyer's back.
          //
          // The election rides the Payment across the Payment Provider redirect
          // and is read back inside the commit's transaction; the return leg
          // carries no election of its own. NOTHING HERE CAN FAIL THE PURCHASE:
          // the commit re-judges eligibility and ignores an election it did not
          // offer, so a cart that changed in a tab left open overnight still
          // sells the buyer the Ticket they asked for.
          //
          // It goes into `upgrade_elected` and the cart's `lines` are sent
          // UNCHANGED: dropping the free line here would be this app performing
          // the Upgrade, and a second implementation of a rule the commit owns.
          ...(upgrade !== null ? { upgrade_elected: upgradeElected } : {}),
          lines: selectionLines(quantities),
          // The language this page is being read in, stated rather than left to
          // be inferred. It is written into the checkout context cookie so the
          // buyer comes back from the Payment Provider into the language they
          // left in, and it goes no further — the Go API stays Locale-unaware.
          // The route can also read it off the Referer, but that header is one
          // Referrer-Policy or privacy extension away from not being there, and
          // this page is the one thing that knows the answer for certain.
          locale,
        }),
      });
      const envelope = (await response.json()) as Envelope<BeginCheckoutResult>;
      if (!response.ok || envelope.error || !envelope.data) {
        const apiError = envelope.error;
        setFieldErrors(fieldErrorsFromDetails(errorCopy, apiError?.details));
        setError({
          code: apiError?.code ?? null,
          message: apiError?.message ?? null,
          details: apiError?.details,
          fallback: "startFailed",
        });
        setSubmitting(false);
        return;
      }
      const destination = checkoutDestination(envelope.data);
      if (!destination) {
        // The API accepted the checkout but named nowhere to go, so there is
        // nothing truthful to navigate to. Say so rather than assign a missing
        // redirect_url, which the browser would resolve against this event page.
        setError({ code: null, message: null, details: undefined, fallback: "startFailed" });
        setSubmitting(false);
        return;
      }
      // Full-page navigation: to the provider's hosted payment page, or straight
      // to the confirmation when there was nothing to pay. submitting stays true
      // so the button cannot fire a second Payment while the browser unloads.
      //
      // The two destinations are told apart by their shape, because only one of
      // them is ours to localize: an absolute provider URL is left exactly as
      // the API sent it, while the confirmation page is a path on this
      // Storefront and gains the language the buyer is reading in.
      window.location.assign(
        destination.startsWith("/") ? localizedPath(locale, destination) : destination,
      );
    } catch {
      setError({ code: null, message: null, details: undefined, fallback: "networkFailed" });
      setSubmitting(false);
    }
  }

  /**
   * Refused over what was in the cart: back to the steppers with fresh remaining
   * counts and Purchase Limits, and nothing selected.
   */
  function backToTickets() {
    setCheckoutOpen(false);
    setQuantities({});
    // The answers go with the cart they were about. Kept, they would be replies
    // for tickets nobody is buying any more — harmless on the wire, since the
    // slots decide what travels, and confusing on screen the moment the buyer
    // picks the same Ticket Type again and finds somebody else's size in it.
    setAnswers({});
    router.refresh();
  }

  const capacityExceeded = error?.code === "CAPACITY_EXCEEDED";
  // A Purchase Limit refusal is about the Customer, not the Event (ADR 0025), so
  // it gets its own title — "you already have yours" against "not enough tickets
  // left". All three share this 409's handling because the remedy is the same one:
  // the cart cannot be paid for as it stands, and no field on the form is what is
  // wrong with it. Filling the form in again would be refused identically.
  //
  const purchaseLimitExceeded = error?.code === "PURCHASE_LIMIT_EXCEEDED";
  // A Ticket Type whose Sales Cutoff passed while this tab was open (ADR 0070).
  // A third title beside the other two, and pointedly not the sold-out one: the
  // Event may have plenty of these left. It joins the same 409 handling because
  // the remedy is the same one — the cart cannot be paid for as it stands, and
  // nothing on the form is what is wrong with it.
  const ticketTypeClosed = error?.code === "TICKET_TYPE_CLOSED";
  const cartRefused = capacityExceeded || purchaseLimitExceeded || ticketTypeClosed;


  // Neither the title nor the body claims the Customer already HOLDS any of
  // these tickets, though this refusal usually means they do. The API refuses
  // whenever held + requested exceeds the Purchase Limit, so `already_held` is 0
  // when the REQUEST alone was too large — unreachable through this page, whose
  // steppers never offer more than offerableQuantity, but well within the API's
  // contract and reachable by a caller that is not this page. One sentence has
  // to be true of both, so it states the two numbers and lets the buyer read
  // them, rather than asserting a possession that may be zero.
  //
  // Selecting between two sentences on `already_held` was the alternative, and
  // ADR 0023 forecloses it: copy resolves on `error.code` and "nothing else
  // selects" it, the interception list there being closed rather than an
  // invitation.

  return (
    <>
      {/* What the address asked for and this page could not give back
          (ADR 0054). It is drawn ABOVE the ticket list, before the buyer reads
          a single price, because it is the difference between the basket they
          pressed Buy on and the one in front of them now — and a difference
          they discover at the total is a difference they were misled about.

          It is not dismissed and does not fade when the steppers move: it is a
          statement about what happened on arrival, and that stays true however
          the buyer edits things afterwards.

          Nothing here reasons about the rules; `reason` already carries the
          verdict from lib/selection-url.ts, and the only job left is choosing
          the sentence. Sold out and a spent Purchase Limit get different words
          on purpose — a Customer who has used their own allowance must never
          read it as the Event being full (ADR 0025). */}
      {adjustments.length > 0 ? (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>{t("restored.title")}</AlertTitle>
          <AlertDescription>
            <ul className="list-disc space-y-1 pl-4">
              {adjustments.map((adjustment) => (
                <li key={adjustment.ticketTypeId}>
                  {adjustmentSentence(adjustment)}
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="space-y-3">
        {ticketTypes.map((ticketType) => {
          const quantity = quantities[ticketType.id] ?? 0;
          // The most this stepper may offer: remaining capacity, narrowed by
          // what is left of this Customer's own allowance under the Purchase
          // Limit (ADR 0025). Recomputed per render rather than memoized because
          // it is arithmetic on numbers already in hand.
          const offerable = offerableQuantity(ticketType);
          // Unavailable to THIS Customer and to nobody else: their allowance is
          // gone while the Ticket Type is still on sale (#168). It is worded and
          // badged apart from sold out on purpose — a Customer who has used
          // their limit must never walk away believing the Event is full, which
          // is the one wrong conclusion this whole state exists to prevent.
          //
          // False for every anonymous visitor, who has no known holdings at all,
          // so the page they see is unchanged.
          const limitReached = allowanceSpent(ticketType);
          // Past its Sales Cutoff, on the SERVER's clock and never on this
          // browser's (ADR 0070). The verdict arrives on the payload already
          // made; deriving it here from sales_cutoff_at would let a machine set
          // to yesterday draw a stepper the API is going to refuse, which is the
          // precise failure the verdict exists to prevent.
          //
          // It joins the unsellable set rather than the sold-out one: the words
          // stay different everywhere, and the treatment is the same because the
          // buyer can do the same thing with either card — nothing.
          const sellable = !ticketType.sold_out && !limitReached && !ticketType.closed;
          return (
            <Card key={ticketType.id} className={sellable ? undefined : "opacity-70"}>
              <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="font-semibold">{ticketType.name}</h3>
                    {/* ONE badge, ranked sold out → closed → limit reached, and
                        on a card in none of those, the countdown to its Sales
                        Cutoff. The same component the read-only card draws, from
                        the same module, so this Event cannot say one thing while
                        it is selling and another once it has ended (ADR 0070).

                        Every input is handed over and the choice is made there:
                        the rank and the ladder are decisions with unit tests
                        behind them, and a card that re-derived either would be
                        where the two lists start to disagree. */}
                    <TicketTypeStateBadge
                      ticketType={ticketType}
                      limitReached={limitReached}
                      timezone={timezone}
                      now={now}
                    />
                    {/* A price claim only while there is still a sale to make it
                        about (ADR 0070): "37% off" on a Ticket Type nobody can
                        buy advertises a bargain that does not exist. It is not
                        ranked against the state badge — a live Promotion on an
                        open Ticket Type that closes on Friday is two true things
                        at once — so a still-selling card can wear both.

                        Gated on the TICKET TYPE being buyable and not on
                        `sellable`, which also excludes a reader who has spent
                        their own Purchase Limit. The Promotion is a fact about
                        the price everybody is being quoted, and one reader
                        having taken their share does not make it stop being
                        true — this is the same pair the read-only card uses. */}
                    {!ticketType.sold_out && !ticketType.closed ? (
                      <PromotionBadge ticketType={ticketType} />
                    ) : null}
                  </div>
                  {ticketType.description ? (
                    <p className="text-sm text-muted-foreground">{ticketType.description}</p>
                  ) : null}
                  <PromotionDeadline ticketType={ticketType} timezone={timezone} />
                  {/* When the door shut, on the Event's clock — in the slot the
                      Promotion deadline uses, because it answers the same shape
                      of question. A Ticket Type can carry both at once: a
                      Promotion that ran until Friday on a tier that closed on
                      Saturday is two true sentences. */}
                  <SalesClosedLine ticketType={ticketType} timezone={timezone} />
                  {/* The remaining count stays on a Ticket Type whose allowance
                      is spent, and it is the evidence for the sentence beside
                      it: "seven remaining" under "your limit reached" is the
                      Event visibly not being full.

                      It comes OFF a closed one, though, and for the opposite
                      reason: that stock is real and unbuyable, so quoting it
                      would be an offer this page cannot honour. Closed is
                      already told apart from sold out in words, so it does not
                      need the count to prove the Event is not full. */}
                  {!ticketType.sold_out && !ticketType.closed ? (
                    <p className="text-sm text-muted-foreground">
                      {eventCopy("remaining", { count: ticketType.remaining })}
                    </p>
                  ) : null}
                  {/* The null test is the compiler's, not a doubt: a spent
                      allowance implies a Purchase Limit. It is written as a
                      narrowing rather than as a `?? 0` default so the sentence
                      can never be rendered around a limit nobody set. */}
                  {limitReached && ticketType.max_per_customer !== null ? (
                    <p className="text-sm font-medium text-foreground">
                      {eventCopy("limitReachedNote", { limit: ticketType.max_per_customer })}
                    </p>
                  ) : null}
                </div>
                <div className="flex shrink-0 items-center justify-between gap-4 sm:flex-col sm:items-end">
                  <TicketTypePrice ticketType={ticketType} />
                  {sellable ? (
                    <div className="flex items-center gap-1">
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-11"
                        aria-label={t("decreaseQuantity", { ticketType: ticketType.name })}
                        disabled={quantity === 0}
                        onClick={() => adjust(ticketType, -1)}
                      >
                        −
                      </Button>
                      <span
                        className="w-10 text-center text-base font-semibold tabular-nums"
                        aria-label={t("quantityLabel", { ticketType: ticketType.name })}
                        aria-live="polite"
                      >
                        {quantity}
                      </span>
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-11"
                        aria-label={t("increaseQuantity", { ticketType: ticketType.name })}
                        disabled={quantity >= offerable}
                        onClick={() => adjust(ticketType, 1)}
                      >
                        +
                      </Button>
                    </div>
                  ) : null}
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>

      {allClosed ? (
        // Every Ticket Type has closed, so the sticky bar is not drawn at all and
        // one sentence takes its place (ADR 0070). The list above is untouched —
        // its cards, its descriptions and its prices all stay — and this is what
        // stops a page of dimmed cards from reading as a page that failed to
        // load.
        //
        // The sentence sits where the bar was but does not follow the reader down
        // the page: a control you might want under your thumb is one thing, a
        // piece of bad news that will not go away is another.
        //
        // It is deliberately not the sold-out sentence. An Event that is merely
        // exhausted says something else, and this feature does not write it.
        <div className="-mx-4 mt-4 border-t px-4 py-3">
          <p className="text-sm font-medium text-foreground">{eventCopy("allSalesClosed")}</p>
          {priceIncludesFee ? (
            <p className="text-xs text-muted-foreground">{eventCopy("feeIncluded")}</p>
          ) : null}
        </div>
      ) : (
        /* Sticky total bar: the running total and primary CTA stay
           thumb-reachable while the ticket list scrolls (docs/design/storefront.md). */
        <div className="sticky bottom-0 -mx-4 mt-4 border-t bg-background/95 px-4 py-3 backdrop-blur supports-[backdrop-filter]:bg-background/80">
          <div className="flex items-center justify-between gap-4">
            <div>
              {/* "No tickets selected" is the zero case of the same sentence, not
                  a different one: which of the three a language needs is the
                  catalog's plural rules to decide, not this component's. */}
              <p className="text-sm text-muted-foreground">{t("selectionCount", { count })}</p>
              <p className="text-lg font-semibold" data-testid="selection-total">
                {formatPrice(total, currency, formatLocale)}
              </p>
              {priceIncludesFee ? (
                <p className="text-xs text-muted-foreground">{eventCopy("feeIncluded")}</p>
              ) : null}
            </div>
            {/* THE WALL AT BUY, and the only place it stands (ADR 0054, #385).

                For a buyer with an identity this is the button it has always
                been. For a visitor without one it is a LINK — a real one, so it
                reads as going somewhere, opens in a new tab, and can be seen for
                what it is before it is pressed — pointing at the ordinary sign-in
                page and carrying the basket in its destination. Nothing above
                this bar changed: the prices, the counts and the steppers are the
                same for everybody, because gating discovery is the one thing the
                Storefront may not do (ADR 0002, ADR 0037).

                The disabled state is identical in both arms and is about the
                basket rather than the buyer: an empty cart has nowhere to go,
                signed in or out. The link's `aria-disabled` is what says so,
                since an anchor cannot be disabled. */}
            {identity === null ? (
              <Button asChild size="lg" className={cn("h-11", count === 0 && "pointer-events-none opacity-50")}>
                <Link
                  href={checkoutSignInHref(pathname, quantities)}
                  aria-disabled={count === 0}
                  tabIndex={count === 0 ? -1 : undefined}
                >
                  {t("getTickets")}
                </Link>
              </Button>
            ) : (
              <Button type="button" size="lg" className="h-11" disabled={count === 0} onClick={openCheckout}>
                {t("getTickets")}
              </Button>
            )}
          </div>
        </div>
      )}

      <Dialog
        open={checkoutOpen}
        onOpenChange={(open) => {
          // The dialog must not close underneath an in-flight begin request:
          // that is the blocking "processing" state (docs/design/foundation.md).
          if (!submitting) setCheckoutOpen(open);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("title")}</DialogTitle>
            <DialogDescription>{t("description", { event: eventName })}</DialogDescription>
          </DialogHeader>

          {/* WHO THIS PURCHASE IS ADDRESSED TO, said loudly and first
              (ADR 0054, #385).

              This is not fine print and must never become it. A Customer
              Session satisfies the wall at Buy for its whole life with no
              re-proof at the till — which is defensible, because the Customer
              Area already exposes strictly more behind the same session — so
              the mitigation for the shared machine is that the address is
              IMPOSSIBLE TO MISS. Hence the top of the dialog, an accent border,
              a filled panel, and the address at heading size on its own line
              rather than in a sentence.

              Beside it, the way out: signing in as somebody else, which returns
              to this Event with this basket and this dialog open, so changing
              your mind about which account you are costs you nothing you had
              already chosen. */}
          {identity ? (
            <div
              className="rounded-lg border-2 border-primary/50 bg-primary/5 p-4"
              data-testid="checkout-identity"
            >
              <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {t("identity.label")}
              </p>
              <p className="mt-1 break-all text-lg font-semibold leading-tight">
                {identity.email}
              </p>
              <p className="mt-1 text-sm text-muted-foreground">{t("identity.note")}</p>
              {/* Ends the session before it lands on /signin, because /signin
                  sends a signed-in visitor straight on to their destination —
                  a plain link here would bounce them back to this same dialog
                  under the same address, having achieved nothing. */}
              <div className="mt-2">
                <SignInOtherAddressButton next={checkoutReturn} label={t("identity.switch")} />
              </div>
            </div>
          ) : null}

          <div className="space-y-1 rounded-lg border bg-muted/40 p-3 text-sm">
            {ticketTypes
              .filter((ticketType) => (quantities[ticketType.id] ?? 0) > 0)
              .map((ticketType) => {
                const quantity = quantities[ticketType.id] ?? 0;
                return (
                  <div key={ticketType.id} className="flex items-center justify-between gap-2">
                    <span>{t("lineQuantity", { count: quantity, ticketType: ticketType.name })}</span>
                    <span className="tabular-nums">
                      {formatPrice(quantity * ticketType.price_cents, ticketType.currency, formatLocale)}
                    </span>
                  </div>
                );
              })}
            <div className="flex items-center justify-between gap-2 border-t pt-1 font-semibold">
              <span>{t("total")}</span>
              <span className="tabular-nums">{formatPrice(total, currency, formatLocale)}</span>
            </div>
            {priceIncludesFee ? (
              <p className="text-xs font-normal text-muted-foreground">{eventCopy("feeIncluded")}</p>
            ) : null}
          </div>

          {error ? (
            <Alert variant="destructive">
              {capacityExceeded ? <AlertTitle>{t("capacityTitle")}</AlertTitle> : null}
              {purchaseLimitExceeded ? <AlertTitle>{t("purchaseLimitTitle")}</AlertTitle> : null}
              {ticketTypeClosed ? <AlertTitle>{t("closedTitle")}</AlertTitle> : null}
              {/* The API decided which failure this is; the catalog decides how
                  to say it, in this page's language, falling back to the API's
                  own message for a code it does not know. This app's own two
                  sentences are for the failures the API never got to report. */}
              <AlertDescription>
                {apiErrorMessage(errorCopy, error) ?? t(error.fallback)}
              </AlertDescription>
            </Alert>
          ) : null}

          {cartRefused ? (
            <DialogFooter>
              <Button type="button" variant="secondary" className="h-11 w-full" onClick={backToTickets}>
                {t("backToSelection")}
              </Button>
            </DialogFooter>
          ) : !identity ? (
            /*
              No identity, no form. Unreachable in practice — the Buy control is
              a link to sign-in for a visitor with no Customer Session, so this
              dialog never opens for one — and written as a branch rather than
              as an assertion so that a caller which opens it anyway meets an
              honest refusal instead of a form addressed to nobody. It is also
              what the API would do: the session-gated begin-checkout refuses a
              request with no session outright (#384).
            */
            <Alert variant="destructive">
              <AlertDescription>{t("identity.required")}</AlertDescription>
            </Alert>
          ) : !policy ||
            (consentBoxes.terms_acceptance && !terms) ||
            (consentBoxes.adulthood_declaration && !terms?.adulthood_declaration_label) ? (
            /*
              No notice, no form. The API could not be reached for the current
              Policy Version, so this dialog cannot show what is being accepted
              — and a checkout that collected an acceptance of nothing would be
              worse than an honest failure. It is also what the API would do
              anyway: it refuses a checkout without Policy Acceptance, and it is
              the same read that would have supplied the words. The Privacy
              Policy page 404s in the same situation for the same reason
              (ADR 0036). The Terms artifact is under the same rule (#537), and
              only where its box is OWED: a buyer current on the Terms loses
              nothing to a failed read of a document they are not being asked
              about.

              The Adulthood Declaration's LABEL is under it too (#588). Its box
              is owed only where the Terms box is, so `terms` is already present
              by the time it matters; what can still be missing is the label
              itself, on an edition published with the artifact in the
              prevailing text and not in the translation. That reader is owed a
              box this app cannot word, and the honest failure beats both
              alternatives — skipping the box would send a checkout the API is
              about to refuse, and drawing it wordless is what ADR 0069 §3
              forbids.
            */
            <Alert variant="destructive">
              <AlertDescription>{t("consent.unavailable")}</AlertDescription>
            </Alert>
          ) : (
            <form className="space-y-4" onSubmit={handleSubmit} noValidate>
              {/* No email field. The address is stated above, off the session,
                  and there is no way to type a different one here — which is the
                  load-bearing half of ADR 0054: while the field exists the
                  mistake is expressible, and eventually something expresses it.

                  The name IS still asked for, and usually for the first time:
                  signing in mints a Customer with no name. */}
              <div className="grid gap-4 sm:grid-cols-2">
                <FormField
                  id="checkout-first-name"
                  label={t("firstNameLabel")}
                  error={fieldErrors.customer_first_name}
                >
                  <Input
                    name="given-name"
                    autoComplete="given-name"
                    required
                    value={firstName}
                    onChange={(event) => setFirstName(event.target.value)}
                  />
                </FormField>
                <FormField
                  id="checkout-last-name"
                  label={t("lastNameLabel")}
                  error={fieldErrors.customer_last_name}
                >
                  <Input
                    name="family-name"
                    autoComplete="family-name"
                    required
                    value={lastName}
                    onChange={(event) => setLastName(event.target.value)}
                  />
                </FormField>
              </div>
              {/* The Tax ID: required on every Online Sale so the Organization
                  can declare it (ADR 0016). Type and number are one fact split
                  across two controls, so they sit on one row. */}
              <div className="grid gap-4 sm:grid-cols-[minmax(0,9rem)_1fr]">
                <FormField
                  id="checkout-tax-id-type"
                  label={t("taxIdTypeLabel")}
                  error={fieldErrors.customer_tax_id_type}
                >
                  <select
                    name="tax-id-type"
                    className={SELECT_CLASS}
                    required
                    value={taxIdType}
                    onChange={(event) => {
                      if (isTaxIdType(event.target.value)) setTaxIdType(event.target.value);
                    }}
                  >
                    {/* Cédula, RUC and Pasaporte are the names of Ecuadorian
                        documents, so they read the same in both languages and
                        are not in the catalog. */}
                    {TAX_ID_TYPES.map((type) => (
                      <option key={type} value={type}>
                        {TAX_ID_TYPE_LABELS[type]}
                      </option>
                    ))}
                  </select>
                </FormField>
                <FormField
                  id="checkout-tax-id-number"
                  label={t("taxIdNumberLabel")}
                  error={fieldErrors.customer_tax_id_number}
                >
                  <Input
                    name="tax-id-number"
                    inputMode={taxIdType === "passport" ? "text" : "numeric"}
                    autoComplete="off"
                    required
                    value={taxIdNumber}
                    onChange={(event) => setTaxIdNumber(event.target.value)}
                  />
                </FormField>
              </div>
              {/* The phone number: optional, on purpose (#103). A buyer who
                  fills it reaches the payment page with nothing left to enter
                  but their card; a buyer who skips it loses nothing and types it
                  there instead. Making it required would move friction to the
                  screen where abandonment costs most, and would invite exactly
                  the junk input PayPhone's fraud rules punish — so there is no
                  `required` here, and no value at all beyond the Customer's own
                  stored number (#108). Country and number are one fact split
                  across two controls, so they share a row like the Tax ID
                  above. */}
              <div className="grid gap-4 sm:grid-cols-[minmax(0,11rem)_1fr]">
                <FormField id="checkout-phone-country" label={t("phoneCountryLabel")}>
                  <select
                    name="phone-country"
                    className={SELECT_CLASS}
                    value={phoneDiallingCode}
                    onChange={(event) => setPhoneDiallingCode(event.target.value)}
                  >
                    {/* Keyed by region code, valued by dialling code: the region
                        is the row's identity and its name is only a rendering of
                        it, so the key survives a change of language. The dialling
                        codes are not unique (+1 covers the US, Canada and twenty
                        more), so picking one of a shared code shows the first
                        country listed under it — the accepted cosmetic
                        imperfection from #103, and the number submitted is
                        identical either way. */}
                    {countryRows.map((country) => (
                      <option key={country.regionCode} value={country.diallingCode}>
                        {t("countryOption", {
                          country: country.name,
                          diallingCode: country.diallingCode,
                        })}
                      </option>
                    ))}
                  </select>
                </FormField>
                <FormField
                  id="checkout-phone"
                  label={t("phoneLabel")}
                  error={fieldErrors.customer_phone}
                >
                  <Input
                    name="tel-national"
                    type="tel"
                    inputMode="tel"
                    autoComplete="tel-national"
                    value={phoneNationalNumber}
                    onChange={(event) => setPhoneNationalNumber(event.target.value)}
                  />
                </FormField>
              </div>
              {/*
                The buyer's own Ticket's questions (#311, ADR 0048), drawn
                ABOVE the consent section and below the buyer's own details:
                they are facts about the buyer, asked once they have said who
                they are, and consent stays adjacent to the pay button it gates.

                It gates nothing. There is no required check anywhere in it, and
                the submit button below deliberately does not mention it.
              */}
              {own !== null ?
                <CheckoutAnswers
                  slot={own}
                  values={answers}
                  onChange={(key, value) =>
                    setAnswers((current) => ({ ...current, [key]: value }))
                  }
                  labels={{
                    title: t("answers.title", { ticketType: own.ticketTypeName }),
                    hint: t("answers.hint"),
                    optional: t("answers.optional"),
                    optionalLabel: (question: string) => t("answers.optionalLabel", { question }),
                    noAnswer: t("answers.noAnswer"),
                  }}
                />
              : null}
              {/*
                The Upgrade Prompt (#652, ADR 0074), drawn where the platform
                offers one and nowhere else — the arithmetic is upgradeOffer's
                and this line only asks it.

                It sits BESIDE the answer section and above consent, among the
                other things that can be skipped, because that is what it is: a
                question about the buyer's own Ticket that gates nothing. The pay
                button below deliberately does not mention it, and there is no
                required check anywhere in it.

                The sentence above the box names which free Ticket this is about
                — the one in the basket, or the one already held — and never
                which mechanism gives it up. The buyer is told neither.
              */}
              {upgrade !== null ? (
                <UpgradePrompt
                  checked={upgradeElected}
                  onChange={setUpgradeElected}
                  labels={{
                    title: t("upgrade.title"),
                    situation:
                      upgrade.surrendered === null ? t("upgrade.earlier") : t("upgrade.inBasket"),
                    label: t("upgrade.label"),
                    hint: t("upgrade.hint"),
                  }}
                />
              ) : null}
              {/*
                The consent section: the Short Notice and the three boxes, in
                the dialog the purchase happens in, because the guidance
                requires the information to be present AT the moment of capture
                (parent #249, user story 7).

                The notice is COLLAPSED by default and the summary line says
                what it is. It is a whole privacy notice sitting on a form whose
                job is to sell a ticket, and rendered open it would be the
                largest thing in the dialog by some margin — pushing the boxes
                and the pay button below the fold on a phone, which is where
                this checkout mostly happens. Collapsed, it is one line the
                buyer can open in place, and the full policy is a link away.
                Nothing is hidden that has to be read: the checkbox labels are
                always visible, and each says what it authorizes.
              */}
              {showConsent ? (
                <div className="space-y-3 rounded-lg border bg-muted/40 p-3">
                  <details className="text-sm">
                    <summary className="cursor-pointer font-medium">{t("consent.notice")}</summary>
                    <Markdown className="mt-2 text-sm [&>p]:mt-2 [&>p]:first:mt-0">
                      {policy.short_notice}
                    </Markdown>
                  </details>
                  <p className="text-sm">
                    <Link
                      href={PRIVACY_POLICY_PATH}
                      target="_blank"
                      className="font-medium underline underline-offset-4"
                    >
                      {t("consent.readPolicy")}
                    </Link>
                  </p>
                  {consentBoxes.policy_acceptance ? (
                    <ConsentCheckbox
                      id="consent-policy-acceptance"
                      checked={policyAccepted}
                      onChange={setPolicyAccepted}
                      label={policy.consent_labels.policy_acceptance}
                      optionalLabel={null}
                      className="bg-background"
                    />
                  ) : null}
                  {consentBoxes.marketing_consent ? (
                    <ConsentCheckbox
                      id="consent-marketing"
                      checked={marketingConsent}
                      onChange={setMarketingConsent}
                      label={policy.consent_labels.marketing_consent}
                      optionalLabel={t("consent.optional")}
                      className="bg-background"
                    />
                  ) : null}
                  {consentBoxes.networking_consent ? (
                    <ConsentCheckbox
                      id="consent-networking"
                      checked={networkingConsent}
                      onChange={setNetworkingConsent}
                      label={policy.consent_labels.networking_consent}
                      optionalLabel={t("consent.optional")}
                      className="bg-background"
                    />
                  ) : null}
                  {/*
                    The Terms box (#537, ADR 0066): the contractual acceptance,
                    separate from every privacy answer, worded by the backend
                    artifact and never pre-ticked. Drawn for exactly one buyer —
                    one whose live session spans a Terms edition — and linking
                    to the public terms page, the full Spanish document, in a
                    new tab exactly as the sign-in step's does (#536).
                  */}
                  {consentBoxes.terms_acceptance && terms ? (
                    <>
                      <ConsentCheckbox
                        id="consent-terms-acceptance"
                        checked={termsAccepted}
                        onChange={setTermsAccepted}
                        label={terms.acceptance_label}
                        optionalLabel={null}
                        className="bg-background"
                      />
                      <p className="text-sm">
                        <Link
                          href={TERMS_PATH}
                          target="_blank"
                          className="font-medium underline underline-offset-4"
                        >
                          {t("consent.readTerms")}
                        </Link>
                      </p>
                    </>
                  ) : null}
                  {/*
                    The Adulthood Declaration (#588, ADR 0069): a SECOND box
                    beside the Terms one, mandatory, un-premarked, and worded by
                    the backend artifact exactly as its neighbour is — the label
                    is evidence, covered by the edition's fingerprint, so
                    nothing here may reword it and no catalog string may stand
                    in for it.

                    Drawn iff the edition in effect publishes the label, which
                    is why it needs both the box AND the words before it
                    appears: the answer to "does this edition ask?" is the
                    presence of the artifact, and an edition that carries none
                    draws nothing here and owes nothing at the API.

                    Its DOM id is `consent-adulthood-declaration`, the
                    `#consent-<box>` convention this dialog and the sign-in step
                    already share and the e2e specs assert absence against.
                  */}
                  {consentBoxes.adulthood_declaration && terms?.adulthood_declaration_label ? (
                    <ConsentCheckbox
                      id="consent-adulthood-declaration"
                      checked={adulthoodDeclared}
                      onChange={setAdulthoodDeclared}
                      label={terms.adulthood_declaration_label}
                      optionalLabel={null}
                      className="bg-background"
                    />
                  ) : null}
                </div>
              ) : null}
              <Button
                type="submit"
                className="h-11 w-full"
                // The required box gates the pay action, and only the required
                // one: declining the optional two costs nothing (parent #249,
                // user story 9). The API refuses the same checkout on its own
                // account, so this is what the buyer sees rather than what makes
                // it true.
                //
                // It gates only where it was DRAWN. A Customer who has already
                // accepted the current Policy Version is shown no such box and
                // must not be held behind one they cannot tick — the API owes
                // them no acceptance either, and would take this checkout.
                //
                // Held until the session read settles, because until then this
                // dialog does not know which of those two people it is serving.
                // It is the same read the fields above are waiting on, and a
                // guest's costs no call to the API at all.
                disabled={
                  submitting ||
                  (consentBoxes.policy_acceptance && !policyAccepted) ||
                  (consentBoxes.terms_acceptance && !termsAccepted) ||
                  (consentBoxes.adulthood_declaration && !adulthoodDeclared)
                }
                aria-busy={submitting}
              >
                {submitting ? t("submitting") : t("submit")}
              </Button>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
