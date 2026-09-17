"use client";

import type { ReactNode } from "react";

import type { AppLocale } from "@ticket-pos/locale";
import { Badge } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import type { DossierSale } from "@/lib/customer-dossier";
import {
  dossierInvoiceKindKey,
  dossierInvoiceRoleKey,
  dossierInvoiceStatusKey,
  dossierInvoiceStatusVariant,
  phoneGivenOnSale,
} from "@/lib/dossier-sale-surroundings";
import { NOTHING_TO_SHOW, formatDateTime } from "@/lib/format";
import { taxIdSnapshot } from "@/lib/sales-api";

import { TAX_ID_KEYS } from "../../../sales-list";

type DossierSaleSurroundingsProps = {
  sale: DossierSale;
  zone: string;
  locale: AppLocale;
};

/**
 * What surrounds one Sale (#639): the phone given on its checkout, the
 * Affiliate Link that brought it, when it was re-addressed and its Sale
 * Invoices. Never an address: a re-addressing is only ever a time.
 */
export function DossierSaleSurroundings({ sale, zone, locale }: DossierSaleSurroundingsProps) {
  const t = useTranslations("customerDossier");
  const tSales = useTranslations("sales");

  const phone = phoneGivenOnSale(sale);
  const reAddressedAt = formatDateTime(sale.re_addressed_at, zone, locale);
  const invoices = sale.sale_invoices ?? [];

  return (
    <dl className="mt-3 grid gap-x-6 gap-y-2 border-t pt-3 sm:grid-cols-2">
      <Fact label={t("phoneLabel")}>
        {phone.state === "none" ? (
          <span className="text-muted-foreground">{t("phoneNone")}</span>
        ) : phone.href ? (
          <a href={phone.href} className="underline underline-offset-2">
            {phone.phone}
          </a>
        ) : (
          phone.phone
        )}
      </Fact>
      <Fact label={t("affiliateLinkLabel")}>{sale.affiliate_link_name || NOTHING_TO_SHOW}</Fact>
      {reAddressedAt ? <Fact label={t("reAddressedLabel")}>{t("reAddressedAt", { when: reAddressedAt })}</Fact> : null}
      <div className="min-w-0 sm:col-span-2">
        <dt className="text-xs text-muted-foreground">{t("saleInvoicesLabel")}</dt>
        <dd>
          {invoices.length === 0 ? (
            <span className="text-muted-foreground">{t("saleInvoicesNone")}</span>
          ) : (
            <ul className="space-y-2">
              {invoices.map((invoice, index) => {
                const kindKey = dossierInvoiceKindKey(invoice.kind);
                const statusKey = dossierInvoiceStatusKey(invoice.status);
                const roleKey = dossierInvoiceRoleKey(invoice.role);
                const taxId = taxIdSnapshot(invoice.recipient_tax_id_type, invoice.recipient_tax_id);
                return (
                  <li key={`${invoice.number ?? "unnumbered"}-${index}`} className="break-words">
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                      <span>{kindKey ? t(kindKey) : invoice.kind}</span>
                      <span className="font-mono">{invoice.number ?? t("invoiceUnnumbered")}</span>
                      <Badge variant={dossierInvoiceStatusVariant(invoice.status)}>
                        {statusKey ? t(statusKey) : invoice.status}
                      </Badge>
                      {roleKey ? <Badge variant="outline">{t(roleKey)}</Badge> : null}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      {t("invoiceRecipient", {
                        name: invoice.recipient_legal_name || NOTHING_TO_SHOW,
                        taxId: taxId
                          ? tSales("taxIdValue", {
                              type: taxId.token ? tSales(TAX_ID_KEYS[taxId.token]) : taxId.rawType,
                              number: taxId.number,
                            })
                          : NOTHING_TO_SHOW,
                      })}
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </dd>
      </div>
    </dl>
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
