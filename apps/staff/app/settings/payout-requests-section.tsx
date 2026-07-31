"use client";

import { useCallback, useEffect, useState } from "react";

import { Button, FormField, Input, toast } from "@ticket-pos/ui";

import { formatPriceCents, parsePriceToCents } from "@/lib/events-api";
import { maskAccountNumber, normalizeAccountNumber } from "@/lib/payout-profile";
import { isOutstanding, payoutRequestAmountProblem, payoutRequestStatusLabel } from "@/lib/payout-requests";

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
};

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
  const [profile, setProfile] = useState<PayoutProfile | null>(null);
  const [form, setForm] = useState<PayoutProfileFormValues>(emptyPayoutProfileForm);
  const [requests, setRequests] = useState<PayoutRequest[]>([]);
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [amountProblem, setAmountProblem] = useState<string | null>(null);
  const [editingBankDetails, setEditingBankDetails] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const formatCents = useCallback(
    (cents: number) => formatPriceCents(cents, currency),
    [currency],
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
      setLoadError(error instanceof Error ? error.message : "Failed to load payout requests");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const outstanding = requests.find((request) => isOutstanding(request.status)) ?? null;

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

  function applyError(error: unknown, fallback: string) {
    if (error instanceof FieldValidationError) {
      setFieldErrors(error.fields);
      // The bank fields must be visible for their refusals to mean anything.
      setEditingBankDetails(true);
      toast.error("Check the highlighted fields");
      return;
    }
    toast.error(error instanceof Error ? error.message : fallback);
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
      toast.success("Bank details saved");
    } catch (error) {
      applyError(error, "Failed to save the bank details");
    } finally {
      setBusy(false);
    }
  }

  async function submitRequest() {
    const amountCents = parsePriceToCents(amount);
    const problem = payoutRequestAmountProblem(amountCents, payableBalanceCents, formatCents);
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
        toast.success("You already have a request waiting. Cancel it first to ask for a different amount.");
      } else {
        toast.success("Payout request sent");
      }
      await load();
    } catch (error) {
      applyError(error, "Failed to send the payout request");
    } finally {
      setBusy(false);
    }
  }

  async function cancelRequest(requestID: string) {
    setBusy(true);
    try {
      await callRequests<PayoutRequest>(`${PAYOUT_REQUESTS_PATH}/${requestID}/cancel`, { method: "POST" });
      toast.success("Payout request cancelled");
      await load();
    } catch (error) {
      applyError(error, "Failed to cancel the payout request");
    } finally {
      setBusy(false);
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading payout requests...</p>;
  }
  if (loadError) {
    return <p className="text-sm text-destructive">{loadError}</p>;
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <p className="text-sm font-medium">Bank details</p>
          {profile && !editingBankDetails ? (
            <Button type="button" variant="outline" onClick={() => setEditingBankDetails(true)}>
              Edit
            </Button>
          ) : null}
        </div>

        {profile && !editingBankDetails ? (
          <p className="text-sm text-muted-foreground">
            Paying to {profile.bank_name} {maskAccountNumber(profile.account_number)}, in the name of{" "}
            {profile.account_holder_name}. Last updated {new Date(profile.updated_at).toLocaleDateString()}.
          </p>
        ) : (
          <>
            {!profile ? (
              <p className="text-sm text-muted-foreground">
                Tell us where to pay you. Without this a payout has to start with a message asking for your
                bank details.
              </p>
            ) : null}
            <PayoutProfileFields
              values={form}
              errors={fieldErrors}
              onChange={updateField}
              disabled={busy}
            />
            <Button type="button" variant="outline" disabled={busy} onClick={() => void saveBankDetails()}>
              Save bank details
            </Button>
          </>
        )}
      </div>

      <div className="space-y-3">
        <p className="text-sm font-medium">Request a payout</p>
        {outstanding ? (
          // A pending request cannot be edited, only cancelled and re-asked.
          // That is what keeps "outstanding" singular, and it means nobody at
          // the platform is ever looking at a figure that changed under them.
          <div className="space-y-3 rounded-md border p-4">
            <p className="font-medium tabular-nums">
              {formatPriceCents(outstanding.amount_cents, currency)} requested
            </p>
            {outstanding.note ? (
              <p className="text-sm text-muted-foreground">{outstanding.note}</p>
            ) : null}
            <p className="text-sm text-muted-foreground">
              Asked for on {new Date(outstanding.requested_at).toLocaleDateString()} by{" "}
              {outstanding.requested_by}, paying to {outstanding.payout_profile.bank_name}{" "}
              {maskAccountNumber(outstanding.payout_profile.account_number)}. We will be in touch once it has
              been paid.
            </p>
            <p className="text-sm text-muted-foreground">
              To ask for a different amount, cancel this request and make a new one.
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => void cancelRequest(outstanding.id)}
            >
              Cancel request
            </Button>
          </div>
        ) : (
          <div className="space-y-4">
            <FormField
              id="payout-request-amount"
              label="Amount"
              description={`You can request up to ${formatCents(Math.max(payableBalanceCents, 0))} right now.`}
              error={amountProblem ?? undefined}
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
            <FormField
              id="payout-request-note"
              label="Note (optional)"
              description="Anything the form does not capture — a deadline, an invoice number."
            >
              <Input value={note} disabled={busy} onChange={(event) => setNote(event.target.value)} />
            </FormField>
            <Button type="button" disabled={busy} onClick={() => void submitRequest()}>
              {busy ? "Sending..." : "Request payout"}
            </Button>
          </div>
        )}
      </div>

      <div className="space-y-3">
        <p className="text-sm font-medium">Request history</p>
        {requests.length === 0 ? (
          <p className="text-sm text-muted-foreground">No payout requests yet.</p>
        ) : (
          <div className="space-y-3">
            {requests.map((request) => (
              <div
                key={request.id}
                className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-start sm:justify-between"
              >
                <div>
                  <p className="font-medium tabular-nums">
                    {formatPriceCents(request.amount_cents, currency)}
                  </p>
                  {request.note ? <p className="text-sm text-muted-foreground">{request.note}</p> : null}
                  {/* A decline always carries its reason: a queue that refuses
                      silently generates the support thread it was built to
                      prevent (ADR 0026). */}
                  {request.resolution_reason ? (
                    <p className="text-sm text-destructive">Declined: {request.resolution_reason}</p>
                  ) : null}
                  <p className="text-sm text-muted-foreground">
                    Paying to {request.payout_profile.bank_name}{" "}
                    {maskAccountNumber(request.payout_profile.account_number)}
                  </p>
                </div>
                <div className="sm:text-right">
                  <p className="text-sm font-medium">{payoutRequestStatusLabel(request.status)}</p>
                  <p className="text-sm text-muted-foreground">
                    {new Date(request.requested_at).toLocaleDateString()}
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
