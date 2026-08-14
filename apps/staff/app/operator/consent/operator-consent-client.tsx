"use client";

import { useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
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
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
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
 * It reads in the operator's Staff Locale like every other staff surface
 * (ADR 0041, #292). The Customer's own confirmation email is written in THEIR
 * language by the API, which is where that decision belongs and which nothing
 * here changes — the last bullet of the confirmation says so out loud.
 */

/** The longest artefact reference the API accepts. */
const REQUEST_REFERENCE_MAX_LENGTH = 500;

/**
 * The `operator` catalog key each consent state reads under.
 *
 * NULL IS SPELLED OUT rather than shown as a dash, because "never asked" is the
 * state most easily mistaken for a refusal — and mistaking it would mean an
 * operator recording a withdrawal of something nobody ever granted.
 */
const CONSENT_STATE_KEYS = {
  granted: "consentGranted",
  denied: "consentDenied",
  pending_confirmation: "consentPendingConfirmation",
} as const satisfies Record<OperatorConsentValue, string>;

/** Whether a withdrawal of this consent would actually take something away. */
function wouldTakeSomethingAway(value: OperatorConsentValue | null): boolean {
  return value === "granted" || value === "pending_confirmation";
}

/** A withdrawal the operator has stated and is being asked to confirm. */
type PendingWithdrawal = {
  marketing: boolean;
  networking: boolean;
  reference: string;
};

/** One label/value row. */
function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-sm text-muted-foreground">{label}</p>
      <div className="font-medium">{children}</div>
    </div>
  );
}

export function OperatorConsentClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
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

  /** A stored consent state as a word, with "never asked" for the absent one. */
  const consentLabel = (value: OperatorConsentValue | null) =>
    value && CONSENT_STATE_KEYS[value] ? t(CONSENT_STATE_KEYS[value]) : t("consentNeverAsked");

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
      // An address nobody holds is the ordinary outcome of a typo on a posted
      // form, not a failure worth an alarming red box — so it gets its own card
      // below and never comes through the error copy at all.
      if (lookupError instanceof ApiError && lookupError.code === "CUSTOMER_NOT_FOUND") {
        setNoSuchCustomer(true);
      } else {
        setError(
          (lookupError instanceof ApiError ? apiErrorMessage(errorCopy, lookupError) : null) ??
            t("consentLookupFailed"),
        );
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
    setConsentError(nothingNamed ? t("consentNothingNamed") : null);
    setReferenceError(
      !trimmedReference
        ? t("consentArtefactRequired")
        : trimmedReference.length > REQUEST_REFERENCE_MAX_LENGTH
          ? t("consentArtefactTooLong", { max: REQUEST_REFERENCE_MAX_LENGTH })
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
      toast.success(took ? t("consentWithdrawalRecorded") : t("consentWithdrawalNoChange"));
    } catch (submitError) {
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("consentWithdrawalFailed"),
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader title={t("consentTitle")} description={t("consentDescription")} />

      <Card>
        <CardContent>
          <form
            className="grid gap-4 sm:grid-cols-[2fr_auto] sm:items-end"
            onSubmit={handleLookUpSubmit}
          >
            <FormField id="operator-consent-email" label={t("consentEmailLabel")}>
              <Input
                type="email"
                value={address}
                onChange={(event) => setAddress(event.target.value)}
                placeholder={t("consentEmailPlaceholder")}
                autoComplete="off"
                spellCheck={false}
              />
            </FormField>
            <Button type="submit" disabled={!address.trim() || loading}>
              {loading ? t("consentLookingUp") : t("consentFindCustomer")}
            </Button>
          </form>
        </CardContent>
      </Card>

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("consentLookupFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {noSuchCustomer ? (
        <Card>
          <CardContent>
            <p className="text-sm text-muted-foreground">{t("consentNoCustomer")}</p>
          </CardContent>
        </Card>
      ) : null}

      {found ? (
        <>
          <Card>
            <CardHeader>
              {/* A Customer's name and address are data, never copy. */}
              <CardTitle>
                {`${found.customer.first_name} ${found.customer.last_name}`.trim()}
              </CardTitle>
              <CardDescription>{found.customer.email}</CardDescription>
            </CardHeader>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              <Fact label={t("consentMarketing")}>
                {consentLabel(found.consent.marketing_consent)}
              </Fact>
              <Fact label={t("consentNetworking")}>
                {consentLabel(found.consent.networking_consent)}
              </Fact>
              {/*
                A Policy Acceptance is a platform-wide fact and belongs to no
                Event, so it is drawn on the platform's clock (ADR 0041).
              */}
              <Fact label={t("consentPolicyAccepted")}>
                {formatDateTime(found.consent.policy_accepted_at, PLATFORM_TIME_ZONE, locale) ??
                  t("consentNever")}
              </Fact>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t("consentWithdrawTitle")}</CardTitle>
              <CardDescription>{t("consentWithdrawDescription")}</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="space-y-4" onSubmit={handleWithdrawSubmit}>
                <FormField
                  id="operator-consent-boxes"
                  label={t("consentWhatWithdraws")}
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
                      {t("consentMarketing")}
                      {wouldTakeSomethingAway(found.consent.marketing_consent) ? null : (
                        <span className="text-muted-foreground">
                          {t("consentNothingToTakeAway", {
                            // Lower-cased for the reader's language rather than
                            // the machine's: the state is a word this catalog
                            // already owns, said mid-sentence here.
                            state: consentLabel(
                              found.consent.marketing_consent,
                            ).toLocaleLowerCase(locale),
                          })}
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
                      {t("consentNetworking")}
                      {wouldTakeSomethingAway(found.consent.networking_consent) ? null : (
                        <span className="text-muted-foreground">
                          {t("consentNothingToTakeAway", {
                            state: consentLabel(
                              found.consent.networking_consent,
                            ).toLocaleLowerCase(locale),
                          })}
                        </span>
                      )}
                    </label>
                  </div>
                </FormField>

                <FormField
                  id="operator-consent-reference"
                  label={t("consentArtefactLabel")}
                  error={referenceError ?? undefined}
                >
                  <Input
                    value={reference}
                    onChange={(event) => setReference(event.target.value)}
                    placeholder={t("consentArtefactPlaceholder")}
                    autoComplete="off"
                    disabled={submitting}
                  />
                  <p className="mt-1 text-sm text-muted-foreground">{t("consentArtefactHint")}</p>
                </FormField>

                <Button type="submit" disabled={submitting}>
                  {t("consentRecordWithdrawal")}
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
            <DialogTitle>{t("consentConfirmTitle")}</DialogTitle>
            <DialogDescription>
              {t("consentConfirmBody", { email: found?.customer.email ?? "" })}
            </DialogDescription>
          </DialogHeader>
          <ul className="list-disc space-y-1 pl-5 text-sm">
            {pending?.marketing ? <li>{t("consentConfirmMarketing")}</li> : null}
            {pending?.networking ? <li>{t("consentConfirmNetworking")}</li> : null}
            <li>{t("consentConfirmContinues")}</li>
            <li>{t("consentConfirmEmail")}</li>
          </ul>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setPending(null)}
              disabled={submitting}
            >
              {t("cancel")}
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
              {submitting ? t("recording") : t("consentRecordWithdrawal")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
