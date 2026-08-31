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
  DEFAULT_STANDING,
  STAFF_STANDINGS,
  type StaffAcceptanceRow,
} from "@/lib/acceptance-browsers";
import { fetchStaffAcceptances } from "@/lib/acceptance-browsers-api";
import { staffRecordHref } from "@/lib/legal-records";

import { StandingBadge } from "../standing-badge";

// The staff acceptance browser (#565, spec #556, ADR 0067): which of the people
// who sign into the Staff platform owe a Terms Acceptance.
//
// A SEPARATE SCREEN FROM THE CUSTOMER BROWSER, deliberately. Two populations
// with different keys, a different number of status columns and — here — a
// fourth state the other cannot have are not worth forcing through one
// configuration object.
//
// THE TERMS AND ONLY THE TERMS. There is exactly one staff gate: everybody who
// signs in accepts the Términos y Condiciones "en calidad de organizador".
// Staff accept no Privacy Policy, so there is no document filter here and the
// heading states the document as a fact instead of offering a menu.
//
// FOUR STATES, INCLUDING *Former*: somebody who accepted at some point and is
// on neither membership table now. It is COMPUTED AND NEVER STORED — a
// departure is a DELETE from `members`, and nothing anywhere records that
// somebody used to be staff — and it is a FILTER VALUE rather than a hidden
// state: a leaver owes nothing, so listing them among the outstanding would
// fill the default filter with people nobody can chase.
//
// EACH ROW IS KEYED ON A DIGEST because a staff person has no id — the person
// key of the Staff platform is an email — and an address must never reach a
// URL. The digest is shown so an operator can match a row to a link; it is
// written to no row, no log, no file and no export.

function StaffRow({ row }: { row: StaffAcceptanceRow }) {
  return (
    <tr className="border-b last:border-b-0">
      <td className="py-3 pr-4">
        {/*
          The address is in the BODY and on the screen; never in a URL. The LINK
          carries the DIGEST instead (#566) — a staff person has no id, so the
          digest is what names them in a route, and it discloses nobody to
          anybody without the key.
        */}
        <Link className="font-medium underline underline-offset-4" href={staffRecordHref(row.digest)}>
          {row.email}
        </Link>
        <p className="font-mono text-xs text-muted-foreground">{row.digest}</p>
      </td>
      <td className="py-3 pr-4">
        <StandingBadge standing={row.standing} />
      </td>
    </tr>
  );
}

export function OperatorStaffAcceptancesClient() {
  const t = useTranslations("operator.legalAcceptances");
  const tOperator = useTranslations("operator");
  const errorCopy = useMessages().errors;

  const [standing, setStanding] = useState<AcceptanceStanding>(DEFAULT_STANDING);
  const [search, setSearch] = useState("");
  const [applied, setApplied] = useState("");

  const [rows, setRows] = useState<StaffAcceptanceRow[]>([]);
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
      const page = await fetchStaffAcceptances({ standing, searchEmail: applied });
      setRows(page.rows);
      setCursor(page.next_cursor);
    } catch (caught) {
      if (caught instanceof ApiError && caught.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        // STAFF_DIGEST_UNAVAILABLE arrives here: on a deployment with no link
        // secret the screen REFUSES TO SERVE rather than name people under an
        // empty key, and the operator is told why instead of shown an empty
        // list that would read as "nobody owes anything".
        setError(apiErrorMessage(errorCopy, caught as ApiError));
      }
      setRows([]);
      setCursor(null);
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [standing, applied]);

  useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (!cursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    try {
      const page = await fetchStaffAcceptances({ standing, searchEmail: applied, cursor });
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
          { label: t("staffTitle") },
        ]}
      />
      <PageHeader title={t("staffTitle")} description={t("staffDescription")} />

      <Card>
        <CardHeader>
          <CardTitle>{t("filtersHeading")}</CardTitle>
          <CardDescription>{t("staffFiltersDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap gap-2">
            {STAFF_STANDINGS.map((candidate) => (
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
          <CardTitle>{t("staffTableHeading")}</CardTitle>
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
                    <th className="py-2 pr-4 font-medium">{t("colTerms")}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <StaffRow key={row.digest} row={row} />
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
