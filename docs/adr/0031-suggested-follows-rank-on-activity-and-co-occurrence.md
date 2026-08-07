# Suggested Follows rank on Activity and Co-occurrence, on the page ADR 0030 called a management surface

## Status

accepted

Amends ADR 0030 in one respect — see *The Following page stops being purely a management surface*.

## Context and decision

Customers can Follow a Tag or an Organization, and see everything they Follow at `/following`
(ADR 0030). Nothing has ever offered them a second Follow: the Follow control appears only where
its subject appears, so finding something to Follow requires already standing in front of it. The
feature was asked for as suggestions "based on popularity and the tags he already follows". We are
shipping neither of those two things literally. Five decisions here will look wrong without this
record.

- **Ranking measures supply, not audience.** A subject's rank comes from its **Activity** — the
  count of discoverable upcoming Events carrying a Tag, or run by an Organization — never from how
  many Customers Follow it. No follower count is computed, stored or exposed, and CONTEXT.md
  prohibits "popularity" as a name for what we do compute.
- **Relatedness comes from the catalogue, not from other Customers.** Tags are related by
  **Co-occurrence** — one Event carrying both — and an Organization is related to a Tag when its
  upcoming Events carry it. There is no collaborative filtering in this or any disguise. Raw
  co-occurrence is normalised by each candidate Tag's own frequency.
- **Followed Organizations seed derived Tags.** The Tags on a followed Organization's upcoming
  Events are treated as weakly followed, weighted below Tags the Customer chose, and are never
  offered back to that Customer as suggestions.
- **Suggestions are a separate read from the Follows listing.** They get their own endpoint rather
  than riding on `GET /customer/follows`.
- **The panel lives on `/following`**, below the Customer's own list — the page ADR 0030 and its own
  code comment describe as deliberately not a discovery surface.

The whole feature is a query at request time. There is no migration, no new table, no cache and no
scheduled recomputation: the Tag pool, the Event–Tag join, the Events table and the two Follow
tables answer every question it asks.

## Why this way

- **Follower counts would have been noise wearing the costume of consensus.** The Follow tables are
  days old, so nearly every count is zero or one; a ranking built on them would have sorted
  arbitrarily while looking authoritative. Activity has no such cold start, and it answers the
  better question anyway. The promise a Follow makes is the weekly Digest, so what a suggestion
  should predict is *whether Following this will send you anything* — which upcoming-Event count
  answers directly and follower count does not. A subject with a thousand Followers and nothing
  upcoming is a Follow that pays nothing.
- **Co-occurrence works from a single Follow; collaborative filtering needs a crowd we do not have.**
  The Customer most in need of a suggestion is the one who has followed exactly one thing, and
  catalogue-derived relatedness serves them on the day they arrive. It also improves on its own as
  Events accumulate, with no rewrite — the property that made it the right bet at a small catalogue
  size, where every recommender is guessing and the only question is which one gets better for free.
  The normalisation is not a refinement but a requirement: without it the broadest Tags in the
  shared pool co-occur with everything and would be suggested to every Customer on the platform.
- **Co-occurrence is also what relates Organizations to Tags at all.** Tags attach to Events, never
  to Organizations, and there is no Organization–Tag table. Reaching Organizations through their
  Events' Tags means one mechanism serves both followable kinds, and means this feature adds no
  association to the domain that the domain did not already have.
- **Without derived Tags, the most engaged Customer gets the most generic panel.** Following an
  Organization is the natural first Follow for someone who came to buy a ticket, and a Customer with
  three Organization Follows and no Tag Follows would otherwise fall through to the cold-start path
  and be shown the same list as someone who has followed nothing. Weighting derived Tags below
  chosen ones keeps the inference honest — you followed the Organization, not necessarily its genre —
  and never suggesting a derived Tag back prevents the panel's most obvious way of looking foolish,
  which is presenting a Customer's own Follow to them as a discovery.
- **Extending the Follows listing would have made every shopper pay for one private page.** ADR 0030
  deliberately built no per-subject "do I Follow this" probe, so the explorer, every Event page and
  every Organization page call that listing to decide whether each Follow control is filled. Adding
  suggestions to its response would run the Co-occurrence query on every render of the platform's
  hot, public, conversion-critical surfaces and discard the result. Two reads also let the two hold
  different postures: the listing must be exact, because a stale one draws a wrong heart, while
  suggestions are advisory and a failure among them degrades to no panel rather than to an error.
- **The Following page stops being purely a management surface, and that is the amendment.** ADR 0030
  argued against building a Following *feed*: a second browsable stream of Events competing with the
  explorer that already exists. That argument still holds and no feed is built here. What is added
  is a way to make another Follow on the page that is about Follows — subjects to subscribe to, never
  Events to browse — which is adjacent to management rather than a stream. The alternative sites were
  worse: the explorer is a public, shared-cacheable page where personalising would carry a real
  architectural cost and would compete with someone's actual booking task, and a third discovery page
  would compete with two that already exist. Recorded openly, as ADR 0030 recorded its own amendment
  to ADR 0027, so a future reader meets a change of position rather than a contradiction.

## Consequences

- **Custom Tags are suggestible, which puts untranslated names in front of Spanish readers.** ADR 0027
  keeps Custom Tags untranslated by rule and migration 050 constrains `display_name_es` to curated
  Tags, so a suggested Custom Tag appears in English inside a Spanish page — including inside the
  reason line naming it. This was chosen with the cost known, to keep narrow interests as
  discoverable as broad ones. It is mitigated, not solved: the panel marks a suggested Custom Tag
  with the same badge and words the Following list already uses. A Custom Tag must also be carried
  by more than one upcoming Event to qualify, because a Custom Tag on a single Event is usually that
  Event's own name.
- **Panels will often be short, and sometimes absent.** The guards stack multiplicatively against a
  small catalogue — normalisation, the Custom Tag floor, excluding existing Follows,
  discoverable-and-upcoming only. Showing what qualifies and hiding at zero is preferred to relaxing
  the guards: padding with unrelated subjects would produce Follows that yield irrelevant Digests,
  and the credibility lost would be the Digest's rather than the panel's.
- **The ranking is two-tiered, and normalisation ranks rather than excludes.** Co-occurrence runs
  first; Activity fills whatever slots it left, for *every* Customer and not only for one who
  Follows nothing. A reader who has taken in the normalisation argument above and then meets a
  ride-along Tag sitting in a panel will conclude the normalisation is broken and set about
  repairing it — it misled a careful reviewer of this very branch — so: it is not broken, and there
  are two ways such a Tag legitimately arrives. It can place *below* the genuinely related Tags,
  because dividing by a candidate's own frequency reorders and has no power to drop anything; or it
  can come through the Activity tier, which does not consider relatedness at all. Both were chosen.
  Excluding ubiquitous Tags outright needs a threshold that is arbitrary at any catalogue size and
  actively wrong at this one, where a Tag on most Events is likely to be the kind of Event the
  platform mostly sells rather than noise. Dropping the Activity tier was the other alternative, and
  it leaves a Customer who Follows one narrow Tag looking at a single chip. What the two tiers owe
  the reader is honesty about which is which, which is why every suggestion states its reason and
  why "there is a lot on under this" is written out rather than left as a blank line.

- **No dismissal exists, deliberately.** The natural dismissal is Following the thing. A "not
  interested" control would need a table, and at current catalogue size it would permanently remove
  one of very few candidates from an already thin panel. The Follow tables can answer it whenever
  the need is real.
