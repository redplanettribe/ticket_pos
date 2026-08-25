"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

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
  FormField,
  Input,
  PageHeader,
  toast,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";

import { apiErrorMessage, fieldErrorMessages } from "@/lib/api-errors";
import { ApiError, isoToDateTimeLocal } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatMoney } from "@/lib/format";
import {
  INVOICE_IVA_RATES,
  INVOICE_PAYMENT_METHODS,
  INVOICE_PAYMENT_METHOD_DEFAULT,
  type InvoiceIVARate,
  type IssueInvoiceBody,
  type IssueInvoiceLineBody,
  type OperatorInvoiceTotals,
  fetchOperatorEcuadorIssuer,
  issueOperatorInvoice,
  previewOperatorInvoiceTotals,
} from "@/lib/operator-api";

// The New invoice form (#454). It refuses to issue while the Issuer is
// incomplete or has no certificate — and says why — so no number is wasted.
// The emission date is today in Ecuador, shown read-only. Totals are the
// server's: the form previews them, and never types them.

const SELECT_CLASS =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60";

type LineForm = {
  description: string;
  quantity: string;
  unitPrice: string;
  discount: string;
  ivaRate: InvoiceIVARate;
};

type FieldForm = { name: string; value: string };

const EMPTY_LINE: LineForm = { description: "", quantity: "1", unitPrice: "", discount: "", ivaRate: "15" };

const IVA_RATE_KEYS = {
  "15": "invoicingIvaRate15",
  "0": "invoicingIvaRate0",
  exento: "invoicingIvaRateExento",
  no_objeto: "invoicingIvaRateNoObjeto",
} as const;

// Dollars-and-cents string to integer cents, or null when it is not a number.
function toCents(value: string): number | null {
  const trimmed = value.trim();
  if (trimmed === "") return 0;
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return null;
  return Math.round(Number.parseFloat(trimmed) * 100);
}

function toLineBody(line: LineForm): IssueInvoiceLineBody | null {
  const unit = toCents(line.unitPrice);
  const discount = toCents(line.discount);
  if (unit === null || discount === null) return null;
  if (!/^\d+(\.\d{1,6})?$/.test(line.quantity.trim())) return null;
  return {
    description: line.description.trim(),
    quantity: line.quantity.trim(),
    unit_price_cents: unit,
    discount_cents: discount,
    iva_rate: line.ivaRate,
  };
}

// Today in Ecuador as YYYY-MM-DD, the same day the server fixes on the
// factura: the platform's clock, never the reader's machine, read through the
// same helper the Event forms use for a day in a stated zone.
function ecuadorToday(): string {
  return isoToDateTimeLocal(new Date().toISOString(), PLATFORM_TIME_ZONE).slice(0, 10);
}

export function OperatorNewInvoiceClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());
  const router = useRouter();

  const [ready, setReady] = useState<"loading" | "ready" | "no_issuer" | "no_certificate" | "forbidden" | "error">(
    "loading",
  );
  const [taxIdType, setTaxIdType] = useState<"ruc" | "cedula" | "passport">("ruc");
  const [taxId, setTaxId] = useState("");
  const [legalName, setLegalName] = useState("");
  const [address, setAddress] = useState("");
  const [email, setEmail] = useState("");
  const [lines, setLines] = useState<LineForm[]>([{ ...EMPTY_LINE }]);
  const [paymentMethod, setPaymentMethod] = useState<string>(INVOICE_PAYMENT_METHOD_DEFAULT);
  const [fields, setFields] = useState<FieldForm[]>([]);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [totals, setTotals] = useState<OperatorInvoiceTotals | null>(null);
  const [issuing, setIssuing] = useState(false);
  const emissionDate = useMemo(ecuadorToday, []);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const issuer = await fetchOperatorEcuadorIssuer();
        if (cancelled) return;
        if (!issuer) {
          setReady("no_issuer");
        } else if (!issuer.certificate) {
          setReady("no_certificate");
        } else {
          setReady("ready");
        }
      } catch (error) {
        if (cancelled) return;
        setReady(error instanceof ApiError && error.code === "FORBIDDEN" ? "forbidden" : "error");
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  // The server's totals, previewed as the lines change: what the factura will
  // carry, never the browser's own arithmetic.
  const previewToken = useRef(0);
  const refreshTotals = useCallback(async (currentLines: LineForm[]) => {
    const bodies = currentLines.map(toLineBody);
    if (bodies.some((line) => line === null) || bodies.length === 0) {
      setTotals(null);
      return;
    }
    const token = ++previewToken.current;
    try {
      const preview = await previewOperatorInvoiceTotals(bodies as IssueInvoiceLineBody[]);
      if (token === previewToken.current) setTotals(preview);
    } catch {
      if (token === previewToken.current) setTotals(null);
    }
  }, []);

  useEffect(() => {
    const handle = setTimeout(() => void refreshTotals(lines), 300);
    return () => clearTimeout(handle);
  }, [lines, refreshTotals]);

  function updateLine(index: number, patch: Partial<LineForm>) {
    setLines((current) => current.map((line, i) => (i === index ? { ...line, ...patch } : line)));
  }

  function updateField(index: number, patch: Partial<FieldForm>) {
    setFields((current) => current.map((field, i) => (i === index ? { ...field, ...patch } : field)));
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setFieldErrors({});
    const lineBodies = lines.map(toLineBody);
    const body: IssueInvoiceBody = {
      recipient: {
        tax_id_type: taxIdType,
        tax_id: taxId.trim(),
        legal_name: legalName.trim(),
        address: address.trim(),
        email: email.trim(),
      },
      lines: lineBodies.filter((line): line is IssueInvoiceLineBody => line !== null),
      payment_method: paymentMethod,
      additional_fields: fields
        .map((field) => ({ name: field.name.trim(), value: field.value.trim() }))
        .filter((field) => field.name !== "" || field.value !== ""),
    };
    setIssuing(true);
    try {
      const invoice = await issueOperatorInvoice(body);
      toast.success(t("invoicingListTitle"));
      router.push(`/operator/invoicing/${invoice.id}`);
    } catch (error) {
      if (error instanceof ApiError && error.code === "VALIDATION_FAILED") {
        setFieldErrors(fieldErrorMessages(errorCopy, error.details));
        toast.error(t("invoicingCheckFieldsShort"));
      } else {
        toast.error(
          (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ?? t("invoicingIssueFailed"),
        );
      }
      setIssuing(false);
    }
  }

  if (ready === "loading") {
    return <p className="text-sm text-muted-foreground">{t("invoicingFormLoading")}</p>;
  }
  if (ready === "forbidden") {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }
  if (ready === "error") {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("invoicingIssueFailedTitle")}</AlertTitle>
        <AlertDescription>{t("invoicingIssueFailed")}</AlertDescription>
      </Alert>
    );
  }
  if (ready === "no_issuer" || ready === "no_certificate") {
    return (
      <div className="space-y-6">
        <Breadcrumb
          items={[
            { label: t("breadcrumbOperator"), href: "/operator" },
            { label: t("invoicingBreadcrumbList"), href: "/operator/invoicing" },
            { label: t("invoicingNewTitle") },
          ]}
        />
        <Alert variant="destructive">
          <AlertTitle>{t("invoicingNotReadyTitle")}</AlertTitle>
          <AlertDescription>
            {ready === "no_issuer" ? t("invoicingNotReadyNoIssuer") : t("invoicingNotReadyNoCertificate")}
          </AlertDescription>
        </Alert>
        <Button asChild>
          <Link href="/operator/invoicing/issuer">{t("invoicingGoToIssuer")}</Link>
        </Button>
      </div>
    );
  }

  const money = (cents: number) => formatMoney(cents, "USD", locale);

  return (
    <form className="space-y-6" onSubmit={handleSubmit}>
      <Breadcrumb
        items={[
          { label: t("breadcrumbOperator"), href: "/operator" },
          { label: t("invoicingBreadcrumbList"), href: "/operator/invoicing" },
          { label: t("invoicingNewTitle") },
        ]}
      />
      <PageHeader title={t("invoicingNewTitle")} description={t("invoicingNewDescription")} />

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingRecipientTitle")}</CardTitle>
          <CardDescription>{t("invoicingRecipientDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <FormField id="tax_id_type" label={t("invoicingRecipientTaxIdType")}>
            <select
              className={SELECT_CLASS}
              value={taxIdType}
              onChange={(event) => setTaxIdType(event.target.value as typeof taxIdType)}
            >
              <option value="ruc">{t("invoicingRecipientTaxIdTypeRuc")}</option>
              <option value="cedula">{t("invoicingRecipientTaxIdTypeCedula")}</option>
              <option value="passport">{t("invoicingRecipientTaxIdTypePassport")}</option>
            </select>
          </FormField>
          <FormField id="tax_id" label={t("invoicingRecipientTaxId")} error={fieldErrors["recipient.tax_id"]}>
            <Input value={taxId} onChange={(event) => setTaxId(event.target.value)} />
          </FormField>
          <FormField
            id="legal_name"
            label={t("invoicingRecipientLegalName")}
            error={fieldErrors["recipient.legal_name"]}
            className="sm:col-span-2"
          >
            <Input value={legalName} onChange={(event) => setLegalName(event.target.value)} />
          </FormField>
          <FormField
            id="address"
            label={t("invoicingRecipientAddress")}
            error={fieldErrors["recipient.address"]}
            className="sm:col-span-2"
          >
            <Input value={address} onChange={(event) => setAddress(event.target.value)} />
          </FormField>
          <FormField
            id="email"
            label={t("invoicingRecipientEmail")}
            description={t("invoicingRecipientEmailHint")}
            error={fieldErrors["recipient.email"]}
            className="sm:col-span-2"
          >
            <Input type="email" value={email} onChange={(event) => setEmail(event.target.value)} />
          </FormField>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>{t("invoicingLinesTitle")}</CardTitle>
          <Button type="button" variant="outline" size="sm" onClick={() => setLines((c) => [...c, { ...EMPTY_LINE }])}>
            {t("invoicingAddLine")}
          </Button>
        </CardHeader>
        <CardContent className="space-y-4">
          {lines.map((line, index) => (
            <div key={index} className="grid gap-3 rounded-md border p-3 sm:grid-cols-2">
              <FormField
                id={`line-${index}-description`}
                label={t("invoicingLineDescription")}
                error={fieldErrors[`lines[${index}].description`]}
                className="sm:col-span-2"
              >
                <Input value={line.description} onChange={(event) => updateLine(index, { description: event.target.value })} />
              </FormField>
              <FormField
                id={`line-${index}-quantity`}
                label={t("invoicingLineQuantity")}
                error={fieldErrors[`lines[${index}].quantity`]}
              >
                <Input value={line.quantity} onChange={(event) => updateLine(index, { quantity: event.target.value })} />
              </FormField>
              <FormField
                id={`line-${index}-unit`}
                label={t("invoicingLineUnitPrice")}
                error={fieldErrors[`lines[${index}].unit_price_cents`]}
              >
                <Input
                  inputMode="decimal"
                  value={line.unitPrice}
                  onChange={(event) => updateLine(index, { unitPrice: event.target.value })}
                />
              </FormField>
              <FormField id={`line-${index}-discount`} label={t("invoicingLineDiscount")}>
                <Input
                  inputMode="decimal"
                  value={line.discount}
                  onChange={(event) => updateLine(index, { discount: event.target.value })}
                />
              </FormField>
              <FormField id={`line-${index}-iva`} label={t("invoicingLineIvaRate")}>
                <select
                  className={SELECT_CLASS}
                  value={line.ivaRate}
                  onChange={(event) => updateLine(index, { ivaRate: event.target.value as InvoiceIVARate })}
                >
                  {INVOICE_IVA_RATES.map((rate) => (
                    <option key={rate} value={rate}>
                      {t(IVA_RATE_KEYS[rate])}
                    </option>
                  ))}
                </select>
              </FormField>
              {lines.length > 1 ? (
                <div className="sm:col-span-2">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setLines((c) => c.filter((_, i) => i !== index))}
                  >
                    {t("invoicingRemoveLine")}
                  </Button>
                </div>
              ) : null}
            </div>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingPaymentTitle")}</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <FormField id="payment_method" label={t("invoicingPaymentMethod")}>
            <select
              className={SELECT_CLASS}
              value={paymentMethod}
              onChange={(event) => setPaymentMethod(event.target.value)}
            >
              {INVOICE_PAYMENT_METHODS.map((method) => (
                <option key={method.code} value={method.code}>
                  {method.code} — {method.label}
                </option>
              ))}
            </select>
          </FormField>
          <FormField id="emission_date" label={t("invoicingEmissionDate")} description={t("invoicingEmissionDateHint")}>
            <Input value={emissionDate} readOnly disabled />
          </FormField>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>{t("invoicingAdditionalTitle")}</CardTitle>
            <CardDescription>{t("invoicingAdditionalDescription")}</CardDescription>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={fields.length >= 14}
            onClick={() => setFields((c) => [...c, { name: "", value: "" }])}
          >
            {t("invoicingAddField")}
          </Button>
        </CardHeader>
        <CardContent className="space-y-3">
          {fields.map((field, index) => (
            <div key={index} className="grid gap-3 sm:grid-cols-2">
              <FormField id={`field-${index}-name`} label={t("invoicingAdditionalName")}>
                <Input value={field.name} onChange={(event) => updateField(index, { name: event.target.value })} />
              </FormField>
              <FormField id={`field-${index}-value`} label={t("invoicingAdditionalValue")}>
                <div className="flex gap-2">
                  <Input value={field.value} onChange={(event) => updateField(index, { value: event.target.value })} />
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setFields((c) => c.filter((_, i) => i !== index))}
                  >
                    {t("invoicingRemoveField")}
                  </Button>
                </div>
              </FormField>
            </div>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("invoicingTotalsTitle")}</CardTitle>
          <CardDescription>{t("invoicingTotalsComputed")}</CardDescription>
        </CardHeader>
        <CardContent>
          {totals ? (
            <dl className="space-y-1 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("invoicingTotalSubtotal")}</dt>
                <dd className="tabular-nums">{money(totals.subtotal_cents)}</dd>
              </div>
              {totals.discount_cents > 0 ? (
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">{t("invoicingTotalDiscount")}</dt>
                  <dd className="tabular-nums">{money(totals.discount_cents)}</dd>
                </div>
              ) : null}
              <div className="flex justify-between">
                <dt className="text-muted-foreground">{t("invoicingTotalIva")}</dt>
                <dd className="tabular-nums">{money(totals.iva_cents)}</dd>
              </div>
              <div className="flex justify-between font-medium">
                <dt>{t("invoicingTotalTotal")}</dt>
                <dd className="tabular-nums">{money(totals.total_cents)}</dd>
              </div>
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">{t("invoicingTotalsUnavailable")}</p>
          )}
        </CardContent>
      </Card>

      {fieldErrors["lines"] ? (
        <Alert variant="destructive">
          <AlertDescription>{fieldErrors["lines"]}</AlertDescription>
        </Alert>
      ) : null}
      {fieldErrors["additional_fields"] ? (
        <Alert variant="destructive">
          <AlertDescription>{fieldErrors["additional_fields"]}</AlertDescription>
        </Alert>
      ) : null}

      <div className="flex justify-end">
        <Button type="submit" disabled={issuing}>
          {issuing ? t("invoicingIssuing") : t("invoicingIssue")}
        </Button>
      </div>
    </form>
  );
}
