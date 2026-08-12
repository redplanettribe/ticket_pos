"use client";

import { Markdown, cn } from "@ticket-pos/ui";

/**
 * One consent checkbox, drawn unticked, with its label as the API worded it.
 *
 * THE LABEL IS EVIDENCE, not copy. It is markdown from the current Policy
 * Version, so it is rendered rather than interpolated into a sentence of this
 * app's own: the edition's fingerprint covers exactly these bytes, and what a
 * Customer is shown here has to be what the platform will later claim they
 * accepted (ADR 0036). Nothing in this component may reword it.
 *
 * Only the "Optional" chip belongs to this app, and it arrives as a prop rather
 * than from a translator in scope, so this component can live at module level.
 * That is not a style preference: declared inside a form component it would be a
 * new component type on every render, and React would remount the input — taking
 * the focus with it, mid-interaction.
 *
 * SHARED BY BOTH CAPTURE SURFACES, and it did not start that way. The sign-in
 * consent step and the checkout dialog were built in parallel by two tickets
 * that could not see each other, and each grew its own copy; the comment
 * defending that said a third surface would be the moment to lift them out. What
 * actually settled it was a fix to one being hand-copied into the other, which
 * is the drift the copies were supposed to be too far apart to suffer. The words
 * were never the risk — those come from one API read on both — but the shape of
 * the thing rendering them was.
 */
export function ConsentCheckbox({
  id,
  checked,
  onChange,
  label,
  optionalLabel,
  className,
}: {
  id: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  /** The box's label, as markdown, exactly as the Policy Version words it. */
  label: string;
  /** The "Optional" chip's words, or null on the required box. */
  optionalLabel: string | null;
  /** Surface-specific trim — the checkout dialog fills its rows, the card does not. */
  className?: string;
}) {
  return (
    <label
      htmlFor={id}
      className={cn("flex items-start gap-3 rounded-lg border p-3 text-sm", className)}
    >
      <input
        id={id}
        name={id}
        type="checkbox"
        className="mt-1 h-4 w-4 shrink-0"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="space-y-1">
        {optionalLabel ? (
          <span className="block text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {optionalLabel}
          </span>
        ) : null}
        <Markdown className="text-sm [&>p]:mt-0">{label}</Markdown>
      </span>
    </label>
  );
}
