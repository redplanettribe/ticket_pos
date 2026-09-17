"use client";

import type { ReactNode } from "react";

import { toAppLocale, type AppLocale } from "@ticket-pos/locale";
import { Badge } from "@ticket-pos/ui";
import { useLocale, useTranslations } from "next-intl";

import {
  nameGivenOnSale,
  saleStatusBadgeVariant,
  saleStatusToken,
  type DossierSale,
} from "@/lib/customer-dossier";
import {
  NOTHING_TO_SHOW,
  PLATFORM_TIME_ZONE,
  formatDateTime,
  formatMoney,
  formatNumber,
} from "@/lib/format";
import {
  paymentMethodToken,
  saleChannelToken,
  saleOriginToken,
  saleSourceToken,
  taxIdSnapshot,
} from "@/lib/sales-api";

import {
  CHANNEL_KEYS,
  ORIGIN_KEYS,
  PAYMENT_METHOD_KEYS,
  SOURCE_KEYS,
  TAX_ID_KEYS,
} from "../../../sales-list";

type DossierSalesSectionProps = {
  sales: DossierSale[];
  timezone: string | null;
};

/** This Event's Ticket Sales to the person, newest first, one card each. */
export function DossierSalesSection({ sales, timezone }: DossierSalesSectionProps) {
  const t = useTranslations("customerDossier");
  const locale = toAppLocale(useLocale());

  return (
    <section aria-labelledby="dossier-sales" className="space-y-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="dossier-sales" className="text-base font-semibold">
          {t("salesHeading")}
        </h2>
        <span className="text-sm text-muted-foreground">{t("salesCount", { count: sales.length })}</span>
      </div>
      <ul className="space-y-3">
        {sales.map((sale) => (
          <li key={sale.id}>
            <DossierSaleCard sale={sale} zone={timezone ?? PLATFORM_TIME_ZONE} locale={locale} />
          </li>
        ))}
      </ul>
    </section>
  );
}

type DossierSaleCardProps = {
  sale: DossierSale;
  zone: string;
  locale: AppLocale;
};

/**
 * One Sale as this Event recorded it. Words for channel, source, origin,
 * payment method and Tax ID Type are the sales list's own (`sales` namespace),
 * so the two screens cannot name one Sale differently.
 */
export function DossierSaleCard({ sale, zone, locale }: DossierSaleCardProps) {
  const t = useTranslations("customerDossier");
  const tSales = useTranslations("sales");

  const statusToken = saleStatusToken(sale.status);
  const statusLabel =
    statusToken === "active"
      ? t("statusActive")
      : statusToken === "reversed"
        ? tSales("reversedBadge")
        : statusToken === "corrected"
          ? tSales("correctedBadge")
          : sale.status;
  const reversedAt = statusToken !== "active" ? formatDateTime(sale.reversed_at, zone, locale) : null;

  const channelToken = saleChannelToken(sale.channel);
  const channel = channelToken ? tSales(CHANNEL_KEYS[channelToken]) : sale.channel;
  const sourceToken = saleSourceToken(sale.source);
  const originToken = saleOriginToken(sale.origin);
  const origin = originToken ? tSales(ORIGIN_KEYS[originToken]) : sale.origin;
  const paymentToken = paymentMethodToken(sale.payment_method);
  const taxId = taxIdSnapshot(sale.tax_id_type, sale.tax_id_number);
  const name = nameGivenOnSale(sale);

  return (
    <article className="rounded-md border p-3 text-sm sm:p-4">
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="font-mono font-medium">{sale.confirmation_ref}</span>
        <Badge variant={saleStatusBadgeVariant(statusToken)}>{statusLabel}</Badge>
        {sale.replaced_by_confirmation_ref ? (
          <span className="font-mono text-xs text-muted-foreground">
            {tSales("correctedTo", { reference: sale.replaced_by_confirmation_ref })}
          </span>
        ) : null}
        {reversedAt ? (
          <span className="text-xs text-muted-foreground">{t("reversedAt", { when: reversedAt })}</span>
        ) : null}
      </header>
      <dl className="mt-3 grid gap-x-6 gap-y-2 sm:grid-cols-2">
        <Fact label={t("nameGivenLabel")}>{name || NOTHING_TO_SHOW}</Fact>
        <Fact label={t("taxIdGivenLabel")}>
          {taxId
            ? tSales("taxIdValue", {
                type: taxId.token ? tSales(TAX_ID_KEYS[taxId.token]) : taxId.rawType,
                number: taxId.number,
              })
            : NOTHING_TO_SHOW}
        </Fact>
        <Fact label={tSales("colSold")}>{formatDateTime(sale.sold_at, zone, locale) ?? NOTHING_TO_SHOW}</Fact>
        <Fact label={tSales("colRecorded")}>
          {formatDateTime(sale.recorded_at, zone, locale) ?? NOTHING_TO_SHOW}
        </Fact>
        <Fact label={tSales("colChannel")}>
          <div>
            {sourceToken || sale.source
              ? tSales("channelWithSource", {
                  channel,
                  source: sourceToken ? tSales(SOURCE_KEYS[sourceToken]) : (sale.source ?? ""),
                })
              : channel}
          </div>
          {origin ? <div className="text-xs text-muted-foreground">{origin}</div> : null}
        </Fact>
        <Fact label={tSales("colTicketTypes")}>
          {sale.ticket_types.length === 0 ? (
            NOTHING_TO_SHOW
          ) : (
            <ul>
              {sale.ticket_types.map((type) => (
                <li key={type.ticket_type_id}>
                  {tSales("ticketTypeQuantity", {
                    quantity: formatNumber(type.quantity, locale),
                    name: type.ticket_type_name,
                  })}
                </li>
              ))}
            </ul>
          )}
        </Fact>
        <Fact label={tSales("colAmount")}>
          <span className="tabular-nums">{formatMoney(sale.amount_cents, sale.currency, locale)}</span>
        </Fact>
        <Fact label={tSales("paymentMethodLabel")}>
          {paymentToken ? tSales(PAYMENT_METHOD_KEYS[paymentToken]) : (sale.payment_method ?? NOTHING_TO_SHOW)}
        </Fact>
      </dl>
    </article>
  );
}

/** One label and its value; stacks on a narrow screen. */
function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-words">{children}</dd>
    </div>
  );
}
