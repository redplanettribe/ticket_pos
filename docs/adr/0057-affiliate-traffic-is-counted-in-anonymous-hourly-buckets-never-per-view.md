# Affiliate traffic is counted in anonymous hourly buckets, never per view

Status: accepted. Amends ADR 0022.

The affiliate tab gains time-series graphs: per-link Clicks, Attributed Sales, Attribution Rate, and — new to the domain — Page Views of the Event page as a whole, so a link's traffic can be compared against everything the page received. ADR 0022 rejected stored view data outright ("a table that grows with page views rather than with sales"); we now store view data, but in the one shape that honors that objection: an hourly upsert bucket per (Event, Affiliate Link or none, UTC hour) holding nothing but a count. Growth is bounded by time × links, not by traffic; there is no visitor identity, IP, user agent, or dedup, so there is no personal data, nothing to purge, and no ADR 0045 dark-ship obligation — and spamming the public endpoint can only make a number wrong, never fill a disk.

## Considered options

- **Raw view events, aggregated at read time.** Rejected: it is exactly the table ADR 0022 refused — unbounded growth on an unauthenticated public write, a retention/purge obligation under our privacy precedents, and it buys nothing at our stated floor of hourly granularity.
- **Leave `click_count` as the only record.** Rejected: a scalar has no history, so no trend can ever be drawn from it.

## Consequences

- **A Click is a Page View that carried a live link.** The gap between all Page Views and all Clicks is organic traffic; it needs no counter of its own.
- **`click_count` remains authoritative.** The scalar is still incremented alongside the bucket (dual-write); the headline number and the delete-only-without-history rule keep their meaning. Buckets begin at launch, so a link's bucket sum will never equal a pre-existing `click_count` — the graph is not a ledger of the counter.
- **Counts are loads, not people.** Refreshes, bots, and prefetches count. ADR 0022's caveat still governs every figure: a floor, not a measurement, and staff copy must never imply otherwise.
- **Attributed Sales stay derived.** Per-hour sales come from `ticket_sales` (`affiliate_link_id`, `sold_at`, `status = 'active'`) at read time, so a Reversal retroactively edits the graph, as everywhere else.
- **Attribution Rate is never hourly.** Last-click attribution spans a 7-day window, so hour-by-hour division produces rates over 100% and division by zero; the Rate view offers Daily and Cumulative only, defaulting to Cumulative.
- **Hourly buckets are the floor forever.** Minute-level trends, uniques, and sessions are permanently out of reach of this data — reopening any of them means reopening this ADR.
- **Deactivated links count nothing**, matching the existing click rule: their series shows zeros while their arrivals still count as Page Views. No active-period history is kept to shade the chart. _Amended 2026-08-24 (#426): Link Trends lists and draws only the series with something to say in the window chosen, so an all-zero series — a deactivated link among them — is unlisted there rather than drawn flat; the data still counts nothing, as above._
- **External Registration events get views-only graphs**: Sales and Rate are hidden, not zeroed, mirroring how the tab already nulls their sales figures.

## Amendment: the counting views are drawn as lines

Recorded after the original decision shipped, and amending it rather than superseding it. The Clicks and Attributed Sales views are drawn as one line per series, not as grouped bars, matching the Attribution Rate view. Reason: with the seventeen-odd series an Event can carry, a grouped bar per bucket is an unreadable sliver, while a line per series reads at any series count. A bucket with nothing counted is still drawn as a zero, not a gap — an hour with no clicks is a measurement, unlike a rate with no denominator.

## Amendment: the affiliate tab became the Reach surface

Recorded 2026-08-25 (#464), amending where the graphs live and nothing about what they count. "The affiliate tab" above is the **Reach** surface now — the staff surface for reading how an Event's page was reached — with the graphs as its first sub-tab (Reach Trends, formerly Link Trends) and the Affiliate Links table as its second. The data, the dual-write, the hourly floor and every consequence above are unchanged; only the address and the words around the chart moved.
