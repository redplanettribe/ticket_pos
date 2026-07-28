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
import { useRouter } from "next/navigation";
import { useRef, useState, type FormEvent } from "react";

import type { BeginCheckoutResult, PublicTicketType } from "@/lib/api";
import {
  checkoutDestination,
  clampQuantity,
  selectionLines,
  totalCents,
  totalQuantity,
} from "@/lib/checkout";
import { formatPrice } from "@/lib/format";
import {
  COUNTRIES,
  ECUADOR_DIALLING_CODE,
  composePhone,
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
};

/** The one line a pass-on Customer ever sees about the fee. No amount, no breakdown. */
const SERVICE_FEE_NOTE = "Prices include the service fee.";

function formatRemaining(remaining: number): string {
  return `${new Intl.NumberFormat("en-US").format(remaining)} remaining`;
}

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

function fieldErrorsFromDetails(details: unknown): FieldErrors {
  const errors: FieldErrors = {};
  if (typeof details !== "object" || details === null) return errors;
  const fields = (details as { fields?: unknown }).fields;
  if (!Array.isArray(fields)) return errors;
  for (const entry of fields) {
    if (typeof entry !== "object" || entry === null) continue;
    const { field, message } = entry as { field?: unknown; message?: unknown };
    if (typeof field !== "string" || typeof message !== "string") continue;
    if (isFormField(field)) {
      errors[field] = message;
    }
  }
  return errors;
}

/** Matches the Input component's height and border so the select reads as a peer. */
const SELECT_CLASS =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50";

export function TicketSelection({
  orgSlug,
  eventSlug,
  eventName,
  ticketTypes,
  priceIncludesFee,
}: TicketSelectionProps) {
  const router = useRouter();
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
  const [error, setError] = useState<{ code: string | null; message: string } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const prefillAttempted = useRef(false);
  const taxIdTouched = useRef(false);
  const phoneTouched = useRef(false);

  const count = totalQuantity(quantities);
  const total = totalCents(ticketTypes, quantities);
  const currency = ticketTypes[0]?.currency ?? "USD";

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
    const taxIdProblem = validateTaxId(taxIdType, taxIdNumber);
    if (taxIdProblem) {
      setError(null);
      setFieldErrors({ customer_tax_id_number: taxIdProblem });
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
      setFieldErrors({ customer_phone: phoneProblem });
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
        }),
      });
      const envelope = (await response.json()) as Envelope<BeginCheckoutResult>;
      if (!response.ok || envelope.error || !envelope.data) {
        const apiError = envelope.error;
        setFieldErrors(fieldErrorsFromDetails(apiError?.details));
        setError({
          code: apiError?.code ?? null,
          message: apiError?.message ?? "The checkout could not be started. Please try again.",
        });
        setSubmitting(false);
        return;
      }
      const destination = checkoutDestination(envelope.data);
      if (!destination) {
        // The API accepted the checkout but named nowhere to go, so there is
        // nothing truthful to navigate to. Say so rather than assign a missing
        // redirect_url, which the browser would resolve against this event page.
        setError({
          code: null,
          message: "The checkout could not be started. Please try again.",
        });
        setSubmitting(false);
        return;
      }
      // Full-page navigation: to the provider's hosted payment page, or straight
      // to the confirmation when there was nothing to pay. submitting stays true
      // so the button cannot fire a second Payment while the browser unloads.
      window.location.assign(destination);
    } catch {
      setError({
        code: null,
        message: "The checkout could not be started. Please check your connection and try again.",
      });
      setSubmitting(false);
    }
  }

  /** Sold-out mid-checkout: back to the steppers with fresh remaining counts. */
  function backToTickets() {
    setCheckoutOpen(false);
    setQuantities({});
    router.refresh();
  }

  const capacityExceeded = error?.code === "CAPACITY_EXCEEDED";

  return (
    <>
      <div className="space-y-3">
        {ticketTypes.map((ticketType) => {
          const quantity = quantities[ticketType.id] ?? 0;
          return (
            <Card key={ticketType.id} className={ticketType.sold_out ? "opacity-70" : undefined}>
              <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="font-semibold">{ticketType.name}</h3>
                    {ticketType.sold_out ? <Badge variant="secondary">Sold out</Badge> : null}
                  </div>
                  {ticketType.description ? (
                    <p className="text-sm text-muted-foreground">{ticketType.description}</p>
                  ) : null}
                  {!ticketType.sold_out ? (
                    <p className="text-sm text-muted-foreground">{formatRemaining(ticketType.remaining)}</p>
                  ) : null}
                </div>
                <div className="flex shrink-0 items-center justify-between gap-4 sm:flex-col sm:items-end">
                  <p className="font-semibold">{formatPrice(ticketType.price_cents, ticketType.currency)}</p>
                  {!ticketType.sold_out ? (
                    <div className="flex items-center gap-1">
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-11"
                        aria-label={`Remove one ${ticketType.name} ticket`}
                        disabled={quantity === 0}
                        onClick={() => adjust(ticketType, -1)}
                      >
                        −
                      </Button>
                      <span
                        className="w-10 text-center text-base font-semibold tabular-nums"
                        aria-label={`${ticketType.name} quantity`}
                        aria-live="polite"
                      >
                        {quantity}
                      </span>
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-11"
                        aria-label={`Add one ${ticketType.name} ticket`}
                        disabled={quantity >= ticketType.remaining}
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
            <p className="text-sm text-muted-foreground">
              {count === 0 ? "No tickets selected" : `${count} ${count === 1 ? "ticket" : "tickets"}`}
            </p>
            <p className="text-lg font-semibold" data-testid="selection-total">
              {formatPrice(total, currency)}
            </p>
            {priceIncludesFee ? (
              <p className="text-xs text-muted-foreground">{SERVICE_FEE_NOTE}</p>
            ) : null}
          </div>
          <Button type="button" size="lg" className="h-11" disabled={count === 0} onClick={openCheckout}>
            Get tickets
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
            <DialogTitle>Checkout</DialogTitle>
            <DialogDescription>
              {eventName} — we&apos;ll email your confirmation to this address.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-1 rounded-lg border bg-muted/40 p-3 text-sm">
            {ticketTypes
              .filter((ticketType) => (quantities[ticketType.id] ?? 0) > 0)
              .map((ticketType) => {
                const quantity = quantities[ticketType.id] ?? 0;
                return (
                  <div key={ticketType.id} className="flex items-center justify-between gap-2">
                    <span>
                      {quantity} × {ticketType.name}
                    </span>
                    <span className="tabular-nums">
                      {formatPrice(quantity * ticketType.price_cents, ticketType.currency)}
                    </span>
                  </div>
                );
              })}
            <div className="flex items-center justify-between gap-2 border-t pt-1 font-semibold">
              <span>Total</span>
              <span className="tabular-nums">{formatPrice(total, currency)}</span>
            </div>
            {priceIncludesFee ? (
              <p className="text-xs font-normal text-muted-foreground">{SERVICE_FEE_NOTE}</p>
            ) : null}
          </div>

          {error ? (
            <Alert variant="destructive">
              {capacityExceeded ? <AlertTitle>Not enough tickets left</AlertTitle> : null}
              <AlertDescription>{error.message}</AlertDescription>
            </Alert>
          ) : null}

          {capacityExceeded ? (
            <DialogFooter>
              <Button type="button" variant="secondary" className="h-11 w-full" onClick={backToTickets}>
                Back to ticket selection
              </Button>
            </DialogFooter>
          ) : (
            <form className="space-y-4" onSubmit={handleSubmit} noValidate>
              <FormField id="checkout-email" label="Email" error={fieldErrors.customer_email}>
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
                <FormField id="checkout-first-name" label="First name" error={fieldErrors.customer_first_name}>
                  <Input
                    name="given-name"
                    autoComplete="given-name"
                    required
                    value={firstName}
                    onChange={(event) => setFirstName(event.target.value)}
                  />
                </FormField>
                <FormField id="checkout-last-name" label="Last name" error={fieldErrors.customer_last_name}>
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
                  label="ID type"
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
                    {TAX_ID_TYPES.map((type) => (
                      <option key={type} value={type}>
                        {TAX_ID_TYPE_LABELS[type]}
                      </option>
                    ))}
                  </select>
                </FormField>
                <FormField
                  id="checkout-tax-id-number"
                  label="ID number"
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
                <FormField id="checkout-phone-country" label="Country code">
                  <select
                    name="phone-country"
                    className={SELECT_CLASS}
                    value={phoneDiallingCode}
                    onChange={(event) => {
                      phoneTouched.current = true;
                      setPhoneDiallingCode(event.target.value);
                    }}
                  >
                    {/* Keyed by name, valued by dialling code: the codes are not
                        unique (+1 covers the US, Canada and twenty more), so
                        picking one of a shared code shows the first country
                        listed under it. The accepted cosmetic imperfection from
                        #103 — the number submitted is identical either way. */}
                    {COUNTRIES.map((country) => (
                      <option key={country.name} value={country.diallingCode}>
                        {country.name} ({country.diallingCode})
                      </option>
                    ))}
                  </select>
                </FormField>
                <FormField
                  id="checkout-phone"
                  label="Phone (optional)"
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
                {submitting ? "Starting payment…" : "Continue to payment"}
              </Button>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
