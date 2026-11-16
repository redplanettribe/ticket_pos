"use client";

import { useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import { Alert, AlertDescription, AlertTitle, Button, toast } from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { closingSweepLine, closingSweepMonthLabel } from "@/lib/closing-sweep";
import { ApiError } from "@/lib/events-api";
import {
  type OperatorClosingSweepStatus,
  fetchOperatorClosingSweep,
  sweepAgainOperatorClosingSweep,
} from "@/lib/operator-api";

/**
 * The Closing Sweep's line on the Operator Dashboard (#750, spec #742, ADR
 * 0078): "N facturas from <month> couldn't be swept", once the month's sweep
 * has ended with facturas the SRI gave no answer about, and a Sweep again
 * that makes just those due on the Drainer's next tick. Nothing while every
 * factura was swept. The month and the counts are the API's.
 *
 * IT LOADS BESIDE THE PAGE, NOT WITH IT, as the Certificate Expiry Warning
 * does: the line is alive whatever Sale Invoicing's flag, and a failed read
 * draws nothing rather than taking the dashboard down.
 */
export function ClosingSweepBanner() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [status, setStatus] = useState<OperatorClosingSweepStatus | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let current = true;
    void fetchOperatorClosingSweep()
      .then((loaded) => {
        if (current) setStatus(loaded);
      })
      .catch(() => {
        if (current) setStatus(null);
      });
    return () => {
      current = false;
    };
  }, []);

  const line = status === null ? null : closingSweepLine(status);
  if (status === null || line === null) {
    return null;
  }

  async function sweepAgain() {
    setBusy(true);
    try {
      setStatus(await sweepAgainOperatorClosingSweep());
      toast.success(t("closingSweepAgainDone"));
    } catch (actionError) {
      toast.error(
        (actionError instanceof ApiError ? apiErrorMessage(errorCopy, actionError) : null) ?? t("closingSweepAgainFailed"),
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <Alert variant="warning">
      <AlertTitle>
        {t("closingSweepUnswept", { count: line.count, month: closingSweepMonthLabel(line.month, locale) })}
      </AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t("closingSweepDescription")}</p>
        {status.due_again_count > 0 ? <p>{t("closingSweepDueAgain", { count: status.due_again_count })}</p> : null}
        {line.sweepAgain ? (
          <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void sweepAgain()}>
            {busy ? t("closingSweepAgainBusy") : t("closingSweepAgain")}
          </Button>
        ) : null}
      </AlertDescription>
    </Alert>
  );
}
