"use client";

import { AuthCard, Button } from "@ticket-pos/ui";
import { useTranslations } from "next-intl";
import Link from "next/link";

import { LogoutButton } from "@/app/logout-button";
import { ShellLanguageSwitcher } from "@/app/shell-language-switcher";

import { CreateOrganizationForm } from "./create-organization-form";

/**
 * The FIRST screen a brand-new organizer sees after signing in — the one the
 * Storefront's "Create an event" invitation leads to — and the reason the
 * onboarding surface was the one that mattered most in this batch: it is the
 * very first thing somebody does on the platform, and it used to be the hardest
 * thing to do in a second language.
 *
 * It renders outside the app shell, so it carries the shell's two personal
 * controls itself. The language switcher is the shell one rather than the login
 * one because there is a session now: it writes the stored Staff Locale, which
 * is also what words the mail this person is about to receive (ADR 0041).
 */
export function CreateOrganizationGate() {
  const t = useTranslations("onboarding");

  return (
    <AuthCard
      title={t("gateTitle")}
      description={t("gateDescription")}
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
 *
 * The link sits INSIDE the sentence rather than after it, because in Spanish it
 * does not fall in the same place: the catalog holds the whole sentence with a
 * `<link>` tag around the clickable part, so word order is the translator's
 * problem rather than this file's (messages/README.md).
 */
function DifferentAddressNote() {
  const t = useTranslations("onboarding");

  return (
    <p className="border-t pt-4 text-sm text-muted-foreground">
      {t.rich("differentAddressNote", {
        link: (chunks) => (
          <Button asChild variant="link" className="h-auto p-0 text-sm font-normal">
            <Link href="/login">{chunks}</Link>
          </Button>
        ),
      })}
    </p>
  );
}
