"use client";

import { Alert, AlertDescription, AuthCard, Button, Markdown } from "@ticket-pos/ui";
import { useMessages, useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import { LanguageSwitcher } from "@/app/language-switcher";
import { apiErrorMessage } from "@/lib/api-errors";

type TermsGateFormProps = {
  /**
   * The checkbox's words: markdown from the published artifact, rendered
   * verbatim. Evidence, not catalog copy — it may not be reworded, summarised
   * or pre-ticked.
   */
  acceptanceLabel: string;
  /**
   * The single-use token that pins, server-side, the edition and the language
   * this render showed. It is what makes the acceptance evidence the text that
   * was on screen rather than whatever is current when the box is ticked.
   */
  gateToken: string;
  /** The public Storefront page holding the document these words came from. */
  termsUrl: string;
  /** Where the person was going when the gate stopped them. */
  returnPath: string;
};

type Envelope = {
  data: { accepted: boolean } | null;
  error: { code: string; message: string; details?: unknown } | null;
};

/**
 * The interstitial's one form (#570, ADR 0067).
 *
 * It is the sign-in door's terms step, minus everything that step does to a
 * session: no token is exchanged for a credential here, because the person
 * already holds one. Submitting records an acceptance and returns them to what
 * they were doing.
 *
 * There is no "not now". The gate is the platform declining to show pages under
 * a contract nobody has accepted, and an escape hatch would be the gate not
 * binding. Signing out is the only other way past it — the shell's own control,
 * unchanged, and it costs a passcode to come back rather than being spent for
 * them.
 */
export function TermsGateForm({
  acceptanceLabel,
  gateToken,
  termsUrl,
  returnPath,
}: TermsGateFormProps) {
  const t = useTranslations("termsGate");
  // The `errors` namespace as plain data: lib/api-errors turns an API error
  // code into the sentence this catalog has for it, in the reader's language.
  const errorCopy = useMessages().errors;
  const router = useRouter();

  const [checked, setChecked] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError(null);

    try {
      const response = await fetch("/api/auth/terms-gate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ gate_token: gateToken, terms_acceptance: checked }),
      });
      const envelope = (await response.json()) as Envelope;

      if (!response.ok || !envelope.data?.accepted) {
        // The pinned box is minutes long, and a page left open outlives it.
        // The recovery is a fresh render — which costs a round trip and
        // nothing else, no session and no passcode — so this asks for one
        // rather than telling somebody to sign in again.
        if (envelope.error?.code === "PENDING_TERMS_INVALID") {
          setError(t("expired"));
          router.refresh();
          return;
        }
        setError(apiErrorMessage(errorCopy, envelope.error) ?? t("failed"));
        return;
      }

      // Back to where they were going. `refresh` first, so the navigation is
      // decided against a session that no longer owes anything — otherwise the
      // middleware answers the next request from a cached decision and bounces
      // the person straight back here.
      router.refresh();
      router.replace(returnPath);
    } catch {
      setError(t("failed"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthCard title={t("title")} description={t("description")} footer={<LanguageSwitcher />}>
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <form className="space-y-4" onSubmit={handleSubmit}>
        {/*
          The label is the published artifact rendered verbatim — never reworded
          and never pre-ticked (§3, ADR 0066). Only the link line and the button
          belong to this app's catalog. The full document lives on the public
          Storefront terms page, so the link opens it in a new tab and this card
          survives the reading.
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
            checked={checked}
            onChange={(event) => setChecked(event.target.checked)}
          />
          <Markdown className="text-sm [&>p]:mt-0">{acceptanceLabel}</Markdown>
        </label>
        <a
          href={termsUrl}
          target="_blank"
          rel="noreferrer"
          className="block text-sm text-muted-foreground underline underline-offset-4"
        >
          {t("linkLabel")}
        </a>
        {/* Disabled unticked as a courtesy; the API refuses an unticked
            submission regardless, and refusing costs the person nothing but the
            click — the token is not spent on a refusal here. */}
        <Button type="submit" className="w-full" disabled={loading || !checked} aria-busy={loading}>
          {loading ? t("accepting") : t("accept")}
        </Button>
      </form>
    </AuthCard>
  );
}
