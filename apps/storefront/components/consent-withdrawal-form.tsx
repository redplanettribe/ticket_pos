"use client";

import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  FormField,
  Input,
} from "@ticket-pos/ui";
import { useLocale, useMessages, useTranslations } from "next-intl";
import { useState, type FormEvent } from "react";

import { Link } from "@/i18n/navigation";
import { apiErrorMessage } from "@/lib/api-errors";

/**
 * Withdrawing a consent without signing in (#270, parent #265, ADR 0039).
 *
 * THREE STEPS AND NO SESSION AT THE END OF THEM. A passcode goes to the address,
 * the passcode is redeemed for a short-lived proof, and that proof is spent on a
 * withdrawal. Nothing in this flow sets a session cookie — the API mints no
 * session for either request — so somebody who exercises a right on a borrowed
 * machine walks away as signed out as they arrived.
 *
 * IT CAN ONLY TAKE CONSENT AWAY. Every control below is a "withdraw this", the
 * request it composes carries `false` for the consents selected and omits the
 * rest, and there is no shape of this form that could ask for a grant. That is
 * this component's half of the guarantee; the API's half is that it refuses
 * anything else on that endpoint, which is what actually makes it true (a form
 * is a courtesy, an endpoint is a guarantee).
 *
 * IT SHOWS NO STORED STATE, deliberately. This surface never reports which
 * consents somebody currently holds: a passcode proves an address, and the page
 * that reads it is being looked at by whoever redeemed the code. Withdrawing
 * something that was never granted is harmless and is recorded honestly as
 * having changed nothing — which is a better trade than publishing a Customer's
 * consent state to a surface this thin. The Customer Area is where somebody
 * signed in sees what is true.
 *
 * NOTHING HAPPENS UNTIL THE LAST BUTTON. Arriving here, and even proving an
 * address here, records nothing at all: reading about a right is not exercising
 * it, and the platform must not write a refusal because somebody looked.
 */

type Step = "email" | "code" | "choose" | "done";

/**
 * One "withdraw this" control.
 *
 * Its own component rather than ConsentCheckbox, which renders a Policy
 * Version's label as evidence (ADR 0036) — these words are this app's copy about
 * an act, not the notice anybody is agreeing to, and passing app copy through
 * the component whose whole contract is "these bytes are what the fingerprint
 * covers" would blur the one distinction that component exists to hold.
 *
 * Declared at module level so React does not remount the input, and its focus,
 * on every render of the form.
 */
function WithdrawalCheckbox({
  id,
  checked,
  onChange,
  label,
  description,
}: {
  id: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  label: string;
  description: string;
}) {
  return (
    <label htmlFor={id} className="flex items-start gap-3 rounded-lg border p-3 text-sm">
      <input
        id={id}
        name={id}
        type="checkbox"
        className="mt-1 h-4 w-4 shrink-0"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="space-y-1">
        <span className="block font-medium">{label}</span>
        <span className="text-muted-foreground block">{description}</span>
      </span>
    </label>
  );
}

type Envelope<T> = {
  data: T | null;
  error: { code: string; message: string } | null;
};

type WithdrawalProof = {
  pending_consent_token: string;
  expires_at: string;
};

type WithdrawalOutcome = {
  withdrawal: {
    marketing_consent: string;
    networking_consent: string;
    withdrew: { marketing_consent: boolean; networking_consent: boolean };
  } | null;
};

/**
 * Why a step did not go through: the API's code, its words, and this app's own
 * sentence for the request that never arrived (ADR 0023).
 */
type WithdrawalError = {
  code: string | null;
  message: string | null;
  fallback: "sendFailed" | "verifyFailed" | "withdrawFailed" | "networkFailed";
};

export function ConsentWithdrawalForm() {
  const t = useTranslations("withdrawConsent");
  const errorCopy = useMessages().errors;
  // The language of the page, sent with the proof so that the confirmation of
  // the withdrawal is written in it — a message about somebody's legal rights is
  // the last one that may arrive in a language they cannot read (ADR 0033).
  const locale = useLocale();

  const [step, setStep] = useState<Step>("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [passcodeSent, setPasscodeSent] = useState(false);
  // The proof, held in component state and nowhere else: it is spent within
  // minutes, and a token in storage is a token that outlives the tab.
  const [token, setToken] = useState("");
  // WHAT TO TAKE AWAY. Both start unticked and nothing in this component ever
  // sets them from stored state — there is none here to read, and a pre-ticked
  // control would be this page deciding for somebody what they came to decide.
  const [withdrawMarketing, setWithdrawMarketing] = useState(false);
  const [withdrawNetworking, setWithdrawNetworking] = useState(false);
  const [withdrew, setWithdrew] = useState({ marketing: false, networking: false });
  const [error, setError] = useState<WithdrawalError | null>(null);
  const [loading, setLoading] = useState(false);

  /** The API's sentence in this page's language, and this page's own when it has none. */
  function errorMessage(failure: WithdrawalError): string {
    // The one code this surface rewords for itself: "this sign-in has expired"
    // is the API's wording, and nobody here is signing in.
    if (failure.code === "PENDING_CONSENT_INVALID") return t("proofExpired");
    return apiErrorMessage(errorCopy, failure) ?? t(failure.fallback);
  }

  async function requestPasscode(address: string) {
    setLoading(true);
    setError(null);
    setPasscodeSent(false);
    try {
      const response = await fetch("/api/customer/auth/request-passcode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        // The same request the sign-in page makes, because it is the same
        // passcode: this feature adds a way to spend a proof of email ownership,
        // never a second way to obtain one.
        body: JSON.stringify({ email: address, locale }),
      });
      const envelope = (await response.json()) as Envelope<unknown>;
      if (!response.ok || envelope.error) {
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "sendFailed",
        });
        return;
      }
      setCode("");
      setPasscodeSent(true);
      setStep("code");
    } catch {
      setError({ code: null, message: null, fallback: "networkFailed" });
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
    setError(null);
    try {
      const response = await fetch("/api/customer/consent/withdrawal/passcode", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, code, locale }),
      });
      const envelope = (await response.json()) as Envelope<WithdrawalProof>;
      if (!response.ok || envelope.error || !envelope.data?.pending_consent_token) {
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "verifyFailed",
        });
        return;
      }
      // Proven, and still signed out. Nothing has been recorded and nothing has
      // been withdrawn: this only says the next request may be made.
      setToken(envelope.data.pending_consent_token);
      setStep("choose");
    } catch {
      setError({ code: null, message: null, fallback: "networkFailed" });
    } finally {
      setLoading(false);
    }
  }

  async function handleWithdraw(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!withdrawMarketing && !withdrawNetworking) return;
    setLoading(true);
    setError(null);
    try {
      const response = await fetch("/api/customer/consent/withdrawal", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          pending_consent_token: token,
          // A selected consent is sent as `false` — the answer that takes it
          // away — and an unselected one is OMITTED rather than sent as true.
          // Absent means "not on this submission" and leaves the consent exactly
          // as it stands; there is no value this form can send that grants
          // anything.
          ...(withdrawMarketing ? { marketing_consent: false } : {}),
          ...(withdrawNetworking ? { networking_consent: false } : {}),
        }),
      });
      const envelope = (await response.json()) as Envelope<WithdrawalOutcome>;
      if (!response.ok || envelope.error) {
        setError({
          code: envelope.error?.code ?? null,
          message: envelope.error?.message ?? null,
          fallback: "withdrawFailed",
        });
        return;
      }
      // What was actually TAKEN AWAY, as the API reports it — never what was
      // asked for. Withdrawing a consent that was already denied succeeds and
      // changes nothing, and telling somebody it had been withdrawn would report
      // a change that did not happen.
      setWithdrew({
        marketing: envelope.data?.withdrawal?.withdrew.marketing_consent ?? false,
        networking: envelope.data?.withdrawal?.withdrew.networking_consent ?? false,
      });
      setStep("done");
    } catch {
      setError({ code: null, message: null, fallback: "networkFailed" });
    } finally {
      setLoading(false);
    }
  }

  const failure = error ? errorMessage(error) : null;

  if (step === "done") {
    const movedSomething = withdrew.marketing || withdrew.networking;
    return (
      <div className="space-y-4">
        <Alert>
          <AlertTitle>{movedSomething ? t("doneTitle") : t("doneNothingTitle")}</AlertTitle>
          <AlertDescription>
            {movedSomething ? t("doneDescription") : t("doneNothingDescription")}
          </AlertDescription>
        </Alert>
        {/* Said after the act, because this is the moment somebody wonders what
            they have just given up. It never claims processing has stopped:
            this platform keeps doing what it does for anybody holding a ticket. */}
        <div className="text-muted-foreground space-y-2 text-sm">
          <p>{t("continues")}</p>
          <p>{t("reversible")}</p>
          <p>{t("notDeletion")}</p>
        </div>
        <p className="text-sm">
          {/* Signed out, still. Going back in is an ordinary sign-in and is
              offered as one rather than performed for them. */}
          <Link href="/signin" className="underline">
            {t("signInLink")}
          </Link>
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {failure ? (
        <Alert variant="destructive">
          <AlertTitle>{t("failedTitle")}</AlertTitle>
          <AlertDescription>{failure}</AlertDescription>
        </Alert>
      ) : null}

      {passcodeSent && !failure && step === "code" ? (
        <p role="status" className="rounded-lg border bg-muted/50 px-4 py-3 text-sm">
          {t("passcodeSent")}
        </p>
      ) : null}

      {step === "email" ? (
        <form className="space-y-4" onSubmit={handleRequestPasscode} noValidate>
          <FormField id="email" label={t("emailLabel")}>
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
            {loading ? t("sending") : t("sendPasscode")}
          </Button>
        </form>
      ) : null}

      {step === "code" ? (
        <form className="space-y-4" onSubmit={handleVerifyPasscode} noValidate>
          <FormField id="code" label={t("passcodeLabel")}>
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
            {loading ? t("verifying") : t("verify")}
          </Button>
          <Button
            type="button"
            variant="secondary"
            className="h-11 w-full"
            disabled={loading}
            onClick={() => void requestPasscode(email)}
          >
            {t("resend")}
          </Button>
          <Button
            type="button"
            variant="ghost"
            className="h-11 w-full"
            disabled={loading}
            onClick={() => {
              setStep("email");
              setCode("");
              setPasscodeSent(false);
              setError(null);
            }}
          >
            {t("differentEmail")}
          </Button>
        </form>
      ) : null}

      {step === "choose" ? (
        <form className="space-y-4" onSubmit={handleWithdraw} noValidate>
          <p className="text-sm">{t("chooseDescription")}</p>
          {/* Two controls, each one a "withdraw this". Ticking both is the whole
              of withdrawing everything — one submission, one act, one record. */}
          <WithdrawalCheckbox
            id="withdraw_marketing_consent"
            checked={withdrawMarketing}
            onChange={setWithdrawMarketing}
            label={t("marketingLabel")}
            description={t("marketingDescription")}
          />
          <WithdrawalCheckbox
            id="withdraw_networking_consent"
            checked={withdrawNetworking}
            onChange={setWithdrawNetworking}
            label={t("networkingLabel")}
            description={t("networkingDescription")}
          />
          <div className="text-muted-foreground space-y-2 text-sm">
            <p>{t("continues")}</p>
            <p>{t("reversible")}</p>
            <p>{t("notDeletion")}</p>
          </div>
          <Button
            type="submit"
            className="h-11 w-full"
            // Nothing selected is nothing to withdraw. The API refuses such a
            // submission on its own account; this is what the person sees.
            disabled={loading || (!withdrawMarketing && !withdrawNetworking)}
            aria-busy={loading}
          >
            {loading ? t("withdrawing") : t("withdraw")}
          </Button>
        </form>
      ) : null}
    </div>
  );
}
