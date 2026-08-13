import { GOOGLE_SIGN_IN_START_PATH, isGoogleSignInConfigured } from "@/lib/google-signin";
import { resolveSignInCopy } from "@/lib/login-copy";

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
 * so it can be tested without a request; everything unrecognised resolves to
 * today's copy there, so nothing needs guarding here.
 */
export default async function LoginPage({ searchParams }: LoginPageProps) {
  const { google, intent } = await searchParams;

  return (
    <LoginForm
      copy={resolveSignInCopy(intent)}
      googleFailed={google === "failed"}
      googleSignInHref={isGoogleSignInConfigured() ? GOOGLE_SIGN_IN_START_PATH : null}
    />
  );
}
