"use client";

import { useTranslations } from "next-intl";

import { type AcceptanceStanding, standingTone } from "@/lib/acceptance-browsers";

// One state, rendered (#565).
//
// ONE COMPONENT FOR BOTH SCREENS, which is the point: the customer browser and
// the staff browser are deliberately two screens with two payloads, but
// `outstanding` must not look like one thing on one of them and another on the
// other. The vocabulary is shared even though nothing else is.
//
// OUTSTANDING IS THE ONLY ONE THAT READS AS A PROBLEM, because it is the only
// state anybody can act on. `never_seen` is the ordinary condition of about a
// third of the customer base — people a box-office sale or a Sale Import
// created who have never acted on a Storefront surface — and drawing it as an
// alarm would make the screen unreadable on the day it matters most. `former`
// is a settled fact about somebody who has left and owes nothing.
// The copy key per state, written out as a literal map rather than derived by a
// helper, because next-intl's `t` takes a KEY LITERAL and typechecks it against
// the catalogue: a computed string would compile only by being widened to
// `string`, which is exactly the check worth keeping. The pure module's
// `standingLabelKey` is the same mapping for the unit test, which asserts the
// four keys exist in both catalogues.
const LABEL_KEY = {
  current: "standingCurrent",
  outstanding: "standingOutstanding",
  never_seen: "standingNeverSeen",
  former: "standingFormer",
} as const;

export function StandingBadge({ standing }: { standing: AcceptanceStanding }) {
  const t = useTranslations("operator.legalAcceptances");
  const tone = standingTone(standing);
  const className =
    tone === "attention"
      ? "bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-100"
      : tone === "positive"
        ? "bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-100"
        : "bg-muted text-muted-foreground";

  return (
    <span className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${className}`}>
      {t(LABEL_KEY[standing])}
    </span>
  );
}
