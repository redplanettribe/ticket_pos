"use client";

import { useEffect, useState } from "react";

import { toAppLocale } from "@ticket-pos/locale";
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
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
import { ApiError } from "@/lib/events-api";
import { PLATFORM_TIME_ZONE, formatDateTime } from "@/lib/format";
import {
  ECUADOR_ISSUER_ENVIRONMENTS,
  ECUADOR_ISSUER_REGIMENES,
  type EcuadorIssuerBody,
  type EcuadorIssuerEnvironment,
  type EcuadorIssuerRegimen,
  type OperatorEcuadorIssuer,
  fetchOperatorEcuadorIssuer,
  saveOperatorEcuadorIssuer,
} from "@/lib/operator-api";

/**
 * The Ecuador Issuer page (#451, parent #450, ADR 0059): where a Platform
 * Operator records the platform's own registration with the SRI.
 *
 * ONE FORM FOR ONE ROW. There is at most one Issuer per country and the
 * platform is the only Issuer, so this page is not a list and never will be:
 * it loads the row, or the absence of one, and saves it back. The first save
 * creates it, every save after replaces it, and the API answers with what it
 * stored — which is what the form shows, so an operator reads back the
 * platform's version of what they typed and never their own.
 *
 * THE FIELD NAMES ARE THE SRI'S. An operator fills this in with the RUC
 * certificate in the other hand, and that document is in Spanish; the English
 * catalog keeps the SRI's term in brackets beside a translation, the Spanish
 * catalog uses the term alone. The régimen and environment options are the
 * API's tokens with the catalog holding the word for each.
 *
 * NOTHING HERE IS READ-ONLY YET. The freezes — RUC once any factura exists,
 * establecimiento and punto de emisión once a sequence has started — are #453's,
 * and the certificate card is #452's; both land on this page.
 */

/** The régimen catalog key for each API token. */
const REGIMEN_KEYS = {
  general: "invoicingRegimenGeneral",
  rimpe_contribuyente: "invoicingRegimenRimpeContribuyente",
  rimpe_negocio_popular: "invoicingRegimenRimpeNegocioPopular",
} as const satisfies Record<EcuadorIssuerRegimen, string>;

/** The environment catalog key for each API token, as a select option. */
const ENVIRONMENT_KEYS = {
  test: "invoicingEnvironmentTest",
  production: "invoicingEnvironmentProduction",
} as const satisfies Record<EcuadorIssuerEnvironment, string>;

/** The environment catalog key for each API token, as the badge on the page. */
const ENVIRONMENT_BADGE_KEYS = {
  test: "invoicingEnvironmentBadgeTest",
  production: "invoicingEnvironmentBadgeProduction",
} as const satisfies Record<EcuadorIssuerEnvironment, string>;

/** The form's fields: the body with the optional resolution held as text. */
type IssuerFormValues = Omit<EcuadorIssuerBody, "agente_retencion"> & {
  agente_retencion: string;
};

/**
 * The empty form. The two enums start on the value an operator setting up for
 * the first time wants: the test environment, because certification comes
 * before production, and the general régimen, because it is the commonest.
 */
const EMPTY_FORM: IssuerFormValues = {
  environment: "test",
  ruc: "",
  razon_social: "",
  nombre_comercial: "",
  direccion_matriz: "",
  direccion_establecimiento: "",
  establecimiento: "001",
  punto_emision: "001",
  obligado_contabilidad: false,
  regimen: "general",
  agente_retencion: "",
};

function toFormValues(issuer: OperatorEcuadorIssuer): IssuerFormValues {
  return {
    environment: issuer.environment,
    ruc: issuer.ruc,
    razon_social: issuer.razon_social,
    nombre_comercial: issuer.nombre_comercial,
    direccion_matriz: issuer.direccion_matriz,
    direccion_establecimiento: issuer.direccion_establecimiento,
    establecimiento: issuer.establecimiento,
    punto_emision: issuer.punto_emision,
    obligado_contabilidad: issuer.obligado_contabilidad,
    regimen: issuer.regimen,
    agente_retencion: issuer.agente_retencion ?? "",
  };
}

function toBody(values: IssuerFormValues): EcuadorIssuerBody {
  const agenteRetencion = values.agente_retencion.trim();
  return {
    ...values,
    // Blank is "not a withholding agent", and the API spells that null.
    agente_retencion: agenteRetencion === "" ? null : agenteRetencion,
  };
}

const SELECT_CLASS_NAME =
  "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60";

export function OperatorIssuerClient() {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const [issuer, setIssuer] = useState<OperatorEcuadorIssuer | null>(null);
  const [values, setValues] = useState<IssuerFormValues>(EMPTY_FORM);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const found = await fetchOperatorEcuadorIssuer();
        if (cancelled) return;
        setIssuer(found);
        setValues(found ? toFormValues(found) : EMPTY_FORM);
      } catch (error) {
        if (cancelled) return;
        if (error instanceof ApiError && error.code === "FORBIDDEN") {
          setForbidden(true);
        } else {
          setLoadError(
            (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ??
              t("invoicingIssuerLoadFailed"),
          );
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [errorCopy, t]);

  function update<K extends keyof IssuerFormValues>(field: K, value: IssuerFormValues[K]) {
    setValues((current) => ({ ...current, [field]: value }));
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setFieldErrors({});
    try {
      const saved = await saveOperatorEcuadorIssuer(toBody(values));
      setIssuer(saved);
      setValues(toFormValues(saved));
      toast.success(t("invoicingSaved"));
    } catch (error) {
      // Each field's refusal is shown beside the input it is about, resolved
      // from the API's CODE against the catalog with the API's English as the
      // floor (ADR 0023); anything else is one toast.
      if (error instanceof ApiError && error.code === "VALIDATION_FAILED") {
        setFieldErrors(fieldErrorMessages(errorCopy, error.details));
        toast.error(t("invoicingCheckFields"));
      } else {
        toast.error(
          (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ??
            t("invoicingSaveFailed"),
        );
      }
    } finally {
      setSaving(false);
    }
  }

  if (loading) {
    return <p className="text-sm text-muted-foreground">{t("invoicingIssuerLoading")}</p>;
  }

  if (forbidden) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("accessDeniedTitle")}</AlertTitle>
        <AlertDescription>{t("accessDenied")}</AlertDescription>
      </Alert>
    );
  }

  if (loadError) {
    return (
      <Alert variant="destructive">
        <AlertTitle>{t("invoicingIssuerLoadFailedTitle")}</AlertTitle>
        <AlertDescription>{loadError}</AlertDescription>
      </Alert>
    );
  }

  // The badge reads the STORED environment, never the select: an operator who
  // has picked production and not yet saved is still issuing under test.
  const lastSaved = issuer ? formatDateTime(issuer.updated_at, PLATFORM_TIME_ZONE, locale) : null;

  return (
    <div className="space-y-6">
      <PageHeader title={t("invoicingIssuerTitle")} description={t("invoicingIssuerDescription")} />

      {issuer ? (
        <div className="flex flex-wrap items-center gap-3">
          <Badge variant={issuer.environment === "production" ? "default" : "secondary"}>
            {t(ENVIRONMENT_BADGE_KEYS[issuer.environment])}
          </Badge>
          {lastSaved ? (
            <p className="text-sm text-muted-foreground">
              {t("invoicingIssuerLastSaved", { when: lastSaved })}
            </p>
          ) : null}
        </div>
      ) : (
        <Card>
          <CardContent>
            <p className="text-sm text-muted-foreground">{t("invoicingIssuerNoneYet")}</p>
          </CardContent>
        </Card>
      )}

      <form className="space-y-6" onSubmit={handleSubmit}>
        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingEnvironmentTitle")}</CardTitle>
            <CardDescription>{t("invoicingEnvironmentDescription")}</CardDescription>
          </CardHeader>
          <CardContent>
            <FormField
              id="issuer-environment"
              label={t("invoicingEnvironmentLabel")}
              error={fieldErrors.environment}
            >
              <select
                className={SELECT_CLASS_NAME}
                value={values.environment}
                disabled={saving}
                onChange={(event) =>
                  update("environment", event.target.value as EcuadorIssuerEnvironment)
                }
              >
                {ECUADOR_ISSUER_ENVIRONMENTS.map((environment) => (
                  <option key={environment} value={environment}>
                    {t(ENVIRONMENT_KEYS[environment])}
                  </option>
                ))}
              </select>
            </FormField>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t("invoicingDetailsTitle")}</CardTitle>
            <CardDescription>{t("invoicingDetailsDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <FormField
              id="issuer-ruc"
              label={t("invoicingRucLabel")}
              description={t("invoicingRucHint")}
              error={fieldErrors.ruc}
            >
              <Input
                value={values.ruc}
                disabled={saving}
                inputMode="numeric"
                autoComplete="off"
                maxLength={13}
                onChange={(event) => update("ruc", event.target.value)}
              />
            </FormField>

            <FormField
              id="issuer-razon-social"
              label={t("invoicingRazonSocialLabel")}
              error={fieldErrors.razon_social}
            >
              <Input
                value={values.razon_social}
                disabled={saving}
                autoComplete="organization"
                onChange={(event) => update("razon_social", event.target.value)}
              />
            </FormField>

            <FormField
              id="issuer-nombre-comercial"
              label={t("invoicingNombreComercialLabel")}
              description={t("invoicingNombreComercialHint")}
              error={fieldErrors.nombre_comercial}
            >
              <Input
                value={values.nombre_comercial}
                disabled={saving}
                autoComplete="off"
                onChange={(event) => update("nombre_comercial", event.target.value)}
              />
            </FormField>

            <FormField
              id="issuer-direccion-matriz"
              label={t("invoicingDireccionMatrizLabel")}
              error={fieldErrors.direccion_matriz}
            >
              <Input
                value={values.direccion_matriz}
                disabled={saving}
                autoComplete="off"
                onChange={(event) => update("direccion_matriz", event.target.value)}
              />
            </FormField>

            <FormField
              id="issuer-direccion-establecimiento"
              label={t("invoicingDireccionEstablecimientoLabel")}
              error={fieldErrors.direccion_establecimiento}
            >
              <Input
                value={values.direccion_establecimiento}
                disabled={saving}
                autoComplete="off"
                onChange={(event) => update("direccion_establecimiento", event.target.value)}
              />
            </FormField>

            <div className="grid gap-4 sm:grid-cols-2">
              <FormField
                id="issuer-establecimiento"
                label={t("invoicingEstablecimientoLabel")}
                description={t("invoicingThreeDigitsHint")}
                error={fieldErrors.establecimiento}
              >
                <Input
                  value={values.establecimiento}
                  disabled={saving}
                  inputMode="numeric"
                  autoComplete="off"
                  maxLength={3}
                  onChange={(event) => update("establecimiento", event.target.value)}
                />
              </FormField>

              <FormField
                id="issuer-punto-emision"
                label={t("invoicingPuntoEmisionLabel")}
                description={t("invoicingThreeDigitsHint")}
                error={fieldErrors.punto_emision}
              >
                <Input
                  value={values.punto_emision}
                  disabled={saving}
                  inputMode="numeric"
                  autoComplete="off"
                  maxLength={3}
                  onChange={(event) => update("punto_emision", event.target.value)}
                />
              </FormField>
            </div>

            <FormField
              id="issuer-regimen"
              label={t("invoicingRegimenLabel")}
              error={fieldErrors.regimen}
            >
              <select
                className={SELECT_CLASS_NAME}
                value={values.regimen}
                disabled={saving}
                onChange={(event) => update("regimen", event.target.value as EcuadorIssuerRegimen)}
              >
                {ECUADOR_ISSUER_REGIMENES.map((regimen) => (
                  <option key={regimen} value={regimen}>
                    {t(REGIMEN_KEYS[regimen])}
                  </option>
                ))}
              </select>
            </FormField>

            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={values.obligado_contabilidad}
                disabled={saving}
                onChange={(event) => update("obligado_contabilidad", event.target.checked)}
              />
              {t("invoicingObligadoContabilidadLabel")}
            </label>

            <FormField
              id="issuer-agente-retencion"
              label={t("invoicingAgenteRetencionLabel")}
              description={t("invoicingAgenteRetencionHint")}
              error={fieldErrors.agente_retencion}
            >
              <Input
                value={values.agente_retencion}
                disabled={saving}
                autoComplete="off"
                onChange={(event) => update("agente_retencion", event.target.value)}
              />
            </FormField>
          </CardContent>
        </Card>

        <Button type="submit" disabled={saving}>
          {saving ? t("invoicingSaving") : t("invoicingSave")}
        </Button>
      </form>
    </div>
  );
}
