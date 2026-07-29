import { cloneElement, isValidElement, type ReactElement, type ReactNode } from "react";

import { Label } from "./ui/label";
import { cn } from "../lib/utils";

type FormFieldProps = {
  id: string;
  label: string;
  children: ReactNode;
  error?: string | null;
  description?: string;
  className?: string;
};

export function FormField({ id, label, children, error, description, className }: FormFieldProps) {
  const errorId = error ? `${id}-error` : undefined;
  const descriptionId = description ? `${id}-description` : undefined;
  const describedBy = [descriptionId, errorId].filter(Boolean).join(" ") || undefined;

  const control =
    isValidElement(children) ?
      cloneElement(children as ReactElement<{ id?: string; "aria-describedby"?: string; "aria-invalid"?: boolean }>, {
        id,
        "aria-describedby": describedBy,
        "aria-invalid": error ? true : undefined,
      })
    : children;

  // The description reads under the control, not between the label and it: the
  // label stays attached to the field it names, and fields laid out side by
  // side keep their inputs on one line however long each hint runs.
  return (
    <div className={cn("space-y-2", className)}>
      <Label htmlFor={id}>{label}</Label>
      {control}
      {description ? (
        <p id={descriptionId} className="text-sm text-muted-foreground">
          {description}
        </p>
      ) : null}
      {error ? (
        <p id={errorId} role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}
