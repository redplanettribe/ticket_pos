"use client";

import { useEffect, useRef, useState } from "react";

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
  type EcuadorIssuerFrozenField,
  type EcuadorIssuerRegimen,
  type OperatorEcuadorIssuer,
  fetchOperatorEcuadorIssuer,
  saveOperatorEcuadorIssuer,
  uploadOperatorEcuadorIssuerCertificate,
} from "@/lib/operator-api";

import { CertificateExpiryAlert } from "../certificate-expiry-alert";

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
 * THE CERTIFICATE IS A SECOND FORM ON THE SAME PAGE (#453). It is the Issuer's
 * signing key in custody, uploaded as multipart through the BFF and the API and
 * read back as metadata only; the card below the details shows what is on file
 * and takes the next upload. It is its own form because it is its own request —
 * a save of the details never touches the certificate, and an upload never
 * touches the details.
 *
 * THREE FIELDS FREEZE (#455). The API's read names them in `frozen_fields`:
 * the RUC once any factura exists in either environment (it is inside every
 * clave de acceso), establecimiento and punto de emisión once a sequence has
 * started under them (numbering must stay continuous). Those inputs render
 * read-only with the reason in place of their hint, and a save that changed
 * one anyway would be refused with ISSUER_FIELD_FROZEN. Everything else stays
 * editable at any time.
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
  // Likewise the freezes are the stored Issuer's: what has been issued under
  // it, which no unsaved edit changes.
  const frozen = new Set<EcuadorIssuerFrozenField>(issuer?.frozen_fields ?? []);

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
              description={frozen.has("ruc") ? t("invoicingFrozenRuc") : t("invoicingRucHint")}
              error={fieldErrors.ruc}
            >
              <Input
                value={values.ruc}
                disabled={saving}
                readOnly={frozen.has("ruc")}
                aria-readonly={frozen.has("ruc")}
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
                description={
                  frozen.has("establecimiento") ? t("invoicingFrozenNumbering") : t("invoicingThreeDigitsHint")
                }
                error={fieldErrors.establecimiento}
              >
                <Input
                  value={values.establecimiento}
                  disabled={saving}
                  readOnly={frozen.has("establecimiento")}
                  aria-readonly={frozen.has("establecimiento")}
                  inputMode="numeric"
                  autoComplete="off"
                  maxLength={3}
                  onChange={(event) => update("establecimiento", event.target.value)}
                />
              </FormField>

              <FormField
                id="issuer-punto-emision"
                label={t("invoicingPuntoEmisionLabel")}
                description={
                  frozen.has("punto_emision") ? t("invoicingFrozenNumbering") : t("invoicingThreeDigitsHint")
                }
                error={fieldErrors.punto_emision}
              >
                <Input
                  value={values.punto_emision}
                  disabled={saving}
                  readOnly={frozen.has("punto_emision")}
                  aria-readonly={frozen.has("punto_emision")}
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

      <CertificateCard
        issuer={issuer}
        onUploaded={(saved) => {
          setIssuer(saved);
          setValues(toFormValues(saved));
        }}
      />
    </div>
  );
}

/**
 * The signing certificate in custody and the form that replaces it.
 *
 * WHAT IT SHOWS IS ALL THE SERVER KNOWS WITHOUT OPENING THE FILE: subject, the
 * RUC found inside, the validity window, the fingerprint and when it was
 * uploaded. The bytes and the password never come back, so there is nothing to
 * hide here and nothing to show but this. A RUC that differs from the Issuer's
 * is a WARNING and never a block — some certification authorities put the RUC
 * somewhere the server does not look, and the SRI's own check is the final word.
 *
 * EVERY REFUSAL IS THE API'S, AND IT IS SHOWN IN PLACE. A wrong password, a
 * file without an RSA key, a file that is not a .p12 and a server with no
 * certificate key each arrive as their own code and are rendered as an alert on
 * the card, from the catalog: the last of those is a deployment setting and
 * must not be mistaken for a bad file, which a toast that vanishes would invite.
 */
function CertificateCard({
  issuer,
  onUploaded,
}: {
  issuer: OperatorEcuadorIssuer | null;
  onUploaded: (saved: OperatorEcuadorIssuer) => void;
}) {
  const t = useTranslations("operator");
  const errorCopy = useMessages().errors;
  const locale = toAppLocale(useLocale());

  const fileInput = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [password, setPassword] = useState("");
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  const certificate = issuer?.certificate ?? null;
  const canUpload = issuer !== null;

  async function handleUpload(event: React.FormEvent) {
    event.preventDefault();
    if (!file) {
      setFieldErrors({ file: t("invoicingCertificateFileRequired") });
      return;
    }
    setUploading(true);
    setUploadError(null);
    setFieldErrors({});
    try {
      const saved = await uploadOperatorEcuadorIssuerCertificate(file, password);
      onUploaded(saved);
      // The password was checked and is now in custody; nothing on this page
      // should keep it a moment longer.
      setPassword("");
      setFile(null);
      if (fileInput.current) fileInput.current.value = "";
      toast.success(t("invoicingCertificateUploaded"));
    } catch (error) {
      if (error instanceof ApiError && error.code === "VALIDATION_FAILED") {
        setFieldErrors(fieldErrorMessages(errorCopy, error.details));
      } else {
        setUploadError(
          (error instanceof ApiError ? apiErrorMessage(errorCopy, error) : null) ??
            t("invoicingCertificateUploadFailed"),
        );
      }
    } finally {
      setUploading(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("invoicingCertificateTitle")}</CardTitle>
        <CardDescription>{t("invoicingCertificateDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {certificate ? (
          <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[max-content_1fr]">
            <dt className="text-muted-foreground">{t("invoicingCertificateSubject")}</dt>
            {/* A distinguished name and a fingerprint are the certificate's own and are never translated. */}
            <dd className="break-all">{certificate.subject}</dd>
            <dt className="text-muted-foreground">{t("invoicingCertificateRuc")}</dt>
            <dd>{certificate.ruc === "" ? t("invoicingCertificateRucNone") : certificate.ruc}</dd>
            <dt className="text-muted-foreground">{t("invoicingCertificateValidFrom")}</dt>
            <dd>{formatDateTime(certificate.not_before, PLATFORM_TIME_ZONE, locale)}</dd>
            <dt className="text-muted-foreground">{t("invoicingCertificateValidUntil")}</dt>
            <dd>
              {formatDateTime(certificate.not_after, PLATFORM_TIME_ZONE, locale)}
              {/* The Certificate Expiry Warning (#500, ADR 0063 §5), unlinked: the
                  remedy is the form underneath. */}
              {issuer ? (
                <CertificateExpiryAlert expiry={issuer.certificate_expiry} className="mt-2" />
              ) : null}
            </dd>
            <dt className="text-muted-foreground">{t("invoicingCertificateFingerprint")}</dt>
            <dd className="break-all font-mono text-xs">{certificate.fingerprint_sha256}</dd>
            <dt className="text-muted-foreground">{t("invoicingCertificateUploadedAt")}</dt>
            <dd>{formatDateTime(certificate.uploaded_at, PLATFORM_TIME_ZONE, locale)}</dd>
          </dl>
        ) : (
          <p className="text-sm text-muted-foreground">
            {canUpload ? t("invoicingCertificateNone") : t("invoicingCertificateNeedsIssuer")}
          </p>
        )}

        {issuer && certificate && issuer.certificate_ruc_mismatch ? (
          <Alert variant="warning">
            <AlertTitle>{t("invoicingCertificateMismatchTitle")}</AlertTitle>
            <AlertDescription>
              {t("invoicingCertificateMismatch", {
                certificateRuc: certificate.ruc,
                issuerRuc: issuer.ruc,
              })}
            </AlertDescription>
          </Alert>
        ) : null}

        {uploadError ? (
          <Alert variant="destructive">
            <AlertTitle>{t("invoicingCertificateUploadFailedTitle")}</AlertTitle>
            <AlertDescription>{uploadError}</AlertDescription>
          </Alert>
        ) : null}

        <form className="space-y-4" onSubmit={handleUpload}>
          <FormField
            id="issuer-certificate-file"
            label={t("invoicingCertificateFileLabel")}
            description={t("invoicingCertificateFileHint")}
            error={fieldErrors.file}
          >
            <input
              ref={fileInput}
              type="file"
              accept=".p12,.pfx,application/x-pkcs12"
              disabled={!canUpload || uploading}
              className="text-sm file:mr-3 file:rounded-md file:border file:border-input file:bg-background file:px-3 file:py-1.5 file:text-sm disabled:opacity-60"
              onChange={(event) => setFile(event.target.files?.[0] ?? null)}
            />
          </FormField>
          {/* A filename is the reader's own and is never translated. */}
          {file ? (
            <p className="text-sm text-muted-foreground">
              {t("invoicingCertificateSelectedFile", { name: file.name })}
            </p>
          ) : null}

          <FormField
            id="issuer-certificate-password"
            label={t("invoicingCertificatePasswordLabel")}
            description={t("invoicingCertificatePasswordHint")}
            error={fieldErrors.password}
          >
            <Input
              type="password"
              value={password}
              disabled={!canUpload || uploading}
              autoComplete="off"
              onChange={(event) => setPassword(event.target.value)}
            />
          </FormField>

          <Button type="submit" disabled={!canUpload || uploading}>
            {uploading
              ? t("invoicingCertificateUploading")
              : certificate
                ? t("invoicingCertificateReplace")
                : t("invoicingCertificateUpload")}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
