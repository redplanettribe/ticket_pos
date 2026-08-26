"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Card,
  CardContent,
  PageHeader,
  cn,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { type AppLocale, formatMoney, formatNumber } from "@/lib/format";
import {
  type OperatorOrganizationRow,
  type OperatorPagination,
  fetchOperatorOrganizations,
} from "@/lib/operator-api";

/**
 * Money that can legitimately be negative — an Organization that owes the
 * platform after a post-settlement reversal. Shown as-is, in the destructive
 * colour, never clamped: the sign is the information.
 *
 * The currency is the ORGANIZATION's and the locale is the reader's: one decides
 * which money this is, the other only the marks around it (ADR 0041). A roll of
 * every organization on the platform may well mix currencies, and none of them
 * is converted.
 */
function SignedAmount({
  cents,
  currency,
  locale,
}: {
  cents: number;
  currency: string;
  locale: AppLocale;
}) {
  return (
    <span className={cn("tabular-nums", cents < 0 && "text-destructive")}>
      {formatMoney(cents, currency, locale)}
    </span>
  );
}

/**
 * Every Organization on the platform with its Events and its Withdrawable
 * Balance, and the way into the one an operator came for (#193).
 *
 * A page of its own rather than a section of the Overview: this is the roll of
 * who the platform owes, and it is read on its own terms.
 */
export function OperatorOrganizationsClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const [organizations, setOrganizations] = useState<OperatorOrganizationRow[]>([]);
  const [pagination, setPagination] = useState<OperatorPagination | null>(null);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const organizationsPage = await fetchOperatorOrganizations(page);
      setOrganizations(organizationsPage.data);
      setPagination(organizationsPage.pagination);
      setForbidden(false);
    } catch (loadError) {
      if (loadError instanceof ApiError && loadError.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(
          (loadError instanceof ApiError ? apiErrorMessage(errorCopy, loadError) : null) ??
            t("organizationsLoadFailed"),
        );
      }
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("organizationsLoading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("organizationsLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader title={t("organizationsTitle")} description={t("organizationsDescription")} />

      <Card>
        <CardContent>
          {organizations.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("organizationsEmpty")}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colOrganization")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colSlug")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colCurrency")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colEvents")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colWithdrawableBalance")}</th>
                  </tr>
                </thead>
                <tbody>
                  {organizations.map((organization) => (
                    <tr key={organization.id} className="border-b last:border-b-0">
                      {/* The Organization's name and slug are data, and read as
                          coined in both languages (ADR 0041). */}
                      <td className="py-3 pr-4">
                        <span className="inline-flex items-center gap-2">
                          <Link
                            href={`/operator/organizations/${organization.id}`}
                            className="font-medium hover:underline"
                          >
                            {organization.name}
                          </Link>
                          {/* A House Organization is the platform's own, and
                              the roll says so beside the name (#472, ADR 0060). */}
                          {organization.is_house_organization ? (
                            <Badge className="w-fit">{t("houseBadge")}</Badge>
                          ) : null}
                        </span>
                      </td>
                      <td className="py-3 pr-4 font-mono text-xs text-muted-foreground">
                        {organization.slug}
                      </td>
                      <td className="py-3 pr-4">{organization.currency}</td>
                      <td className="py-3 pr-4 tabular-nums">
                        {formatNumber(organization.events_count, locale)}
                      </td>
                      <td className="py-3 pr-4">
                        <SignedAmount
                          cents={organization.withdrawable_balance_cents}
                          currency={organization.currency}
                          locale={locale}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {pagination && pagination.total_pages > 1 ? (
            <div className="mt-4 flex items-center justify-between">
              <p className="text-sm text-muted-foreground">
                {t("organizationsPageSummary", {
                  page: formatNumber(pagination.page, locale),
                  pages: formatNumber(pagination.total_pages, locale),
                  total: pagination.total,
                })}
              </p>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pagination.page <= 1}
                  onClick={() => setPage((current) => Math.max(1, current - 1))}
                >
                  {t("previousPage")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pagination.page >= pagination.total_pages}
                  onClick={() => setPage((current) => current + 1)}
                >
                  {t("nextPage")}
                </Button>
              </div>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
