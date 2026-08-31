"use client";

import { useCallback, useEffect, useState } from "react";

import Link from "next/link";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Breadcrumb,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  PageHeader,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import {
  type AcceptanceStanding,
  CUSTOMER_DOCUMENTS,
  CUSTOMER_STANDINGS,
  type CustomerAcceptanceRow,
  DEFAULT_STANDING,
  type LegalDocument,
} from "@/lib/acceptance-browsers";
import { fetchCustomerAcceptances } from "@/lib/acceptance-browsers-api";
import { customerRecordHref } from "@/lib/legal-records";

import { StandingBadge } from "../standing-badge";

// The customer acceptance browser (#565, spec #556, ADR 0067): who has not
// accepted the current Privacy Policy, or the current Terms.
//
// ONE ROW PER PERSON WITH TWO STATUS COLUMNS. A person is one human being, so
// somebody who owes the Terms and is fine on the Policy is one row rather than
// two, and the operator never reconciles a screen by eye.
//
// THE FILTER PICKS WHICH DOCUMENT THE PAGE IS ABOUT; both statuses are shown
// regardless. There is NO PER-EDITION FILTER, so the screen answers "who owes
// something" rather than becoming an edition report — and no edition column,
// for the same reason.
//
// NO OPTIONAL-CONSENT COLUMN, and its absence is the deliberate kind: a
// filterable roster with a marketing-consent column IS a segmentation tool,
// whatever it is called. Optional consents live on the per-subject record,
// read one person at a time. There is also no total, no CSV and no export.
//
// PAGING IS "LOAD MORE" AND NOT PAGE NUMBERS, because the API is keyset: there
// is no total and so no page count, and the only navigation a cursor supports
// is forward. That is the departure ADR 0067 records — after a gating
// publication the outstanding set is the entire customer base, and deep offsets
// would go quadratic exactly when the screen matters most.

function CustomerRow({ row }: { row: CustomerAcceptanceRow }) {
  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        {/*
          THE LINK TO THIS PERSON'S RECORD (#566), and it carries the OPAQUE
          CUSTOMER ID this row already had — Customer identity is UUID-keyed, so
          reading about somebody puts no address in a URL, a query string, a
          referer or an access log (#565). The browser answers "who owes an
          acceptance"; the record answers "what did this person do", and this is
          the seam between the two.
        */}
        <Link className="font-medium underline underline-offset-4" href={customerRecordHref(row.customer_id)}>
          {row.name}
        </Link>
        {/* The address is in the BODY and on the screen; never in a URL. */}
        <p className="font-mono text-xs text-muted-foreground">{row.email}</p>
      </td>
      <td className="py-3 pr-4">
        <StandingBadge standing={row.policy_standing} />
      </td>
      <td className="py-3 pr-4">
        <StandingBadge standing={row.terms_standing} />
      </td>
    </tr>
  );
}

export function OperatorCustomerAcceptancesClient() {
  const t = useTranslations("operator.legalAcceptances");
  const tOperator = useTranslations("operator");
  const errorCopy = useMessages().errors;

  const [document, setDocument] = useState<LegalDocument>("policy");
  const [standing, setStanding] = useState<AcceptanceStanding>(DEFAULT_STANDING);
  // `search` is what is typed; `applied` is what the last request asked for.
  // Two states because the search is SUBMITTED rather than live: each keystroke
  // is a page read of somebody's personal data, and a screen that fires one per
  // character would turn a lookup into a scan.
  const [search, setSearch] = useState("");
  const [applied, setApplied] = useState("");

  const [rows, setRows] = useState<CustomerAcceptanceRow[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    setForbidden(false);
    try {
      const page = await fetchCustomerAcceptances(document, { standing, searchEmail: applied });
      setRows(page.rows);
      setCursor(page.next_cursor);
    } catch (caught) {
      if (caught instanceof ApiError && caught.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(apiErrorMessage(errorCopy, caught as ApiError));
      }
      setRows([]);
      setCursor(null);
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [document, standing, applied]);

  useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (!cursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    try {
      const page = await fetchCustomerAcceptances(document, { standing, searchEmail: applied, cursor });
      // APPENDED, never replaced: the walk is forward-only, and an operator
      // working a re-gate list must not lose the rows they have already read.
      setRows((current) => [...current, ...page.rows]);
      setCursor(page.next_cursor);
    } catch (caught) {
      setError(apiErrorMessage(errorCopy, caught as ApiError));
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <div className="space-y-6">
      <Breadcrumb
        items={[
          { label: tOperator("breadcrumbOperator"), href: "/operator" },
          { label: t("breadcrumbLegalCenter"), href: "/operator/legal" },
          { label: t("customersTitle") },
        ]}
      />
      <PageHeader title={t("customersTitle")} description={t("customersDescription")} />

      <Card>
        <CardHeader>
          <CardTitle>{t("filtersHeading")}</CardTitle>
          <CardDescription>{t("filtersDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap gap-2">
            {CUSTOMER_DOCUMENTS.map((candidate) => (
              <Button
                key={candidate}
                type="button"
                variant={candidate === document ? "default" : "secondary"}
                onClick={() => setDocument(candidate)}
              >
                {t(candidate === "policy" ? "documentPolicy" : "documentTerms")}
              </Button>
            ))}
          </div>
          <div className="flex flex-wrap gap-2">
            {CUSTOMER_STANDINGS.map((candidate) => (
              <Button
                key={candidate}
                type="button"
                variant={candidate === standing ? "default" : "secondary"}
                onClick={() => setStanding(candidate)}
              >
                <StandingBadge standing={candidate} />
              </Button>
            ))}
          </div>
          {/*
            THE SEARCH IS A FORM AND ITS TERM GOES IN A POSTED BODY. It is never
            put in the URL, never in a query string, and never in the referer
            the next click sends — reading about somebody must not leak them
            into a log (#565).
          */}
          <form
            className="grid gap-4 sm:grid-cols-[2fr_auto] sm:items-end"
            onSubmit={(event) => {
              event.preventDefault();
              setApplied(search.trim());
            }}
          >
            <Input
              type="text"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t("searchPlaceholder")}
              autoComplete="off"
              spellCheck={false}
              aria-label={t("searchLabel")}
            />
            <Button type="submit">{t("searchAction")}</Button>
          </form>
        </CardContent>
      </Card>

      {forbidden ? (
        <Alert variant="destructive">
          <AlertTitle>{tOperator("accessDeniedTitle")}</AlertTitle>
          <AlertDescription>{tOperator("accessDenied")}</AlertDescription>
        </Alert>
      ) : null}
      {error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>{t("customersTableHeading")}</CardTitle>
          {/*
            NO TOTAL. It is the expensive half of the query and the least
            actionable number on the screen (ADR 0067), so the caption says what
            a page is rather than how many people there are.
          */}
          <CardDescription>{t("pageDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <p className="text-sm text-muted-foreground">{t("loading")}</p>
          ) : rows.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("empty")}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colPerson")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colPolicy")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colTerms")}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <CustomerRow key={row.customer_id} row={row} />
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {cursor ? (
            <div className="mt-4">
              <Button variant="secondary" onClick={loadMore} disabled={loadingMore} aria-busy={loadingMore}>
                {loadingMore ? t("loadingMore") : t("loadMore")}
              </Button>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
