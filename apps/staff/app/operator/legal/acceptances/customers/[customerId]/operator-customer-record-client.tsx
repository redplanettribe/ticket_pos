"use client";

import { useCallback, useEffect, useState } from "react";

import Link from "next/link";

import { toAppLocale } from "@ticket-pos/locale";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  FormField,
  Input,
  PageHeader,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage } from "@/lib/api-errors";
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import {
  customerEvidencePackPath,
  type ConsentAct,
  type LegalCustomerRecord,
  type LegalEditionRef,
  type OptionalConsentValue,
  CONSENT_ACT_PAGE_SIZE,
  adulthoodLabelKey,
  answerLabelKey,
  presentedLocaleState,
  staffRecordHref,
  wouldTakeSomethingAway,
} from "@/lib/legal-records";
import {
  fetchConsentActs,
  fetchCustomerLegalRecord,
  recordConsentWithdrawal,
} from "@/lib/legal-records-api";

import { EvidencePackCard } from "../../evidence-pack-card";
import { StandingBadge } from "../../standing-badge";

/**
 * ONE PERSON'S CONSENT RECORD (#566, spec #556, ADR 0067).
 *
 * THE RECORD IS THE EVIDENCE RATHER THAN A SUMMARY OF IT. Every act is listed
 * with what it answered, what it replaced, which surface it happened on, in
 * which language the notice was rendered, and against which exact edition — so
 * an operator answering a data subject's request is reading the log rather than
 * somebody's précis of it.
 *
 * NULL IS SPELLED IN WORDS, everywhere, and this is the rule the screen exists
 * to keep. A blank cell beside "Marketing consent" reads as a refusal, and
 * mistaking "this box was not shown on that surface" for "No" is exactly how an
 * operator comes to withdraw something nobody ever granted. So: "not shown",
 * "not recorded", "never asked" — never an empty cell and never a dash.
 *
 * EXACTLY TWO ACTS ARE OFFERED, and the absences are as ruled as the presences:
 *
 *   - WITHDRAW AN OPTIONAL CONSENT. The old /operator/consent page folded in
 *     here; this is where it lives now.
 *   - GENERATE A CONSENT EVIDENCE PACK (#568). One deterministic ZIP, built on
 *     demand and never stored, spanning BOTH populations for this address — so
 *     the file this page produces and the one the staff record produces are the
 *     same bytes. There is no self-service equivalent anywhere.
 *
 * And deliberately NOT: no control that manufactures an acceptance, so no
 * consent can exist that the person did not give; no per-person re-gate, so
 * "publish an edition for an audience of one" is not a thing anybody can do; no
 * erasure, because a deletion request escalates to counsel rather than being
 * automated behind a button; and NO WITHDRAW TERMS and no withdraw of a Policy
 * Acceptance — a contract's basis is performance rather than consent, and
 * clearing an acceptance would re-gate the person rather than free them. None
 * of those absences is enforced by this file: the API has no route for any of
 * them, which is what makes the guarantee the platform's rather than a screen's.
 *
 * It reads in the operator's Staff Locale like every other staff surface (ADR
 * 0041). The Customer's own confirmation email is written in THEIR language by
 * the API, which is where that decision belongs.
 */

/** The longest artefact reference the API accepts. */
const REQUEST_REFERENCE_MAX_LENGTH = 500;

/** The `operator.legalRecords` key each stored consent state reads under. */
const CONSENT_STATE_KEYS = {
  granted: "consentGranted",
  denied: "consentDenied",
  pending_confirmation: "consentPendingConfirmation",
} as const satisfies Record<OptionalConsentValue, string>;

/** A withdrawal the operator has stated and is being asked to confirm. */
type PendingWithdrawal = {
  marketing: boolean;
  networking: boolean;
  reference: string;
};

/** One label/value pair. The value is always a word, never an empty cell. */
function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-sm text-muted-foreground">{label}</p>
      <div className="font-medium">{children}</div>
    </div>
  );
}

export function OperatorCustomerRecordClient({ customerId }: { customerId: string }) {
  const t = useTranslations("operator.legalRecords");
  const tAcceptances = useTranslations("operator.legalAcceptances");
  const tOperator = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const [record, setRecord] = useState<LegalCustomerRecord | null>(null);
  const [acts, setActs] = useState<ConsentAct[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The withdrawal's form. Both boxes start unticked and are NEVER pre-filled
  // from the stored state: what the platform holds decides what a withdrawal
  // WOULD change, never what to present as already asked for.
  const [marketing, setMarketing] = useState(false);
  const [networking, setNetworking] = useState(false);
  const [reference, setReference] = useState("");
  const [referenceError, setReferenceError] = useState<string | null>(null);
  const [consentError, setConsentError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [pending, setPending] = useState<PendingWithdrawal | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNotFound(false);
    try {
      // TWO REQUESTS, because the history is its own endpoint — so page 1 and
      // page 2 have the same shape, and "load more" is a request that can be
      // replayed on its own.
      const [subject, page] = await Promise.all([
        fetchCustomerLegalRecord(customerId),
        fetchConsentActs(customerId),
      ]);
      setRecord(subject);
      setActs(page.acts);
      setCursor(page.next_cursor);
    } catch (caught) {
      if (caught instanceof ApiError && caught.code === "LEGAL_SUBJECT_NOT_FOUND") {
        // A stale link. Told plainly rather than shown as a blank record the
        // operator might then act on.
        setNotFound(true);
      } else {
        setError(apiErrorMessage(errorCopy, caught as ApiError));
      }
      setRecord(null);
      setActs([]);
      setCursor(null);
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [customerId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (!cursor || loadingMore) {
      return;
    }
    setLoadingMore(true);
    try {
      const page = await fetchConsentActs(customerId, cursor);
      // APPENDED, never replaced: the walk is forward-only, and an operator
      // reading a record must not lose the acts they have already read.
      setActs((current) => [...current, ...page.acts]);
      setCursor(page.next_cursor);
    } catch (caught) {
      setError(apiErrorMessage(errorCopy, caught as ApiError));
    } finally {
      setLoadingMore(false);
    }
  }

  /** A stored consent state as a word, with "never asked" for the absent one. */
  const consentLabel = (value: OptionalConsentValue | null) =>
    value ? t(CONSENT_STATE_KEYS[value]) : t("consentNeverAsked");

  /** An edition as a sentence: its label, or "not recorded" where it has none. */
  const editionLabel = (edition: LegalEditionRef | null) =>
    edition ? (edition.label || t("notRecorded")) : t("notRecorded");

  function handleWithdrawSubmit(event: React.FormEvent) {
    event.preventDefault();
    const trimmedReference = reference.trim();
    const nothingNamed = !marketing && !networking;
    setConsentError(nothingNamed ? t("consentNothingNamed") : null);
    setReferenceError(
      !trimmedReference
        ? t("consentArtefactRequired")
        : trimmedReference.length > REQUEST_REFERENCE_MAX_LENGTH
          ? t("consentArtefactTooLong", { max: REQUEST_REFERENCE_MAX_LENGTH })
          : null,
    );
    if (nothingNamed || !trimmedReference || trimmedReference.length > REQUEST_REFERENCE_MAX_LENGTH) {
      return;
    }
    setPending({ marketing, networking, reference: trimmedReference });
  }

  async function submitWithdrawal(withdrawal: PendingWithdrawal) {
    setSubmitting(true);
    try {
      // Each consent is OMITTED where the form did not ask for it. Sending
      // `false` for a box nobody mentioned would record an answer to a question
      // that was never put, and would turn a marketing withdrawal into a
      // withdrawal of everything.
      const result = await recordConsentWithdrawal(customerId, {
        ...(withdrawal.marketing ? { marketing_consent: false as const } : {}),
        ...(withdrawal.networking ? { networking_consent: false as const } : {}),
        request_reference: withdrawal.reference,
      });
      setPending(null);
      setMarketing(false);
      setNetworking(false);
      setReference("");
      // What the act TOOK AWAY, in the platform's own words rather than the
      // operator's: a form asking to withdraw something already withdrawn is
      // recorded faithfully and moves nothing, and saying "done" would be
      // telling them a change happened when none did.
      const took = result.withdrew?.marketing_consent || result.withdrew?.networking_consent;
      toast.success(took ? t("consentWithdrawalRecorded") : t("consentWithdrawalNoChange"));
      // Reloaded rather than patched in place: the act just wrote a new row to
      // the history, and a record that showed the new state without the act
      // that produced it would be a summary again.
      await load();
    } catch (submitError) {
      toast.error(
        (submitError instanceof ApiError ? apiErrorMessage(errorCopy, submitError) : null) ??
          t("consentWithdrawalFailed"),
      );
    } finally {
      setSubmitting(false);
    }
  }

  const breadcrumb = (
    <Breadcrumb
      items={[
        { label: tOperator("breadcrumbOperator"), href: "/operator" },
        { label: tAcceptances("breadcrumbLegalCenter"), href: "/operator/legal" },
        {
          label: tAcceptances("customersTitle"),
          href: "/operator/legal/acceptances/customers",
        },
        { label: t("customerTitle") },
      ]}
    />
  );

  if (notFound) {
    return (
      <div className="space-y-6">
        {breadcrumb}
        <PageHeader title={t("customerTitle")} description={t("customerDescription")} />
        <Card>
          <CardContent>
            <p className="text-sm text-muted-foreground">{t("subjectNotFound")}</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {breadcrumb}
      <PageHeader title={t("customerTitle")} description={t("customerDescription")} />

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("loadFailedTitle")}</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {loading ? <p className="text-sm text-muted-foreground">{t("loading")}</p> : null}

      {record ? (
        <>
          <Card>
            <CardHeader>
              {/* A Customer's name and address are data, never copy. */}
              <CardTitle>
                {`${record.customer.first_name} ${record.customer.last_name}`.trim()}
              </CardTitle>
              <CardDescription>{record.customer.email}</CardDescription>
            </CardHeader>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              {/*
                EACH GATE NAMES THE EXACT EDITION, so the standing can be traced
                back to the bytes the person was actually shown.
              */}
              <Fact label={t("policyStanding")}>
                <span className="flex flex-wrap items-center gap-2">
                  <StandingBadge standing={record.customer.policy.standing} />
                  <span className="text-sm font-normal text-muted-foreground">
                    {t("editionNamed", { edition: editionLabel(record.customer.policy.edition) })}
                  </span>
                </span>
              </Fact>
              <Fact label={t("termsStanding")}>
                <span className="flex flex-wrap items-center gap-2">
                  <StandingBadge standing={record.customer.terms.standing} />
                  <span className="text-sm font-normal text-muted-foreground">
                    {t("editionNamed", { edition: editionLabel(record.customer.terms.edition) })}
                  </span>
                </span>
              </Fact>
              <Fact label={t("policyAcceptedAt")}>
                {formatDateTime(record.customer.policy.accepted_at, PLATFORM_TIME_ZONE, locale) ??
                  t("never")}
              </Fact>
              <Fact label={t("termsAcceptedAt")}>
                {formatDateTime(record.customer.terms.accepted_at, PLATFORM_TIME_ZONE, locale) ??
                  t("never")}
              </Fact>
              <Fact label={t("consentMarketing")}>
                {consentLabel(record.customer.marketing_consent)}
              </Fact>
              <Fact label={t("consentNetworking")}>
                {consentLabel(record.customer.networking_consent)}
              </Fact>
            </CardContent>
          </Card>

          {/*
            THE CROSS-LINK, offered only where there is somebody to link to, and
            resolved SERVER-SIDE from an address that never left the server. Two
            records, never merged: the platform links them and stops short of
            asserting an identity no row anywhere establishes.
          */}
          {record.staff_digest ? (
            <Card>
              <CardHeader>
                <CardTitle>{t("crossLinkTitle")}</CardTitle>
                <CardDescription>{t("crossLinkToStaff")}</CardDescription>
              </CardHeader>
              <CardContent>
                <Link
                  className="text-sm underline underline-offset-4"
                  href={staffRecordHref(record.staff_digest)}
                >
                  {t("crossLinkToStaffAction")}
                </Link>
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader>
              <CardTitle>{t("historyTitle")}</CardTitle>
              {/*
                THE VISIBLE COUNT, so a truncated page is distinguishable from a
                complete history. Read with the "show more" button below: a
                count and no button is the whole record; a count and a button is
                what has been read so far.
              */}
              <CardDescription>
                {cursor
                  ? t("historyTruncated", { count: acts.length, pageSize: CONSENT_ACT_PAGE_SIZE })
                  : t("historyComplete", { count: acts.length })}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {acts.length === 0 ? (
                <p className="text-sm text-muted-foreground">{t("historyEmpty")}</p>
              ) : (
                <ul className="space-y-4">
                  {acts.map((act) => (
                    <ActRow key={act.id} act={act} editionLabel={editionLabel} />
                  ))}
                </ul>
              )}
              {cursor ? (
                <div className="mt-4">
                  <Button
                    variant="secondary"
                    onClick={loadMore}
                    disabled={loadingMore}
                    aria-busy={loadingMore}
                  >
                    {loadingMore ? t("loadingMore") : t("loadMore")}
                  </Button>
                </div>
              ) : null}
            </CardContent>
          </Card>

          <EvidencePackCard path={customerEvidencePackPath(customerId)} />

          <Card>
            <CardHeader>
              <CardTitle>{t("consentWithdrawTitle")}</CardTitle>
              <CardDescription>{t("consentWithdrawDescription")}</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="space-y-4" onSubmit={handleWithdrawSubmit}>
                <FormField
                  id="legal-record-consent-boxes"
                  label={t("consentWhatWithdraws")}
                  error={consentError ?? undefined}
                >
                  {/*
                    TWO BOXES AND ONLY TWO. There is no Terms box and no Policy
                    Acceptance box, and their absence is not an oversight: a
                    contract's basis is performance rather than consent, and
                    clearing an acceptance would re-gate the person rather than
                    free them. The API has no route for either.
                  */}
                  <div className="space-y-2">
                    <label className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={marketing}
                        onChange={(event) => setMarketing(event.target.checked)}
                        disabled={submitting}
                      />
                      {t("consentMarketing")}
                      {wouldTakeSomethingAway(record.customer.marketing_consent) ? null : (
                        <span className="text-muted-foreground">
                          {t("consentNothingToTakeAway", {
                            state: consentLabel(
                              record.customer.marketing_consent,
                            ).toLocaleLowerCase(locale),
                          })}
                        </span>
                      )}
                    </label>
                    <label className="flex items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        checked={networking}
                        onChange={(event) => setNetworking(event.target.checked)}
                        disabled={submitting}
                      />
                      {t("consentNetworking")}
                      {wouldTakeSomethingAway(record.customer.networking_consent) ? null : (
                        <span className="text-muted-foreground">
                          {t("consentNothingToTakeAway", {
                            state: consentLabel(
                              record.customer.networking_consent,
                            ).toLocaleLowerCase(locale),
                          })}
                        </span>
                      )}
                    </label>
                  </div>
                </FormField>

                <FormField
                  id="legal-record-consent-reference"
                  label={t("consentArtefactLabel")}
                  error={referenceError ?? undefined}
                >
                  <Input
                    value={reference}
                    onChange={(event) => setReference(event.target.value)}
                    placeholder={t("consentArtefactPlaceholder")}
                    autoComplete="off"
                    disabled={submitting}
                  />
                  <p className="mt-1 text-sm text-muted-foreground">{t("consentArtefactHint")}</p>
                </FormField>

                <Button type="submit" disabled={submitting}>
                  {t("consentRecordWithdrawal")}
                </Button>
              </form>
            </CardContent>
          </Card>
        </>
      ) : null}

      <Dialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPending(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("consentConfirmTitle")}</DialogTitle>
            <DialogDescription>
              {t("consentConfirmBody", { email: record?.customer.email ?? "" })}
            </DialogDescription>
          </DialogHeader>
          <ul className="list-disc space-y-1 pl-5 text-sm">
            {pending?.marketing ? <li>{t("consentConfirmMarketing")}</li> : null}
            {pending?.networking ? <li>{t("consentConfirmNetworking")}</li> : null}
            <li>{t("consentConfirmContinues")}</li>
            <li>{t("consentConfirmEmail")}</li>
          </ul>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setPending(null)}
              disabled={submitting}
            >
              {tOperator("cancel")}
            </Button>
            <Button
              type="button"
              onClick={() => {
                if (pending !== null) {
                  void submitWithdrawal(pending);
                }
              }}
              disabled={submitting}
            >
              {submitting ? tOperator("recording") : t("consentRecordWithdrawal")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/**
 * One act in the history.
 *
 * EVERY FIELD IS A SENTENCE, and a NULL is a word rather than a gap: "not
 * shown" for a box that was not on that surface, "not recorded" for something
 * the surface did not collect. The one field that can be ABSENT ENTIRELY is the
 * presented language — on a channel that showed no document there is no
 * question to answer, and a row that said "not recorded" there would claim text
 * was displayed and its language forgotten.
 */
function ActRow({
  act,
  editionLabel,
}: {
  act: ConsentAct;
  editionLabel: (edition: LegalEditionRef | null) => string;
}) {
  const t = useTranslations("operator.legalRecords");
  const locale = toAppLocale(useLocale());
  const localeState = presentedLocaleState(act);

  return (
    <li className="rounded-md border p-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="font-medium">{t(`channel_${act.channel}` as never)}</p>
        <p className="text-sm text-muted-foreground">
          {formatDateTime(act.captured_at, PLATFORM_TIME_ZONE, locale) ?? t("notRecorded")}
        </p>
      </div>
      <dl className="mt-3 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
        <Detail label={t("actPolicyEdition")}>{editionLabel(act.policy_edition)}</Detail>
        <Detail label={t("actPolicyAcceptance")}>{t(answerLabelKey(act.policy_acceptance) as never)}</Detail>
        <Detail label={t("actMarketing")}>
          {t(answerLabelKey(act.marketing_consent) as never)}
          {act.prior_marketing_consent
            ? ` ${t("actPreviously", { state: act.prior_marketing_consent })}`
            : ""}
        </Detail>
        <Detail label={t("actNetworking")}>
          {t(answerLabelKey(act.networking_consent) as never)}
          {act.prior_networking_consent
            ? ` ${t("actPreviously", { state: act.prior_networking_consent })}`
            : ""}
        </Detail>
        {act.terms_acceptance !== null || act.terms_edition ? (
          <Detail label={t("actTermsAcceptance")}>
            {`${t(answerLabelKey(act.terms_acceptance) as never)} — ${editionLabel(act.terms_edition)}`}
          </Detail>
        ) : null}
        {/*
          THE ADULTHOOD DECLARATION IS SHOWN ON EVERY ACT (#590, ADR 0069),
          unlike the Terms row above it, and the difference is deliberate. An
          act captured under an edition that carried no 18+ artifact reads
          "never asked" — the screen's existing words for a question nobody was
          put — because a row that simply vanished would leave the operator to
          decide what its absence meant, and the one wrong reading is "No". No
          cell is blank here and no null is ever a refusal: a refusal wrote
          nothing at all, so there is no such record to show.
        */}
        <Detail label={t("actAdulthoodDeclaration")}>
          {t(adulthoodLabelKey(act.adulthood_declaration) as never)}
        </Detail>
        <Detail label={t("actEmailProven")}>
          {act.email_proven ? t("answerYes") : t("answerNo")}
        </Detail>
        {/*
          ABSENT MEANS ABSENT: the field is not rendered at all on a channel
          that presented no document.
        */}
        {localeState === "absent" ? null : (
          <Detail label={t("actPresentedLocale")}>
            {localeState === "not-recorded" ? t("notRecorded") : act.presented_locale}
          </Detail>
        )}
        <Detail label={t("actEmail")}>{act.email}</Detail>
        <Detail label={t("actIP")}>{act.ip ?? t("notRecorded")}</Detail>
        {act.recorded_by ? (
          <Detail label={t("actRecordedBy")}>{act.recorded_by}</Detail>
        ) : null}
        {act.request_reference ? (
          <Detail label={t("actRequestReference")}>{act.request_reference}</Detail>
        ) : null}
      </dl>
    </li>
  );
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="break-words">{children}</dd>
    </div>
  );
}
