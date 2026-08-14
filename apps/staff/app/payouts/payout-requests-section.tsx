"use client";

import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import { Button, FormField, Input, toast } from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage, fieldErrorMessages } from "@/lib/api-errors";
import { ApiError, parsePriceToCents } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDate, formatMoney } from "@/lib/format";
import { maskAccountNumber, normalizeAccountNumber } from "@/lib/payout-profile";
import {
  type PayoutRequestAmountProblem,
  isCancellable,
  isOutstanding,
  payoutRequestAmountProblem,
  resolutionNotice,
} from "@/lib/payout-requests";

import { usePayoutRequestStatusName } from "../payout-request-status";

import {
  type APIEnvelope,
  FieldValidationError,
  type PayoutProfile,
  type PayoutProfileFormValues,
  PayoutProfileFields,
  emptyPayoutProfileForm,
  profileToFormValues,
  requestProfile,
  unwrapEnvelope,
} from "./payout-profile-form";

// The Organization's side of a settlement: where it is paid, the ask to be paid,
// and what became of every earlier ask (#175, ADR 0026).
//
// The three are one section rather than three cards because they are one story.
// The bank fields are the Payout Profile editor AND the request form's bank
// fields — the same six inputs, saved to the profile whenever a request is
// submitted — because an organizer correcting an account number here means their
// account number changed, and fixing the same typo in two places is worse than
// the alternative.
//
// The cap is the server's. This form explains it before the button is pressed,
// which is a kindness and not a gate: the Payable Balance the page is holding is
// a figure from a moment ago, and the request is refused or accepted against the
// one the server reads when it is asked. Nothing here re-checks anything after
// the ask is recorded, because nothing anywhere does — the balance moves
// afterwards and the operator standing at the bank decides what to do about it.

const PAYOUT_REQUESTS_PATH = "/api/settings/organization/payout-requests";

/**
 * Where a failed request sends the organizer. The Payout Profile lives at the
 * top of this same section, so the "next step" after a bounced transfer is an
 * anchor rather than a route — and the button that uses it opens the editor as
 * well as scrolling to it, because arriving at a read-only summary of the
 * account number that was just rejected is not the point.
 */
const BANK_DETAILS_ANCHOR = "payout-bank-details";

type PayoutRequestProfileSnapshot = {
  bank_name: string;
  account_type: string;
  account_number: string;
  account_holder_name: string;
  tax_id_type: string;
  tax_id_number: string;
};

type PayoutRequest = {
  id: string;
  amount_cents: number;
  note: string | null;
  status: string;
  requested_by: string;
  requested_at: string;
  /** What could have been asked for at the moment of asking. A snapshot, never refreshed. */
  payable_balance_cents: number;
  /** Where this ask said to pay. A later profile edit does not touch it. */
  payout_profile: PayoutRequestProfileSnapshot;
  resolution_reason: string | null;
  resolved_by: string | null;
  resolved_at: string | null;
  payout_id: string | null;
  /** When an operator sent the transfer. Null until one is submitted (#187). */
  transfer_submitted_at: string | null;
};

/** The `payouts` catalog key each refusable amount is refused with. */
const AMOUNT_PROBLEM_KEYS = {
  not_positive: "amountProblemNotPositive",
  nothing_cleared: "amountProblemNothingCleared",
  above_payable: "amountProblemAbovePayable",
} as const satisfies Record<PayoutRequestAmountProblem, string>;

async function callRequests<T>(path: string, init?: RequestInit): Promise<T | null> {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const envelope = (await response.json()) as APIEnvelope<T>;
  return unwrapEnvelope(response, envelope);
}

/**
 * `payableBalanceCents` is the figure the payouts summary above is already
 * showing, so this section never fetches a balance of its own — two readings of
 * the same money on one page would eventually disagree.
 *
 * There is no callback back up to the page after a request is submitted or
 * cancelled, and that absence is the feature: a request moves nothing and counts
 * for nothing, so neither balance above can have changed and re-reading them
 * would only suggest otherwise (ADR 0026).
 */
export function PayoutRequestsSection({
  currency,
  payableBalanceCents,
}: {
  currency: string;
  payableBalanceCents: number;
}) {
  const t = useTranslations("payouts");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  // ONE KEY PER STATE, READ BY EVERY SCREEN THAT DRAWS ONE — the outstanding
  // card and the request history here, the operator's queue and request detail
  // there, and the notice email that links to them. `app/payout-request-status`
  // is the one place a token becomes a word, the way `app/role-name` is for a
  // role, so a state cannot acquire a second Spanish word by being drawn twice.
  const statusLabel = usePayoutRequestStatusName();
  const [profile, setProfile] = useState<PayoutProfile | null>(null);
  const [form, setForm] = useState<PayoutProfileFormValues>(emptyPayoutProfileForm);
  const [requests, setRequests] = useState<PayoutRequest[]>([]);
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [amountProblem, setAmountProblem] = useState<PayoutRequestAmountProblem | null>(null);
  const [editingBankDetails, setEditingBankDetails] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  /**
   * Money in the Organization's currency, dates on the platform's clock, both
   * with the reader's marks. The currency is the Organization's whichever
   * language this page is in, and the zone is Ecuador's because a Payout Request
   * belongs to no Event and therefore to no Event's timezone (ADR 0041).
   */
  const formatCents = useCallback(
    (cents: number) => formatMoney(cents, currency, locale),
    [currency, locale],
  );
  const formatMoment = useCallback(
    (value: string | null | undefined) => formatDate(value, PLATFORM_TIME_ZONE, locale),
    [locale],
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [storedProfile, history] = await Promise.all([
        requestProfile(),
        callRequests<PayoutRequest[]>(PAYOUT_REQUESTS_PATH),
      ]);
      setProfile(storedProfile);
      setForm(storedProfile ? profileToFormValues(storedProfile) : emptyPayoutProfileForm);
      // An Organization with nowhere to be paid has the fields open from the
      // start: the first thing it must do is fill them in, and hiding them
      // behind a toggle would hide the only action available.
      setEditingBankDetails(storedProfile === null);
      setRequests(history ?? []);
      setLoadError(null);
    } catch (error) {
      setLoadError(
        (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ??
          t("requestsLoadFailed"),
      );
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const outstanding = requests.find((request) => isOutstanding(request.status)) ?? null;
  // THE DATE IS THE POINT of the sentence this feeds, which is why it is read
  // before the sentence is chosen: without it an organizer cannot tell whether
  // the 48 hours they were promised have run out. formatDate answers null for an
  // instant it cannot read, so a missing or malformed one renders nothing rather
  // than a reassurance nobody can check.
  const transferSentOn = outstanding ? formatMoment(outstanding.transfer_submitted_at) : null;

  function updateField(field: keyof PayoutProfileFormValues, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
    // A field being corrected stops carrying its old refusal, so the message
    // under it always describes what is in it now.
    setFieldErrors((current) => {
      if (!current[field]) {
        return current;
      }
      const rest = { ...current };
      delete rest[field];
      return rest;
    });
  }

  /**
   * The account number is normalised before it is sent so what is stored is what
   * the organizer sees, rather than their dashes vanishing on the next load.
   */
  function profileBody(): PayoutProfileFormValues {
    return { ...form, account_number: normalizeAccountNumber(form.account_number) };
  }

  /**
   * The refusal, in the reader's language: each field's own verdict under the
   * input it is about, or the envelope's sentence in a toast.
   *
   * Both halves are resolved from the API's CODES rather than its sentences
   * (ADR 0023) — `fieldErrorMessages` for the per-field ones, `apiErrorMessage`
   * for the envelope — with the API's English as the floor under any code this
   * catalog has not heard of, and the caller's own sentence under a request that
   * never reached the API at all.
   */
  function applyError(error: unknown, fallback: string) {
    if (error instanceof FieldValidationError) {
      setFieldErrors(fieldErrorMessages(errorCopy, error.details));
      // The bank fields must be visible for their refusals to mean anything.
      setEditingBankDetails(true);
      toast.error(t("profileCheckFields"));
      return;
    }
    toast.error((error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? fallback);
  }

  async function saveBankDetails() {
    setBusy(true);
    setFieldErrors({});
    try {
      const body = profileBody();
      const saved = await requestProfile({ method: "PUT", body: JSON.stringify(body) });
      setProfile(saved);
      setForm((current) => ({ ...current, account_number: body.account_number }));
      setEditingBankDetails(false);
      toast.success(t("profileSaved"));
    } catch (error) {
      applyError(error, t("profileSaveFailed"));
    } finally {
      setBusy(false);
    }
  }

  async function submitRequest() {
    const amountCents = parsePriceToCents(amount);
    const problem = payoutRequestAmountProblem(amountCents, payableBalanceCents);
    setAmountProblem(problem);
    if (problem !== null || amountCents === null) {
      return;
    }

    setBusy(true);
    setFieldErrors({});
    try {
      // The bank details ride along with every request, which is what makes this
      // form the profile editor: whatever is in the fields becomes the profile
      // and is snapshotted onto the request in one call (ADR 0026).
      const body = {
        amount_cents: amountCents,
        ...(note.trim() ? { note: note.trim() } : {}),
        payout_profile: profileBody(),
      };
      const submitted = await callRequests<PayoutRequest>(PAYOUT_REQUESTS_PATH, {
        method: "POST",
        body: JSON.stringify(body),
      });
      setAmount("");
      setNote("");
      setEditingBankDetails(false);
      if (submitted && outstanding && submitted.id === outstanding.id) {
        // The API handed back the ask that was already outstanding rather than
        // recording a new one — the courtesy that tells an organizer where their
        // earlier request went instead of a bare conflict (ADR 0024, ADR 0026).
        toast.success(t("alreadyOutstanding"));
      } else {
        toast.success(t("submitted"));
      }
      await load();
    } catch (error) {
      applyError(error, t("submitFailed"));
    } finally {
      setBusy(false);
    }
  }

  async function cancelRequest(requestID: string) {
    setBusy(true);
    try {
      await callRequests<PayoutRequest>(`${PAYOUT_REQUESTS_PATH}/${requestID}/cancel`, { method: "POST" });
      toast.success(t("cancelled"));
      await load();
    } catch (error) {
      // PAYOUT_REQUEST_NOT_PENDING is the refusal an organizer who pressed
      // cancel a moment too late reads, and it is catalogued so they read it in
      // their own language. The one thing the API's English says that the
      // catalogued sentence cannot is WHICH of the two things happened —
      // resolved, or a transfer already on its way — because one code carries
      // both (the status is in `details`, deliberately). So the sentence says
      // what is true of both and sends them to the page, which reloads
      // immediately below and shows the state the request actually reached.
      applyError(error, t("cancelFailed"));
      // And the page catches up with what it just learned: the request moved on
      // while this tab was looking at it, so the button that was pressed should
      // not still be there afterwards.
      await load();
    } finally {
      setBusy(false);
    }
  }

  /**
   * Opens the Payout Profile editor, which is the bank details block at the top
   * of this same section — the next step after a transfer bounced, and the
   * reason the failure's button is a button rather than a link. The profile is
   * not a page to navigate to; it is three inches up, already loaded.
   */
  function editBankDetails() {
    setEditingBankDetails(true);
    document.getElementById(BANK_DETAILS_ANCHOR)?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("requestsLoading")}</p>;
  }
  if (loadError) {
    return <p className="text-sm text-destructive">{loadError}</p>;
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3" id={BANK_DETAILS_ANCHOR}>
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <p className="text-sm font-medium">{t("profileTitle")}</p>
          {profile && !editingBankDetails ? (
            <Button type="button" variant="outline" onClick={() => setEditingBankDetails(true)}>
              {t("profileEdit")}
            </Button>
          ) : null}
        </div>

        {profile && !editingBankDetails ? (
          // Interpolated rather than glued together out of JSX fragments: where
          // the bank, the masked account and the name fall in the sentence is
          // the translator's to decide, not the markup's.
          <p className="text-sm text-muted-foreground">
            {t("profilePayingTo", {
              bank: profile.bank_name,
              account: maskAccountNumber(profile.account_number),
              holder: profile.account_holder_name,
              updated: formatMoment(profile.updated_at) ?? "",
            })}
          </p>
        ) : (
          <>
            {!profile ? (
              <p className="text-sm text-muted-foreground">{t("profileMissing")}</p>
            ) : null}
            <PayoutProfileFields
              values={form}
              errors={fieldErrors}
              onChange={updateField}
              disabled={busy}
            />
            <Button type="button" variant="outline" disabled={busy} onClick={() => void saveBankDetails()}>
              {t("profileSave")}
            </Button>
          </>
        )}
      </div>

      <div className="space-y-3">
        <p className="text-sm font-medium">{t("requestTitle")}</p>
        {outstanding ? (
          // A pending request cannot be edited, only cancelled and re-asked.
          // That is what keeps "outstanding" singular, and it means nobody at
          // the platform is ever looking at a figure that changed under them.
          //
          // Outstanding is two states now, and the whole reason this feature
          // exists is that they are different news: `pending` is "nobody has
          // looked at this yet" and `processing` is "your money is on its way"
          // (#181). Everything below that differs between them differs because
          // of that sentence.
          <div className="space-y-3 rounded-md border p-4">
            <p className="font-medium tabular-nums">
              {outstanding.status === "processing"
                ? t("outstandingAmountProcessing", {
                    amount: formatCents(outstanding.amount_cents),
                    status: statusLabel(outstanding.status),
                  })
                : t("outstandingAmount", { amount: formatCents(outstanding.amount_cents) })}
            </p>
            {outstanding.note ? (
              <p className="text-sm text-muted-foreground">{outstanding.note}</p>
            ) : null}
            <p className="text-sm text-muted-foreground">
              {t("outstandingAskedBy", {
                date: formatMoment(outstanding.requested_at) ?? "",
                who: outstanding.requested_by,
                bank: outstanding.payout_profile.bank_name,
                account: maskAccountNumber(outstanding.payout_profile.account_number),
              })}
              {outstanding.status === "processing" ? null : ` ${t("outstandingWillBeInTouch")}`}
            </p>
            {isCancellable(outstanding.status) ? (
              <>
                <p className="text-sm text-muted-foreground">{t("outstandingCancelHint")}</p>
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => void cancelRequest(outstanding.id)}
                >
                  {t("cancel")}
                </Button>
              </>
            ) : (
              // THE CANCEL BUTTON IS REPLACED BY THIS SENTENCE, not merely
              // removed. A button that vanishes with nothing in its place reads
              // as a page that lost something; the sentence says what took it
              // away, and the date in it is what lets an organizer tell whether
              // the 48 hours they were promised have already run out.
              // A missing date means no sentence at all: see transferSentOn
              // above for why nothing beats a dateless reassurance.
              transferSentOn !== null ? (
                <p className="text-sm text-muted-foreground">
                  {t("transferSent", { date: transferSentOn })}
                </p>
              ) : null
            )}
          </div>
        ) : (
          <div className="space-y-4">
            <FormField
              id="payout-request-amount"
              label={t("amountLabel")}
              description={t("amountHint", {
                max: formatCents(Math.max(payableBalanceCents, 0)),
              })}
              error={
                amountProblem
                  ? t(AMOUNT_PROBLEM_KEYS[amountProblem], {
                      max: formatCents(Math.max(payableBalanceCents, 0)),
                    })
                  : undefined
              }
            >
              <Input
                inputMode="decimal"
                value={amount}
                disabled={busy}
                placeholder="0.00"
                onChange={(event) => {
                  setAmount(event.target.value);
                  setAmountProblem(null);
                }}
              />
            </FormField>
            <FormField id="payout-request-note" label={t("noteLabel")} description={t("noteHint")}>
              <Input value={note} disabled={busy} onChange={(event) => setNote(event.target.value)} />
            </FormField>
            <Button type="button" disabled={busy} onClick={() => void submitRequest()}>
              {busy ? t("submitting") : t("submit")}
            </Button>
          </div>
        )}
      </div>

      <div className="space-y-3">
        <p className="text-sm font-medium">{t("requestHistoryTitle")}</p>
        {requests.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("requestHistoryEmpty")}</p>
        ) : (
          <div className="space-y-3">
            {requests.map((request) => {
              const resolution = resolutionNotice(request.status, request.resolution_reason);
              return (
                <div
                  key={request.id}
                  className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-start sm:justify-between"
                >
                  <div>
                    <p className="font-medium tabular-nums">{formatCents(request.amount_cents)}</p>
                    {request.note ? <p className="text-sm text-muted-foreground">{request.note}</p> : null}
                    {/* A decline always carries its reason: a queue that refuses
                        silently generates the support thread it was built to
                        prevent (ADR 0026). A failure carries one too, in the same
                        column and NEVER in the same words — resolutionNotice is
                        where that distinction is kept, and its header says why it
                        matters more than it looks. The two sentences say "Motivo:"
                        in Spanish, as the notice email announcing them does. */}
                    {resolution ? (
                      <p className="text-sm text-destructive">
                        {resolution.kind === "declined"
                          ? t("resolutionDeclined", { reason: resolution.reason })
                          : t("resolutionFailed", { reason: resolution.reason })}
                      </p>
                    ) : null}
                    {/* A failed request is the one state with something for the
                        organizer to DO, and it only ever appears here: a failure
                        is terminal, so it is not the outstanding request above.
                        The next step travels with the news. */}
                    {request.status === "failed" ? (
                      <p className="text-sm text-muted-foreground">
                        {t("transferFailedNextStep")}{" "}
                        <Button
                          type="button"
                          variant="link"
                          size="sm"
                          className="h-auto p-0 align-baseline"
                          onClick={editBankDetails}
                        >
                          {t("checkProfile")}
                        </Button>
                      </p>
                    ) : null}
                    <p className="text-sm text-muted-foreground">
                      {t("requestPayingTo", {
                        bank: request.payout_profile.bank_name,
                        account: maskAccountNumber(request.payout_profile.account_number),
                      })}
                    </p>
                  </div>
                  <div className="sm:text-right">
                    <p className="text-sm font-medium">{statusLabel(request.status)}</p>
                    <p className="text-sm text-muted-foreground">
                      {formatMoment(request.requested_at)}
                    </p>
                    {/* The date the transfer was sent, beside the date it was
                        asked for. On a processing row it is the checkable half of
                        the sentence above; on a failed one it is when the money
                        went out before it came back. */}
                    {request.transfer_submitted_at ? (
                      <p className="text-sm text-muted-foreground">
                        {t("requestSentOn", { date: formatMoment(request.transfer_submitted_at) ?? "" })}
                      </p>
                    ) : null}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
