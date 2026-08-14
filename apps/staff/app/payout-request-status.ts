"use client";

import { useTranslations } from "next-intl";

import { type PayoutRequestStatus, payoutRequestStatusToken } from "@/lib/payout-requests";

/**
 * The one place a staff surface turns a Payout Request status token into a word.
 *
 * Written the way `app/role-name.ts` is written, and for the same reason. The six
 * states are read on four screens now — the organizer's outstanding card, their
 * request history, the operator's queue, an Organization's history and the
 * request detail — and a status rendered per screen is how a reader ends up
 * having to work out whether two words mean one state. The mail says it too
 * (backend/internal/platform/email_content.go), so a notice and the screen it
 * links to must agree.
 *
 * The words live in `payouts` and not in `operator`, even though the operator's
 * screens are the majority of the callers, because the vocabulary belongs to the
 * domain rather than to a surface: `payouts.requestStatusPending` is *En espera*
 * for the organizer waiting and for the operator answering alike. That is the
 * whole of the promise — one state, one word, one place it is written down.
 *
 * It lives under `app/` and not `lib/` on purpose: it reads the catalog, and
 * `lib/` never does (messages/README.md).
 */
const STATUS_KEYS = {
  pending: "requestStatusPending",
  processing: "requestStatusProcessing",
  paid: "requestStatusPaid",
  declined: "requestStatusDeclined",
  cancelled: "requestStatusCancelled",
  failed: "requestStatusFailed",
} as const satisfies Record<PayoutRequestStatus, string>;

/**
 * Reads a status token as the name of that state in the reader's language.
 *
 * A state this client has not been taught comes back exactly as the server named
 * it: the server is the authority on which states exist, and an untranslated
 * word is better than a blank where a status should be — the same floor ADR 0023
 * puts under an error code the catalog has never heard of.
 */
export function usePayoutRequestStatusName(): (status: string) => string {
  const t = useTranslations("payouts");
  return (status: string) => {
    const token = payoutRequestStatusToken(status);
    return token ? t(STATUS_KEYS[token]) : status;
  };
}
