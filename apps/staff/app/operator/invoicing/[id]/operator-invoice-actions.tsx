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
  toast,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import {
  type InvoiceStatus,
  type OperatorInvoiceDetail,
  checkOperatorInvoice,
  resendOperatorInvoice,
} from "@/lib/operator-api";

/**
 * Check status and Resend (#455): the two things a Platform Operator can do
 * to a Tax Invoice the SRI has not authorized.
 *
 * SHOWN ONLY WHEN ALLOWED. An authorized invoice is a legal artifact and the
 * API refuses both actions on it, so this card is not rendered for one at all
 * rather than rendered disabled: there is nothing to do, and a greyed button
 * would invite the question of why.
 *
 * THE OUTCOME IS THE INVOICE. Both calls answer with the invoice as it then
 * stands and hand it to the page, which re-renders status, messages,
 * authorization and the ledger from it; the toast only says what changed.
 * When the SRI holds the document and is still deciding — after a plain
 * RECIBIDA, or after a resend the SRI met with "clave already registered"
 * (43) or "in processing" (70) — the API says so with `check_status_hint`,
 * and the card shows "it is there, check status" instead of an error.
 */

const OUTCOME_KEYS = {
  authorized: "invoicingCheckedAuthorized",
  not_authorized: "invoicingCheckedNotAuthorized",
  rejected: "invoicingCheckedRejected",
  pending: "invoicingCheckedPending",
} as const satisfies Partial<Record<InvoiceStatus, string>>;

export function OperatorInvoiceActions({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const [busy, setBusy] = useState<"check" | "resend" | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (invoice.status === "authorized") {
    return null;
  }

  async function run(action: "check" | "resend") {
    setBusy(action);
    setError(null);
    try {
      const updated =
        action === "check" ? await checkOperatorInvoice(invoice.id) : await resendOperatorInvoice(invoice.id);
      onUpdated(updated);
      if (updated.status === "authorized") {
        toast.success(t(OUTCOME_KEYS.authorized));
      } else {
        toast.info(t(OUTCOME_KEYS[updated.status as keyof typeof OUTCOME_KEYS] ?? "invoicingCheckedPending"));
      }
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ??
          t(action === "check" ? "invoicingCheckFailed" : "invoicingResendFailed"),
      );
    } finally {
      setBusy(null);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("invoicingActionsTitle")}</CardTitle>
        <CardDescription>{t("invoicingActionsDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {invoice.check_status_hint ? (
          <Alert variant="warning">
            <AlertTitle>{t("invoicingCheckStatusHintTitle")}</AlertTitle>
            <AlertDescription>{t("invoicingCheckStatusHint")}</AlertDescription>
          </Alert>
        ) : null}
        {error ? (
          <Alert variant="destructive">
            <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <Button type="button" disabled={busy !== null} onClick={() => void run("check")}>
            {busy === "check" ? t("invoicingCheckingStatus") : t("invoicingCheckStatus")}
          </Button>
          <Button type="button" variant="outline" disabled={busy !== null} onClick={() => void run("resend")}>
            {busy === "resend" ? t("invoicingResending") : t("invoicingResend")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
