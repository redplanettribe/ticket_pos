# Organization Trends reads live on the platform clock, and shows Event Staff every count but no money

Status: accepted.

The Organization's Home carries Organization Trends, its counterpart to Platform Trends: Takings, Tickets Sold, Ticket Sales, Page Views and Events held over a chosen span, compared with an earlier span and ranked Event by Event.
It reads live from the sales and the Page View buckets, the way Sales Trends does, and freezes nothing.
A sale counts on the day it was made, and a Sale Reversal takes it out of that day, so a month's figure moves when one of its sales is reversed or a Sale Import lands in it.
Every day is the platform's day (America/Guayaquil), not each Event's own day, and every Member sees the whole Organization; Event Staff see every count but neither Takings nor the Payable Balance.

## Considered options

- **Freeze closed days, as the Daily Close does for Platform Trends.** Rejected: Platform Trends freezes because its activity markers are deleted after thirteen months and must outlive them as numbers (ADR 0080). Nothing here is deleted. Takings is defined as dropped by a Sale Reversal, and an Organization asking how March went means March's sales as they stand, including the reversed sale it has already refunded and the history it imported last week. A frozen March would disagree with the Sales list and with Sales Trends on the same Event.
- **Count a sale on the day it reached the platform**, as Active Organization does. Rejected for the reason Sales Trends gives: a Sale Import of last year's history would be drawn as this week's sales.
- **Each Event's own timezone**, as Sales Trends uses. Rejected: an Organization day would then be built from Event days beginning at different instants, so the Events beneath would not add up to the Organization above them. Every Event held today is in Ecuador, so the two clocks differ only on paper; the day a foreign Event is held, its Organization Trends day and its Sales Trends day may disagree near midnight, and that is accepted.
- **Each Event Owner sees only their own Events.** Rejected: Event Owner is a role held on the membership, and Event assignments are not enforced anywhere, so an Event Owner already reads every Event's Sales Trends and Reach. Slicing Home would be the first enforcement of Event assignments, and that is a feature of its own.
- **Event Staff see nothing, or see everything.** Rejected both ways. They already see every Ticket Sale on the Sales list, so the counts tell them nothing new and leaving them out would make Home empty for them. Takings stays behind the guard it carries on every other surface (Org Admin and Event Owner), and the Payable Balance behind the Org Admin guard of Payouts.

## Consequences

- **Past figures can change.** A comparison against last year reads last year as it stands today. This is the deliberate difference from Platform Trends, whose figures for the same sales may therefore disagree with an Organization's own; the two surfaces answer different questions and neither is reconciled to the other.
- **Every reading is computed on request**, so its cost grows with an Organization's sales and with the hourly Page View buckets over the span. A long All time span is the expensive case. If it becomes slow, the remedy is a cache or a precomputed total that is kept in step with reversals, never a freeze.
- **No rate is built on Page Views**, for the reason Platform Trends carries none.
