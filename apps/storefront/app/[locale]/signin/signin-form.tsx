"use client";

import {
  Alert,
  AlertDescription,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  FormField,
  Input,
  Markdown,
  buttonVariants,
  cn,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useEffect, useRef, useState, type FormEvent } from "react";

import { Link, useRouter } from "@/i18n/navigation";
import type { PrivacyPolicy } from "@/lib/api";
import { apiErrorMessage } from "@/lib/api-errors";
import type { ConsentRequired } from "@/lib/customer-session";
import { PRIVACY_POLICY_PATH } from "@/lib/privacy-policy";

/**
 * The three steps this page can be on. "consent" is reached only from "code" or
 * from a Google sign-in that came back held: the address is proven by then, and
 * what is missing is Policy Acceptance of the edition in effect (#251).
 */
type Step = "email" | "code" | "consent";

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

/**
 * The two failures a new passcode cannot fix: asking again is exactly what is
 * being refused. Every other passcode failure — mistyped, expired, too many
 * attempts — keeps "Send a new passcode" on screen beside the API's own wording,
 * so a visitor is never told their code is dead without being shown the way out.
 */
const RESEND_REFUSED_ERRORS = new Set(["OTP_RATE_LIMITED", "OTP_GLOBAL_CEILING_REACHED"]);

/**
 * Why a step did not go through, as a fact rather than as a sentence.
 *
 * `code` is doing two jobs, and they are the same job: it decides whether
 * "Send a new passcode" stays on screen, and it decides which sentence is shown
 * (ADR 0023). Both are readings of the API's verdict, never a second opinion
 * about it. `message` is the API's own words, kept for a code the catalog has
 * never heard of; `fallback` is this app's sentence for the request that never
 * arrived.
 */
type SignInError = {
  code: string | null;
  message: string | null;
  fallback:
    | "sendFailed"
    | "sendNetworkFailed"
    | "verifyFailed"
    | "verifyNetworkFailed"
    | "consentFailed"
    | "consentNetworkFailed";
};

type SignInFormProps = {
  /** Where to go once the visitor is signed in; already validated server-side. */
  next: string;
  /**
   * The address to start the email field with, empty when this app knows none.
   *
   * It arrives from the guest surfaces that offer "Sign in to undo" (#121): a
   * buyer who has just checked out as a guest, or who opened a Confirmation
   * Link, should not have to retype the address they bought under to reach the
   * undo. Already guarded server-side, and a prefill regardless — the passcode
   * is what proves anything, and the field stays editable so somebody who bought
   * under a different address can correct it.
   */
  initialEmail: string;
  /** True when the visitor arrived here because their Customer Session had run out. */
  expired: boolean;
  /**
   * Set when the visitor arrived by a Confirmation Link that did not work.
   * "expired" means the link was genuine but has outlived its Event, so this
   * person really did buy a ticket; "invalid" means it never proved anything.
   * Either way the recovery is the same form, which is why they land here.
   */
  linkFailure: "expired" | "invalid" | null;
  /**
   * True when the visitor was just bounced back from a Google Sign-In that did
   * not complete. One flag for every cause — cancelled picker, bad `state`,
   * expired cookie, refused exchange — because the message must not distinguish
   * them (PRD decision 9).
   */
  googleFailed: boolean;
  /**
   * The Follow the visitor pressed before signing in, or null when they came
   * here for the ordinary reason (#219).
   *
   * Already guarded server-side, and relayed verbatim on the verify below — not
   * acted on here. This app cannot make a Follow: it has no session token to
   * make one against, by design (ADR 0008), and the API writes it against the
   * session that verification produces and against nothing this form could say.
   * In particular the `email` beside it is what the passcode is proving, never
   * who is being subscribed.
   */
  followIntent: string | null;
  /**
   * Where the Google button points, or null when this deployment has no Google
   * credentials and the button must not be offered at all.
   */
  googleSignInHref: string | null;
  /**
   * The current Policy Version's Short Notice and checkbox labels, as the API
   * serves them — null when it could not be reached.
   *
   * THE WORDS OF THE NOTICE AND THE LABELS ARE NOT IN THE MESSAGE CATALOGS, and
   * that is the one thing to know about the consent step. They are evidence:
   * the Policy Version records the SHA-256 of exactly these strings, so what a
   * Customer is shown here is byte-for-byte what the platform will later claim
   * they accepted (ADR 0036). The step's own chrome — its heading, the word
   * "Optional", the button — is ordinary copy and lives in the catalogs like
   * everything else.
   */
  policy: PrivacyPolicy | null;
  /**
   * The consent step a Google Sign-In was held at, carried across the callback
   * redirect in an httpOnly cookie and read by the page (#252) — null for every
   * other way of arriving here.
   *
   * It is THE SAME SHAPE the passcode verify answers with, and that sameness is
   * the whole of this feature: past this prop there is no Google path in this
   * component. The step renders from one piece of state, the submission goes to
   * one endpoint, and neither can tell which door produced the token, exactly as
   * the API cannot (ADR 0011).
   *
   * When it is set the form OPENS on the consent step. There is nothing before
   * it to show: the address is already proven, so an email field would be asking
   * for something this visitor has already given.
   */
  pendingConsent: ConsentRequired | null;
};

/** Google's four-colour G, inline so the button needs no network request. */
function GoogleMark() {
  return (
    <svg viewBox="0 0 48 48" aria-hidden="true" className="h-5 w-5">
      <path
        fill="#EA4335"
        d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"
      />
      <path
        fill="#4285F4"
        d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"
      />
      <path
        fill="#FBBC05"
        d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24s.92 7.54 2.56 10.78l7.97-6.19z"
      />
      <path
        fill="#34A853"
        d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"
      />
    </svg>
  );
}

/**
 * Two steps, one page, no navigation between them: entering an email swaps the
 * form for the passcode field with the email still in component state, matching
 * the Staff app's sign-in.
 *
 * Every request this component makes is to a relative /api/customer/... route on
 * this same origin. It never addresses the Go API, and it never sees a session
 * token — the token lives in an httpOnly cookie the verify route sets (ADR 0008).
 */
export function SignInForm({
  next,
  initialEmail,
  expired,
  linkFailure,
  googleFailed,
  followIntent,
  googleSignInHref,
  policy,
  pendingConsent,
}: SignInFormProps) {
  const router = useRouter();
  const t = useTranslations("signin");
  // The language of the address this form is rendered under, sent with the
  // verify so it can be remembered as the Customer's Digest Locale (ADR 0030).
  const locale = useLocale();
  // Keyed by API codes rather than by message keys, so it is read as plain data
  // rather than through `t`.
  const errorCopy = useMessages().errors;
  // A held Google Sign-In opens on the consent step; everyone else opens on the
  // email step. It is an initial value rather than an effect because the step is
  // already decided by the time this renders — the page read the cookie
  // server-side — and a flash of the email form before it corrected itself would
  // invite somebody to start typing an address they have already proven.
  const [step, setStep] = useState<Step>(pendingConsent ? "consent" : "email");
  const [email, setEmail] = useState(initialEmail);
  const [code, setCode] = useState("");
  // Whether a passcode has just gone out — a fact, not a sentence. The words are
  // this app's and are looked up at render, so state cannot strand copy in the
  // language it was set in.
  const [passcodeSent, setPasscodeSent] = useState(false);
  const [error, setError] = useState<SignInError | null>(null);
  const [loading, setLoading] = useState(false);
  // The consent step, once a verify has held this sign-in: which boxes to show,
  // and the token that finishes it. Held in component state and nowhere else —
  // it is spent within the minute, and a token in storage is a token that
  // outlives the tab.
  //
  // Seeded from the Google door's held sign-in when there is one (#252) and set
  // by the passcode verify otherwise: one piece of state for both doors, so
  // nothing downstream of here knows which produced it.
  const [consent, setConsent] = useState<ConsentRequired | null>(pendingConsent);
  // The three answers. ALL START FALSE, always, and nothing in this component
  // ever sets them from anything but a person clicking: consent has to be
  // affirmative, so a pre-ticked box is not a shortcut but a lie about what
  // somebody did (ADR 0034).
  const [policyAccepted, setPolicyAccepted] = useState(false);
  const [marketingConsent, setMarketingConsent] = useState(false);
  const [networkingConsent, setNetworkingConsent] = useState(false);
  const clearedStaleSession = useRef(false);

  // An expired session leaves a cookie behind that will never authenticate
  // again. Sign-out is the one route that can erase it, so ask it to, once.
  useEffect(() => {
    if (!expired || clearedStaleSession.current) return;
    clearedStaleSession.current = true;
    void fetch("/api/customer/auth/sign-out", { method: "POST" }).catch(() => {});
  }, [expired]);

  async function requestPasscode(address: string) {
    setLoading(true);
    setError(null);
    setPasscodeSent(false);

    try {
      const response = await fetch("/api/customer/auth/request-passcode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        // The Locale rides along so the passcode email is written in the
        // language of the page it was asked from (#244, ADR 0033). It words
        // that one email and nothing else: unlike the verify below, this
        // request proves nothing about who owns the address, so the API does
        // not remember it as the Customer's Mail Locale. Read from the address,
        // as every reading of a Locale in this app is — never the browser.
        body: JSON.stringify({ email: address, locale }),
      });
      // The success payload carries a `message`, and it is deliberately not read.
      // It is one constant sentence for every address, known or not
      // (backend/internal/customers/service/auth.go), so it says nothing this
      // app does not already know — and rendering it would put an English
      // sentence on a Spanish page. What IS in it, the refusal to confirm
      // whether the address is known, the catalog copy below keeps.
      const envelope = (await response.json()) as Envelope<unknown>;
      if (!response.ok || envelope.error) {
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "sendFailed",
        });
        return;
      }
      setCode("");
      setPasscodeSent(true);
      setStep("code");
    } catch {
      setError({ code: null, message: null, fallback: "sendNetworkFailed" });
    } finally {
      setLoading(false);
    }
  }

  async function handleRequestPasscode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await requestPasscode(email);
  }

  async function handleVerifyPasscode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);

    try {
      const response = await fetch("/api/customer/auth/verify-passcode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        // The Locale rides along on the verify and on nothing else: it is
        // remembered as this Customer's Digest Locale, so the Follow Digest —
        // mail, with no address of its own to carry a language — is written in
        // the language this page is in. It reads the address, like every other
        // reading of a Locale in this app, never the browser or a cookie.
        // The Follow intent rides the verify too, and only the verify: it is the
        // one request that produces a session, and the API writes the Follow
        // against that session. Sending it with the passcode REQUEST would have
        // been sending it with an unproven address.
        body: JSON.stringify({ email, code, locale, ...(followIntent ? { follow: followIntent } : {}) }),
      });
      const envelope = (await response.json()) as Envelope<{
        consent_required: ConsentRequired | null;
      }>;
      if (!response.ok || envelope.error) {
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "verifyFailed",
        });
        return;
      }
      // The passcode was right and there is still no session: this Customer has
      // not accepted the Policy Version in effect, so the sign-in continues on
      // the consent step rather than finishing (#251). Nothing was set in a
      // cookie — walking away from here leaves them signed out.
      if (envelope.data?.consent_required) {
        setConsent(envelope.data.consent_required);
        setStep("consent");
        return;
      }
      router.push(next);
      router.refresh();
    } catch {
      setError({ code: null, message: null, fallback: "verifyNetworkFailed" });
    } finally {
      setLoading(false);
    }
  }

  /**
   * Exchanges the answers for the session the verify withheld.
   *
   * The required box gates the button below, and this sends whatever the person
   * did regardless — including the optional boxes they left alone, because an
   * unticked box that was SHOWN is an explicit No and the platform records it as
   * one (ADR 0034). The API refuses a submission without Policy Acceptance on
   * its own account; the disabled button is a courtesy, never the guarantee.
   */
  async function handleSubmitConsent(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!consent) return;
    setLoading(true);
    setError(null);

    try {
      const response = await fetch("/api/customer/auth/consent", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          pending_consent_token: consent.pending_consent_token,
          policy_acceptance: policyAccepted,
          marketing_consent: marketingConsent,
          networking_consent: networkingConsent,
          // The Follow intent rides THIS request now, because this is the one
          // that produces a session and the API writes the Follow against the
          // session it just minted (#219). Losing it here would punish somebody
          // for having been asked about consent.
          ...(followIntent ? { follow: followIntent } : {}),
        }),
      });
      const envelope = (await response.json()) as Envelope<unknown>;
      if (!response.ok || envelope.error) {
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "consentFailed",
        });
        return;
      }
      router.push(next);
      router.refresh();
    } catch {
      setError({ code: null, message: null, fallback: "consentNetworkFailed" });
    } finally {
      setLoading(false);
    }
  }

  const showResend =
    step === "code" && !(error?.code != null && RESEND_REFUSED_ERRORS.has(error.code));

  /**
   * One optional checkbox, drawn unticked, with its label as the API worded it.
   *
   * The label is markdown from the Policy Version, so it is rendered rather than
   * interpolated: it is part of the text the edition's fingerprint covers, and
   * this component may not reword it. Only the "Optional" chip beside it belongs
   * to this app, and it is there because the guidance requires an optional
   * consent to LOOK optional — a box that reads like the required one beside it
   * is not freely given.
   */
  function ConsentCheckbox({
    id,
    checked,
    onChange,
    label,
    optional,
  }: {
    id: string;
    checked: boolean;
    onChange: (value: boolean) => void;
    label: string;
    optional: boolean;
  }) {
    return (
      <label htmlFor={id} className="flex items-start gap-3 rounded-lg border p-3 text-sm">
        <input
          id={id}
          name={id}
          type="checkbox"
          className="mt-1 h-4 w-4 shrink-0"
          checked={checked}
          onChange={(event) => onChange(event.target.checked)}
        />
        <span className="space-y-1">
          {optional ? (
            <span className="block text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {t("consent.optional")}
            </span>
          ) : null}
          <Markdown className="text-sm [&>p]:mt-0">{label}</Markdown>
        </span>
      </label>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-xl">{t("title")}</CardTitle>
        <CardDescription>
          {/* The address goes inside the sentence rather than being appended to
              it: which side of it the words fall on is the translator's. */}
          {step === "email" ? t("emailStep") : null}
          {step === "code" ? t("codeStep", { email }) : null}
          {step === "consent" ? t("consent.description") : null}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {expired ? (
          <Alert>
            <AlertDescription>{t("sessionEnded")}</AlertDescription>
          </Alert>
        ) : null}

        {linkFailure ? (
          <Alert>
            {/* Two sentences, not one with the reason swapped in: "expired" is
                told a genuine link ran out and "invalid" is told to check what
                they pasted, and those stay separate keys so a translator is
                never handed a fragment to fit into someone else's grammar. */}
            <AlertDescription>{t(`link.${linkFailure}`)}</AlertDescription>
          </Alert>
        ) : null}

        {googleFailed && !error ? (
          <Alert variant="destructive">
            {/* The one thing said about any failed Google Sign-In. It never
                states whether the address is known here, because the passcode
                request endpoint deliberately will not either. */}
            <AlertDescription>{t("googleFailed")}</AlertDescription>
          </Alert>
        ) : null}

        {error ? (
          <Alert variant="destructive">
            {/* The API said which passcode failure this is; the catalog says it
                in the language this page is being read in, and hands back the
                API's own message for a code it has never heard of. */}
            <AlertDescription>
              {apiErrorMessage(errorCopy, error) ?? t(error.fallback)}
            </AlertDescription>
          </Alert>
        ) : null}

        {passcodeSent && !error ? (
          <p role="status" className="rounded-lg border bg-muted/50 px-4 py-3 text-sm">
            {t("passcodeSent")}
          </p>
        ) : null}

        {/*
          Google leads on this surface (PRD decision 6): the population is
          consumers on phones with a live Google session, and the mailbox
          round-trip below is exactly what this button removes. It is a plain
          anchor, not a fetch — /api/customer/auth/google/start is a navigation
          that sets a cookie and redirects, and it must not be prefetched, which
          is why this is not a next/link either.

          It appears on the email step only. Once a passcode has been sent, the
          visitor is mid-flow with their mailbox open, and offering a second door
          there would be noise.
        */}
        {googleSignInHref && step === "email" ? (
          <div className="space-y-4">
            <a
              href={googleSignInHref}
              className={cn(buttonVariants({ variant: "secondary" }), "h-11 w-full gap-3")}
            >
              <GoogleMark />
              {t("google")}
            </a>
            <div className="flex items-center gap-3">
              <span className="h-px flex-1 bg-border" aria-hidden="true" />
              <span className="text-xs text-muted-foreground">{t("orEmail")}</span>
              <span className="h-px flex-1 bg-border" aria-hidden="true" />
            </div>
          </div>
        ) : null}

        {step === "consent" ? (
          <form className="space-y-4" onSubmit={handleSubmitConsent} noValidate>
            {policy ? (
              <>
                {/*
                  The Short Notice, inline and in full, exactly as the API served
                  it. It is capa 1 of the notice this person is about to accept,
                  and it is here rather than a link away because the guidance
                  requires the information to be present AT the moment of
                  capture. The link below is the rest of it.
                */}
                <div className="rounded-lg border bg-muted/40 p-3 text-sm">
                  <Markdown className="text-sm [&>p]:mt-2 [&>p]:first:mt-0">
                    {policy.short_notice}
                  </Markdown>
                  <p className="mt-3">
                    <Link
                      href={PRIVACY_POLICY_PATH}
                      target="_blank"
                      className="font-medium underline underline-offset-4"
                    >
                      {t("consent.readPolicy")}
                    </Link>
                  </p>
                </div>

                {consent?.boxes.policy_acceptance ? (
                  <ConsentCheckbox
                    id="policy_acceptance"
                    checked={policyAccepted}
                    onChange={setPolicyAccepted}
                    label={policy.consent_labels.policy_acceptance}
                    optional={false}
                  />
                ) : null}
                {consent?.boxes.marketing_consent ? (
                  <ConsentCheckbox
                    id="marketing_consent"
                    checked={marketingConsent}
                    onChange={setMarketingConsent}
                    label={policy.consent_labels.marketing_consent}
                    optional
                  />
                ) : null}
                {consent?.boxes.networking_consent ? (
                  <ConsentCheckbox
                    id="networking_consent"
                    checked={networkingConsent}
                    onChange={setNetworkingConsent}
                    label={policy.consent_labels.networking_consent}
                    optional
                  />
                ) : null}

                <Button
                  type="submit"
                  className="h-11 w-full"
                  // The required box gates the button. The API refuses the same
                  // submission on its own account, so this is what the person
                  // sees rather than what makes it true.
                  disabled={loading || !policyAccepted}
                  aria-busy={loading}
                >
                  {loading ? t("signingIn") : t("consent.submit")}
                </Button>
              </>
            ) : (
              /*
                No notice, no boxes. An API this app cannot reach means it cannot
                show what is being accepted, and a checkbox with no text beside
                it would collect a consent to nothing — which is worse than an
                honest failure, exactly as the Privacy Policy page 404s rather
                than rendering empty.
              */
              <Alert variant="destructive">
                <AlertDescription>{t("consent.unavailable")}</AlertDescription>
              </Alert>
            )}
          </form>
        ) : step === "email" ? (
          <form className="space-y-4" onSubmit={handleRequestPasscode} noValidate>
            <FormField id="email" label={t("emailLabel")}>
              <Input
                name="email"
                type="email"
                autoComplete="email"
                autoFocus
                required
                value={email}
                onChange={(event) => setEmail(event.target.value)}
              />
            </FormField>
            <Button type="submit" className="h-11 w-full" disabled={loading} aria-busy={loading}>
              {loading ? t("sending") : t("sendPasscode")}
            </Button>
          </form>
        ) : (
          <form className="space-y-4" onSubmit={handleVerifyPasscode} noValidate>
            <FormField id="code" label={t("passcodeLabel")}>
              <Input
                name="code"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]{6}"
                maxLength={6}
                autoFocus
                required
                value={code}
                onChange={(event) => setCode(event.target.value.replace(/\D/g, ""))}
              />
            </FormField>
            <Button type="submit" className="h-11 w-full" disabled={loading} aria-busy={loading}>
              {loading ? t("signingIn") : t("submit")}
            </Button>
            {showResend ? (
              <Button
                type="button"
                variant="secondary"
                className="h-11 w-full"
                disabled={loading}
                onClick={() => void requestPasscode(email)}
              >
                {t("resend")}
              </Button>
            ) : null}
            <Button
              type="button"
              variant="ghost"
              className="h-11 w-full"
              disabled={loading}
              onClick={() => {
                setStep("email");
                setCode("");
                setPasscodeSent(false);
                setError(null);
              }}
            >
              {t("differentEmail")}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
