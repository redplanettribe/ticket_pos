import type { ReactNode } from "react";

import { Logo } from "./logo";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";

type AuthCardProps = {
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
};

export function AuthCard({ title, description, children, footer }: AuthCardProps) {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-muted/40 p-4">
      <Logo withWordmark className="mb-6 text-lg text-primary" markClassName="size-7" />
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="text-xl">{title}</CardTitle>
          {description ? <CardDescription>{description}</CardDescription> : null}
        </CardHeader>
        <CardContent className="space-y-4">{children}</CardContent>
      </Card>
      {footer ? <div className="mt-6">{footer}</div> : null}
    </main>
  );
}
