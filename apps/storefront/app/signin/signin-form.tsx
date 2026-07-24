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
  /** True when the visitor arrived here because their Customer Session had run out. */
  expired: boolean;
  /**
   * Set when the visitor arrived by a Confirmation Link that did not work.
   * "expired" means the link was genuine but has outlived its Event, so this
   * person really did buy a ticket; "invalid" means it never proved anything.
   * Either way the recovery is the same form, which is why they land here.
   */
  linkFailure: "expired" | "invalid" | null;
};

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
export function SignInForm({ next, expired, linkFailure }: SignInFormProps) {
  const router = useRouter();
  const [step, setStep] = useState<Step>("email");
  const [email, setEmail] = useState("");
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
