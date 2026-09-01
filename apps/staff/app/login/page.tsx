import { cookies } from "next/headers";
import { getLocale } from "next-intl/server";

import { APIError, callBackend } from "@/lib/api";
import { GOOGLE_SIGN_IN_START_PATH, isGoogleSignInConfigured } from "@/lib/google-signin";
import { resolveSignInIntent } from "@/lib/login-copy";
import { PENDING_TERMS_COOKIE } from "@/lib/pending-terms";
import { storefrontTermsUrl } from "@/lib/storefront";

import { LoginForm, type PendingTermsStep } from "./login-form";

import type { AppLocale } from "@ticket-pos/locale";

/**
 * The one language the Terms can never stop being published in (#559).
 *
 * `terms.PrevailingLocale` on the backend: the Spanish text IS the contract
 * (§37) and the English one is a translation of it, so a publish that would
 * drop Spanish is refused outright. Here it is the floor this gate falls back
 * to — the same floor identity/service/terms.go applies before it will mint a
 * session — so a missing translation can never cost an organizer the gate.
 *
 * A local constant rather than a shared one: the Staff app has exactly this one
 * use for it, and the guarantee behind it is that changing the language costs a
 * code change on both sides.
 */
const TERMS_PREVAILING_LOCALE: AppLocale = "es";

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
 * cookie. A missing cookie renders the ordinary card: nothing was held.
 *
 * WHAT A MISSING LABEL MAY NOT DO IS DROP THE GATE (#559). This used to return
 * null on every failure, which renders the ordinary sign-in card — a card with
 * no acceptance step on it at all, offered to somebody the gate is holding.
 * A language the current edition does not publish is now floored at the
 * PREVAILING one, exactly as identity/service/terms.go's termsGateLabels does
 * for the same reason: falling back to the text that legally binds them is the
 * only safe answer, and the reader gets a link to the Spanish page rather than
 * a link to a not-found one. An organizer must never be past this gate because
 * a translation was missing.
 *
 * And when even the prevailing label cannot be had — the edition publishes
 * none, or the read failed — this THROWS. The backend refuses to mint a session
 * in that state too, so the honest answer is an error page rather than a card
 * that looks like a way in and is not.
 *
 * The passcode door needs no equivalent: its label arrives on the verify
 * response, already in the language that request was detected as, floored by
 * the same backend rule.
 */
async function pendingTermsFromCookie(locale: AppLocale): Promise<HeldTerms | null> {
  const store = await cookies();
  if (!store.get(PENDING_TERMS_COOKIE)?.value) {
    return null;
  }

  const step = await termsStepIn(locale);
  if (step) {
    await refuseAHalfPublishedDeclaration(step, locale);
    return step;
  }

  // This language is not in the current edition's published set. The contract
  // itself still is: TERMS_PREVAILING_LOCALE can never be dropped.
  const prevailing = await termsStepIn(TERMS_PREVAILING_LOCALE);
  if (prevailing) {
    return prevailing;
  }
  throw new Error(
    "the current Terms edition publishes no acceptance label in the prevailing locale; " +
      "refusing to render a sign-in card with no acceptance step on it",
  );
}

/**
 * Refuses to render a Google terms step that is MISSING a box the edition owes
 * (#587, ADR 0069).
 *
 * WHETHER AN EDITION ASKS IS A FACT ABOUT THE EDITION, not about the reader:
 * the Spanish text is the contract (§37) and the English one is its courtesy
 * translation, so the backend answers "does this edition ask?" from the
 * prevailing document and owes the box to everybody or to nobody. This page
 * cannot see that from one Locale's payload — the public terms endpoint answers
 * strictly, and "this edition does not ask" and "this translation is missing
 * the artifact" arrive looking identical — so where the reader's step carries
 * no declaration label it asks the prevailing text as well.
 *
 * Then it THROWS rather than rendering, exactly as the missing-acceptance-label
 * case above does and for the same reason: the API is about to refuse a
 * submission with no declaration on it, so a card drawing only the Terms box
 * looks like a way in and is not. It is an incomplete publish — the Legal Draft
 * authors both languages in parallel columns precisely so it cannot happen by
 * accident — and the backend's own gate refuses it identically on the passcode
 * door and the interstitial.
 *
 * The extra read costs a request on the rare Google-held terms step, and only
 * where the reader's own text carries no declaration. It buys the one thing
 * worth buying: nobody is ever shown a sign-in card missing a mandatory box.
 */
async function refuseAHalfPublishedDeclaration(step: HeldTerms, locale: AppLocale): Promise<void> {
  if (step.step.adulthoodDeclarationLabel) {
    return;
  }
  const prevailing =
    step.locale === TERMS_PREVAILING_LOCALE ? step : await termsStepIn(TERMS_PREVAILING_LOCALE);
  if (prevailing?.step.adulthoodDeclarationLabel) {
    throw new Error(
      `the current Terms edition asks the adulthood declaration but publishes no label for it ` +
        `in ${locale}; refusing to render a sign-in card missing a mandatory box`,
    );
  }
}

/**
 * A held terms step, and the language its label was actually taken from.
 *
 * The two travel together because the link beside the checkbox must open the
 * document the label came from: a person floored at the prevailing text needs
 * the Spanish page, and sending them to the English one — which is not
 * published in this state — would be a link into a not-found page.
 */
type HeldTerms = { step: PendingTermsStep; locale: AppLocale };

/**
 * The terms step in one language, or null when that language is not published.
 *
 * Null means a 404 and nothing else: every other failure throws out of
 * callBackend and out of this page, because "the label is missing" and "the API
 * is unreachable" must not produce the same card.
 */
async function termsStepIn(locale: AppLocale): Promise<HeldTerms | null> {
  try {
    const envelope = await callBackend<{
      acceptance_label: string;
      // Absent from the payload when this edition carries no
      // `label-adulthood-declaration` artifact (#587, ADR 0069). Its presence
      // is the whole of whether the second box is drawn.
      adulthood_declaration_label?: string;
      version: string;
    }>(`/api/v1/public/terms/${locale}`);
    if (!envelope.data?.acceptance_label) {
      return null;
    }
    return {
      step: {
        acceptanceLabel: envelope.data.acceptance_label,
        adulthoodDeclarationLabel: envelope.data.adulthood_declaration_label ?? null,
        version: envelope.data.version,
        tokenInCookie: true,
      },
      locale,
    };
  } catch (error) {
    if (error instanceof APIError && error.status === 404) {
      return null;
    }
    throw error;
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
      // The document the label beside the checkbox came from, which is the
      // reader's language in the ordinary case and the prevailing one when
      // this edition does not publish theirs (#559). A link across, never a
      // link to a page that is not there.
      termsUrl={storefrontTermsUrl(pendingTerms?.locale ?? locale)}
      initialPendingTerms={pendingTerms?.step ?? null}
    />
  );
}
