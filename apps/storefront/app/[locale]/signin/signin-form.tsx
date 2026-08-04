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
  buttonVariants,
  cn,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useEffect, useRef, useState, type FormEvent } from "react";

import { useRouter } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";

type Step = "email" | "code";

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
  fallback: "sendFailed" | "sendNetworkFailed" | "verifyFailed" | "verifyNetworkFailed";
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
}: SignInFormProps) {
  const router = useRouter();
  const t = useTranslations("signin");
  // The language of the address this form is rendered under, sent with the
  // verify so it can be remembered as the Customer's Digest Locale (ADR 0030).
  const locale = useLocale();
  // Keyed by API codes rather than by message keys, so it is read as plain data
  // rather than through `t`.
  const errorCopy = useMessages().errors;
  const [step, setStep] = useState<Step>("email");
  const [email, setEmail] = useState(initialEmail);
  const [code, setCode] = useState("");
  // Whether a passcode has just gone out — a fact, not a sentence. The words are
  // this app's and are looked up at render, so state cannot strand copy in the
  // language it was set in.
  const [passcodeSent, setPasscodeSent] = useState(false);
  const [error, setError] = useState<SignInError | null>(null);
  const [loading, setLoading] = useState(false);
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
        body: JSON.stringify({ email: address }),
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
      const envelope = (await response.json()) as Envelope<unknown>;
      if (!response.ok || envelope.error) {
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "verifyFailed",
        });
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

  const showResend =
    step === "code" && !(error?.code != null && RESEND_REFUSED_ERRORS.has(error.code));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-xl">{t("title")}</CardTitle>
        <CardDescription>
          {/* The address goes inside the sentence rather than being appended to
              it: which side of it the words fall on is the translator's. */}
          {step === "email" ? t("emailStep") : t("codeStep", { email })}
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

        {step === "email" ? (
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
