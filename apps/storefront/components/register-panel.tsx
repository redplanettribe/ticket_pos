import { Button, Card, CardContent } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";

import { registrationDestination } from "@/lib/registration";

/**
 * The whole of an externally registered Event's sign-up surface: one Register
 * call to action, and the name of the site it hands the Customer to.
 *
 * It stands in place of the entire tickets section — heading, ticket selection
 * and sticky total alike — rather than beside a trimmed-down version of it. That
 * is the point of replacing the section wholesale: a price, a stepper or a total
 * anywhere on this page would say the platform is selling something, and it is
 * not. There is nothing here to add up.
 *
 * The destination's hostname sits under the button because the Customer is being
 * handed to a stranger and should learn which one before they click rather than
 * after.
 */
export function RegisterPanel({
  registrationUrl,
  hasEnded,
}: {
  /** The Event's Registration Link, or null while it has none. */
  registrationUrl: string | null;
  /** Whether the Event is over, which drops the call to action. */
  hasEnded: boolean;
}) {
  const t = useTranslations("event");
  const destination = registrationDestination(registrationUrl);
  // An ended Event stays reachable and stops offering a way in, mirroring the
  // ticketed branch degrading to a read-only list. A missing or unusable link
  // lands in the same place from the other direction: there is nowhere to send
  // anybody, and a dead button would be worse than none.
  const canRegister = !hasEnded && destination !== null;

  return (
    <section className="mt-8 space-y-4" aria-labelledby="registration-heading">
      <h2 id="registration-heading" className="text-lg font-semibold tracking-tight">
        {t("registrationHeading")}
      </h2>
      <Card>
        <CardContent className="flex flex-col items-start gap-2 p-4">
          {canRegister ? (
            <>
              <Button asChild className="h-11 w-full sm:w-auto">
                {/* The Registration Link leaves this platform, so it opens in a
                    new tab and is fully de-referred, exactly as an outbound link
                    in organizer-authored Markdown is (packages/ui markdown.tsx):
                    it should not hand the opener away, and should not lend the
                    platform's standing to wherever it goes. */}
                <a href={destination.href} target="_blank" rel="noopener noreferrer nofollow">
                  {t("registerCta")}
                </a>
              </Button>
              <p className="text-sm text-muted-foreground">
                {t("registerContinueOn", { host: destination.hostname })}
              </p>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">
              {hasEnded ? t("registrationClosed") : t("registrationUnavailable")}
            </p>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
