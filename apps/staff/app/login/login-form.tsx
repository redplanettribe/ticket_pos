"use client";

import {
  Alert,
  AlertDescription,
  AuthCard,
  Button,
  FormField,
  Input,
  Markdown,
  buttonVariants,
  cn,
} from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import { LanguageSwitcher } from "@/app/language-switcher";
import { apiErrorMessage } from "@/lib/api-errors";
import { applyAuthFork } from "@/lib/auth-fork";
import { asksAdulthoodDeclaration, termsGateAnswersComplete } from "@/lib/terms-gate";

import type { SignInIntent } from "@/lib/login-copy";

type Step = "email" | "code" | "terms";

/**
 * The terms step's server-known half (#538, ADR 0066): the checkbox label as
 * the backend artifact words it — evidence, never catalog copy — the edition it
 * belongs to, and whether the single-use token is parked in the httpOnly
 * cookie (the Google door) rather than held by this form (the passcode door).
 */
export type PendingTermsStep = {
  acceptanceLabel: string;
  /**
   * The Adulthood Declaration's own checkbox words, or null when the edition in
   * effect carries no `label-adulthood-declaration` artifact and asks nothing
   * (#587, ADR 0069). Its presence is the whole of whether the second box is
   * drawn — see lib/terms-gate.
   */
  adulthoodDeclarationLabel: string | null;
  version: string;
  tokenInCookie: boolean;
};

type TermsRequiredPayload = {
  pending_terms_token: string;
  acceptance_label: string;
  /** Absent from the payload when this edition does not ask (#587). */
  adulthood_declaration_label?: string;
  version: string;
};

type LoginFormProps = {
  /**
   * Which of the two cards is being shown, resolved by the page from the
   * `intent` parameter. A token rather than a pair of sentences: the words are
   * the catalog's, in whichever language this request resolved to, and lib/
   * stays free of copy (#286). Every arrival that is not the Storefront's create
   * invitation resolves to "default".
   */
  intent: SignInIntent;
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
  /**
   * The public Storefront terms page. The Staff app hosts no copy of the
   * document (#538): the terms step links out, and this is where.
   */
  termsUrl: string;
  /**
   * Non-null when a Google sign-in was held at the terms gate and the callback
   * parked the token in the cookie: the card opens on the terms step directly.
   */
  initialPendingTerms: PendingTermsStep | null;
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

type SessionData = {
  email: string;
  memberships: Array<{ member_id: string }>;
  active_member: { member_id: string } | null;
};

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string; details?: unknown } | null;
};

export function LoginForm({
  intent,
  googleFailed,
  googleSignInHref,
  termsUrl,
  initialPendingTerms,
}: LoginFormProps) {
  // Every sentence on this page comes from here, in the language i18n/request.ts
  // resolved for this request — cookie, then Accept-Language, then English. The
  // one exception is an error the API worded (below).
  const t = useTranslations("login");
  // The `errors` namespace as plain data. lib/api-errors turns the API's error
  // CODE into a sentence in this page's language, with the API's own English as
  // the floor beneath a code the catalog has never heard of (ADR 0023).
  const errorCopy = useMessages().errors;
  const router = useRouter();
  // A Google sign-in the gate held arrives with its terms step already known
  // (#538): the card opens on it, token parked in the httpOnly cookie.
  const [step, setStep] = useState<Step>(initialPendingTerms ? "terms" : "email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [pendingTerms, setPendingTerms] = useState<PendingTermsStep | null>(initialPendingTerms);
  // The passcode door's token, held by the form; null on the Google door,
  // where the accept route reads the cookie instead.
  const [pendingTermsToken, setPendingTermsToken] = useState<string | null>(null);
  // Un-premarked, always: stored state decides whether to ASK, never what to
  // show as already agreed.
  const [termsChecked, setTermsChecked] = useState(false);
  // The Adulthood Declaration (#587, ADR 0069): its own box, its own state, its
  // own answer, un-premarked for the reason above. Never folded into the one
  // above — a combined tick evidences only that somebody accepted a document
  // containing an age sentence, which is the inference ADR 0069 replaces.
  const [adulthoodDeclared, setAdulthoodDeclared] = useState(false);

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
        // A passcode refused for a reason the catalog knows — rate limiting, a
        // ceiling reached — reads in this page's language. Anything else keeps
        // the API's English, which is what this line rendered before the catalog
        // existed. `t("sendFailed")` is only for an envelope carrying no words at
        // all.
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("sendFailed"));
        return;
      }
      // Deliberately not `envelope.data.message`. That sentence is the API's,
      // and the API does not speak Spanish and must not start (ADR 0027) — on
      // the happy path, which every person signing in takes, an English
      // confirmation in the middle of a Spanish card is the whole page's
      // translation undone.
      setMessage(t("passcodeSent"));
      setStep("code");
    } catch {
      setError(t("sendFailed"));
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
      const envelope = (await response.json()) as Envelope<{
        session: SessionData | null;
        terms_required: TermsRequiredPayload | null;
      }>;
      if (!response.ok || envelope.error) {
        // The failures a person actually meets here — a wrong passcode, an
        // expired one, too many attempts — are all cataloged, so the one moment
        // this page is least forgiving is not the moment it switches to English.
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("verifyFailed"));
        return;
      }

      // The passcode proved the address and the terms gate held the sign-in
      // (#538): no session yet, one step to finish. The label is the backend
      // artifact's, rendered verbatim.
      const termsRequired = envelope.data?.terms_required;
      if (termsRequired) {
        setPendingTerms({
          acceptanceLabel: termsRequired.acceptance_label,
          adulthoodDeclarationLabel: termsRequired.adulthood_declaration_label ?? null,
          version: termsRequired.version,
          tokenInCookie: false,
        });
        setPendingTermsToken(termsRequired.pending_terms_token);
        setTermsChecked(false);
        setAdulthoodDeclared(false);
        setStep("terms");
        return;
      }

      const session = envelope.data?.session;
      if (!session) {
        setError(t("sessionFailed"));
        return;
      }

      const fork = await applyAuthFork(session);
      if (fork.failure) {
        setError(
          apiErrorMessage(errorCopy, fork.failure.apiError) ?? t("selectOrganizationFailed"),
        );
        return;
      }
      router.push(fork.path);
      router.refresh();
    } catch {
      setError(t("verifyFailed"));
    } finally {
      setLoading(false);
    }
  }

  async function handleAcceptTerms(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLoading(true);
    setError(null);

    try {
      const response = await fetch("/api/auth/accept-terms", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          // The Google door's token lives in the httpOnly cookie; the route
          // falls back to it when the form holds none.
          ...(pendingTermsToken ? { pending_terms_token: pendingTermsToken } : {}),
          terms_acceptance: termsChecked,
          // Sent only where the box was drawn (#587). Where it was drawn and
          // left unticked this sends `false`, and the API refuses it with
          // ADULTHOOD_DECLARATION_REQUIRED before a session is minted and
          // before any row is written — the disabled button is the courtesy,
          // that refusal is the guarantee.
          ...(asksAdulthoodDeclaration(pendingTerms?.adulthoodDeclarationLabel)
            ? { adulthood_declaration: adulthoodDeclared }
            : {}),
        }),
      });
      const envelope = (await response.json()) as Envelope<{ session: SessionData | null }>;
      if (!response.ok || envelope.error) {
        // A spent or expired token reads in this page's language where the
        // catalog knows the code; the recovery either way is the sentence in
        // termsFailed — sign in again, which re-mints everything.
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("termsFailed"));
        return;
      }

      const session = envelope.data?.session;
      if (!session) {
        setError(t("sessionFailed"));
        return;
      }

      const fork = await applyAuthFork(session);
      if (fork.failure) {
        setError(
          apiErrorMessage(errorCopy, fork.failure.apiError) ?? t("selectOrganizationFailed"),
        );
        return;
      }
      router.push(fork.path);
      router.refresh();
    } catch {
      setError(t("termsFailed"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthCard
      title={intent === "create" ? t("createTitle") : t("title")}
      // The code step describes the mailbox the passcode went to, whatever
      // brought the visitor here: once a passcode has been sent, the only useful
      // sentence is where to find it.
      //
      // Interpolated rather than concatenated, so the address can sit wherever
      // the Spanish wants it rather than wherever the English put it.
      description={
        step === "terms"
          ? t("termsDescription")
          : step === "email"
            ? intent === "create"
              ? t("createDescription")
              : t("description")
            : t("codeDescription", { email })
      }
      // The escape hatch for a language detected wrongly. It sits under the card
      // rather than in it: it is about the page, not about signing in, and this
      // is the one surface a signed-out person can reach at all.
      footer={<LanguageSwitcher />}
    >
      {googleFailed && !error ? (
        <Alert variant="destructive">
          <AlertDescription>{t("googleFailed")}</AlertDescription>
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

      {step === "terms" && pendingTerms ? (
        <form className="space-y-4" onSubmit={handleAcceptTerms}>
          {/*
            The label is evidence, not copy: markdown from the current Terms
            Version, rendered verbatim, never reworded and never pre-ticked
            (§3, ADR 0066). Only the link line and the button belong to this
            app's catalog. The full document lives on the public Storefront
            terms page — this app hosts no copy — so the link opens it in a new
            tab and this card survives the reading.
          */}
          <label
            htmlFor="terms-acceptance"
            className="flex items-start gap-3 rounded-lg border p-3 text-sm"
          >
            <input
              id="terms-acceptance"
              name="terms-acceptance"
              type="checkbox"
              className="mt-1 h-4 w-4 shrink-0"
              checked={termsChecked}
              onChange={(event) => setTermsChecked(event.target.checked)}
            />
            <Markdown className="text-sm [&>p]:mt-0">{pendingTerms.acceptanceLabel}</Markdown>
          </label>
          {/*
            The Adulthood Declaration (#587, ADR 0069): a second, separate,
            un-premarked box, drawn iff the edition being accepted publishes the
            artifact that words it. Its label is evidence too — hashed into the
            edition's fingerprint — so nothing here rewords it and no catalog
            string stands in for it. The link below belongs to both boxes: the
            declaration is made in the contract's own terms.
          */}
          {pendingTerms.adulthoodDeclarationLabel ? (
            <label
              htmlFor="adulthood-declaration"
              className="flex items-start gap-3 rounded-lg border p-3 text-sm"
            >
              <input
                id="adulthood-declaration"
                name="adulthood-declaration"
                type="checkbox"
                className="mt-1 h-4 w-4 shrink-0"
                checked={adulthoodDeclared}
                onChange={(event) => setAdulthoodDeclared(event.target.checked)}
              />
              <Markdown className="text-sm [&>p]:mt-0">
                {pendingTerms.adulthoodDeclarationLabel}
              </Markdown>
            </label>
          ) : null}
          <a
            href={termsUrl}
            target="_blank"
            rel="noreferrer"
            className="block text-sm text-muted-foreground underline underline-offset-4"
          >
            {t("termsLinkLabel")}
          </a>
          {/* Disabled until EVERY owed box is ticked, as a courtesy; the API
              refuses an incomplete submission regardless. Which boxes are owed
              is decided in lib/terms-gate, once, for this door and the
              interstitial alike. */}
          <Button
            type="submit"
            className="w-full"
            disabled={
              loading ||
              !termsGateAnswersComplete({
                adulthoodDeclarationLabel: pendingTerms.adulthoodDeclarationLabel,
                termsAccepted: termsChecked,
                adulthoodDeclared,
              })
            }
            aria-busy={loading}
          >
            {loading ? t("termsAccepting") : t("termsAccept")}
          </Button>
        </form>
      ) : step === "email" ? (
        <form className="space-y-4" onSubmit={handleRequestOTP}>
          <FormField id="email" label={t("emailLabel")}>
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
            {loading ? t("sending") : t("sendCode")}
          </Button>
        </form>
      ) : (
        <form className="space-y-4" onSubmit={handleVerifyOTP}>
          <FormField id="code" label={t("passcodeLabel")}>
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
            {loading ? t("verifying") : t("verify")}
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
            {t("useDifferentEmail")}
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
            <span className="text-xs text-muted-foreground">{t("or")}</span>
            <span className="h-px flex-1 bg-border" aria-hidden="true" />
          </div>
          <a
            href={googleSignInHref}
            className={cn(buttonVariants({ variant: "secondary" }), "h-11 w-full gap-3")}
          >
            <GoogleMark />
            {t("continueWithGoogle")}
          </a>
        </div>
      ) : null}
    </AuthCard>
  );
}
