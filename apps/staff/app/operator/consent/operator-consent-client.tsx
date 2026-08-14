"use client";

import { useState } from "react";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Input,
  PageHeader,
  toast,
} from "@ticket-pos/ui";

import {
  type OperatorConsentValue,
  type OperatorCustomerConsent,
  fetchOperatorCustomerConsent,
  recordOperatorConsentWithdrawal,
} from "@/lib/operator-api";

/**
 * The Platform Operator's Consent Withdrawal surface (#271, parent #265).
 *
 * A withdrawal form arrives by post, or an email lands at the data-protection
 * address. Without this page the only way to honour either was to edit the
 * database by hand, which writes no evidence at all — and an evidence log with
 * a hole exactly where the unusual cases went is worse than no log, because it
 * is confidently wrong.
 *
 * THE PAGE CAN ONLY WITHDRAW. There is no control here that grants anything,
 * and — more to the point — the absence of a control is not what guarantees it:
 * the API refuses an affirmative answer in the platform's single consent-write
 * path, so this page is merely honest about what is on offer rather than being
 * the thing that enforces it.
 *
 * THE ADDRESS IS NEVER PUT IN THE URL, unlike the sale lookup's reference. A
 * Sale Confirmation reference is a code somebody was given; an email address is
 * a person, and leaving one in browser history, a referer header or a shoulder-
 * surfable address bar is a disclosure this page has no reason to make. So the
 * lookup happens in place.
 *
 * Still English, but no longer English by decision: the staff app is being
 * translated (ADR 0041, #281) and this surface has simply not been migrated yet
 * — #286 landed the catalogs and the login page, and the Operator Dashboard
 * follows. The Customer's own confirmation email is written in THEIR language by
 * the API, which is where that decision belongs and which nothing here changes.
 */

/** The longest artefact reference the API accepts. */
const REQUEST_REFERENCE_MAX_LENGTH = 500;

/** One label/value row. */
function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-sm text-muted-foreground">{label}</p>
      <div className="font-medium">{children}</div>
    </div>
  );
}

/**
 * How a consent state reads to somebody deciding what a form would change.
 *
 * Null is spelled out rather than shown as a dash, because "never asked" is the
 * state most easily mistaken for a refusal — and mistaking it would mean an
 * operator recording a withdrawal of something nobody ever granted.
 */
function consentLabel(value: OperatorConsentValue | null): string {
  switch (value) {
    case "granted":
      return "Granted";
    case "denied":
      return "Withdrawn or refused";
    case "pending_confirmation":
      return "Pending confirmation — ticked by somebody who never proved the address";
    default:
      return "Never asked";
  }
}

/** Whether a withdrawal of this consent would actually take something away. */
function wouldTakeSomethingAway(value: OperatorConsentValue | null): boolean {
  return value === "granted" || value === "pending_confirmation";
}

function formatInstant(value: string | null): string {
  if (!value) {
    return "Never";
  }
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}

/** A withdrawal the operator has stated and is being asked to confirm. */
type PendingWithdrawal = {
  marketing: boolean;
  networking: boolean;
  reference: string;
};

export function OperatorConsentClient() {
  const [address, setAddress] = useState("");
  const [found, setFound] = useState<OperatorCustomerConsent | null>(null);
  const [loading, setLoading] = useState(false);
  const [noSuchCustomer, setNoSuchCustomer] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The withdrawal's form. Both boxes start unticked and are never pre-filled
  // from the stored state: what the platform holds decides what a withdrawal
  // WOULD change, never what to present as already asked for.
  const [marketing, setMarketing] = useState(false);
  const [networking, setNetworking] = useState(false);
  const [reference, setReference] = useState("");
  const [referenceError, setReferenceError] = useState<string | null>(null);
  const [consentError, setConsentError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [pending, setPending] = useState<PendingWithdrawal | null>(null);

  async function lookUp(email: string) {
    setLoading(true);
    setError(null);
    setNoSuchCustomer(false);
    setFound(null);
    // A new person means a fresh form: carrying the previous artefact reference
    // onto somebody else's record is how the wrong form gets filed against the
    // wrong human being.
    setMarketing(false);
    setNetworking(false);
    setReference("");
    setReferenceError(null);
    setConsentError(null);
    try {
      setFound(await fetchOperatorCustomerConsent(email));
    } catch (lookupError) {
      const code =
        lookupError && typeof lookupError === "object" && "code" in lookupError
          ? (lookupError as { code?: string }).code
          : undefined;
      // An address nobody holds is the ordinary outcome of a typo on a posted
      // form, not a failure worth an alarming red box.
      if (code === "CUSTOMER_NOT_FOUND") {
        setNoSuchCustomer(true);
      } else {
        setError(lookupError instanceof Error ? lookupError.message : "Failed to look the customer up");
      }
    } finally {
      setLoading(false);
    }
  }

  function handleLookUpSubmit(event: React.FormEvent) {
    event.preventDefault();
    const trimmed = address.trim();
    if (!trimmed) {
      return;
    }
    void lookUp(trimmed);
  }

  function handleWithdrawSubmit(event: React.FormEvent) {
    event.preventDefault();
    const trimmedReference = reference.trim();
    const nothingNamed = !marketing && !networking;
    setConsentError(nothingNamed ? "Say which consent the form withdraws." : null);
    setReferenceError(
      !trimmedReference
        ? "Name the form, letter or email this withdrawal answers."
        : trimmedReference.length > REQUEST_REFERENCE_MAX_LENGTH
          ? `Must be at most ${REQUEST_REFERENCE_MAX_LENGTH} characters.`
          : null,
    );
    if (nothingNamed || !trimmedReference || trimmedReference.length > REQUEST_REFERENCE_MAX_LENGTH) {
      return;
    }
    setPending({ marketing, networking, reference: trimmedReference });
  }

  async function submitWithdrawal(withdrawal: PendingWithdrawal, email: string) {
    setSubmitting(true);
    try {
      // Each consent is OMITTED where the form did not ask for it. Sending
      // `false` for a box nobody mentioned would record an answer to a question
      // that was never put, and would turn a marketing withdrawal into a
      // withdrawal of everything.
      const result = await recordOperatorConsentWithdrawal(email, {
        ...(withdrawal.marketing ? { marketing_consent: false as const } : {}),
        ...(withdrawal.networking ? { networking_consent: false as const } : {}),
        request_reference: withdrawal.reference,
      });
      setPending(null);
      setFound(result);
      setMarketing(false);
      setNetworking(false);
      setReference("");
      // What the act TOOK AWAY, in the platform's own words rather than in the
      // operator's: a form asking to withdraw something already withdrawn is
      // recorded faithfully and moves nothing, and telling the operator it
      // worked would be telling them a change happened when none did.
      const took = result.withdrew?.marketing_consent || result.withdrew?.networking_consent;
      if (took) {
        toast.success("Withdrawal recorded. The customer has been emailed a confirmation.");
      } else {
        toast.success("Withdrawal recorded. Nothing changed, so no confirmation was emailed.");
      }
    } catch (submitError) {
      toast.error(
        submitError instanceof Error ? submitError.message : "Failed to record the withdrawal",
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Customer consent"
        description="Find a customer by email address and record a consent withdrawal that arrived by post or by email."
      />

      <Card>
        <CardContent>
          <form
            className="grid gap-4 sm:grid-cols-[2fr_auto] sm:items-end"
            onSubmit={handleLookUpSubmit}
          >
            <FormField id="operator-consent-email" label="Customer email address">
              <Input
                type="email"
                value={address}
                onChange={(event) => setAddress(event.target.value)}
                placeholder="name@example.com"
                autoComplete="off"
                spellCheck={false}
              />
            </FormField>
            <Button type="submit" disabled={!address.trim() || loading}>
              {loading ? "Looking up..." : "Find customer"}
            </Button>
          </form>
        </CardContent>
      </Card>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Could not look the customer up</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {noSuchCustomer ? (
        <Card>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              No customer on this platform has that email address. Check the address on the form —
              it may differ from the one they wrote to you from.
            </p>
          </CardContent>
        </Card>
      ) : null}

      {found ? (
        <>
          <Card>
            <CardHeader>
              <CardTitle>
                {found.customer.first_name} {found.customer.last_name}
              </CardTitle>
              <CardDescription>{found.customer.email}</CardDescription>
            </CardHeader>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              <Fact label="Marketing consent">{consentLabel(found.consent.marketing_consent)}</Fact>
              <Fact label="Networking consent">
                {consentLabel(found.consent.networking_consent)}
              </Fact>
              <Fact label="Privacy policy accepted">
                {formatInstant(found.consent.policy_accepted_at)}
              </Fact>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Record a consent withdrawal</CardTitle>
              <CardDescription>
                Only what the artefact actually asks for. This surface can withdraw a consent and
                can never grant one — the API refuses a grant, not just this form. Privacy policy
                acceptance cannot be withdrawn.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <form className="space-y-4" onSubmit={handleWithdrawSubmit}>
                <FormField
                  id="operator-consent-boxes"
                  label="What does the form withdraw?"
                  error={consentError ?? undefined}
                >
                  <div className="space-y-2">
                    <label className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={marketing}
                        onChange={(event) => setMarketing(event.target.checked)}
                        disabled={submitting}
                      />
                      Marketing consent
                      {wouldTakeSomethingAway(found.consent.marketing_consent) ? null : (
                        <span className="text-muted-foreground">
                          (nothing to take away — already {consentLabel(
                            found.consent.marketing_consent,
                          ).toLowerCase()})
                        </span>
                      )}
                    </label>
                    <label className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={networking}
                        onChange={(event) => setNetworking(event.target.checked)}
                        disabled={submitting}
                      />
                      Networking consent
                      {wouldTakeSomethingAway(found.consent.networking_consent) ? null : (
                        <span className="text-muted-foreground">
                          (nothing to take away — already {consentLabel(
                            found.consent.networking_consent,
                          ).toLowerCase()})
                        </span>
                      )}
                    </label>
                  </div>
                </FormField>

                <FormField
                  id="operator-consent-reference"
                  label="Which artefact is this?"
                  error={referenceError ?? undefined}
                >
                  <Input
                    value={reference}
                    onChange={(event) => setReference(event.target.value)}
                    placeholder="Formulario de Revocatoria, signed 2026-08-01, received by post 2026-08-05"
                    autoComplete="off"
                    disabled={submitting}
                  />
                  <p className="mt-1 text-sm text-muted-foreground">
                    Required. Name the form, letter or email you are holding, so the record points
                    at the paper. The paper itself is not stored here.
                  </p>
                </FormField>

                <Button type="submit" disabled={submitting}>
                  Record withdrawal
                </Button>
              </form>
            </CardContent>
          </Card>
        </>
      ) : null}

      <Dialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPending(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Record this withdrawal?</DialogTitle>
            <DialogDescription>
              You are recording it on {found?.customer.email}&apos;s behalf. Your email address is
              stored on the record, alongside the artefact you named.
            </DialogDescription>
          </DialogHeader>
          <ul className="list-disc space-y-1 pl-5 text-sm">
            {pending?.marketing ? (
              <li>
                Marketing consent is withdrawn. Campaign email stops, including the weekly follow
                digest.
              </li>
            ) : null}
            {pending?.networking ? (
              <li>Networking consent is withdrawn. Their profile stops being shown to attendees.</li>
            ) : null}
            <li>
              Their account, tickets, receipts, passcodes and reversal notices all continue. This is
              not a deletion, and they can turn either consent back on themselves.
            </li>
            <li>
              They are emailed a confirmation, in their own language — but only if this actually
              takes something away.
            </li>
          </ul>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setPending(null)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => {
                if (pending !== null && found !== null) {
                  void submitWithdrawal(pending, found.customer.email);
                }
              }}
              disabled={submitting}
            >
              {submitting ? "Recording..." : "Record withdrawal"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
