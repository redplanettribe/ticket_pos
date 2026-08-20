import assert from "node:assert/strict";
import test from "node:test";

import { formatMoney } from "./format.ts";
import {
  colorSlotFor,
  cumulativePlotWidth,
  cumulativeTrends,
  drawnTicketTypes,
  formatTakings,
  formatTakingsTick,
  formatTrendsDay,
  hasSales,
  toggleTicketTypeSelection,
  trendsPlotWidth,
  trendsYTicks,
  trendsSeries,
  trendsYMax,
  TRENDS_MIN_BAR_WIDTH,
  TRENDS_MIN_PLOT_WIDTH,
  type SalesTrends,
  type TrendsDay,
  type TrendsTicketType,
} from "./sales-trends.ts";

// A three-tier catalog in display order. VIP sorts last on purpose: the chart
// stacks in catalog order, not in the order sales arrived.
const CATALOG: TrendsTicketType[] = [
  { id: "tt_early", name: "Early Bird", sort_order: 0 },
  { id: "tt_ga", name: "General Admission", sort_order: 1 },
  { id: "tt_vip", name: "VIP", sort_order: 2 },
];

const ALL = CATALOG.map((type) => type.id);

// Three days with a silent one in the middle — the zero-filled day the API
// sends as an empty `lines` array.
const DAYS: TrendsDay[] = [
  {
    date: "2026-08-01",
    lines: [
      { ticket_type_id: "tt_early", quantity: 10, takings_cents: 50_000 },
      { ticket_type_id: "tt_ga", quantity: 4, takings_cents: 40_000 },
    ],
  },
  { date: "2026-08-02", lines: [] },
  {
    date: "2026-08-03",
    lines: [
      { ticket_type_id: "tt_ga", quantity: 3, takings_cents: 30_000 },
      { ticket_type_id: "tt_vip", quantity: 1, takings_cents: 25_000 },
    ],
  },
];

// --- chip selection -------------------------------------------------------

test("clicking a selected chip removes that Ticket Type from the drawn set", () => {
  assert.deepEqual(toggleTicketTypeSelection(CATALOG, ALL, "tt_ga"), ["tt_early", "tt_vip"]);
});

test("clicking a deselected chip puts it back in catalog order, not click order", () => {
  const afterRemoving = toggleTicketTypeSelection(CATALOG, ALL, "tt_early");
  assert.deepEqual(toggleTicketTypeSelection(CATALOG, afterRemoving, "tt_early"), ALL);
});

test("deselecting all but one leaves the single-Ticket-Type view", () => {
  const only = ["tt_early", "tt_ga", "tt_vip"]
    .filter((id) => id !== "tt_vip")
    .reduce((selected, id) => toggleTicketTypeSelection(CATALOG, selected, id), ALL as string[]);
  assert.deepEqual(only, ["tt_vip"]);
});

// An empty chart answers nothing, and the reader deselecting their last chip
// already has the view they were reaching for.
test("the last drawn Ticket Type cannot be deselected", () => {
  assert.deepEqual(toggleTicketTypeSelection(CATALOG, ["tt_vip"], "tt_vip"), ["tt_vip"]);
});

test("the drawn Ticket Types keep catalog display order", () => {
  assert.deepEqual(
    drawnTicketTypes(CATALOG, ["tt_vip", "tt_early"]).map((type) => type.name),
    ["Early Bird", "VIP"],
  );
});

// --- selection to series --------------------------------------------------

test("every day keeps its slot, including the silent one", () => {
  const series = trendsSeries(DAYS, ALL, "quantity", "en");
  assert.deepEqual(
    series.map((datum) => datum.key),
    ["2026-08-01", "2026-08-02", "2026-08-03"],
  );
  assert.equal(series[1].total, 0);
  assert.deepEqual(series[1].values, { tt_early: 0, tt_ga: 0, tt_vip: 0 });
});

test("a Ticket Type that sold nothing on a day is drawn as zero, not as a gap", () => {
  const series = trendsSeries(DAYS, ALL, "quantity", "en");
  assert.equal(series[0].values.tt_vip, 0);
  assert.equal(series[0].values.tt_early, 10);
});

test("a deselected Ticket Type leaves both the segments and the total", () => {
  const series = trendsSeries(DAYS, ["tt_ga", "tt_vip"], "quantity", "en");
  assert.deepEqual(series[0].values, { tt_ga: 4, tt_vip: 0 });
  // Early Bird's 10 tickets are gone from the bar, not merely hidden behind it.
  assert.equal(series[0].total, 4);
  assert.equal(Object.hasOwn(series[0].values, "tt_early"), false);
});

test("the same selection plots Takings when asked for Takings", () => {
  const series = trendsSeries(DAYS, ALL, "takings_cents", "en");
  assert.equal(series[0].total, 90_000);
  assert.equal(series[2].values.tt_vip, 25_000);
});

// --- the Takings series ---------------------------------------------------

// The pair is the feature: one selection, one span of days, two measures. If
// the two charts could ever be handed different days or different Ticket Types,
// reading a day off one and the same day off the other would be a lie.
test("tickets and Takings are the same days and the same Ticket Types, differing only in the figure", () => {
  const selected = ["tt_early", "tt_ga"];
  const tickets = trendsSeries(DAYS, selected, "quantity", "en");
  const takings = trendsSeries(DAYS, selected, "takings_cents", "en");

  assert.deepEqual(
    takings.map((datum) => datum.key),
    tickets.map((datum) => datum.key),
  );
  assert.deepEqual(
    takings.map((datum) => Object.keys(datum.values)),
    tickets.map((datum) => Object.keys(datum.values)),
  );
  assert.deepEqual(tickets[0].values, { tt_early: 10, tt_ga: 4 });
  assert.deepEqual(takings[0].values, { tt_early: 50_000, tt_ga: 40_000 });
});

test("deselecting a Ticket Type takes its Takings out of the bar, not merely out of sight", () => {
  const takings = trendsSeries(DAYS, ["tt_ga", "tt_vip"], "takings_cents", "en");
  assert.equal(takings[0].total, 40_000);
  assert.equal(Object.hasOwn(takings[0].values, "tt_early"), false);
});

test("a silent day has zero Takings rather than losing its slot", () => {
  const takings = trendsSeries(DAYS, ALL, "takings_cents", "en");
  assert.equal(takings[1].key, "2026-08-02");
  assert.equal(takings[1].total, 0);
});

// A comp tier moves stock and earns nothing. It has to be in the tickets chart
// — the tickets were really given out — and it has to be flat in the Takings
// chart, or a free Event would look like it made money.
test("a free Ticket Type counts tickets and contributes nothing to Takings", () => {
  const catalog: TrendsTicketType[] = [
    ...CATALOG,
    { id: "tt_comp", name: "Comp", sort_order: 3 },
  ];
  const selected = catalog.map((type) => type.id);
  const days: TrendsDay[] = [
    {
      date: "2026-08-01",
      lines: [
        { ticket_type_id: "tt_ga", quantity: 2, takings_cents: 20_000 },
        { ticket_type_id: "tt_comp", quantity: 6, takings_cents: 0 },
      ],
    },
  ];

  const tickets = trendsSeries(days, selected, "quantity", "en");
  assert.equal(tickets[0].values.tt_comp, 6);
  assert.equal(tickets[0].total, 8);

  const takings = trendsSeries(days, selected, "takings_cents", "en");
  assert.equal(takings[0].values.tt_comp, 0);
  assert.equal(takings[0].total, 20_000);
});

test("an Event that only gave tickets away charts its tickets and no Takings", () => {
  const days: TrendsDay[] = [
    { date: "2026-08-01", lines: [{ ticket_type_id: "tt_ga", quantity: 40, takings_cents: 0 }] },
  ];
  assert.equal(trendsSeries(days, ALL, "quantity", "en")[0].total, 40);
  assert.equal(trendsSeries(days, ALL, "takings_cents", "en")[0].total, 0);
  // The Takings axis still draws a scale rather than collapsing to a line.
  assert.equal(trendsYMax(trendsSeries(days, ALL, "takings_cents", "en")), 1);
});

// --- Y-max derivation -----------------------------------------------------

test("the Y max covers the tallest stack of the whole catalog", () => {
  // Aug 1 is the tallest day at 14 tickets; the axis rounds up to clear it.
  assert.equal(trendsYMax(trendsSeries(DAYS, ALL, "quantity", "en")), 20);
});

// The point of the chips: the remaining bars get the full height of the chart.
test("deselecting the dominant Ticket Type rescales the Y axis down", () => {
  const withEarly = trendsYMax(trendsSeries(DAYS, ALL, "quantity", "en"));
  const withoutEarly = trendsYMax(trendsSeries(DAYS, ["tt_ga", "tt_vip"], "quantity", "en"));
  // Without Early Bird the tallest day is 4 tickets, so the axis drops to 5.
  assert.equal(withoutEarly, 5);
  assert.ok(withoutEarly < withEarly);
});

test("the Y max rounds up to a tick a person reads without counting", () => {
  assert.equal(trendsYMax([datum(0)]), 1);
  assert.equal(trendsYMax([datum(7)]), 10);
  assert.equal(trendsYMax([datum(11)]), 20);
  assert.equal(trendsYMax([datum(230)]), 250);
  assert.equal(trendsYMax([datum(6_400)]), 10_000);
});

test("an all-zero selection still draws a scale rather than collapsing", () => {
  assert.equal(trendsYMax(trendsSeries([{ date: "2026-08-02", lines: [] }], ALL, "quantity", "en")), 1);
  assert.equal(trendsYMax([]), 1);
});

// One selection, two axes. The charts share their chips and their days, but a
// ticket count and a money figure have nothing to say to each other about
// scale, so each one is derived from its own data.
test("the Takings axis scales to Takings, independently of the tickets axis", () => {
  const tickets = trendsYMax(trendsSeries(DAYS, ALL, "quantity", "en"));
  const takings = trendsYMax(trendsSeries(DAYS, ALL, "takings_cents", "en"));
  assert.equal(tickets, 20); // 14 tickets on the tallest day
  assert.equal(takings, 100_000); // $900.00 on the tallest day
});

test("deselecting a Ticket Type rescales the Takings axis on its own terms", () => {
  // VIP is a small pile of tickets and most of the money. Dropping it barely
  // moves the tickets axis and collapses the Takings one, which is exactly the
  // asymmetry the two axes exist to show.
  const days: TrendsDay[] = [
    {
      date: "2026-08-01",
      lines: [
        { ticket_type_id: "tt_ga", quantity: 40, takings_cents: 40_000 },
        { ticket_type_id: "tt_vip", quantity: 4, takings_cents: 400_000 },
      ],
    },
  ];
  const selected = ["tt_ga", "tt_vip"];

  assert.equal(trendsYMax(trendsSeries(days, selected, "quantity", "en")), 50);
  assert.equal(trendsYMax(trendsSeries(days, selected, "takings_cents", "en")), 500_000);

  // The tickets axis hardly notices four tickets leaving; the Takings axis
  // drops by a factor of ten. Neither is derived from the other.
  assert.equal(trendsYMax(trendsSeries(days, ["tt_ga"], "quantity", "en")), 50);
  assert.equal(trendsYMax(trendsSeries(days, ["tt_ga"], "takings_cents", "en")), 50_000);
});

function datum(total: number) {
  return { key: "d", label: "d", values: { only: total }, total };
}

test("the Y axis is labelled in even steps that end on its top", () => {
  // Left to the library the step is chosen without reference to the top, so an
  // axis topped at 50 comes out 0, 15, 30, 45, 50 — unequal gaps and the last
  // two labels almost touching.
  assert.deepEqual(trendsYTicks(50), [0, 10, 20, 30, 40, 50]);
  assert.deepEqual(trendsYTicks(20), [0, 4, 8, 12, 16, 20]);
  // Takings are counted in cents, so a $50 day is a top of 5000.
  assert.deepEqual(trendsYTicks(5000), [0, 1000, 2000, 3000, 4000, 5000]);
  for (const top of [1, 2, 5, 10, 20, 25, 50, 100, 250, 5000]) {
    const ticks = trendsYTicks(top);
    assert.equal(ticks[0], 0, `${top} starts at zero`);
    assert.equal(ticks[ticks.length - 1], top, `${top} ends on the top`);
    assert.equal(new Set(ticks).size, ticks.length, `${top} repeats no label`);
  }
});

test("a top too small to divide is labelled at its ends alone", () => {
  // A one-ticket day. Fractions of a ticket on the axis would be worse than two
  // labels, and trendsYMax floors at 1 so this is a real case.
  assert.deepEqual(trendsYTicks(1), [0, 1]);
  assert.deepEqual(trendsYTicks(0), [0]);
});

// --- how wide a span is drawn --------------------------------------------

// The plot's share of a card, i.e. the card measured less the chart's own
// chrome. The plot takes the larger of this and what the span demands: the span
// winning is the scrolling case, this winning is the ordinary one.
const A_CARD = 800;

test("a day is owed the same room at every range", () => {
  const young = trendsPlotWidth(30);
  const old = trendsPlotWidth(400);
  assert.equal(young / 30, TRENDS_MIN_BAR_WIDTH);
  assert.equal(old / 400, TRENDS_MIN_BAR_WIDTH);
  // Which is the whole argument against rebucketing: a reader who learned the
  // chart on the young Event is reading the same object on the old one.
  assert.equal(young / 30, old / 400);
});

test("a span longer than the card grows past it rather than compressing into it", () => {
  assert.ok(trendsPlotWidth(400, A_CARD) > A_CARD);
  // The card cannot take days away: a narrower window loses none, and every day
  // keeps the room it is owed however little space there is.
  assert.equal(trendsPlotWidth(400, A_CARD), 400 * TRENDS_MIN_BAR_WIDTH);
  assert.equal(trendsPlotWidth(400, 300), 400 * TRENDS_MIN_BAR_WIDTH);
});

test("a span the card can hold fills it rather than sitting in one corner", () => {
  // Three weeks needs less room than the card has. Drawn at its own width it
  // would occupy the left half and leave the axis stopping short of the card's
  // edge, which reads as the chart having been shoved aside — so it takes the
  // whole width instead. The bars do not fatten to fill it: the chart caps a
  // bar's width, so the extra room goes to the gaps between them.
  assert.ok(21 * TRENDS_MIN_BAR_WIDTH < A_CARD);
  assert.equal(trendsPlotWidth(21, A_CARD), A_CARD);
});

test("an unmeasured card leaves the span to decide alone", () => {
  // The first render and the server-rendered pass have measured nothing. The
  // chart must still draw something sensible rather than collapse to the floor.
  assert.equal(trendsPlotWidth(400, 0), 400 * TRENDS_MIN_BAR_WIDTH);
  assert.equal(trendsPlotWidth(400), 400 * TRENDS_MIN_BAR_WIDTH);
});

test("a few days still get a plot to sit in", () => {
  // A two-day-old Event drawn two days wide reads as a broken chart rather than
  // as a new Event. The floor is room, not fatter bars: the chart caps a bar's
  // width, so these are ordinary bars with space around them.
  assert.equal(trendsPlotWidth(2), TRENDS_MIN_PLOT_WIDTH);
  assert.equal(trendsPlotWidth(0), TRENDS_MIN_PLOT_WIDTH);
});

test("several hundred days is several hundred bars, one per day", () => {
  const span = longSpan(400);
  const data = trendsSeries(span, ALL, "quantity", "en");
  // No bucket is ever shared between two days, at any length of span.
  assert.equal(data.length, span.length);
  assert.equal(new Set(data.map((entry) => entry.key)).size, span.length);
  assert.equal(trendsPlotWidth(span.length), span.length * TRENDS_MIN_BAR_WIDTH);
});

/** A contiguous, zero-filled span of `count` days, as the API sends one. */
function longSpan(count: number): TrendsDay[] {
  const start = Date.UTC(2026, 0, 1);
  return Array.from({ length: count }, (_, index) => {
    const day = new Date(start + index * 86_400_000).toISOString().slice(0, 10);
    return {
      date: day,
      lines: [{ ticket_type_id: "tt_ga", quantity: index % 7, takings_cents: (index % 7) * 2_500 }],
    };
  });
}

// --- Takings in the Organization's currency -------------------------------

// The claim is reuse, not resemblance: the tooltip must spell money exactly as
// the Sales tab's Net proceeds strip does, or the surfaces the note asks a
// reader to compare would not even be comparable at a glance. Both now go
// through lib/format.ts, which is the one place either can be changed.
test("Takings is spelled with the staff app's one money formatter", () => {
  assert.equal(formatTakings(125_000, "USD", "en"), formatMoney(125_000, "USD", "en"));
  assert.equal(formatTakings(0, "USD", "es"), formatMoney(0, "USD", "es"));
});

test("Takings is stated in the Organization's currency, not a fixed one", () => {
  assert.notEqual(formatTakings(125_000, "EUR", "en"), formatTakings(125_000, "USD", "en"));
  assert.equal(formatTakings(125_000, "EUR", "en"), formatMoney(125_000, "EUR", "en"));
});

// THE RULE (CONTEXT.md, ADR 0041): the reader's language decides the marks and
// decides nothing about the money. The same Takings figure in the same currency
// is the same amount in both languages, spelled with each one's own marks.
test("the reader's language moves the marks around Takings and not the currency", () => {
  const english = formatTakings(125_000, "USD", "en");
  const spanish = formatTakings(125_000, "USD", "es");
  assert.notEqual(english, spanish);
  assert.ok(english.includes("1,250"));
  assert.ok(spanish.includes("1.250"));
  // The currency travelled with the amount, not with the reader.
  assert.equal(formatTakings(125_000, "USD", "es"), formatMoney(125_000, "USD", "es"));
});

// The Y axis is a fixed width so the two charts line up. A tick label that
// outgrew it would push the plot area and break the alignment the pair exists
// for, so a big day abbreviates rather than widening the axis.
test("an axis tick stays short enough for the fixed axis, however big the day", () => {
  const millionaire = 100_000_000; // $1,000,000.00
  for (const locale of ["en", "es"] as const) {
    assert.ok(formatTakingsTick(millionaire, "USD", locale).length <= 10);
    assert.ok(
      formatTakingsTick(millionaire, "USD", locale).length <
        formatTakings(millionaire, "USD", locale).length,
    );
  }
});

test("an axis tick still names the currency", () => {
  assert.notEqual(formatTakingsTick(125_000, "EUR", "en"), formatTakingsTick(125_000, "USD", "en"));
});

test("a zero tick is drawn as zero money, not as a blank", () => {
  assert.ok(formatTakingsTick(0, "USD", "en").includes("0"));
  assert.ok(formatTakingsTick(0, "USD", "es").includes("0"));
});

// --- day labels and the empty state --------------------------------------

// The API has already resolved the day into the Event's timezone. Re-reading it
// in the viewer's zone is what would slide a bar onto the wrong day.
test("a day label reads the calendar date, whatever zone the viewer is in", () => {
  const previous = process.env.TZ;
  try {
    process.env.TZ = "Pacific/Kiritimati"; // UTC+14
    assert.equal(formatTrendsDay("2026-08-01", "en"), "Aug 1");
    process.env.TZ = "Pacific/Niue"; // UTC-11
    assert.equal(formatTrendsDay("2026-08-01", "en"), "Aug 1");
  } finally {
    process.env.TZ = previous;
  }
});

// The month name is the reader's, and the day it names is still the API's. This
// is the whole of what the Staff Locale is allowed to change about a bar.
test("a day label is written in the reader's language and stays the same day", () => {
  const spanish = formatTrendsDay("2026-08-01", "es");
  assert.notEqual(spanish, formatTrendsDay("2026-08-01", "en"));
  assert.ok(spanish.includes("1"));
  assert.ok(spanish.toLowerCase().includes("ago"));
});

test("an unreadable date is shown as it arrived rather than as Invalid Date", () => {
  assert.equal(formatTrendsDay("not-a-date", "en"), "not-a-date");
});

test("an Event that has sold nothing has nothing to chart", () => {
  assert.equal(hasSales(trends([])), false);
  // An Event with External Registration sells no Ticket Sale, so it lands here
  // too — and a span of silent days is just as empty as no span at all.
  assert.equal(hasSales(trends([{ date: "2026-08-02", lines: [] }])), false);
  assert.equal(hasSales(trends(DAYS)), true);
});

test("every Ticket Type draws in a slot of its own, even when the catalog never set an order", () => {
  // sort_order defaults to 0 in the schema, so an Organization that never
  // reordered its Ticket Types has every one of them at 0. Keying colour on
  // sort_order itself would give this whole catalog one colour and a stack
  // nobody could read.
  const unordered = [
    { id: "tt_a", name: "Early Bird", sort_order: 0 },
    { id: "tt_b", name: "General Admission", sort_order: 0 },
    { id: "tt_c", name: "VIP", sort_order: 0 },
  ];
  const slots = unordered.map((type) => colorSlotFor(unordered, type.id));
  assert.deepEqual(slots, [0, 1, 2]);
  assert.equal(new Set(slots).size, unordered.length);
});

test("a Ticket Type keeps its slot whichever chips are switched off", () => {
  // The slot is read off the whole catalog, never the drawn subset, so
  // deselecting a type must not recolour the ones still on screen.
  const before = CATALOG.map((type) => colorSlotFor(CATALOG, type.id));
  const drawn = drawnTicketTypes(CATALOG, ["tt_vip"]);
  assert.equal(colorSlotFor(CATALOG, "tt_vip"), before[2]);
  assert.equal(drawn.length, 1);
});

function trends(days: TrendsDay[]): SalesTrends {
  return {
    timezone: "America/Guayaquil",
    currency: "USD",
    ticket_types: CATALOG,
    days,
    reversed_count: 0,
  };
}

// --- the Cumulative view --------------------------------------------------

// The Cumulative view is the same matrix counted differently: each day carries
// the running total up to and including itself, rather than its own takings.
// It is built by accumulating the Daily view, so the two can never disagree
// about which days exist, which Ticket Types are drawn, or what a day is called.

test("each day carries the total up to it, not the day's own count", () => {
  const daily = trendsSeries(DAYS, ALL, "quantity", "en");
  const running = cumulativeTrends(daily);
  // 14 sold on the 1st, nothing on the 2nd, 4 more on the 3rd.
  assert.deepEqual(
    running.map((datum) => datum.total),
    [14, 14, 18],
  );
  assert.deepEqual(
    daily.map((datum) => datum.total),
    [14, 0, 4],
  );
});

test("a silent day holds the total it inherited rather than dropping to zero", () => {
  const running = cumulativeTrends(trendsSeries(DAYS, ALL, "quantity", "en"));
  // The whole point of the view: a day nobody bought on is a flat stretch, not
  // a hole. A curve that fell back to zero would say the Event un-sold them.
  assert.equal(running[1]?.total, running[0]?.total);
});

test("the running total never falls, whatever the days did", () => {
  const running = cumulativeTrends(trendsSeries(longSpan(120), ALL, "takings_cents", "en"));
  for (let index = 1; index < running.length; index += 1) {
    assert.ok(
      (running[index]?.total ?? 0) >= (running[index - 1]?.total ?? 0),
      `day ${index} fell below the day before it`,
    );
  }
});

test("the running total is kept per Ticket Type, so the stack still says who sold what", () => {
  const running = cumulativeTrends(trendsSeries(DAYS, ALL, "quantity", "en"));
  // GA sold on both busy days and is the only type that did; Early Bird stops
  // contributing after the 1st but never leaves the stack it is already in.
  assert.deepEqual(
    running.map((datum) => datum.values.tt_ga),
    [4, 4, 7],
  );
  assert.deepEqual(
    running.map((datum) => datum.values.tt_early),
    [10, 10, 10],
  );
  assert.deepEqual(
    running.map((datum) => datum.values.tt_vip),
    [0, 0, 1],
  );
});

test("a day's segments still sum to its stack", () => {
  const running = cumulativeTrends(trendsSeries(DAYS, ALL, "takings_cents", "en"));
  for (const datum of running) {
    const summed = Object.values(datum.values).reduce((sum, value) => sum + value, 0);
    assert.equal(summed, datum.total);
  }
});

test("the last day of the span is the whole span's total", () => {
  const daily = trendsSeries(DAYS, ALL, "takings_cents", "en");
  const whole = daily.reduce((sum, datum) => sum + datum.total, 0);
  assert.equal(cumulativeTrends(daily).at(-1)?.total, whole);
  assert.equal(whole, 145_000);
});

test("a deselected Ticket Type never enters the running total", () => {
  // Accumulating what is drawn, rather than drawing a slice of an accumulation:
  // a hidden Ticket Type must be genuinely absent from the curve, not merely
  // invisible inside a total it is still propping up.
  const running = cumulativeTrends(trendsSeries(DAYS, ["tt_ga"], "quantity", "en"));
  assert.deepEqual(
    running.map((datum) => datum.total),
    [4, 4, 7],
  );
  assert.deepEqual(Object.keys(running.at(-1)?.values ?? {}), ["tt_ga"]);
});

test("the days, their order and their labels are the Daily view's own", () => {
  const daily = trendsSeries(DAYS, ALL, "quantity", "en");
  const running = cumulativeTrends(daily);
  // Switching view must move the reader along the same axis they were reading:
  // the same days, in the same places, called the same things.
  assert.deepEqual(
    running.map((datum) => datum.key),
    daily.map((datum) => datum.key),
  );
  assert.deepEqual(
    running.map((datum) => datum.label),
    daily.map((datum) => datum.label),
  );
});

test("accumulating leaves the Daily view untouched", () => {
  // Both views are derived from one matrix on every render; accumulating in
  // place would make the daily bars grow every time the toggle was pressed.
  const daily = trendsSeries(DAYS, ALL, "quantity", "en");
  cumulativeTrends(daily);
  assert.deepEqual(
    daily.map((datum) => datum.total),
    [14, 0, 4],
  );
  assert.equal(daily[1]?.values.tt_early, 0);
});

test("a span with nothing in it accumulates to nothing", () => {
  assert.deepEqual(cumulativeTrends([]), []);
});

test("the axis of a cumulative chart tops its final total, not its biggest day", () => {
  // A long span is where the two scales part company: the tallest single day is
  // a fraction of what the whole span added up to, so an axis built for the
  // Daily view would leave most of the curve above the top of the chart.
  const daily = trendsSeries(longSpan(120), ALL, "quantity", "en");
  const running = cumulativeTrends(daily);
  const finalTotal = running.at(-1)?.total ?? 0;
  assert.ok(trendsYMax(running) >= finalTotal);
  assert.ok(trendsYMax(daily) < finalTotal);
});

// --- how wide the Cumulative view is drawn --------------------------------

test("the whole span is drawn inside the card rather than scrolled through", () => {
  // The Daily view gives every day a minimum width and lets the plot outgrow
  // the card, because a day is a thing to be read one at a time. The Cumulative
  // view is read as a shape — how fast, and from when — and a shape you have to
  // scroll to finish is not a shape. So a year fits in the card exactly as a
  // fortnight does.
  assert.equal(cumulativePlotWidth(A_CARD), A_CARD);
  assert.ok(trendsPlotWidth(400, A_CARD) > cumulativePlotWidth(A_CARD));
});

test("the cumulative width answers to the card alone, at any length of span", () => {
  // Nothing about the span reaches it: the same card draws a month and a decade
  // at one width, which is what makes the toggle a change of counting rather
  // than a change of scale. Stated against the Daily view, where the same two
  // spans differ by two orders of magnitude.
  assert.equal(cumulativePlotWidth(A_CARD), A_CARD);
  assert.notEqual(trendsPlotWidth(30, A_CARD), trendsPlotWidth(3000, A_CARD));
  assert.equal(cumulativePlotWidth(640), 640);
});

test("an unmeasured card still gives the curve somewhere to sit", () => {
  // The first client frame and the server-rendered pass have measured nothing.
  // Here the floor is the whole answer, because no span is going to raise it.
  assert.equal(cumulativePlotWidth(0), TRENDS_MIN_PLOT_WIDTH);
  assert.equal(cumulativePlotWidth(), TRENDS_MIN_PLOT_WIDTH);
});

test("a card narrower than the floor scrolls rather than squeezing the curve", () => {
  // A very narrow card is the one case the Cumulative view overflows, and it
  // overflows to the same floor the Daily view has. Below that width a chart is
  // unreadable in either view, and the axis alone would fill it.
  assert.equal(cumulativePlotWidth(120), TRENDS_MIN_PLOT_WIDTH);
});
