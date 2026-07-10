import type { ReactNode } from "react";

type StorefrontShellProps = {
  children: ReactNode;
  organizationName?: string;
};

export function StorefrontShell({ children, organizationName }: StorefrontShellProps) {
  return (
    <div className="flex min-h-screen flex-col bg-background">
      <header className="border-b">
        <div className="mx-auto flex w-full max-w-5xl items-center px-4 py-4">
          {organizationName ? (
            <p className="text-sm font-medium text-muted-foreground">{organizationName}</p>
          ) : (
            <p className="text-sm font-medium text-muted-foreground">Ticket POS</p>
          )}
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
