// The span of a trends reading, as every trends surface of the staff app
// addresses it: Platform Trends on the Operator Dashboard's Overview and
// Organization Trends on Home alike, as backend/internal/platform/trendspan
// is the span arithmetic both reads share.
//
// A reading is named by its span, the two days of a custom range and its
// comparison, all carried in the page's address so a reload or a shared link
// shows the same reading. Each surface names its own presets and comparisons
// (a `TrendSpanScheme`); reading the address, writing it back and moving from
// one reading to the next are shared here. A scheme may have an all time
// span, which reaches back to the first day and has no comparison.
//
// Relative imports carry the ".ts" extension so the module graph resolves
// under `node --test` as well as the bundler.

/** The days of a span or of a bucket, both ends included, as YYYY-MM-DD. */
export type TrendDays = { first_day: string; last_day: string };

/**
 * Which reading is on screen. `from` and `to` are calendar days
 * ("YYYY-MM-DD") for a custom span and "" for every other.
 */
export type TrendSpanView<Span extends string, Comparison extends string> = {
  span: Span;
  from: string;
  to: string;
  compare: Comparison;
};

/** The raw query params a reading is read from, as Next hands them over: a
 * parameter written twice arrives as an array. */
export type TrendSpanSearchParams = {
  span?: string | string[];
  from?: string | string[];
  to?: string | string[];
  compare?: string | string[];
};

/** What one surface's readings may be. */
export type TrendSpanScheme<Span extends string, Comparison extends string> = {
  /** The spans, in the order the select offers them; "custom" among them. */
  spans: readonly Span[];
  /** The comparisons, in the order the select offers them. */
  comparisons: readonly Comparison[];
  /** What a bare address shows. Never written to the address. */
  defaultView: TrendSpanView<Span, Comparison>;
  /** The span that reaches back to the first day and so has no comparison,
   * or null on a surface that offers none. Its `compare` is the default,
   * never written. */
  allTime: Span | null;
};

/** The first value of a query param, trimmed: a parameter written twice
 * arrives as an array. */
export function firstParam(raw: string | string[] | undefined): string {
  return ((Array.isArray(raw) ? raw[0] : raw) ?? "").trim();
}

const CALENDAR_DAY = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Whether a string is a day that exists on the calendar, spelled YYYY-MM-DD. */
export function isCalendarDay(value: string): boolean {
  const match = CALENDAR_DAY.exec(value);
  if (!match) return false;
  const [year, month, day] = [Number(match[1]), Number(match[2]), Number(match[3])];
  const parsed = new Date(Date.UTC(year, month - 1, day));
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day;
}

/**
 * parseTrendSpanParams reads the reading off the address. A span or a
 * comparison the scheme does not know falls back to the default, each on its
 * own, and so does a custom span without two real calendar days: a mistyped
 * address still shows a reading. What the two days of a custom range may be
 * (their order, how far they reach) is the API's to say, and its refusal is
 * shown in the section.
 */
export function parseTrendSpanParams<Span extends string, Comparison extends string>(
  scheme: TrendSpanScheme<Span, Comparison>,
  searchParams: TrendSpanSearchParams,
): TrendSpanView<Span, Comparison> {
  const { defaultView } = scheme;
  const compareRaw = firstParam(searchParams.compare) as Comparison;
  const compare = scheme.comparisons.includes(compareRaw) ? compareRaw : defaultView.compare;
  const spanRaw = firstParam(searchParams.span) as Span;
  const span = scheme.spans.includes(spanRaw) ? spanRaw : defaultView.span;
  if (span === scheme.allTime) {
    return { span, from: "", to: "", compare: defaultView.compare };
  }
  if (span !== "custom") {
    return { span, from: "", to: "", compare };
  }
  const from = firstParam(searchParams.from);
  const to = firstParam(searchParams.to);
  if (!isCalendarDay(from) || !isCalendarDay(to)) {
    return { ...defaultView, compare };
  }
  return { span, from, to, compare };
}

/**
 * trendSpanQuery is the canonical query string of a reading, with the API's
 * own parameter names. It is both the page's address and the request's
 * query, so the two cannot name different readings. The defaults are left
 * out, and so is the comparison of all time, which has none.
 * `parseTrendSpanParams` reads it back.
 */
export function trendSpanQuery<Span extends string, Comparison extends string>(
  scheme: TrendSpanScheme<Span, Comparison>,
  view: TrendSpanView<Span, Comparison>,
): string {
  const { defaultView } = scheme;
  const params = new URLSearchParams();
  if (view.span !== defaultView.span) params.set("span", view.span);
  if (view.span === "custom") {
    params.set("from", view.from);
    params.set("to", view.to);
  }
  if (view.span !== scheme.allTime && view.compare !== defaultView.compare) {
    params.set("compare", view.compare);
  }
  const query = params.toString();
  return query ? `?${query}` : "";
}

/**
 * The view after a span is chosen. A preset drops the custom days; all time
 * drops the comparison too. Custom starts from the span on screen, as the
 * backend stated it, so the first custom reading is the one already shown
 * and the reader moves its ends from there. With no reading on screen there
 * are no days to start from, and the view stays as it is.
 */
export function trendSpanViewAfterSpanChange<Span extends string, Comparison extends string>(
  scheme: TrendSpanScheme<Span, Comparison>,
  view: TrendSpanView<Span, Comparison>,
  span: Span,
  shown: TrendDays | null,
): TrendSpanView<Span, Comparison> {
  if (span === scheme.allTime) {
    return { span, from: "", to: "", compare: scheme.defaultView.compare };
  }
  if (span !== "custom") {
    return { span, from: "", to: "", compare: view.compare };
  }
  if (shown === null) {
    return view;
  }
  return { span, from: shown.first_day, to: shown.last_day, compare: view.compare };
}

/** The view after one day of the custom range is changed. A day emptied in
 * its input is not taken: a custom span always has both. */
export function trendSpanViewAfterRangeChange<View extends TrendSpanView<string, string>>(
  view: View,
  patch: { from?: string; to?: string },
): View {
  return {
    ...view,
    from: patch.from ? patch.from : view.from,
    to: patch.to ? patch.to : view.to,
  };
}

export function trendSpanViewAfterComparisonChange<Span extends string, Comparison extends string>(
  view: TrendSpanView<Span, Comparison>,
  compare: Comparison,
): TrendSpanView<Span, Comparison> {
  return { ...view, compare };
}
