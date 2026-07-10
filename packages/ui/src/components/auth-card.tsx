import type { ReactNode } from "react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";

type AuthCardProps = {
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  productName?: string;
};

export function AuthCard({
  title,
  description,
  children,
  footer,
  productName = "Ticket POS",
}: AuthCardProps) {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-muted/40 p-4">
      <p className="mb-6 text-sm font-medium text-muted-foreground">{productName}</p>
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
