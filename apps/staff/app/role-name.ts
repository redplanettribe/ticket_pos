"use client";

import { useTranslations } from "next-intl";

/**
 * The one place a staff surface turns an API role token into a word.
 *
 * The words themselves are coined once, in CONTEXT.md, and the catalog follows
 * it under `shell.roleOrgAdmin` and its two siblings: *Administrador de la
 * organización*, *Responsable del evento*, *Personal del evento*. That is not
 * decoration — these names had no Spanish anywhere before ADR 0041, and
 * translated ad hoc as each screen was migrated there would have been three
 * words for Event Staff by the third page.
 *
 * So this module is the guarantee rather than a convenience: the membership
 * list, the organization switcher, the Members card and the Event access card
 * all read a role through `useRoleName`, and none of them holds a label map of
 * its own. A role can therefore not acquire a second Spanish word without
 * changing the catalog, which is one edit in one place.
 *
 * It lives under `app/` and not `lib/` on purpose: it reads the catalog, and
 * `lib/` never does (messages/README.md).
 */
const ROLE_KEYS: Record<string, "roleOrgAdmin" | "roleEventOwner" | "roleEventStaff"> = {
  org_admin: "roleOrgAdmin",
  event_owner: "roleEventOwner",
  event_staff: "roleEventStaff",
};

/**
 * The roles a Member can hold in an Organization, in the order they are
 * offered — widest authority first, which is also the order CONTEXT.md
 * introduces them in.
 */
export const MEMBER_ROLES = ["org_admin", "event_owner", "event_staff"] as const;

/**
 * The roles an Event assignment can grant. An Org Admin already has authority
 * over every Event in the Organization and needs no assignment (CONTEXT.md), so
 * it is not on offer here.
 */
export const ASSIGNMENT_ROLES = ["event_owner", "event_staff"] as const;

/**
 * Reads a role token as the name of that role in the reader's language.
 *
 * A role the API adds later that nobody has translated falls back to the token
 * with its underscore rubbed out, which is exactly what every call site did for
 * every role before ADR 0041. An unfamiliar role reads oddly; it does not read
 * blank.
 */
export function useRoleName(): (role: string) => string {
  const t = useTranslations("shell");
  return (role: string) => {
    const key = ROLE_KEYS[role];
    return key ? t(key) : role.replace("_", " ");
  };
}
