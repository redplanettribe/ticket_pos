"use client";

import {
  Alert,
  AlertDescription,
  AuthCard,
  Button,
  FormField,
  Input,
} from "@ticket-pos/ui";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import { applyAuthFork } from "@/lib/auth-fork";

type Step = "email" | "code";

type SessionData = {
  email: string;
  memberships: Array<{ member_id: string }>;
  active_member: { member_id: string } | null;
};

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

export default function LoginPage() {
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
      title="Sign in"
      description={
        step === "email"
          ? "Enter your email to receive a one-time passcode."
          : `Enter the 6-digit passcode sent to ${email}.`
      }
    >
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
    </AuthCard>
  );
}
