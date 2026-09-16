// The Tax Document Archive's dialog, as pure functions (#630, spec #629):
// the range it starts on, whether a range can be downloaded, and the address
// the download is fetched from.
//
// ECUADOR'S CALENDAR, NOT THE BROWSER'S. The archive's range is a range of
// Emission Dates, which are calendar days in Ecuador, so "last month" and
// "today" are read on Guayaquil's clock whatever machine the operator is on.
// Ecuador keeps UTC-5 all year with no daylight saving, so the day is found by
// shifting the instant five hours and reading its UTC fields — no Intl
// formatting, whose output differs between ICU versions.
//
// Relative imports carry the ".ts" extension so the module resolves under
// `node --test` as well as the bundler.

/** A range of Emission Dates, each "YYYY-MM-DD" ("" when not chosen). */
export type TaxDocumentArchiveRange = {
  from: string;
  to: string;
};

/** Where the browser downloads the archive from: the BFF's proxy of the API route. */
export const TAX_DOCUMENT_ARCHIVE_PATH = "/api/operator/invoicing/archive";

const ECUADOR_UTC_OFFSET_MS = -5 * 60 * 60 * 1000;

/** The instant as a date whose UTC fields read Ecuador's wall clock. */
function inEcuador(now: Date): Date {
  return new Date(now.getTime() + ECUADOR_UTC_OFFSET_MS);
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

function day(year: number, monthIndex: number, dayOfMonth: number): string {
  return `${year}-${pad(monthIndex + 1)}-${pad(dayOfMonth)}`;
}

/** Today's calendar day in Ecuador, "YYYY-MM-DD". */
export function ecuadorToday(now: Date): string {
  const local = inEcuador(now);
  return day(local.getUTCFullYear(), local.getUTCMonth(), local.getUTCDate());
}

/**
 * The calendar month before the one it is now in Ecuador, first day to last.
 * The usual request is "send me last month", so this is where the dialog
 * starts when the list says nothing about a range.
 */
export function previousEcuadorCalendarMonth(now: Date): TaxDocumentArchiveRange {
  const local = inEcuador(now);
  // Date.UTC normalises month -1 into December of the year before, and day 0
  // of a month into the last day of the month before it.
  const firstOfPrevious = new Date(Date.UTC(local.getUTCFullYear(), local.getUTCMonth() - 1, 1));
  const lastOfPrevious = new Date(Date.UTC(local.getUTCFullYear(), local.getUTCMonth(), 0));
  return {
    from: day(firstOfPrevious.getUTCFullYear(), firstOfPrevious.getUTCMonth(), 1),
    to: day(lastOfPrevious.getUTCFullYear(), lastOfPrevious.getUTCMonth(), lastOfPrevious.getUTCDate()),
  };
}

/**
 * The range the dialog opens on. The list's own Emission Date range when the
 * operator has narrowed the list to one, so the same dates are never typed
 * twice; otherwise the previous calendar month in Ecuador.
 *
 * A list range with ONE END is still the operator's range: "since 1 July"
 * runs to today in Ecuador, and "up to 20 August" starts on the first of that
 * month. An archive has two ends, so the open one is filled rather than the
 * range discarded.
 */
export function startingTaxDocumentArchiveRange(
  list: { issuedFrom: string; issuedTo: string },
  now: Date,
): TaxDocumentArchiveRange {
  const { issuedFrom, issuedTo } = list;
  if (issuedFrom === "" && issuedTo === "") {
    return previousEcuadorCalendarMonth(now);
  }
  return {
    from: issuedFrom !== "" ? issuedFrom : `${issuedTo.slice(0, 7)}-01`,
    to: issuedTo !== "" ? issuedTo : ecuadorToday(now),
  };
}

/**
 * Whether a range can be downloaded: both ends chosen, and the start on or
 * before the end. The API refuses anything else; the dialog does not offer it.
 * "YYYY-MM-DD" compares chronologically as text.
 */
export function isTaxDocumentArchiveRangeComplete(range: TaxDocumentArchiveRange): boolean {
  return range.from !== "" && range.to !== "" && !isTaxDocumentArchiveRangeInverted(range);
}

/** Whether both ends are chosen and the start falls after the end: the one mistake the dialog names. */
export function isTaxDocumentArchiveRangeInverted(range: TaxDocumentArchiveRange): boolean {
  return range.from !== "" && range.to !== "" && range.from > range.to;
}

/** The download's address for a range. */
export function taxDocumentArchiveUrl(range: TaxDocumentArchiveRange): string {
  const params = new URLSearchParams({ from: range.from, to: range.to });
  return `${TAX_DOCUMENT_ARCHIVE_PATH}?${params.toString()}`;
}
