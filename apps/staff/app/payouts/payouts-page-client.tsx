"use client";

import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import { Alert, AlertDescription, AlertTitle, Card, CardContent } from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { formatCalendarDay, formatMoney } from "@/lib/format";
import { payableBalance } from "@/lib/payable-balance";

import { PayoutRequestsSection } from "./payout-requests-section";

// The whole money-out story on one page (#190): what the platform owes, where to
// pay it, the ask, and what came back. It used to be one section on a settings
// page that also held the logo and the danger zone; nothing about it changed in
// the move except the labels, which now say what the glossary says.

type Payout = {
  id: string;
  amount_cents: number;
  /** A calendar day ("YYYY-MM-DD"), not an instant. */
  paid_at: string;
  note: string | null;
};

type PayoutsSummary = {
  /** What the platform owes. Signed: negative after a post-settlement reversal. */
  withdrawable_balance_cents: number;
  /**
   * The part of it that has cleared and may be asked for today (ADR 0026).
   * Signed too, never larger than the figure above, and negative when a
   * settlement got ahead of what had cleared.
   */
  payable_balance_cents: number;
  currency: string;
  payouts: Payout[];
};

type APIEnvelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: Record<string, unknown> } | null;
};

/**
 * The refusal is carried as an ApiError so its CODE survives the throw, which is
 * what lets the alert below say the API's verdict in the reader's language
 * (ADR 0023, ADR 0041). It used to be a bare Error, and the page told a
 * permission refusal apart from every other one by looking for the word
 * "permission" inside the sentence — which worked only for as long as there was
 * exactly one language, and read the message the API happens to write today as
 * though it were a contract.
 */
async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, { headers: { "Content-Type": "application/json" } });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    throw new ApiError(
      envelope.error?.message ?? "",
      envelope.error?.code,
      envelope.error?.details,
    );
  }
  if (envelope.data === null) {
    throw new ApiError("", "INTERNAL_ERROR");
  }
  return envelope.data;
}

export function PayoutsPageClient() {
  const t = useTranslations("payouts");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [payouts, setPayouts] = useState<PayoutsSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setPayouts(await fetchJSON<PayoutsSummary>("/api/settings/organization/payouts"));
      setForbidden(false);
    } catch (loadError) {
      // Getting paid is Org-Admin-only on the server as well as in the nav, so
      // a refusal here is its own answer rather than a failure to report: the
      // page says who may read this, and does not offer to retry.
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("loadFailed"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("loading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }

  if (error || !payouts) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
        <AlertDescription>{error ?? t("loadFailed")}</AlertDescription>
      </Alert>
    );
  }

  const balance = payableBalance(
    payouts.withdrawable_balance_cents,
    payouts.payable_balance_cents,
  );
  const money = (cents: number) => formatMoney(cents, payouts.currency, locale);

  return (
    <Card>
      <CardContent className="space-y-6 pt-6">
        {/*
          Both balances, side by side, with the gap between them in words.
          Showing only the smaller figure would be simpler and would send
          every organizer who sold this morning to support asking why they
          are being offered less than they earned (ADR 0026).

          The labels are the glossary's own — an organizer phoning a Platform
          Operator about their payable balance is then speaking the words the
          operator's screens use, and "available balance" is a term CONTEXT.md
          says to avoid.
        */}
        <div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <p className="text-sm text-muted-foreground">{t("withdrawableBalance")}</p>
              <p className="text-3xl font-semibold tabular-nums">
                {money(payouts.withdrawable_balance_cents)}
              </p>
              <p className="text-xs text-muted-foreground">{t("withdrawableBalanceHint")}</p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">{t("payableBalance")}</p>
              <p className="text-3xl font-semibold tabular-nums">
                {money(payouts.payable_balance_cents)}
              </p>
              <p className="text-xs text-muted-foreground">{t("payableBalanceHint")}</p>
            </div>
          </div>
          {/*
            The gap between the two figures, in words, in the reader's language —
            three sentences, one per state, chosen by lib/payable-balance.ts and
            said by the catalog. The amounts inside them are drawn in the
            ORGANIZATION's currency whichever language that is: a locale decides
            the marks around a number and never which money it counts (ADR 0041).
          */}
          <p className="mt-4 text-sm text-muted-foreground">
            {balance.state === "nothing_cleared"
              ? t("balanceNothingCleared")
              : balance.state === "all_cleared"
                ? t("balanceAllCleared", { payable: money(balance.payableCents) })
                : t("balanceSomeUncleared", {
                    payable: money(balance.payableCents),
                    uncleared: money(balance.unclearedCents),
                  })}
          </p>
        </div>

        {/*
          Where the money goes, the ask to be paid, and what became of every
          earlier ask (#175, ADR 0026). It sits between the balances and the
          payout history because that is the order the questions arrive in:
          how much do I have, can I have it, and what have I been paid
          before. It reads the Payable Balance from the summary above rather
          than fetching its own — two readings of the same money on one page
          would eventually disagree.
        */}
        <PayoutRequestsSection
          currency={payouts.currency}
          payableBalanceCents={payouts.payable_balance_cents}
        />

        <div className="space-y-3">
          <p className="text-sm font-medium">{t("historyTitle")}</p>
          {payouts.payouts.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("historyEmpty")}</p>
          ) : (
            <div className="space-y-3">
              {payouts.payouts.map((payout) => (
                <div
                  key={payout.id}
                  className="flex flex-col gap-1 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div>
                    <p className="font-medium tabular-nums">{money(payout.amount_cents)}</p>
                    {payout.note ? <p className="text-sm text-muted-foreground">{payout.note}</p> : null}
                  </div>
                  {/*
                    A calendar day, not an instant, and drawn as one: "2026-03-01"
                    through the Date constructor is UTC midnight, which is the
                    28th of February everywhere this platform sells.
                  */}
                  <p className="text-sm text-muted-foreground">
                    {formatCalendarDay(payout.paid_at, locale)}
                  </p>
                </div>
              ))}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
