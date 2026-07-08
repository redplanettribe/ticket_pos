"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

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
      const membershipCount = session?.memberships.length ?? 0;
      if (membershipCount === 0) {
        router.push("/onboarding/create-organization");
        return;
      }
      if (membershipCount === 1 || session?.active_member) {
        router.push("/");
        return;
      }
      router.push("/select-organization");
    } catch {
      setError("Could not verify passcode");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main>
      <h1>Sign in</h1>
      {step === "email" ? (
        <>
          <p>Enter your email to receive a one-time passcode.</p>
          <form onSubmit={handleRequestOTP}>
            <label htmlFor="email">Email</label>
            <input
              id="email"
              name="email"
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
            <button type="submit" disabled={loading}>
              {loading ? "Sending..." : "Send code"}
            </button>
          </form>
        </>
      ) : (
        <>
          <p>Enter the 6-digit passcode sent to {email}.</p>
          <form onSubmit={handleVerifyOTP}>
            <label htmlFor="code">Passcode</label>
            <input
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
            <button type="submit" disabled={loading}>
              {loading ? "Verifying..." : "Verify"}
            </button>
          </form>
          <button
            type="button"
            onClick={() => {
              setStep("email");
              setCode("");
              setMessage(null);
              setError(null);
            }}
          >
            Use a different email
          </button>
        </>
      )}
      {message ? <p role="status">{message}</p> : null}
      {error ? <p role="alert">{error}</p> : null}
    </main>
  );
}
