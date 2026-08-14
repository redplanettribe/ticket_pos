import type { ReactNode } from "react";

import { Badge } from "./ui/badge";
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from "./ui/card";

type InProgressPanelProps = {
  title: string;
  description: string;
  /**
   * Required, and deliberately so. This defaulted to the English "Coming soon"
   * and rendered it as visible copy on a Spanish page — invisible to every
   * guard, because the compiler and the catalog parity test only see keys that
   * were asked for, and the staff app's literal-string rule stops at its own
   * boundary. A required prop makes a new caller a compile error instead.
   */
  badge: string;
  action?: ReactNode;
};

export function InProgressPanel({ title, description, badge, action }: InProgressPanelProps) {
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
