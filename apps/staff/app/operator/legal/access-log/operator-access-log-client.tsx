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
import { toAppLocale } from "@ticket-pos/locale";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { ACCESS_ACTS, type AccessAct, type AccessEntry, accessSubjectHref } from "@/lib/access-log";
import { fetchAccessLog } from "@/lib/access-log-api";
import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";

// THE CONSENT ACCESS LOG (#569, spec #556, ADR 0067): who has looked at whose
// data, and what they asked for.
//
// FILTERABLE BY ACTOR, ACT AND DATE, AND NEVER BY SUBJECT. That absence is the
// screen's central design decision. An audit log searchable by the person it is
// about would be a second way to look people up — keyed on the record of people
// being looked up, and available to precisely the role whose looking it exists
// to record. There is no subject box here, and the module behind it has nowhere
// to put one.
//
// THE QUESTION IS SHOWN, NEVER THE ROSTER. A `list_read` row says which
// population, which document, which filter, whether it was narrowed by a search
// and how many rows came back — because the log has never held the rows
// themselves, and `searched` is a boolean so that looking one person up did not
// deposit their address here.
//
// AND THERE IS NO EXPORT BUTTON, no purge and no retention control. The log is
// read where it lives: downloading it would be one of the acts it records, and
// evidence that ages out is evidence the platform cannot produce on the day it
// is asked for.
//
// PAGING IS "LOAD MORE", the browsers' shape: the API is keyset, so there is no
// total and no page count, and the only navigation a cursor supports is forward.

// The copy key per act, written out as a literal map rather than derived by the
// pure module's helper, following StandingBadge and for its reason: next-intl's
// `t` takes a KEY LITERAL and typechecks it against the catalogue, so a computed
// string would compile only by being widened to `string` — which is exactly the
// check worth keeping. `accessActLabelKey` is the same mapping for the unit
// test, which asserts the four keys exist in both catalogues.
const ACT_LABEL_KEY = {
  list_read: "actListRead",
  subject_read: "actSubjectRead",
  evidence_export: "actEvidenceExport",
  audit_read: "actAuditRead",
} as const;

/** One logged act. */
function AccessRow({ entry, locale }: { entry: AccessEntry; locale: string }) {
  const t = useTranslations("operator.legalAccessLog");
  const when = formatDateTime(entry.occurred_at, PLATFORM_TIME_ZONE, toAppLocale(locale));
  const href = accessSubjectHref(entry);

  return (
    <tr className="border-b align-top last:border-b-0">
      <td className="py-3 pr-4 whitespace-nowrap text-muted-foreground">{when ?? entry.occurred_at}</td>
      <td className="py-3 pr-4">
        <p className="font-medium">{t(ACT_LABEL_KEY[entry.act])}</p>
      </td>
      {/* The operator, in the body and on the screen — never in a URL. */}
      <td className="py-3 pr-4 font-mono text-xs">{entry.actor_email}</td>
      <td className="py-3 pr-4">
        {/*
          WHO WAS LOOKED AT, where the act named somebody. The link back carries
          the OPAQUE CUSTOMER ID the act was performed under, so following the
          audit log puts no address in a URL. A staff subject is a plain address
          with no link: it is recorded as an address and never as a digest,
          because a key rotation must not orphan a row (#548).
        */}
        {entry.subject_email ? (
          href ? (
            <Link className="underline underline-offset-4" href={href}>
              {entry.subject_email}
            </Link>
          ) : (
            <span className="font-mono text-xs">{entry.subject_email}</span>
          )
        ) : (
          <span className="text-muted-foreground">{t("noSubject")}</span>
        )}
      </td>
      <td className="py-3 pr-4 text-xs text-muted-foreground">
        {/*
          WHAT WAS ASKED, never what came back. A list read shows its population,
          document and filter; a search shows only THAT it was searched; an
          export shows the pack's fingerprint, which is the half of the SHA-256
          the filename carries, so a file in a mailbox matches its row by eye.
        */}
        {entry.population && entry.document ? (
          <p>
            {t("questionSummary", {
              population: t(entry.population === "customer" ? "populationCustomer" : "populationStaff"),
              document: t(entry.document === "policy" ? "documentPolicy" : "documentTerms"),
              filter: entry.status_filter ?? t("filterNone"),
            })}
          </p>
        ) : entry.status_filter ? (
          <p>{t("auditFilter", { filter: entry.status_filter })}</p>
        ) : null}
        {entry.searched ? <p>{t("wasSearched")}</p> : null}
        {entry.result_count !== null ? <p>{t("resultCount", { n: entry.result_count })}</p> : null}
        {entry.pack_sha256 ? <p className="font-mono">{entry.pack_sha256.slice(0, 16)}</p> : null}
      </td>
    </tr>
  );
}

export function OperatorAccessLogClient() {
  const t = useTranslations("operator.legalAccessLog");
  const tOperator = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = useLocale();

  // `actor` is what is typed; `appliedActor` is what the last request asked
  // for. Two states because the filter is SUBMITTED rather than live: each
  // keystroke would be a page of an audit log, and reading the log is itself a
  // logged act — a request per character would fill the log with itself.
  const [actor, setActor] = useState("");
  const [appliedActor, setAppliedActor] = useState("");
  const [act, setAct] = useState<AccessAct | "">("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");

  const [entries, setEntries] = useState<AccessEntry[]>([]);
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
      const page = await fetchAccessLog({ actor: appliedActor, act, from, to });
      setEntries(page.entries);
      setCursor(page.next_cursor);
    } catch (caught) {
      if (caught instanceof ApiError && caught.code === "FORBIDDEN") {
        setForbidden(true);
      } else {
        setError(apiErrorMessage(errorCopy, caught as ApiError));
      }
      setEntries([]);
      setCursor(null);
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [appliedActor, act, from, to]);

  useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (!cursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    try {
      const page = await fetchAccessLog({ actor: appliedActor, act, from, to, cursor });
      // APPENDED, never replaced: the walk is forward-only.
      setEntries((current) => [...current, ...page.entries]);
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
          { label: t("title") },
        ]}
      />
      <PageHeader title={t("title")} description={t("description")} />

      <Card>
        <CardHeader>
          <CardTitle>{t("filtersHeading")}</CardTitle>
          {/*
            THE FILTERS ARE ACTOR, ACT AND DATE. The description says out loud
            that there is no way to search by subject, so the absence reads as a
            decision rather than as a missing feature somebody should add.
          */}
          <CardDescription>{t("filtersDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant={act === "" ? "default" : "secondary"} onClick={() => setAct("")}>
              {t("actAll")}
            </Button>
            {ACCESS_ACTS.map((candidate) => (
              <Button
                key={candidate}
                type="button"
                variant={candidate === act ? "default" : "secondary"}
                onClick={() => setAct(candidate)}
              >
                {t(ACT_LABEL_KEY[candidate])}
              </Button>
            ))}
          </div>
          <form
            className="grid gap-4 sm:grid-cols-[2fr_1fr_1fr_auto] sm:items-end"
            onSubmit={(event) => {
              event.preventDefault();
              setAppliedActor(actor.trim());
            }}
          >
            <Input
              type="text"
              value={actor}
              onChange={(event) => setActor(event.target.value)}
              placeholder={t("actorPlaceholder")}
              autoComplete="off"
              spellCheck={false}
              aria-label={t("actorLabel")}
            />
            <Input
              type="date"
              value={from}
              onChange={(event) => setFrom(event.target.value)}
              aria-label={t("fromLabel")}
            />
            <Input type="date" value={to} onChange={(event) => setTo(event.target.value)} aria-label={t("toLabel")} />
            <Button type="submit">{t("applyAction")}</Button>
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
          <CardTitle>{t("tableHeading")}</CardTitle>
          {/* No total, following the acceptance browsers and ADR 0067. */}
          <CardDescription>{t("pageDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <p className="text-sm text-muted-foreground">{t("loading")}</p>
          ) : entries.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("empty")}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b text-left text-xs uppercase text-muted-foreground">
                    <th className="py-2 pr-4 font-medium">{t("colWhen")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colAct")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colActor")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colSubject")}</th>
                    <th className="py-2 pr-4 font-medium">{t("colQuestion")}</th>
                  </tr>
                </thead>
                <tbody>
                  {entries.map((row) => (
                    <AccessRow key={row.id} entry={row} locale={locale} />
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

      {/*
        RETENTION IS UNBOUNDED AND THERE IS NO EXPORT, said on the screen rather
        than only in a migration comment: an operator who cannot find the purge
        button should learn that there is none on purpose.
      */}
      <p className="text-xs text-muted-foreground">{t("retentionNote")}</p>
    </div>
  );
}
