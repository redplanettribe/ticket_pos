"use client";

import { AuthCard, Button } from "@ticket-pos/ui";
import Link from "next/link";

import { LogoutButton } from "@/app/logout-button";
import { ShellLanguageSwitcher } from "@/app/shell-language-switcher";

import { CreateOrganizationForm } from "./create-organization-form";

/**
 * The onboarding gate's own copy is still English: it is the `onboarding`
 * surface, migrated by a later ticket (messages/README.md).
 *
 * The language switcher is here anyway, and deliberately ahead of the copy it
 * will one day switch. This is the FIRST screen a brand-new organizer sees after
 * signing in — the one the Storefront's "Create an event" invitation leads to —
 * and it renders outside the app shell, so without this the person the whole
 * epic is about would have no way to state their language between the sign-in
 * page and their first Organization. It is the shell switcher rather than the
 * login one because there is a session now: this writes the stored Staff Locale,
 * which is also what words the mail they are about to receive.
 */
export function CreateOrganizationGate() {
  return (
    <AuthCard
      title="Create your organization"
      description="Name your venue and choose a URL slug for your storefront."
      footer={
        <div className="flex items-center justify-between gap-4">
          <ShellLanguageSwitcher />
          <LogoutButton />
        </div>
      }
    >
      <CreateOrganizationForm />
      <DifferentAddressNote />
    </AuthCard>
  );
}

/**
 * A Staff Session with no memberships has two possible causes, and this fork
 * only ever named one of them. The other is that the invitation reached a
 * different address: authority comes from a `members` row matching the session's
 * email exactly as normalised, so an alias signs in fine and belongs nowhere
 * (ADR 0011). Offering organization creation as the sole option tells that
 * person their invitation never happened.
 *
 * Kept deliberately quiet — muted, below the form, a text link rather than a
 * button — because creating an Organization is still the primary action and the
 * common case. The wording asks what the reader may have done; it never says an
 * invitation exists elsewhere, which would reveal who this platform knows.
 */
function DifferentAddressNote() {
  return (
    <p className="border-t pt-4 text-sm text-muted-foreground">
      Expecting an invitation? An invitation is tied to the address it was sent to. If yours went
      somewhere else,{" "}
      <Button asChild variant="link" className="h-auto p-0 text-sm font-normal">
        <Link href="/login">sign in with that address</Link>
      </Button>
      .
    </p>
  );
}
