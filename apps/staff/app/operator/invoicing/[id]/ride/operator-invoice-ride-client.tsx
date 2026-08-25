"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import { Alert, AlertDescription, AlertTitle, Button } from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, PLATFORM_TIME_ZONE, formatCalendarDay, formatDateTime, formatMoney } from "@/lib/format";
import { type InvoiceStatus, type OperatorInvoiceDetail, fetchOperatorInvoice } from "@/lib/operator-api";

import { Code128Svg } from "./code128-svg";

// The RIDE (#456): the Representación Impresa del Documento Electrónico,
// rendered from the detail endpoint's data and nothing else. For an
// authorized invoice it carries the authorization number and date; for any
// other status it prints no authorization number and says plainly where the
// invoice stands, so an invalid RIDE cannot be handed over by mistake
// (research §1.7: a RIDE without the authorization number has no validity).

const TAX_ID_TYPE_KEYS = {
  ruc: "invoicingRecipientTaxIdTypeRuc",
  cedula: "invoicingRecipientTaxIdTypeCedula",
  passport: "invoicingRecipientTaxIdTypePassport",
} as const;

const UNAUTHORIZED_STATUS_KEYS = {
  pending: "invoicingRideStatusPending",
  not_authorized: "invoicingRideStatusNotAuthorized",
  rejected: "invoicingRideStatusRejected",
} as const satisfies Record<Exclude<InvoiceStatus, "authorized">, string>;

const REGIMEN_LEGEND_KEYS = {
  rimpe_contribuyente: "invoicingRideRegimenRimpe",
  rimpe_negocio_popular: "invoicingRideRegimenRimpeNegocioPopular",
} as const;

const cell = "border border-black px-2 py-1 align-top";
const label = "text-[10px] uppercase tracking-wide text-neutral-600";

export function OperatorInvoiceRideClient({ invoiceId }: { invoiceId: string }) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale()) as AppLocale;
  const [invoice, setInvoice] = useState<OperatorInvoiceDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setInvoice(await fetchOperatorInvoice(invoiceId));
    } catch (loadError) {
      setError(
        (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
          t("invoicingDetailLoadFailed"),
      );
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [invoiceId]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <p className="p-6 text-sm text-muted-foreground">{t("invoicingDetailLoading")}</p>;
  }
  if (error || !invoice) {
    return (
      <div className="p-6">
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingDetailLoadFailedTitle")}</AlertTitle>
          <AlertDescription>{error ?? t("invoicingDetailLoadFailed")}</AlertDescription>
        </Alert>
      </div>
    );
  }

  const money = (cents: number) => formatMoney(cents, invoice.currency, locale);
  const authorized = invoice.status === "authorized" && invoice.ecuador.authorization_number;
  const regimenLegendKey = REGIMEN_LEGEND_KEYS[invoice.issuer.regimen as keyof typeof REGIMEN_LEGEND_KEYS];
  const taxIdTypeKey = TAX_ID_TYPE_KEYS[invoice.recipient.tax_id_type as keyof typeof TAX_ID_TYPE_KEYS];

  return (
    <div className="ride-page mx-auto max-w-[210mm] bg-white p-6 text-[12px] leading-snug text-black print:p-0">
      <div className="mb-4 flex items-center justify-between gap-3 print:hidden">
        <Link href={`/operator/invoicing/${encodeURIComponent(invoice.id)}`} className="text-sm text-muted-foreground hover:underline">
          {t("invoicingRideBack")}
        </Link>
        <Button type="button" onClick={() => window.print()}>
          {t("invoicingRidePrint")}
        </Button>
      </div>

      {!authorized ? (
        <div
          role="status"
          className="mb-3 border-2 border-black px-3 py-2 text-center text-sm font-semibold uppercase"
        >
          {t(UNAUTHORIZED_STATUS_KEYS[invoice.status as keyof typeof UNAUTHORIZED_STATUS_KEYS] ?? "invoicingRideStatusPending")}
        </div>
      ) : null}

      <div className="grid grid-cols-2 gap-3 break-inside-avoid">
        <section className="border border-black p-3" aria-label={t("invoicingDetailIssuer")}>
          <p className="text-base font-bold">{invoice.issuer.razon_social}</p>
          {invoice.issuer.nombre_comercial ? <p className="font-semibold">{invoice.issuer.nombre_comercial}</p> : null}
          <dl className="mt-2 space-y-1">
            <div>
              <dt className={label}>{t("invoicingDireccionMatrizLabel")}</dt>
              <dd>{invoice.issuer.direccion_matriz}</dd>
            </div>
            <div>
              <dt className={label}>{t("invoicingDireccionEstablecimientoLabel")}</dt>
              <dd>{invoice.issuer.direccion_establecimiento}</dd>
            </div>
            <div className="flex gap-2">
              <dt className={label}>{t("invoicingObligadoContabilidadLabel")}:</dt>
              <dd className="font-semibold">{invoice.issuer.obligado_contabilidad ? t("invoicingRideYes") : t("invoicingRideNo")}</dd>
            </div>
            {regimenLegendKey ? <p className="font-semibold">{t(regimenLegendKey)}</p> : null}
            {invoice.issuer.agente_retencion ? (
              <p>
                {t("invoicingRideAgenteRetencion")}: {invoice.issuer.agente_retencion}
              </p>
            ) : null}
          </dl>
        </section>

        <section className="border border-black p-3" aria-label={t("invoicingDetailAuthorization")}>
          <p>
            <span className={label}>{t("invoicingRucLabel")}: </span>
            <span className="font-mono font-semibold">{invoice.issuer.ruc}</span>
          </p>
          <p className="mt-1 text-lg font-bold uppercase">{t("invoicingRideTitle")}</p>
          <p>
            <span className={label}>{t("invoicingRideNumber")}: </span>
            <span className="font-mono font-semibold" data-testid="ride-number">{invoice.number}</span>
          </p>
          <div className="mt-2">
            <p className={label}>{t("invoicingRideAuthorizationNumber")}</p>
            {authorized ? (
              <p className="break-all font-mono text-[11px]" data-testid="ride-authorization-number">
                {invoice.ecuador.authorization_number}
              </p>
            ) : (
              <p className="italic text-neutral-600">{t("invoicingAuthorizationNone")}</p>
            )}
          </div>
          {authorized ? (
            <p className="mt-1">
              <span className={label}>{t("invoicingRideAuthorizationDate")}: </span>
              {formatDateTime(invoice.ecuador.authorization_date, PLATFORM_TIME_ZONE, locale)}
            </p>
          ) : null}
          <p className="mt-1">
            <span className={label}>{t("invoicingRideEnvironment")}: </span>
            <span className="font-semibold uppercase">
              {invoice.environment === "test" ? t("invoicingRideEnvironmentTest") : t("invoicingRideEnvironmentProduction")}
            </span>
          </p>
          <p>
            <span className={label}>{t("invoicingRideEmissionType")}: </span>
            <span className="font-semibold uppercase">{t("invoicingRideEmissionTypeNormal")}</span>
          </p>
          <div className="mt-2">
            <p className={label}>{t("invoicingClave")}</p>
            <Code128Svg value={invoice.ecuador.access_key} className="h-11 w-full" />
            <p className="break-all text-center font-mono text-[10px]" data-testid="ride-access-key">
              {invoice.ecuador.access_key}
            </p>
          </div>
        </section>
      </div>

      <section className="mt-3 border border-black p-3 break-inside-avoid" aria-label={t("invoicingDetailRecipient")}>
        <div className="grid grid-cols-2 gap-x-4 gap-y-1">
          <p>
            <span className={label}>{t("invoicingRideRecipientName")}: </span>
            {invoice.recipient.legal_name}
          </p>
          <p>
            <span className={label}>
              {taxIdTypeKey ? t(taxIdTypeKey) : t("invoicingRecipientTaxId")}:{" "}
            </span>
            <span className="font-mono">{invoice.recipient.tax_id}</span>
          </p>
          <p>
            <span className={label}>{t("invoicingEmissionDate")}: </span>
            {formatCalendarDay(invoice.issued_on, locale)}
          </p>
          {invoice.recipient.address ? (
            <p>
              <span className={label}>{t("invoicingRecipientAddress")}: </span>
              {invoice.recipient.address}
            </p>
          ) : null}
          <p>
            <span className={label}>{t("invoicingRecipientEmail")}: </span>
            {invoice.recipient.email}
          </p>
        </div>
      </section>

      <table className="mt-3 w-full border-collapse break-inside-avoid">
        <thead>
          <tr className="bg-neutral-100 text-left text-[10px] uppercase tracking-wide">
            <th className={cell}>{t("invoicingLineDescription")}</th>
            <th className={`${cell} text-right`}>{t("invoicingLineQuantity")}</th>
            <th className={`${cell} text-right`}>{t("invoicingLineUnitPrice")}</th>
            <th className={`${cell} text-right`}>{t("invoicingLineDiscount")}</th>
            <th className={`${cell} text-right`}>{t("invoicingLineIvaRate")}</th>
            <th className={`${cell} text-right`}>{t("invoicingRideLineTotal")}</th>
          </tr>
        </thead>
        <tbody>
          {invoice.lines.map((line) => (
            <tr key={line.position}>
              <td className={cell}>{line.description}</td>
              <td className={`${cell} text-right tabular-nums`}>{line.quantity}</td>
              <td className={`${cell} text-right tabular-nums`}>{money(line.unit_price_cents)}</td>
              <td className={`${cell} text-right tabular-nums`}>{money(line.discount_cents)}</td>
              <td className={`${cell} text-right tabular-nums`}>
                {line.iva_rate === "exento"
                  ? t("invoicingRideRateExento")
                  : line.iva_rate === "no_objeto"
                    ? t("invoicingRideRateNoObjeto")
                    : `${line.rate_percent}%`}
              </td>
              <td className={`${cell} text-right tabular-nums`}>{money(line.base_cents)}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="mt-3 grid grid-cols-2 gap-3 break-inside-avoid">
        <div className="space-y-3">
          {invoice.additional_fields.length > 0 ? (
            <section className="border border-black p-3" aria-label={t("invoicingRideAdditionalInfo")}>
              <p className={`${label} mb-1 font-semibold`}>{t("invoicingRideAdditionalInfo")}</p>
              <dl>
                {invoice.additional_fields.map((field, index) => (
                  <div key={`${field.name}-${index}`} className="flex gap-2">
                    <dt className="font-semibold">{field.name}:</dt>
                    <dd>{field.value}</dd>
                  </div>
                ))}
              </dl>
            </section>
          ) : null}
          <section className="border border-black p-3" aria-label={t("invoicingPaymentMethod")}>
            <p className={`${label} mb-1 font-semibold`}>{t("invoicingPaymentMethod")}</p>
            <p>
              <span className="font-mono">{invoice.payment_method}</span> — {invoice.payment_method_label}{" "}
              <span className="tabular-nums">{money(invoice.totals.total_cents)}</span>
            </p>
          </section>
        </div>

        <table className="w-full border-collapse self-start">
          <tbody>
            {invoice.totals.by_rate.map((rate) => (
              <tr key={rate.iva_rate}>
                <td className={cell}>
                  {rate.iva_rate === "exento"
                    ? t("invoicingRideSubtotalExento")
                    : rate.iva_rate === "no_objeto"
                      ? t("invoicingRideSubtotalNoObjeto")
                      : t("invoicingRideSubtotalRate", { percent: rate.rate_percent })}
                </td>
                <td className={`${cell} text-right tabular-nums`}>{money(rate.base_cents)}</td>
              </tr>
            ))}
            <tr>
              <td className={cell}>{t("invoicingRideSubtotalNoTaxes")}</td>
              <td className={`${cell} text-right tabular-nums`}>{money(invoice.totals.subtotal_cents)}</td>
            </tr>
            <tr>
              <td className={cell}>{t("invoicingTotalDiscount")}</td>
              <td className={`${cell} text-right tabular-nums`}>{money(invoice.totals.discount_cents)}</td>
            </tr>
            {invoice.totals.by_rate
              .filter((rate) => rate.rate_percent > 0)
              .map((rate) => (
                <tr key={`iva-${rate.iva_rate}`}>
                  <td className={cell}>{t("invoicingRideIvaRate", { percent: rate.rate_percent })}</td>
                  <td className={`${cell} text-right tabular-nums`}>{money(rate.iva_cents)}</td>
                </tr>
              ))}
            <tr>
              <td className={cell}>{t("invoicingTotalIva")}</td>
              <td className={`${cell} text-right tabular-nums`}>{money(invoice.totals.iva_cents)}</td>
            </tr>
            <tr className="font-bold">
              <td className={cell}>{t("invoicingRideTotal")}</td>
              <td className={`${cell} text-right tabular-nums`} data-testid="ride-total">
                {money(invoice.totals.total_cents)}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}
