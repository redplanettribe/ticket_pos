"use client";

import {
  Alert,
  AlertDescription,
  AuthCard,
  Button,
  FormField,
  Input,
  buttonVariants,
  cn,
} from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import { applyAuthFork } from "@/lib/auth-fork";
import type { SignInCopy } from "@/lib/login-copy";

type Step = "email" | "code";

type LoginFormProps = {
  /**
   * The card's title and its description on the email step, resolved by the
   * page from the `intent` parameter. Every arrival that is not the Storefront's
   * create invitation resolves to today's copy, so this is not a branch here.
   */
  copy: SignInCopy;
  /**
   * True when the Member was just bounced back from a Google Sign-In that did
   * not complete. Which failure it was is deliberately not knowable here.
   */
  googleFailed: boolean;
  /**
   * Where the Google button points, or null when this deployment has no Google
   * client configured — in which case no button is rendered at all.
   */
  googleSignInHref: string | null;
};

/**
 * The one thing said about any failed Google Sign-In. It never states whether
 * the address is known here, because the passcode request endpoint deliberately
 * will not either.
 */
const GOOGLE_FAILURE_MESSAGE =
  "We couldn't finish signing you in with Google. Try again, or use a passcode above.";

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

type SessionData = {
  email: string;
  memberships: Array<{ member_id: string }>;
  active_member: { member_id: string } | null;
};

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

export function LoginForm({ copy, googleFailed, googleSignInHref }: LoginFormProps) {
  const router = useRouter();
  const [step, setStep] = useState<Step>("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function handleRequestOTP(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    setMessage(null);

    try {
      const response = await fetch("/api/auth/request-otp", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      const envelope = (await response.json()) as Envelope<{ message: string }>;
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "Could not send passcode");
        return;
      }
      setMessage(envelope.data?.message ?? "Passcode sent");
      setStep("code");
    } catch {
      setError("Could not send passcode");
    } finally {
      setLoading(false);
    }
  }

  async function handleVerifyOTP(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);

    try {
      const response = await fetch("/api/auth/verify-otp", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code }),
      });
      const envelope = (await response.json()) as Envelope<{ session: SessionData }>;
      if (!response.ok || envelope.error) {
        setError(envelope.error?.message ?? "Invalid passcode");
        return;
      }

      const session = envelope.data?.session;
      if (!session) {
        setError("Could not load session");
        return;
      }

      const fork = await applyAuthFork(session);
      if (fork.error) {
        setError(fork.error);
        return;
      }
      router.push(fork.path);
      router.refresh();
    } catch {
      setError("Could not verify passcode");
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthCard
      title={copy.title}
      // The code step describes the mailbox the passcode went to, whatever
      // brought the visitor here: once a passcode has been sent, the only useful
      // sentence is where to find it.
      description={
        step === "email" ? copy.description : `Enter the 6-digit passcode sent to ${email}.`
      }
    >
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
      {message ? (
        <p role="status" className="rounded-lg border bg-muted/50 px-4 py-3 text-sm">
          {message}
        </p>
      ) : null}

      {step === "email" ? (
        <form className="space-y-4" onSubmit={handleRequestOTP}>
          <FormField id="email" label="Email">
            <Input
              id="email"
              name="email"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </FormField>
          <Button type="submit" className="w-full" disabled={loading} aria-busy={loading}>
            {loading ? "Sending..." : "Send code"}
          </Button>
        </form>
      ) : (
        <form className="space-y-4" onSubmit={handleVerifyOTP}>
          <FormField id="code" label="Passcode">
            <Input
              id="code"
              name="code"
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]{6}"
              maxLength={6}
              required
              value={code}
              onChange={(event) => setCode(event.target.value)}
            />
          </FormField>
          <Button type="submit" className="w-full" disabled={loading} aria-busy={loading}>
            {loading ? "Verifying..." : "Verify"}
          </Button>
          <Button
            type="button"
            variant="ghost"
            className="w-full"
            onClick={() => {
              setStep("email");
              setCode("");
              setMessage(null);
              setError(null);
            }}
          >
            Use a different email
          </Button>
        </form>
      )}

      {/*
        Google FOLLOWS the email form on this surface (PRD decision 6), and the
        order is the decision rather than a layout preference. Members are
        frequently invited before they ever sign in, and an address that does not
        match the invitation is offered organization CREATION here — a duplicate
        Organization, which is the expensive mistake. Leading with Google would
        maximise how often people meet it.

        A plain anchor, not a fetch and not a next/link: /api/auth/google/start is
        a navigation that sets a cookie and redirects, and it must not be
        prefetched.

        The email step only. Once a passcode has been sent the Member is mid-flow
        with their mailbox open, and a second door there would be noise.
      */}
      {googleSignInHref && step === "email" ? (
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <span className="h-px flex-1 bg-border" aria-hidden="true" />
            <span className="text-xs text-muted-foreground">or</span>
            <span className="h-px flex-1 bg-border" aria-hidden="true" />
          </div>
          <a
            href={googleSignInHref}
            className={cn(buttonVariants({ variant: "secondary" }), "h-11 w-full gap-3")}
          >
            <GoogleMark />
            Continue with Google
          </a>
        </div>
      ) : null}
    </AuthCard>
  );
}
