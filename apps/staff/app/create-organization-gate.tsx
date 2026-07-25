"use client";

import { AuthCard, Button } from "@ticket-pos/ui";
import Link from "next/link";

import { LogoutButton } from "@/app/logout-button";

import { CreateOrganizationForm } from "./create-organization-form";

export function CreateOrganizationGate() {
  return (
    <AuthCard
      title="Create your organization"
      description="Name your venue and choose a URL slug for your storefront."
      footer={<LogoutButton />}
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
