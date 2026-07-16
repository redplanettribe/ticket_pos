import type { ReactNode } from "react";

import { OrgAvatar } from "./org-avatar";

type StorefrontShellProps = {
  children: ReactNode;
  organizationName?: string;
  organizationLogoUrl?: string | null;
};

export function StorefrontShell({ children, organizationName, organizationLogoUrl }: StorefrontShellProps) {
  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="border-b">
        <div className="mx-auto flex w-full max-w-5xl items-center gap-2 px-4 py-4">
          {organizationName ? <OrgAvatar logoUrl={organizationLogoUrl} name={organizationName} /> : null}
          <p className="text-sm font-medium text-muted-foreground">{organizationName ?? "Ticket POS"}</p>
        </div>
      </header>
      <div className="flex-1">{children}</div>
      <footer className="border-t py-6">
        <div className="mx-auto w-full max-w-5xl px-4 text-center text-sm text-muted-foreground">
          <a href="https://ticketpos.example" className="hover:text-foreground">
            Powered by Ticket POS
          </a>
        </div>
      </footer>
    </div>
  );
}
