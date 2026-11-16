"use client";

import { useState } from "react";

import { Alert, AlertDescription, AlertTitle, Button, toast } from "@ticket-pos/ui";
import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { UNRECORDED_ANNULMENT_KEYS, unrecordedAnnulmentOffer } from "@/lib/closing-sweep";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import { type OperatorInvoiceDetail, acknowledgeUnrecordedAnnulment } from "@/lib/operator-api";

/**
 * The Unrecorded Annulment's warning on a factura's page (#750, spec #742,
 * ADR 0078): the Closing Sweep found the SRI reporting this authorized
 * factura annulled, and the platform has no record of it. The platform never
 * records it itself. Where Mark annulled is admitted, the warning points at
 * the record form below; where a live Credit Note refuses it, it offers the
 * acknowledgement; once acknowledged, it says who and when, and stays.
 */
export function UnrecordedAnnulmentWarning({
  invoice,
  onUpdated,
}: {
  invoice: OperatorInvoiceDetail;
  onUpdated: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const locale = toAppLocale(useLocale());
  const offer = unrecordedAnnulmentOffer(invoice);
  if (offer === null) {
    return null;
  }
  const when = (at: string) => formatDateTime(at, PLATFORM_TIME_ZONE, locale) ?? at;
  return (
    <Alert variant={offer.kind === "acknowledged" ? "default" : "destructive"}>
      <AlertTitle>{t("invoicingUnrecordedTitle")}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t("invoicingUnrecordedFound", { when: when(offer.foundAt) })}</p>
        {offer.kind === "acknowledged" ? (
          <p>{t(UNRECORDED_ANNULMENT_KEYS.acknowledged, { by: offer.by, when: when(offer.at) })}</p>
        ) : (
          <p>{t(UNRECORDED_ANNULMENT_KEYS[offer.kind])}</p>
        )}
        {offer.kind === "acknowledge" ? (
          <OperatorInvoiceAcknowledge invoiceId={invoice.id} onAcknowledged={onUpdated} />
        ) : null}
      </AlertDescription>
    </Alert>
  );
}

/**
 * The acknowledgement's press (#750): no confirm to type, since it changes
 * nothing about the factura; who and when are the session's and the clock's.
 * Drawn on the warning and in the needs-attention entry wherever the API
 * admits it; a press that raced a Credit Note's death is refused with the
 * code the API answers.
 */
export function OperatorInvoiceAcknowledge({
  invoiceId,
  onAcknowledged,
}: {
  invoiceId: string;
  onAcknowledged: (invoice: OperatorInvoiceDetail) => void;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function acknowledge() {
    setBusy(true);
    setError(null);
    try {
      const invoice = await acknowledgeUnrecordedAnnulment(invoiceId);
      toast.success(t("invoicingUnrecordedAcknowledgeDone"));
      onAcknowledged(invoice);
    } catch (actionError) {
      setError(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ??
          t("invoicingUnrecordedAcknowledgeFailed"),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-2">
      {error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingActionFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void acknowledge()}>
        {busy ? t("invoicingUnrecordedAcknowledgeSubmitting") : t("invoicingUnrecordedAcknowledgeSubmit")}
      </Button>
    </div>
  );
}
