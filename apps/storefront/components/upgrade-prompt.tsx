"use client";

/**
 * The Upgrade Prompt: the checkout question that offers to give up a free Ticket
 * so the paid one takes its place (ADR 0074, #652).
 *
 * IT DRAWS AND NOTHING ELSE. Whether it appears at all, and which free Ticket it
 * is about, are lib/checkout-answers.ts's findings (upgradeOffer) — the one
 * place the eligibility arithmetic is written on this side. A component that
 * decided any part of that would be a second copy of a rule the backend also
 * holds, in the file least likely to be read when the rule changes.
 *
 * NEVER A MODAL, NEVER A GATE. It sits in the flow of the dialog like any Ticket
 * Question: it can be scrolled past, and the pay button below it does not know
 * it exists. UNTICKED MEANS KEEP BOTH, and it starts unticked for everybody,
 * because the two answers are not equally recoverable — keeping both is fixable
 * at leisure, while an Upgrade destroys a Ticket silently inside the payment's
 * own transaction. A buyer who did not read this must not lose anything by it.
 *
 * THE SENTENCE ABOVE THE BOX IS ABOUT THE TICKET, NEVER THE MECHANISM. "The free
 * ticket in your basket" and "the free ticket you already have" are different
 * true things and the buyer can tell which is theirs; whether the platform drops
 * a line or reverses an earlier Sale is not something they are told, by ADR
 * 0074, and no copy here may start telling them.
 *
 * It takes its words as a prop and lives at module level, on the convention
 * ConsentCheckbox documents: declared inside the dialog it would be a new
 * component type on every render, and React would remount the input, taking the
 * focus with it mid-interaction.
 *
 * It is drawn as the answer section's sibling — the same card, the same heading
 * weight — because that is what it is to the buyer: one more optional question
 * about their own Ticket, in the run of them, rather than something the dialog
 * raises its voice for.
 */
export function UpgradePrompt({
  checked,
  onChange,
  labels,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  labels: {
    /** The section's heading. */
    title: string;
    /**
     * Which free Ticket this is about — the cart's line, or one already held —
     * and what ticking the box costs. THE TWO ARE NOT THE SAME SENTENCE and must
     * not be flattened into one: a free Ticket already held is destroyed for
     * good, while one merely in the cart is left unbought and can be bought
     * again while stock lasts. Saying "gone for good" of both would be a warning
     * that is false half the times it is shown, and a buyer who acted on it
     * would decline an Upgrade for a reason that did not apply to them.
     */
    situation: string;
    /** The box's own words, the same in both situations. */
    label: string;
    /** What leaving it alone keeps — the one line true in both situations. */
    hint: string;
  };
}) {
  return (
    <div className="space-y-3 rounded-lg border bg-muted/40 p-3">
      <div className="space-y-1">
        <p className="text-sm font-medium">{labels.title}</p>
        {/* Which free Ticket this is about, said before the box asks about it. */}
        <p className="text-sm text-muted-foreground">{labels.situation}</p>
      </div>
      <label
        htmlFor="upgrade-elected"
        className="flex items-start gap-3 rounded-lg border bg-background p-3 text-sm"
      >
        <input
          id="upgrade-elected"
          name="upgrade-elected"
          type="checkbox"
          className="mt-1 h-4 w-4 shrink-0"
          checked={checked}
          onChange={(event) => onChange(event.target.checked)}
        />
        <span className="space-y-1">
          <span className="block">{labels.label}</span>
          <span className="block text-xs text-muted-foreground">{labels.hint}</span>
        </span>
      </label>
    </div>
  );
}
