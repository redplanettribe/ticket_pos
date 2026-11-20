/**
 * The Operator Dashboard Overview's layout values: its one heading scale and
 * one grid gap (#838), shared by Platform Trends and the cards beneath it so
 * the page reads as one, and the Platform Trends charts' columns (#839).
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

/**
 * The Platform Trends charts' columns (#839): two to a row from lg up, and
 * one alone on the last row spans it, so no chart sits beside an empty cell.
 * The rule is read from the grid's children (the last child and an odd one),
 * not from which figure comes last, so it holds for any number of charts.
 * Below lg every chart is one to a row, as before.
 */
export const OVERVIEW_CHART_COLUMNS = "lg:grid-cols-2 lg:*:last:odd:col-span-2";
