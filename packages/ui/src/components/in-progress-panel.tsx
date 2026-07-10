import type { ReactNode } from "react";

import { Badge } from "./ui/badge";
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from "./ui/card";

type InProgressPanelProps = {
  title: string;
  description: string;
  badge?: string;
  action?: ReactNode;
};

export function InProgressPanel({
  title,
  description,
  badge = "Coming soon",
  action,
}: InProgressPanelProps) {
  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle>{title}</CardTitle>
          <Badge variant="warning">{badge}</Badge>
        </div>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      {action ? <CardFooter>{action}</CardFooter> : null}
    </Card>
  );
}
