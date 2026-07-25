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

import type { PublicTicketType } from "@/lib/api";
import { clampQuantity, selectionLines, totalCents, totalQuantity } from "@/lib/checkout";
import { formatPrice } from "@/lib/format";

/**
 * Ticket selection and the one checkout step, inline on the event page
 * (docs/design/storefront.md): quantity steppers per Ticket Type, a sticky
 * running total, and a Dialog collecting email + first/last name — prefilled
 * from the Customer Session when one exists, guest checkout otherwise.
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

type SessionData = { email: string; first_name: string; last_name: string };

type TicketSelectionProps = {
  orgSlug: string;
  eventSlug: string;
  eventName: string;
  ticketTypes: PublicTicketType[];
};

function formatRemaining(remaining: number): string {
  return `${new Intl.NumberFormat("en-US").format(remaining)} remaining`;
}

/** The API's begin-checkout field names, mapped onto the form's inputs. */
type FieldErrors = Partial<Record<"customer_email" | "customer_first_name" | "customer_last_name", string>>;

function fieldErrorsFromDetails(details: unknown): FieldErrors {
  const errors: FieldErrors = {};
  if (typeof details !== "object" || details === null) return errors;
  const fields = (details as { fields?: unknown }).fields;
  if (!Array.isArray(fields)) return errors;
  for (const entry of fields) {
    if (typeof entry !== "object" || entry === null) continue;
    const { field, message } = entry as { field?: unknown; message?: unknown };
    if (typeof field !== "string" || typeof message !== "string") continue;
    if (field === "customer_email" || field === "customer_first_name" || field === "customer_last_name") {
      errors[field] = message;
    }
  }
  return errors;
}

export function TicketSelection({ orgSlug, eventSlug, eventName, ticketTypes }: TicketSelectionProps) {
  const router = useRouter();
  const [quantities, setQuantities] = useState<Record<string, number>>({});
  const [checkoutOpen, setCheckoutOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [error, setError] = useState<{ code: string | null; message: string } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const prefillAttempted = useRef(false);

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
          lines: selectionLines(quantities),
        }),
      });
      const envelope = (await response.json()) as Envelope<{ redirect_url: string }>;
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
      // Full-page navigation to the provider's hosted payment page. submitting
      // stays true so the button cannot fire a second Payment while the
      // browser unloads.
      window.location.assign(envelope.data.redirect_url);
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
