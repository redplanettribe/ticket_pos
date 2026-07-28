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
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent } from "react";

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
   * Where the Google button points, or null when this deployment has no Google
   * credentials and the button must not be offered at all.
   */
  googleSignInHref: string | null;
};

/**
 * The one thing said about any failed Google Sign-In. It never states whether
 * the address is known here, because the passcode request endpoint deliberately
 * will not either.
 */
const GOOGLE_FAILURE_MESSAGE =
  "We couldn't finish signing you in with Google. Try again, or use a passcode below.";

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

const LINK_FAILURE_MESSAGE: Record<"expired" | "invalid", string> = {
  expired:
    "That ticket link has expired. Sign in with a passcode and you'll still find your purchase here.",
  invalid:
    "That ticket link didn't work. Sign in with a passcode to see your tickets — check you copied the whole link from your email.",
};

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
  googleSignInHref,
}: SignInFormProps) {
  const router = useRouter();
  const [step, setStep] = useState<Step>("email");
  const [email, setEmail] = useState(initialEmail);
  const [code, setCode] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [errorCode, setErrorCode] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const clearedStaleSession = useRef(false);

  // An expired session leaves a cookie behind that will never authenticate
  // again. Sign-out is the one route that can erase it, so ask it to, once.
  useEffect(() => {
    if (!expired || clearedStaleSession.current) return;
    clearedStaleSession.current = true;
    void fetch("/api/customer/auth/sign-out", { method: "POST" }).catch(() => {});
  }, [expired]);

  function resetErrors() {
    setError(null);
    setErrorCode(null);
  }

  async function requestPasscode(address: string) {
    setLoading(true);
    resetErrors();
    setNotice(null);

    try {
      const response = await fetch("/api/customer/auth/request-passcode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: address }),
      });
      const envelope = (await response.json()) as Envelope<{ message: string }>;
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "Could not send a passcode. Please try again.");
        setErrorCode(envelope.error?.code ?? null);
        return;
      }
      setCode("");
      setNotice(envelope.data?.message ?? "Passcode sent.");
      setStep("code");
    } catch {
      setError("Could not send a passcode. Please check your connection and try again.");
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
    resetErrors();

    try {
      const response = await fetch("/api/customer/auth/verify-passcode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code }),
      });
      const envelope = (await response.json()) as Envelope<unknown>;
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "That passcode was not accepted.");
        setErrorCode(envelope.error?.code ?? null);
        return;
      }
      router.push(next);
      router.refresh();
    } catch {
      setError("Could not check that passcode. Please check your connection and try again.");
    } finally {
      setLoading(false);
    }
  }

  const showResend = step === "code" && !(errorCode !== null && RESEND_REFUSED_ERRORS.has(errorCode));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-xl">Sign in</CardTitle>
        <CardDescription>
          {step === "email"
            ? "Enter the email you used to buy your tickets and we'll send you a 6-digit passcode. No password needed."
            : `Enter the 6-digit passcode we sent to ${email}.`}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {expired ? (
          <Alert>
            <AlertDescription>
              Your session has ended. Sign in again to see your tickets.
            </AlertDescription>
          </Alert>
        ) : null}

        {linkFailure ? (
          <Alert>
            <AlertDescription>{LINK_FAILURE_MESSAGE[linkFailure]}</AlertDescription>
          </Alert>
        ) : null}

        {googleFailed && !error ? (
          <Alert variant="destructive">
            <AlertDescription>{GOOGLE_FAILURE_MESSAGE}</AlertDescription>
          </Alert>
        ) : null}

        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        {notice && !error ? (
          <p role="status" className="rounded-lg border bg-muted/50 px-4 py-3 text-sm">
            {notice}
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
              Continue with Google
            </a>
            <div className="flex items-center gap-3">
              <span className="h-px flex-1 bg-border" aria-hidden="true" />
              <span className="text-xs text-muted-foreground">or continue with email</span>
              <span className="h-px flex-1 bg-border" aria-hidden="true" />
            </div>
          </div>
        ) : null}

        {step === "email" ? (
          <form className="space-y-4" onSubmit={handleRequestPasscode} noValidate>
            <FormField id="email" label="Email">
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
              {loading ? "Sending…" : "Send passcode"}
            </Button>
          </form>
        ) : (
          <form className="space-y-4" onSubmit={handleVerifyPasscode} noValidate>
            <FormField id="code" label="Passcode">
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
              {loading ? "Signing in…" : "Sign in"}
            </Button>
            {showResend ? (
              <Button
                type="button"
                variant="secondary"
                className="h-11 w-full"
                disabled={loading}
                onClick={() => void requestPasscode(email)}
              >
                Send a new passcode
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
                setNotice(null);
                resetErrors();
              }}
            >
              Use a different email
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
