"use client";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
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
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useMemo, useRef, useState, type FormEvent } from "react";

import { useFormatLocale } from "@/i18n/format-locale";
import type { BeginCheckoutResult, PublicTicketType } from "@/lib/api";
import {
  apiErrorMessage,
  fieldCodeMessage,
  fieldErrorMessages,
  type ErrorCatalog,
} from "@/lib/api-errors";
import {
  allowanceSpent,
  checkoutDestination,
  clampQuantity,
  offerableQuantity,
  selectionLines,
  totalCents,
  totalQuantity,
} from "@/lib/checkout";
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
import {
  TAX_ID_TYPES,
  TAX_ID_TYPE_LABELS,
  isTaxIdType,
  normalizeTaxIdNumber,
  validateTaxId,
  type TaxIdType,
} from "@/lib/tax-id";

import { PromotionBadge, PromotionDeadline, TicketTypePrice } from "./promotion";

/**
 * Ticket selection and the one checkout step, inline on the event page
 * (docs/design/storefront.md): quantity steppers per Ticket Type, a sticky
 * running total, and a Dialog collecting email + first/last name + Tax ID, plus
 * an optional phone number — prefilled from the Customer Session when one
 * exists, guest checkout otherwise.
 *
 * The phone is the one field that is optional, and it prefills only from the
 * Customer's own stored number (#108). Nothing else ever gives it a value: no
 * default, no placeholder, no filler. A number that appears in it is one the
 * person themselves put on their profile — anything else would be the static
 * cardholder data PayPhone's rules prohibit.
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

type SessionData = {
  email: string;
  first_name: string;
  last_name: string;
  tax_id_type: string | null;
  tax_id_number: string | null;
  /** The stored phone in canonical E.164 form, null when the Customer has none. */
  phone: string | null;
};

type TicketSelectionProps = {
  orgSlug: string;
  eventSlug: string;
  eventName: string;
  ticketTypes: PublicTicketType[];
  /**
   * Whether the quoted prices carry the platform's service fee, which is the
   * only thing that decides the muted note below. Prices themselves are always
   * shown exactly as the API quotes them: this app never does fee arithmetic
   * (ADR 0014).
   */
  priceIncludesFee: boolean;
  /** The Event's timezone, which a Promotion's deadline is read in (ADR 0021). */
  timezone: string | null;
};

/** The API's begin-checkout field names, mapped onto the form's inputs. */
const FORM_FIELDS = [
  "customer_email",
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

export function TicketSelection({
  orgSlug,
  eventSlug,
  eventName,
  ticketTypes,
  priceIncludesFee,
  timezone,
}: TicketSelectionProps) {
  // next/navigation's router, deliberately: the only thing asked of it here is
  // refresh(), which has no address to localize.
  const router = useRouter();
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
  const [quantities, setQuantities] = useState<Record<string, number>>({});
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  // Cédula is the default because it is what the overwhelming majority of
  // buyers hold; the select is still required, so nothing is submitted under a
  // type the buyer never looked at without them also typing its number.
  const [taxIdType, setTaxIdType] = useState<TaxIdType>("cedula");
  const [taxIdNumber, setTaxIdNumber] = useState("");
  // The phone number, split across a country selector and a text field purely
  // for entry: what is submitted is the single canonical E.164 string the two
  // assemble into (#103). Ecuador is selected by default because it is the home
  // market and the overwhelming majority of buyers — the common case should need
  // no interaction at all.
  //
  // They start empty and are filled in only from the Customer's OWN stored
  // number, once the session read comes back (#108). Nothing else may write
  // them: PayPhone's rules prohibit static or filler cardholder data, so a
  // number in this field is always one the person themselves entered — here or,
  // earlier, on their profile — and an untouched field submits nothing at all.
  const [phoneDiallingCode, setPhoneDiallingCode] = useState(ECUADOR_DIALLING_CODE);
  const [phoneNationalNumber, setPhoneNationalNumber] = useState("");
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [error, setError] = useState<CheckoutError | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const prefillAttempted = useRef(false);
  const taxIdTouched = useRef(false);
  const phoneTouched = useRef(false);

  const count = totalQuantity(quantities);
  const total = totalCents(ticketTypes, quantities);
  const currency = ticketTypes[0]?.currency ?? "USD";
  const formatLocale = useFormatLocale();
  // Two hundred region names and a collation sort, held across the keystrokes
  // that re-render the form around the selector.
  const countryRows = useMemo(() => countries(formatLocale), [formatLocale]);

  function adjust(ticketType: PublicTicketType, delta: number) {
    setQuantities((current) => ({
      ...current,
      [ticketType.id]: clampQuantity((current[ticketType.id] ?? 0) + delta, ticketType),
    }));
  }

  /**
   * Prefill from the Customer Session, once, when checkout opens. A signed-in
   * Customer gets their email and name filled in; anonymous visitors get an
   * empty form and no error — guest checkout is the baseline, not a fallback.
   * Fields the visitor already typed in are never overwritten.
   */
  async function prefillFromSession() {
    if (prefillAttempted.current) return;
    prefillAttempted.current = true;
    try {
      const response = await fetch("/api/customer/auth/session");
      if (!response.ok) return;
      const envelope = (await response.json()) as Envelope<SessionData>;
      if (!envelope.data) return;
      const session = envelope.data;
      setEmail((current) => current || session.email);
      setFirstName((current) => current || session.first_name);
      setLastName((current) => current || session.last_name);
      // The stored Tax ID is a prefill, not a lock: a signed-in Customer may
      // override it for this purchase — buying under a company RUC instead of
      // their cédula — and the override becomes their new stored default
      // (ADR 0016). A Customer who has never supplied one gets the empty form.
      //
      // The pair moves together, so "already typed in" is tracked with a flag
      // rather than by testing the value: the type always holds a default, and a
      // visitor who picked Passport before this response arrived must not find
      // Cédula selected under their own passport number.
      if (!taxIdTouched.current && session.tax_id_type && session.tax_id_number && isTaxIdType(session.tax_id_type)) {
        setTaxIdType(session.tax_id_type);
        setTaxIdNumber(session.tax_id_number);
      }
      // The phone, on exactly the same terms and for the same reason: a
      // returning Customer gives it once and never again (#108). The stored
      // value is canonical E.164 and the form is two controls, so splitPhone
      // resolves it back by longest-prefix match against the country table — a
      // +1 number lands on whichever +1 row the table lists first, which is
      // cosmetic and does not change the number submitted (#103).
      //
      // Touched is tracked with a flag rather than by testing the value, as the
      // Tax ID's is: the selector always holds a dialling code, so there is no
      // "empty" to test, and a buyer who deliberately typed a one-off number
      // must never find their stored one back in its place.
      //
      // A number whose dialling code is in no row at all — only possible if the
      // table shrinks under a number already stored — keeps Ecuador on the
      // selector and shows the whole value in the field, mirroring what "My
      // info" does. Showing a buyer their own number intact and letting the
      // mirror check complain beats mangling it to fit a control.
      if (!phoneTouched.current && session.phone) {
        const { diallingCode, nationalNumber } = splitPhone(session.phone);
        if (diallingCode !== "") setPhoneDiallingCode(diallingCode);
        setPhoneNationalNumber(nationalNumber);
      }
    } catch {
      // Prefill is a convenience; its failure must never block a guest.
    }
  }

  function openCheckout() {
    setError(null);
    setFieldErrors({});
    setCheckoutOpen(true);
    void prefillFromSession();
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
          customer_email: email,
          customer_first_name: firstName,
          customer_last_name: lastName,
          customer_tax_id_type: taxIdType,
          customer_tax_id_number: normalizeTaxIdNumber(taxIdType, taxIdNumber),
          // Present only when the buyer gave a number, and canonical when it is:
          // the key is dropped rather than sent blank, all the way down to the
          // Prepare payload, so a skipped field reaches PayPhone as an absence
          // rather than as a value nobody entered.
          ...(canonicalPhone ? { customer_phone: canonicalPhone } : {}),
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
    router.refresh();
  }

  const capacityExceeded = error?.code === "CAPACITY_EXCEEDED";
  // A Purchase Limit refusal is about the Customer, not the Event (ADR 0025), so
  // it gets its own title — "you already have yours" against "not enough tickets
  // left". The two share this 409's handling because the remedy is the same one:
  // the cart cannot be paid for as it stands, and no field on the form is what is
  // wrong with it. Filling the form in again would be refused identically.
  //
  const purchaseLimitExceeded = error?.code === "PURCHASE_LIMIT_EXCEEDED";
  const cartRefused = capacityExceeded || purchaseLimitExceeded;

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
          const sellable = !ticketType.sold_out && !limitReached;
          return (
            <Card key={ticketType.id} className={sellable ? undefined : "opacity-70"}>
              <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="font-semibold">{ticketType.name}</h3>
                    {ticketType.sold_out ? (
                      <Badge variant="secondary">{eventCopy("soldOut")}</Badge>
                    ) : null}
                    {/* A different variant as well as different words: the two
                        unavailable states have to be told apart at a glance, not
                        only by reading. */}
                    {limitReached ? (
                      <Badge variant="outline">{eventCopy("limitReached")}</Badge>
                    ) : null}
                    <PromotionBadge ticketType={ticketType} />
                  </div>
                  {ticketType.description ? (
                    <p className="text-sm text-muted-foreground">{ticketType.description}</p>
                  ) : null}
                  <PromotionDeadline ticketType={ticketType} timezone={timezone} />
                  {/* The remaining count stays on a Ticket Type whose allowance
                      is spent, and it is the evidence for the sentence beside
                      it: "seven remaining" under "your limit reached" is the
                      Event visibly not being full. */}
                  {!ticketType.sold_out ? (
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

      {/* Sticky total bar: the running total and primary CTA stay
          thumb-reachable while the ticket list scrolls (docs/design/storefront.md). */}
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
          <Button type="button" size="lg" className="h-11" disabled={count === 0} onClick={openCheckout}>
            {t("getTickets")}
          </Button>
        </div>
      </div>

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
          ) : (
            <form className="space-y-4" onSubmit={handleSubmit} noValidate>
              <FormField
                id="checkout-email"
                label={t("emailLabel")}
                error={fieldErrors.customer_email}
              >
                <Input
                  name="email"
                  type="email"
                  autoComplete="email"
                  required
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                />
              </FormField>
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
                      taxIdTouched.current = true;
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
                    onChange={(event) => {
                      taxIdTouched.current = true;
                      setTaxIdNumber(event.target.value);
                    }}
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
                    onChange={(event) => {
                      phoneTouched.current = true;
                      setPhoneDiallingCode(event.target.value);
                    }}
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
                    onChange={(event) => {
                      // Touched, and the prefill stops for good: a buyer
                      // clearing this field means it to stay clear, and one
                      // typing a one-off number means that number (#108).
                      phoneTouched.current = true;
                      setPhoneNationalNumber(event.target.value);
                    }}
                  />
                </FormField>
              </div>
              <Button type="submit" className="h-11 w-full" disabled={submitting} aria-busy={submitting}>
                {submitting ? t("submitting") : t("submit")}
              </Button>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
