/**
 * The Operator Dashboard Overview's one heading scale and one grid gap (#838),
 * shared by Platform Trends and the cards beneath it so the page reads as one.
 *
 * A section title is never smaller than the headings of the cards under it:
 * the section is text-lg (18px) and each card's CardTitle text-base (16px),
 * which is what Platform Trends' chart cards already used. CardTitle's own
 * text-2xl default is left as it is for the pages that have no section title.
 *
 * Every grid of cards on the Overview takes the same gap, so where two grids
 * are two columns wide their column edges line up.
 */
export const OVERVIEW_SECTION_TITLE = "text-lg";
export const OVERVIEW_CARD_TITLE = "text-base";
export const OVERVIEW_GRID_GAP = "gap-4";
