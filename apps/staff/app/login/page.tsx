import { GOOGLE_SIGN_IN_START_PATH, isGoogleSignInConfigured } from "@/lib/google-signin";
import { resolveSignInIntent } from "@/lib/login-copy";

import { LoginForm } from "./login-form";

type LoginPageProps = {
  searchParams: Promise<{ google?: string; intent?: string | string[] }>;
};

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
  const { google, intent } = await searchParams;

  return (
    <LoginForm
      intent={resolveSignInIntent(intent)}
      googleFailed={google === "failed"}
      googleSignInHref={isGoogleSignInConfigured() ? GOOGLE_SIGN_IN_START_PATH : null}
    />
  );
}
