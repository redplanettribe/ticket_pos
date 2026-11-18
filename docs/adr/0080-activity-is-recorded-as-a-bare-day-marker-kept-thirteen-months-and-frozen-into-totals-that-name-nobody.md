# Activity is recorded as a bare day marker, kept thirteen months, and frozen into totals that name nobody

Status: accepted.

Platform Trends needs Active Customers and Active Organizations per day, and nothing recorded them: session rows are deleted on logout or expiry, there is no last-seen column, and a Google Sign-In leaves no trace.
We record one marker per Customer per platform day and one per Organization per platform day, each saying only "was active" - no page, no time of day, no IP, no count of requests.
At the close of each platform day the figures drawn from them (the daily count, the 7-day and 30-day distinct counts, the new against returning split) are frozen into permanent totals that identify no one, and the markers themselves are deleted after thirteen months.

This is the first time the platform records what a person did on a day.
ADR 0022 and ADR 0057 refused exactly that for Storefront visitors, and they still stand: a marker exists only for a person already proven to be a Customer, acting through a Customer Session or accepting a Ticket Assignment, and anonymous traffic stays a Page View count.

## Considered options

- **A bare daily counter.** Rejected: it cannot say that Monday's Customer and Thursday's are the same person, so the 7-day and 30-day distinct counts, which are what make a small platform's activity readable, could never be drawn.
- **A last-seen column on the Customer.** Rejected: it overwrites itself, so it answers "who is active now" and no question about any earlier day.
- **Markers kept forever.** Rejected: once a day is frozen the markers serve only to recompute a figure whose definition turned out wrong, and thirteen months covers one full year-over-year recheck. Beyond that they are a browsing record with no use.
- **Markers discarded after thirty days.** Rejected: a mistaken definition found in month three could not be corrected for the months already frozen.
- **A third-party product analytics service.** Rejected: ADR 0079's rule is that no personal data leaves the platform.

## Consequences

- **The marker is personal data.** Under ADR 0045, Customer recording stays switched off until a Privacy Policy edition describing it is published; until then Platform Trends says Active Customers is not yet recorded rather than showing a zero. Every other figure on Platform Trends needs no new collection and does not wait.
- **Erasing a Customer removes their markers and leaves the frozen totals**, which is why the totals must be frozen as numbers and never recomputed on read from markers that may since have gone.
- **Past days never change.** A figure frozen on Tuesday reads the same next year, even though the markers behind it are gone, so comparisons between periods are stable.
- **History starts on the day recording starts.** No stand-in exists for Active Customers or for the Member half of Active Organizations before it; the Sale half of Active Organizations reaches back to the first Ticket Sale.
- **A "which Customers were active on this day" list is not a feature and must not become one.** The breakdown of Active Customers is by kind (new that day, returning, returning and bought), never by name.
