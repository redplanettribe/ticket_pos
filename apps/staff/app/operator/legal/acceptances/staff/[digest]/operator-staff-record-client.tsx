"use client";

import { useCallback, useEffect, useState } from "react";

import Link from "next/link";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Breadcrumb,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  PageHeader,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import { type StaffLegalRecord, customerRecordHref } from "@/lib/legal-records";
import { fetchStaffLegalRecord } from "@/lib/legal-records-api";

import { StandingBadge } from "../../standing-badge";

/**
 * ONE STAFF PERSON'S ACCEPTANCE RECORD (#566, spec #556, ADR 0067).
 *
 * A SEPARATE SCREEN FROM THE CUSTOMER RECORD, deliberately, and the same
 * decision the two browsers made: two populations with different keys and
 * different evidence. A Customer's record carries two gates and two optional
 * consents; this one carries a single contractual act repeated per edition, and
 * NO OPTIONAL CONSENT AT ALL — staff have none to have.
 *
 * WHERE ONE HUMAN BEING IS BOTH, the two records name each other, resolved
 * server-side from an address that never left the server. They are never
 * merged: no row anywhere says these two are one person, only an address that
 * matches, and a single record would assert an identity the platform cannot
 * evidence.
 *
 * THE HISTORY IS UNPAGED, so the count on this screen is the WHOLE count. A
 * person holds at most one acceptance per edition per capacity, so there is
 * nothing here that can grow without bound the way a Customer's history does.
 *
 * THERE IS NO ACT ON THIS SCREEN. Staff accept the Terms in order to sign in,
 * and a contract's basis is performance rather than consent — so there is
 * nothing to withdraw, nothing to grant, and nothing to re-gate one person
 * with. The absence is the design, and it is enforced by the API having no such
 * route rather than by this file omitting a button.
 */
export function OperatorStaffRecordClient({ digest }: { digest: string }) {
  const t = useTranslations("operator.legalRecords");
  const tAcceptances = useTranslations("operator.legalAcceptances");
  const tOperator = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const [record, setRecord] = useState<StaffLegalRecord | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNotFound(false);
    try {
      setRecord(await fetchStaffLegalRecord(digest));
    } catch (caught) {
      if (caught instanceof ApiError && caught.code === "STAFF_SUBJECT_NOT_FOUND") {
        setNotFound(true);
      } else {
        // STAFF_DIGEST_UNAVAILABLE arrives here: on a deployment with no link
        // secret the screen REFUSES TO SERVE rather than resolve a digest to an
        // arbitrary person, and the operator is told why instead of being shown
        // somebody else's record.
        setError(apiErrorMessage(errorCopy, caught as ApiError));
      }
      setRecord(null);
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [digest]);

  useEffect(() => {
    void load();
  }, [load]);

  const breadcrumb = (
    <Breadcrumb
      items={[
        { label: tOperator("breadcrumbOperator"), href: "/operator" },
        { label: tAcceptances("breadcrumbLegalCenter"), href: "/operator/legal" },
        { label: tAcceptances("staffTitle"), href: "/operator/legal/acceptances/staff" },
        { label: t("staffTitle") },
      ]}
    />
  );

  return (
    <div className="space-y-6">
      {breadcrumb}
      <PageHeader title={t("staffTitle")} description={t("staffDescription")} />

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {notFound ? (
        <Card>
          <CardContent>
            <p className="text-sm text-muted-foreground">{t("staffSubjectNotFound")}</p>
          </CardContent>
        </Card>
      ) : null}

      {loading ? <p className="text-sm text-muted-foreground">{t("loading")}</p> : null}

      {record ? (
        <>
          <Card>
            <CardHeader>
              {/* The address is data and belongs on the screen; never in a URL. */}
              <CardTitle>{record.email}</CardTitle>
              <CardDescription className="font-mono text-xs">{record.digest}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <div>
                <p className="text-sm text-muted-foreground">{t("termsStanding")}</p>
                <StandingBadge standing={record.standing} />
              </div>
              {/*
                THE CROSS-LINK, where the same human being is also a Customer.
                Two records, linked and never merged.
              */}
              {record.customer_id ? (
                <p className="text-sm">
                  <Link
                    className="underline underline-offset-4"
                    href={customerRecordHref(record.customer_id)}
                  >
                    {t("crossLinkToCustomerAction")}
                  </Link>
                </p>
              ) : null}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>{t("staffHistoryTitle")}</CardTitle>
              {/*
                THE COUNT IS THE WHOLE COUNT: this history is unpaged, so there
                is no truncation to distinguish and the sentence says so.
              */}
              <CardDescription>
                {t("staffHistoryComplete", { count: record.visible_count })}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {record.acceptances.length === 0 ? (
                <p className="text-sm text-muted-foreground">{t("staffHistoryEmpty")}</p>
              ) : (
                <ul className="space-y-4">
                  {record.acceptances.map((acceptance) => (
                    <li key={acceptance.id} className="rounded-md border p-4">
                      <div className="flex flex-wrap items-baseline justify-between gap-2">
                        <p className="font-medium">
                          {t("editionNamed", {
                            edition: acceptance.terms_edition.label || t("notRecorded"),
                          })}
                        </p>
                        <p className="text-sm text-muted-foreground">
                          {formatDateTime(acceptance.accepted_at, PLATFORM_TIME_ZONE, locale) ??
                            t("notRecorded")}
                        </p>
                      </div>
                      <dl className="mt-3 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
                        <div>
                          <dt className="text-muted-foreground">{t("actCapacity")}</dt>
                          <dd>{acceptance.capacity}</dd>
                        </div>
                        {/*
                          NULL IS SPELLED IN WORDS here too. The staff gate is
                          the one surface that always presents a document, so
                          this field is never absent — only recorded or not.
                        */}
                        <div>
                          <dt className="text-muted-foreground">{t("actPresentedLocale")}</dt>
                          <dd>{acceptance.presented_locale ?? t("notRecorded")}</dd>
                        </div>
                        <div>
                          <dt className="text-muted-foreground">{t("actIP")}</dt>
                          <dd className="break-words">{acceptance.ip ?? t("notRecorded")}</dd>
                        </div>
                        <div>
                          <dt className="text-muted-foreground">{t("actSession")}</dt>
                          <dd className="break-words">
                            {acceptance.session_id ?? t("notRecorded")}
                          </dd>
                        </div>
                      </dl>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  );
}
