# Suggested Follows

Spec synthesized from domain grilling session.
Canonical vocabulary: [CONTEXT.md](../CONTEXT.md).
Tracked as [issue #229](https://github.com/redplanettribe/ticket_pos/issues/229).
Design record: [ADR 0031](./adr/0031-suggested-follows-rank-on-activity-and-co-occurrence.md).

## Problem Statement

A Customer who has just made their first Follow has no way to make their second. The Follow control
appears where its subject appears — a Tag on an Event page, an Organization on its own page — so
finding something to Follow requires already being in front of it. Nothing on the platform ever
says "here is another one".

This bites hardest at the two moments that matter most. A Customer who has followed nothing has an
empty Following list and a sentence explaining what a Follow is for, but no way to act on it without
leaving for the explorer and hunting. A Customer who has followed one Organization has told the
platform something concrete about their taste, and gets nothing back for it — their next Follow is
as hard to find as their first was.

There is a second, smaller problem underneath: the Following list is hard to reach at all. It exists
at `/following` and is linked only from inside the account menu behind an unlabelled Avatar chip.
A Customer who has never opened that menu has no reason to believe the page exists, which means the
Follows they have made are effectively unmanageable and the Follow Digest arrives from a place they
cannot find.

The platform holds the facts that would fix both. It knows which Tags each Event wears, which
Organization runs it, and which Events are still upcoming and discoverable. It has never used them
to offer anybody anything.

## Solution

Two changes, one small and one substantial.

The Following page gains a panel of **Suggested Follows**: Tags and Organizations the Customer does
not yet Follow, offered beneath the list of the ones they do. Each suggestion carries the reason it
is there — the Tag that produced it — and the same Follow control used everywhere else, so accepting
one is the identical request as following from an Event page, and the accepted suggestion moves up
into the list above.

Suggestions are ranked on two signals, both of them measures of what is actually happening rather
than of what other people have done:

- **Activity** — how many discoverable upcoming Events carry a Tag, or an Organization is running.
  A suggestion worth making is one that will actually put something in next week's Digest.
- **Co-occurrence** — two Tags co-occur when one Event carries both. A Customer who Follows Jazz is
  offered the Tags that ride alongside Jazz on real Events, and the Organizations whose upcoming
  Events carry Jazz.

Organizations the Customer already Follows feed the same machinery: the Tags on their upcoming
Events are treated as weakly followed, so a Customer who has only ever followed Organizations still
gets suggestions shaped by their taste rather than a generic list. A Customer who Follows nothing at
all falls back to Activity alone.

Separately, the Following page gets a **link in the Storefront header**, next to the Avatar chip and
shown to signed-out visitors too — for whom it leads into sign-in and back out onto the page they
were promised.

For Customers this turns one Follow into a way to find the next one, and makes the page holding all
of them reachable in a click. For the platform it is the first use of the Follow tables for anything
other than addressing mail.

## User Stories

### Reaching the Following page

1. As a Customer, I want a link to my Following page in the Storefront header, so that I can reach
   what I Follow without opening a menu I have no reason to open.
2. As a Customer, I want that link next to my Avatar rather than buried in it, so that I discover
   the feature exists at all.
3. As an anonymous visitor, I want to see the Following link too, so that I learn the platform can
   keep track of things I like before I have an account.
4. As an anonymous visitor, I want pressing it to take me into the existing sign-in flow, so that I
   am not silently rejected.
5. As an anonymous visitor who signs in from that link, I want to land on the Following page, so
   that the round trip delivers what the link promised.
6. As a Customer on a narrow screen, I want the link to survive as an icon, so that the header stays
   usable on a phone.
7. As a Customer, I want the account menu to keep its Following entry, so that a place I have
   learned to look does not stop working.

### Being offered something to Follow

8. As a Customer, I want to see Tags and Organizations I do not yet Follow on my Following page, so
   that making my second Follow is as easy as making my first.
9. As a Customer, I want suggestions drawn from what I already Follow, so that they are about me
   rather than about everybody.
10. As a Customer who Follows one Tag, I want suggestions immediately, so that I do not have to build
    up a history before the feature works.
11. As a Customer who Follows only Organizations, I want suggestions shaped by the Events those
    Organizations run, so that my Follows count even though none of them is a Tag.
12. As a Customer who Follows nothing, I want to be shown what is most active on the platform, so
    that the empty state gives me somewhere to start.
13. As a Customer, I want the empty state to keep explaining what a Follow is for, so that
    suggestions add to the explanation rather than replace it.
14. As a Customer, I want suggestions below my own list, so that the page stays about what I Follow.
15. As a Customer, I want to Follow straight from a suggestion, so that acting on one takes a single
    press.
16. As a Customer, I want a suggestion I accept to move into my Follows list, so that the page tells
    the truth immediately after I press.
17. As a Customer, I want a suggestion I accept to stop being suggested, so that I am not offered
    what I already have.
18. As a Customer, I want to be offered nothing I already Follow, so that the panel is never wasted
    space.

### Why a suggestion is there

19. As a Customer, I want each suggestion to name the Tag that produced it, so that the panel reads
    as reasoned rather than random.
20. As a Customer, I want a suggested Organization to say which of my interests its Events match, so
    that I can judge it without opening its page.
21. As a Customer reading Spanish, I want the reason worded in Spanish, so that the page is in one
    language.
22. As a Customer reading Spanish, I want a suggested Custom Tag marked as a Custom Tag, so that an
    English word I did not choose is explained rather than confusing.
23. As a Customer, I want a suggested Preset Tag named in my own language, so that it matches the
    chips I see on the explorer.

### What gets suggested

24. As a Customer, I want suggestions that have upcoming Events behind them, so that Following one
    actually produces a Digest.
25. As a Customer, I want to be offered Custom Tags as well as Preset Tags, so that narrow interests
    are as discoverable as broad ones.
26. As a Customer, I want a Custom Tag used by only one Event never suggested, so that I am not
    offered an Event's own name as if it were a category.
27. As a Customer, I want a Tag that appears on nearly everything never to outrank one genuinely
    related to my interests, so that the panel leads with what is about me rather than with the
    same generic Tag it would show anyone.
28. As a Customer, I want a Tag inferred from an Organization I Follow never offered back to me as a
    suggestion, so that the platform does not present my own Follow to me as a discovery.
29. As a Customer, I want Tags I chose to count for more than Tags inferred from my Organizations, so
    that what I said outweighs what was guessed.
30. As a Customer, I want an Organization with no upcoming Events never suggested, so that Following
    it would not be a dead end.
31. As a Customer, I want suggestions to change as the catalogue changes, so that the panel reflects
    what is on now rather than what was on when I joined.

### When there is little to suggest

32. As a Customer, I want a short panel rather than a padded one, so that the suggestions I see are
    all ones with a reason behind them.
33. As a Customer, I want the panel to disappear entirely when nothing qualifies, so that I am not
    shown an empty box.
34. As a Customer, I want the panel to prefer offering Organizations when few Tags qualify, so that
    the space goes to the suggestions that can be justified.
35. As a Customer who Follows everything that qualifies, I want the page to work normally with no
    panel, so that being a heavy user does not break the page.

### Reliability and privacy

36. As a Customer, I want the page to render my Follows even if suggestions fail, so that a feature I
    did not come for cannot take away the one I did.
37. As a Customer, I want a suggestions failure to be silent, so that I am not shown an error about
    something I never asked for.
38. As a Customer, I want my suggestions never shown to anyone else, so that what the platform infers
    about me stays mine.
39. As a Customer, I want the Following page to stay out of search results, so that the page listing
    my interests is not indexed.
40. As a Customer buying a Ticket, I want Event and Organization pages to stay exactly as fast as
    they were, so that a feature on my account page costs me nothing while I am shopping.

## Implementation Decisions

### Vocabulary

Three terms enter [CONTEXT.md](../CONTEXT.md) under the existing Following section:

- **Suggested Follow** — a Tag or Organization a Customer does not Follow, offered to them on the
  Following page. Named as a Follow they have not made yet, because that is what accepting one
  produces. _Avoid_: recommendation, for you, discover, related, similar.
- **Activity** — how many discoverable upcoming Events carry a Tag, or an Organization is running.
  A measure of supply, never of how many people Follow something. _Avoid_: popularity, trending,
  hot, top.
- **Co-occurrence** — the relationship between two Tags carried by the same Event, and the basis of
  every Suggested Follow. _Avoid_: similarity, affinity, collaborative filtering.

The `_Avoid_` list on Activity is load-bearing rather than stylistic. "Popularity" is the word this
feature was asked for in, and it names crowd behaviour; the signal measures supply. Left
unprohibited it would reach the UI copy and promise social proof the platform does not have.

### Activity measures supply, not followers

Suggestions rank on how many discoverable upcoming Events stand behind a subject, not on how many
Customers Follow it. The payoff of a Follow is the weekly Follow Digest, so the question a
suggestion should answer is whether Following will send the Customer anything — which Event count
answers and follower count does not. Follower counts are also cold: the Follow tables are new, so
nearly every count would be zero or one, and the ranking would be noise presented as consensus.

This decision is recorded in ADR 0031 and leaves ADR 0030's "no follower counts" position intact —
no follower count is computed, stored, or exposed.

### Co-occurrence over collaborative filtering

Relatedness is derived from the catalogue, not from other Customers. Two Tags are related when an
Event carries both; an Organization is related to a Tag when its upcoming Events carry that Tag.
This works from a single Follow, which is the state of the Customer who most needs a suggestion, and
it improves on its own as the catalogue grows rather than needing a crowd to arrive first.

Raw co-occurrence counts are normalised by each candidate Tag's own frequency, so a Tag that
co-occurs with everything does not lead everyone's panel. Without this, the broadest Tags in the pool
would dominate every Customer's.

Normalising RANKS and never EXCLUDES, and saying so is worth the sentence, because it is easy to
read the stronger promise into it. A ride-along Tag can still be offered, below the Tags genuinely
related to the Customer, and that is correct rather than a leak: "carried by much of the catalogue"
and "irrelevant to this reader" are different claims, and on a small catalogue the busiest Tag is
often just the kind of Event this platform mostly sells. A ubiquity threshold that dropped such Tags
outright was considered and rejected for that reason.

Deriving Organization suggestions through Co-occurrence also avoids inventing an Organization-to-Tag
association. Tags attach to Events only; there is no Organization–Tag table and this feature does not
add one.

### Followed Organizations seed derived Tags

The starting set for ranking is the union of the Tags a Customer Follows directly and the Tags
carried by the upcoming Events of the Organizations they Follow. Derived Tags are weighted below
chosen ones, because Following an Organization is a weaker statement about a genre than Following
the genre. A derived Tag is never itself offered as a Suggested Follow — offering a Customer the Tag
just inferred from their own Follow is the panel's most obvious way to look foolish.

Without this, a Customer who Follows three Organizations and no Tags would fall through to the
cold-start path and receive the same generic panel as a Customer who Follows nothing, despite having
told the platform three concrete things.

### Custom Tags are suggestible, above a floor

Every Tag is followable (ADR 0030) and Custom Tags are suggestible too. A Custom Tag must be carried
by more than one upcoming Event to qualify: a Custom Tag on exactly one Event is usually that
Event's own name, and offering an Event name as a category would make the panel look unserious.

The consequence is accepted knowingly. Custom Tags carry no Spanish name by constraint, so a
Spanish-reading Customer will meet English Tag names they did not choose, including inside the
reason line. The panel therefore marks a suggested Custom Tag with the same badge the Following list
already uses, in the same words, so the marker means one thing across the product. A reader seeing
an English word in a Spanish list is owed the reason, and here more than on the Following list,
because the platform rather than the reader put it there.

### A separate read, not an extension of the Follows listing

Suggestions are served by their own endpoint under the Customer namespace, requiring a Customer
Session, and are not added to the existing Follows listing response.

The Follows listing is not a page-local read: ADR 0030 deliberately built no per-subject "do I
Follow this" probe, so the explorer, every Event page and every Organization page call it to decide
whether each Follow control is filled. Extending it would make every public, conversion-critical
page run the Co-occurrence query on every render and discard the result. Two reads also let the two
have different postures — the Follows listing must be exact, because a stale one draws a wrong
heart, while suggestions are advisory.

The Following page is server-rendered, so the Storefront reads suggestions server-side alongside the
Follows listing through the same customer-session module. No new BFF route handler is added; the
existing route handlers exist for controls the browser operates, and nothing here is operated by the
browser except the Follow control, which already has one.

### Computed live, with no new storage

The ranking runs as a query at request time. No materialised table, no scheduled recomputation, no
cache. The catalogue is small enough that the join is trivial, and the separate endpoint is what
makes adding a cache later a change in one place.

**This feature requires no migration.** The Tag pool, the Event–Tag join, the Events table and the
two Follow tables answer every question it asks. The index on Event end dates added for the Follow
Digest already serves the upcoming-Event predicate.

### Presentation

The panel renders below the Follows list, in the same position whether or not the Customer Follows
anything. Placing it above would put things the Customer did not choose ahead of things they did on
the one page that is theirs, and would separate the Follow Digest switch from the list it changes
the meaning of — an adjacency the page arranges deliberately.

Suggestions are presented in two groups rather than one ranked list, because a Tag's normalised
Co-occurrence score and an Organization's Activity are different units and interleaving them by
"score" would invent a comparability that does not exist. Tags render as a compact chip row and
Organizations as rows matching the Following list's row shape, so a reader recognises which kind of
thing is being offered before reading a word.

Both reuse the existing Follow control against the existing Follow endpoints. There is no follow
written specially for this panel: accepting a suggestion and following from an Event page are the
same request, and the control's own refresh is what moves the accepted suggestion into the list
above.

The ranking is two-tiered: Co-occurrence first, then Activity behind it for the slots Co-occurrence
did not fill — for every Customer, not only for one who Follows nothing. Each entry says which tier
put it there, so a reader can tell the suggestions about their taste from the suggestions about what
is on. Every suggestion carries a reason; "there is a lot on under this" is one, and rendering
silence in its place left the Customer with no Follows at all — whose panel is entirely that tier —
with no explanation of any kind.

The panel shows whatever clears the bar rather than padding to a fixed count, and hides entirely
when nothing does. When few Tags qualify, the remaining space goes to Organizations rather than to
weaker Tags — Organizations are the unbounded supply and their relevance is easier to defend. A
suggestions failure degrades to no panel at all, silently: nobody arrives at the Following page for
suggestions, and an error banner about a feature the reader never asked for is worse than its
absence. The Follows listing keeps its own error handling unchanged.

### The header link

The Following link is rendered by the Storefront's own header customer-navigation component, in both
its signed-in and signed-out branches, immediately before the Avatar chip. The shared shell package
takes a node for the trailing edge of the header and needs no change; the shell is shared with Staff,
which has no Follows and must not learn about them.

The link is shown to signed-out visitors, where it leads to sign-in carrying a return to the
Following page — machinery the page already has, because it must already handle a signed-out
arrival. It uses the same heart glyph as the Follow control, so the header and the button mean the
same thing, and drops to icon-only on narrow screens.

The account menu keeps its Following entry. The menu is the complete index of the Customer Area; the
header link is a shortcut, and removing the entry would break a path Customers may already use.
Only Following is promoted — promoting My Tickets alongside it would dilute the one thing this
change is for, which is teaching Customers that Following exists.

### API contract

One new read, returning the two groups already distinguished by presentation, each entry carrying
its subject and the reason it was chosen. Entries reuse the subject shapes the Follows listing
already puts on the wire, so a suggested Organization and a followed Organization are the same shape
and the Storefront has one type for each kind. The reason names the Tag that produced the suggestion
by canonical key, so the Storefront words it from its own catalogues exactly as it words every other
Tag (ADR 0027) and no language crosses the wire.

An empty result is a success with empty groups, never an error. Swagger and the generated API client
are regenerated.

## Testing Decisions

A good test here asserts what a Customer is offered, not how the offer was computed. The ranking is
one query doing several things at once, and every one of them is observable from outside: what
appears, what does not, and in what order. Nothing in this feature justifies reaching beneath the
API to assert on intermediate scores, and tests that did would freeze the ranking's internals while
proving nothing about its behaviour.

**One seam: HTTP integration**, in the Go integration suite, against the new endpoint. The existing
harness can arrange every catalogue this feature needs entirely through the API — creating
discoverable Events, setting Event Tags (which coins Custom Tags), following Tags and Organizations
as a Customer, and acting as a second Organization. So the Co-occurrence query is reachable from
above, and the repository-level test that would otherwise be justified is not: `docs/testing.md`
admits repository tests only as a narrow exception paired with an HTTP test proving the user-visible
outcome, and here the HTTP test proves it alone.

Prior art is `customer_follows_test.go`, which exercises the Follows listing through the same
session helpers, and `follow_digest_test.go`, which arranges tagged Events and Follows for several
Customers in exactly the shape these tests need.

Cases the suite owns:

- A Customer following one Tag is offered Tags that co-occur with it, and never that Tag itself.
- A Tag carried by nearly every Event is not suggested, while a Tag with a genuine association is.
- A Custom Tag on two Events can be suggested; a Custom Tag on one Event cannot.
- A Customer following only an Organization is offered suggestions shaped by that Organization's
  Event Tags, and is never offered those Tags themselves.
- A directly followed Tag outranks a Tag derived from a followed Organization.
- An Organization with no upcoming Events is never suggested; one running an Event carrying a
  followed Tag is.
- A Customer following nothing is offered the most active subjects.
- A Customer following everything that qualifies gets a success with empty groups, not an error.
- Only discoverable upcoming Events count toward Activity — the same subset the explorer lists.
- Each suggestion names the Tag that produced it.
- The endpoint refuses a request with no Customer Session.

Storefront helpers extracted from the panel — deciding what to show when a group is thin, or whether
to render at all — are tested with the runtime's own test runner alongside the existing Follow
helper tests, if and only if such a helper turns out to be worth extracting. This is not a new seam;
it is the seam those helpers already live in.

No Playwright journey is added. The panel's contents depend on which Events happen to be upcoming,
which is flaky by construction in a journey test; the Follow control's cross-runtime path is already
covered by the existing follow-intent journey; and this feature adds no new runtime boundary.

## Out of Scope

- **Follower counts, anywhere.** Not computed, not stored, not shown. ADR 0030's position stands.
- **Dismissing a suggestion.** There is no "not interested" and no table remembering one. The
  natural dismissal is Following the thing, which moves it out of the panel. Dismissal is only
  interpretable at a volume the platform does not have, and at current catalogue size it would
  permanently remove one of very few candidates.
- **Collaborative filtering.** No "Customers who Follow this also Follow that", in this or any
  disguise.
- **Suggestions anywhere but the Following page.** Not on the explorer, not on Event pages, not on
  Organization pages, and not in the Follow Digest.
- **A Following feed.** Unchanged from ADR 0030: the panel offers subjects to Follow, never Events
  to browse.
- **Suggesting Events.** Only Tags and Organizations — the two followable kinds — are suggested.
- **Per-Customer tuning**, weights exposed as settings, or any control over ranking.
- **Caching, precomputation, or scheduled recomputation** of the ranking.
- **Spanish names for Custom Tags.** The constraint making them curated-only stands; the panel marks
  them instead.
- **Promoting My Tickets** or any other Customer Area surface into the header.

## Further Notes

**This feature's quality is bounded by catalogue size, not by the algorithm.** At the current
catalogue — single-digit Events and a few dozen Tag attachments — Co-occurrence has very little to
work with, and panels will often be short or absent. This is understood and accepted: what is being
built is the surface and the mechanism, both of which improve on their own as Events accumulate,
with no rewrite required. A recommender that needed a crowd would not have this property, which is
part of why Co-occurrence was chosen over the alternatives.

The guards stack multiplicatively against a thin catalogue — normalisation, the Custom Tag floor,
excluding existing Follows, and upcoming-and-discoverable only. Keeping them and accepting a short
panel is deliberate: relaxing down to "anything upcoming" would produce suggestions with no relation
to the reader, and the first irrelevant Digest that resulted would damage the Digest's credibility
rather than the panel's.

**This amends ADR 0030 rather than contradicting it.** That ADR, and the Following page's own code
comment, state that the page is a management surface and deliberately not a discovery surface. That
reasoning was aimed at a *feed* — a second browsable stream of Events competing with the explorer —
and it still holds; no feed is built here. What is added is a way to make another Follow from the
page that is about Follows, which is management adjacent rather than a stream. ADR 0031 records the
amendment openly, as ADR 0030 did for ADR 0027, so a future reader meets the change rather than the
contradiction.

The header link is a change to shared Storefront chrome seen by every visitor including signed-out
ones, and is the only part of this spec that touches a surface outside the Customer Area. It is
tracked as its own piece of work so it can be reviewed and reverted independently of the panel.
