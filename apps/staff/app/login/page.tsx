import { cookies } from "next/headers";
import { getLocale } from "next-intl/server";

import { callBackend } from "@/lib/api";
import { GOOGLE_SIGN_IN_START_PATH, isGoogleSignInConfigured } from "@/lib/google-signin";
import { resolveSignInIntent } from "@/lib/login-copy";
import { PENDING_TERMS_COOKIE } from "@/lib/pending-terms";
import { storefrontTermsUrl } from "@/lib/storefront";

import { LoginForm, type PendingTermsStep } from "./login-form";

import type { AppLocale } from "@ticket-pos/locale";

type LoginPageProps = {
  searchParams: Promise<{ google?: string; intent?: string | string[]; terms?: string }>;
};

/**
 * The terms step a Google sign-in was held at (#538): the callback parked the
 * single-use token in an httpOnly cookie and sent the browser here. The page
 * confirms the cookie exists and fetches the checkbox label from the public
 * terms endpoint IN THE READER'S LANGUAGE — the label is evidence and comes
 * from the backend artifact, never from a catalog, but the artifact is
 * published in both languages and an English reader is owed the English one.
 * The token itself stays server-side; the accept route reads it from the
 * cookie. A missing cookie or a failed fetch renders the ordinary card: the
 * person signs in again, which re-mints everything.
 *
 * The passcode door needs no equivalent: its label arrives on the verify
 * response, already in the language that request was detected as.
 */
async function pendingTermsFromCookie(locale: AppLocale): Promise<PendingTermsStep | null> {
  const store = await cookies();
  if (!store.get(PENDING_TERMS_COOKIE)?.value) {
    return null;
  }
  try {
    const envelope = await callBackend<{ acceptance_label: string; version: string }>(
      `/api/v1/public/terms/${locale}`,
    );
    if (!envelope.data) {
      return null;
    }
    return {
      acceptanceLabel: envelope.data.acceptance_label,
      version: envelope.data.version,
      tokenInCookie: true,
    };
  } catch {
    return null;
  }
}

/**
 * The sign-in page is a server component only so that it can read what this
 * deployment is configured with. The form itself is unchanged and still a client
 * component.
 *
 * Absent Google credentials the button is not rendered at all, so a developer
 * running `make dev` without a Google client sees the passcode form and nothing
 * broken (PRD "Local development"). `google=failed` is set by the callback route
 * on every failure, and says nothing about which one.
 *
 * `intent` arrives from the Storefront's "Create an event" invitation and
 * selects the card's copy, and only that. The decision lives in lib/login-copy
 * so it can be tested without a request; everything unrecognised resolves to the
 * ordinary card there, so nothing needs guarding here. What that card SAYS is
 * the catalog's — the module hands over a token and the form looks the words up
 * (#286).
 *
 * The page says nothing about which language it is in, and that is the point of
 * ADR 0041: there is no `[locale]` segment to read, so `/login` is one address
 * that renders in whichever language the reader is owed. The resolution happens
 * once, in i18n/request.ts, off the cookie and Accept-Language.
 */
export default async function LoginPage({ searchParams }: LoginPageProps) {
  const { google, intent, terms } = await searchParams;
  // The language this page is being rendered in — resolved once in
  // i18n/request.ts, off the cookie and Accept-Language (ADR 0041). The terms
  // step's words and the link beside them follow it.
  const locale = (await getLocale()) as AppLocale;

  const pendingTerms = terms === "required" ? await pendingTermsFromCookie(locale) : null;

  return (
    <LoginForm
      intent={resolveSignInIntent(intent)}
      googleFailed={google === "failed"}
      googleSignInHref={isGoogleSignInConfigured() ? GOOGLE_SIGN_IN_START_PATH : null}
      termsUrl={storefrontTermsUrl(locale)}
      initialPendingTerms={pendingTerms}
    />
  );
}
