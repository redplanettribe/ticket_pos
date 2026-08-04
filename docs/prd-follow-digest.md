# Follows and the Follow Digest

Spec synthesized from domain grilling session.
Canonical vocabulary: [CONTEXT.md](../CONTEXT.md).
Tracked as [issue #215](https://github.com/redplanettribe/ticket_pos/issues/215).
Design record: [ADR 0030](./adr/0030-follows-notify-as-a-weekly-digest.md).

## Problem Statement

A Customer who discovers an Event they like on the Storefront has no way to hear about the next
one. Their only options are to remember the Organization's name and check back, or to re-run a
search on the global explorer and hope something new has appeared. Nothing on the platform reaches
out to them.

This hurts both sides. Customers miss Events they would have bought tickets to — often finding out
after tickets have sold out, or after the Event has already happened. Organizations have no way to
build a returning audience: every Event starts its discovery from zero, and an Organization that
sold out a show last month cannot tell the people who came that they are doing it again.

The platform holds the two facts that would fix this — which Tags an Event wears and which
Organization runs it — and a verified email address for every Customer who has ever signed in.
It has never used them to tell anyone anything.

## Solution

A Customer can **Follow** a Tag or an Organization from anywhere they appear on the Storefront.
Following is a standing subscription, not a private bookmark: the point of pressing it is to
receive email.

Once a week, every Customer with at least one Follow receives a single **Follow Digest** — one
email covering everything they follow, never one email per Follow and never one per Event. It
carries two sections:

- **New this week** — Events matching one of their Follows that they have never been shown before.
- **Happening this week** — Events they have already been shown that start within the next seven
  days, including any they already hold Tickets to, marked so they read as an agenda rather than
  an advertisement.

The digest is capped, so following a busy Tag produces a readable email rather than a catalogue.
It is sent in the Customer's own language. Every message carries a working unsubscribe that
silences the email without destroying the Follows behind it.

For Customers this replaces "remember to check back" with one predictable email a week. For
Organizations it turns a one-off Ticket Sale into a channel they can reach again, without giving
them any ability to send mail themselves.

## User Stories

### Following

1. As a Customer, I want to Follow an Organization from its public page, so that I hear about its
   future Events without having to remember its name.
2. As a Customer, I want to Follow a Tag from the global explorer, so that I hear about Events of a
   kind I like regardless of who runs them.
3. As a Customer, I want to Follow a Tag from an Event page where that Tag is shown, so that I can
   act on interest at the moment I feel it.
4. As a Customer, I want to Follow either a Preset Tag or a Custom Tag, so that narrow interests are
   as followable as broad ones.
5. As a Customer, I want the Follow control to show that I already Follow something, so that I do
   not press it twice wondering whether it worked.
6. As a Customer, I want pressing Follow twice to leave me following once, so that a double-click or
   a retried request does not create a duplicate.
7. As a Customer, I want to Unfollow from the same control I followed with, so that leaving is as
   easy as joining.
8. As a Customer, I want to see everything I Follow in one list in my Customer Area, so that I can
   review and prune what I have accumulated.
9. As a Customer, I want to Unfollow from that list, so that I can clean up without hunting down
   the original page.
10. As a Customer, I want my Follows to persist across sessions and devices, so that they belong to
    me rather than to a browser.

### Following while signed out

11. As an anonymous visitor, I want to see the Follow control even though I am not signed in, so
    that I know the option exists before I have an account.
12. As an anonymous visitor, I want pressing Follow to take me into the existing sign-in flow, so
    that I am not silently rejected.
13. As an anonymous visitor, I want the thing I meant to Follow to actually be followed once I have
    verified my email, so that I do not have to find the page and press the button a second time.
14. As an anonymous visitor, I want to land back where I started with the control showing
    "Following", so that the round trip is visibly complete.
15. As a Customer, I want a Follow intent declared before sign-in to be recorded against the account
    that completes verification and no other, so that nobody can use it to subscribe my address.
16. As an anonymous visitor who abandons the sign-in, I want no Follow to be recorded, so that
    changing my mind costs nothing.

### Receiving the digest

17. As a Customer who Follows at least one Tag or Organization, I want one email a week, so that my
    inbox stays predictable no matter how much I Follow.
18. As a Customer, I want that email to arrive on the same day each week, so that I learn when to
    expect it.
19. As a Customer, I want the digest to arrive before the weekend, so that it is useful for planning
    the Events that are mostly on it.
20. As a Customer who Follows twenty things, I want still exactly one email, so that Following more
    is not punished.
21. As a Customer, I want an Event matched by several of my Follows to appear once, so that the
    email does not repeat itself.
22. As a Customer, I want to see why an Event is in my digest — which Tag or Organization brought
    it — so that the mail feels earned rather than random.
23. As a Customer with no Follows, I want no digest at all, so that I am not mailed for a feature I
    have not used.
24. As a Customer whose Follows produced nothing this week, I want no email rather than an empty
    one, so that silence means "nothing on" instead of "we mailed you anyway".
25. As an unverified Customer, I want never to receive a digest, so that a Ticket Sale made on my
    behalf does not subscribe me to anything.

### What is in the digest

26. As a Customer, I want a "New this week" section of Events I have not been shown before, so that
    I learn about things early enough to buy Tickets.
27. As a Customer, I want a "Happening this week" section of Events starting in the next seven days,
    so that I get the reminder I would otherwise have to set myself.
28. As a Customer who has just Followed a busy Tag, I want my first digest to show me what is
    already on, so that Following pays off immediately instead of waiting for the next new Event.
29. As a Customer, I want an Event to appear in "New" rather than both sections when it qualifies for
    both, so that one email never lists the same Event twice.
30. As a Customer, I want each section ordered soonest-first, so that the most urgent thing is at the
    top.
31. As a Customer, I want at most ten Events per section, so that the email is readable.
32. As a Customer whose matches exceed the cap, I want the remainder to appear in later digests
    rather than being dropped, so that Following a busy Tag does not silently hide most of it.
33. As a Customer whose matches exceed the cap, I want a link to see the rest, so that the cap is
    visible rather than a silent truncation.
34. As a Customer, I want that link to open the explorer already filtered by the Tag, or the
    Organization's own page, so that it lands somewhere that actually shows everything.
35. As a Customer, I want an Event I was shown last week not to reappear as new this week, so that
    the digest does not become the same email over and over.
36. As a Customer, I want each Event to show enough to decide — name, Organization, date and time,
    venue — so that I do not have to click through to triage.
37. As a Customer, I want a link straight to the Event page, so that acting on interest is one click.

### Tickets already held

38. As a Customer who already holds Tickets to an Event, I want it marked "You're going" rather than
    advertised to me, so that the digest reads like it knows me.
39. As a Customer who already holds Tickets, I want no "buy Tickets" call to action for that Event,
    so that I am not sold something I own.
40. As a Customer who already holds Tickets, I want a link to my Ticket Sale instead, so that the
    reminder is actionable.
41. As a Customer who already holds Tickets, I want that Event kept out of "New this week", so that
    something I bought is not presented as news.
42. As a Customer who reversed their Ticket Sale, I want not to be told I am going, so that a refund
    is reflected honestly.

### Which Events qualify

43. As an Org Admin, I want only my published Events to appear in digests, so that drafts never
    leak.
44. As an Org Admin, I want only Events I marked Discoverable to appear, so that the digest respects
    the same choice as the public listings.
45. As an Org Admin, I want a cancelled Event to stop appearing, so that nobody is invited to
    something that is not happening.
46. As a Customer, I want Events that have already ended never to appear, so that the digest is
    always about the future.
47. As an Org Admin, I want an Event with no scheduled date not to appear, so that an incomplete
    Event is not advertised.
48. As an Org Admin whose Event registers externally, I want the digest to invite Customers to
    Register rather than to buy Tickets, so that it matches how the Event actually works.
49. As a Customer, I want an externally registered Event never to be marked "You're going", so that
    the digest does not claim to know something it cannot.
50. As a Customer following a Tag, I want an Event tagged after I started following to reach me, so
    that late tagging still works.

### Language

51. As a Spanish-speaking Customer, I want the digest in Spanish, so that the one marketing email
    the platform sends is not in a language I do not read.
52. As a Customer, I want Preset Tag names in my own language in the email, so that it matches what
    the Storefront showed me.
53. As a Customer, I want Custom Tag names shown as coined, so that they are recognisable rather than
    mistranslated.
54. As a Customer, I want the language taken from the Storefront I have been using, so that I never
    have to set it.

### Unsubscribing

55. As a Customer, I want an unsubscribe link in every digest, so that leaving is always one click
    away.
56. As a Customer, I want unsubscribing to stop the email without deleting my Follows, so that I keep
    my list and can turn the mail back on.
57. As a Customer, I want to unsubscribe without signing in, so that a link in an email is enough.
58. As a Customer, I want a mail scanner that prefetches links in my inbox not to unsubscribe me, so
    that my corporate mail filter does not make decisions for me.
59. As a Customer, I want to turn the digest back on from my Customer Area, so that unsubscribing is
    reversible.
60. As a Customer, I want to see clearly in my Customer Area that the digest is off while I still
    Follow things, so that the silence is explained.
61. As a Customer who has unsubscribed, I want to keep receiving Sale Confirmations and One-time
    Passcodes, so that turning off marketing does not break signing in or buying.

### Sending and reliability

62. As a Platform Operator, I want the weekly send to run on a schedule outside any request, so that
    it does not depend on somebody using the site.
63. As a Platform Operator, I want the send paced within the email provider's rate limit, so that a
    large subscriber base does not get messages rejected.
64. As a Platform Operator, I want a failed send retried with backoff, so that a transient provider
    outage does not skip a Customer's week.
65. As a Platform Operator, I want a retry never to send a Customer two digests for the same week,
    so that reliability does not become duplication.
66. As a Platform Operator, I want overlapping scheduler ticks to be safe, so that a slow run
    overlapping the next one cannot double-send.
67. As a Platform Operator, I want the digest sent from a different sending domain than transactional
    mail, so that spam complaints cannot degrade delivery of One-time Passcodes.
68. As a Platform Operator, I want to be able to pause the weekly send without a deploy, so that I
    can stop it if something is wrong.
69. As a Platform Operator, I want the scheduled endpoints unreachable from the public internet, so
    that nobody can trigger a send.
70. As a Platform Operator, I want digest contents computed at send time rather than when queued, so
    that a digest delayed by an hour still reflects reality.
71. As a Platform Operator, I want ledger rows for ended Events cleaned up, so that the table does
    not grow without bound.

## Implementation Decisions

### Domain vocabulary

New terms enter `CONTEXT.md`: **Follow** (a Customer's standing subscription to a Tag or an
Organization), **Follow Digest** (the single weekly email covering all of a Customer's Follows), and
the two sections it carries. "Favorite" is explicitly rejected as a synonym — a Follow exists to
produce email, and naming it after a bookmark would mislead. Where existing glossary entries touch
this (Verified Customer, Customer Area, Tag, Organization), they are amended rather than duplicated.

### One concept, two followable kinds

There is one concept, Follow, applying to both Tags and Organizations. Any Tag is followable,
`curated` or not: per-reader volume is bounded by the weekly cap rather than by restricting the
pool, so limiting Follows to Preset Tags would cost the most valuable case (a narrow interest) for
no benefit.

The two kinds are stored as two tables rather than one polymorphic table, so that both can carry
real foreign keys with `ON DELETE CASCADE` to `tags` and `organizations`, consistent with the
repo's hand-written-SQL, no-ORM convention. They are one concept in the domain and in the API,
which presents a single Follow list.

Following requires an active Customer Session, which by definition implies a verified email — so
ADR 0010's rule that notifications never reach unverified Customers is satisfied structurally, with
no separate check.

### Follow intent through sign-in

The Follow control is rendered for anonymous visitors. Pressing it enters the existing One-time
Passcode / Google Sign-In flow carrying the intended Follow as an explicit parameter through the
sign-in return, not via browser storage, so the intent is server-visible. The Follow is written only
against the Customer Session produced by successful verification; an email supplied alongside the
intent is never trusted as a subscription target.

### "New" is per-Customer, via a sent-ledger

Events carry no `published_at` and nothing timestamps the `discoverable` flip, so novelty cannot be
derived from the Event. It is defined per reader instead: an Event is new to a Customer if it has
never been included in a digest sent to that Customer.

This requires a ledger of (Customer, Event) pairs, written only for Events actually included in a
sent digest. One mechanism serves four purposes — novelty, cross-Follow dedupe, week-to-week
anti-repetition, and send idempotency under retry. It is also the only definition under which a
Customer who Follows a busy Tag gets a useful first digest rather than an empty one.

The ledger is independent of Follows: unfollowing and refollowing does not make previously shown
Events new again, because it records what a person was shown. Rows are pruned once the Event has
ended.

### Digest composition

Eligibility mirrors the public explorer's filter exactly — published, discoverable, and not yet
ended (`COALESCE(ends_at, starts_at) >= now()`, which also excludes dateless Events). Anything
looser would mail out an Event an Organization chose not to list.

- **New this week**: matches a Follow, not in the ledger for this Customer, not an Event they hold
  a live Ticket Sale for.
- **Happening this week**: in the ledger for this Customer, starts within seven days.
- An Event qualifying for both appears in New only.
- Both sections are ordered soonest-first and capped at ten. Overflow is *carried*, not dropped —
  the ledger is written only for Events actually included, so the remainder stays new and surfaces
  in a later digest. Each capped section carries a "+N more" link.
- Overflow links point at existing surfaces: the global explorer filtered by the Tag's
  `canonical_key`, or the Organization's public page. No new listing surface is built.
- Events with `registration_mode = 'external'` show a Register call to action, per ADR 0028, and can
  never be marked as attending.
- Attendance is determined by a live `ticket_sales` row for that Customer and Event with
  `status = 'active'`; reversed sales do not count.
- Each entry records which Follow matched it, both to render the "because you follow X" attribution
  and so that Tag-stuffing can be measured before any policy is written against it.

### Scheduling and delivery

Delivery follows the pattern established by ADR 0024's reversal reconciler, which is the repo's only
scheduled-work precedent: Cloud Scheduler with a dedicated service account and an OIDC token, calling
an endpoint under `/api/v1/internal/`, gated by Cloud Run IAM rather than application middleware.
Sending inline is not viable — the provider's rate limit is roughly two requests per second, a
constraint ADR 0009 already records as unsolved for bulk sends.

Two scheduled jobs and two internal endpoints:

- A **weekly enqueue** (Thursday 09:00 `America/Guayaquil`, the timezone the existing reconciler
  schedules in) creating one pending digest row per eligible Customer.
- A **per-minute drain** claiming a batch, composing each digest at send time, sending it, writing
  the ledger, and marking the row sent.

The pending-digest table follows `sale_reversals`: a status, `attempt_count`, `next_attempt_at`, and
partial indexes on the working set. Uniqueness on (Customer, week) is what makes a retried or
overlapping run unable to double-send. The drain carries a time budget strictly inside the
scheduler's attempt deadline, which is itself inside the Cloud Run request timeout, and both jobs are
pausable through a Terraform variable without a deploy.

Splitting enqueue from drain is partly a testing decision: it lets a test produce a week
deterministically and then drain it in controlled batches.

### Email

`platform.EmailSender` gains one method, `SendFollowDigest`, implemented across all four existing
implementations (Resend, logging, noop, capture). Content lives in `email_content.go` alongside the
others.

This is the first email in the system that branches on language. A locale is stored on the Customer,
captured from the Storefront locale on sign-in — today locale comes from the URL segment and nowhere
else, and the backend has no knowledge of it.

Rendering Preset Tag names server-side requires a narrow amendment to ADR 0027, which put that copy
in the Storefront message catalogues. A Spanish display name is added to the `tags` table for
`curated = true` rows only, seeded by migration. The Storefront keeps its own catalogue unchanged and
Custom Tags stay untranslated, which ADR 0027 already holds to be correct. This duplicates twelve
strings in one direction rather than reversing the decision.

Digests send from a **separate subdomain** with its own provider domain and DNS records, distinct
from the transactional sender. Marketing mail attracts complaints in a way transactional mail does
not, and complaint rates degrade domain reputation — sharing a domain would let an annoying digest
impair delivery of the One-time Passcodes people need to sign in.

### Unsubscribe

A `digest_enabled` boolean on the Customer, defaulting on. Unsubscribing flips it and never touches
Follows: a Customer keeps their list and can re-enable.

Two entry points — a signed link in every digest footer, reusing the existing Confirmation Link
signing machinery rather than a second scheme, and a toggle in the Customer Area beside the Follow
list. The link lands on a Storefront page that confirms with a POST, because mail security scanners
routinely prefetch links; a bare GET would let a scanner unsubscribe people who never clicked.

Transactional mail is unaffected by the switch.

### API surface

Customer-session routes for creating, deleting and listing Follows, and for reading and setting
`digest_enabled`. An unauthenticated POST route for the signed unsubscribe. Two internal routes for
enqueue and drain. All follow the repo's existing envelope, error-code and offset-pagination
conventions, with Swagger annotations regenerated into the API client.

### Documentation

An ADR records the decisions a future reader will find surprising: that a request for per-Event
reminders became a weekly digest; that "new" is defined per reader through a sent-ledger rather than
by an Event timestamp; and that digests send from a separate domain to protect transactional
deliverability. ADR numbering is verified against other branches before allocating, given the prior
0026 collision.

## Testing Decisions

### What makes a good test here

Tests assert externally visible behaviour: the HTTP status, the response envelope
(`data`, `error`, `request_id`), documented error codes, and the emails a Customer actually
receives. They do not assert SQL text, internal call order between layers, or unexported functions.
Domain vocabulary from `CONTEXT.md` is used in test names.

### Primary seam: HTTP integration

Effectively the whole feature is exercised through `backend/integration/`, HTTP in and HTTP out,
with no new test infrastructure. The harness already provides a `CaptureEmailSender`, a fixed clock
injected per service through `WithClock`, and truncate-based isolation with serial execution.

Because the scheduled half is driven by internal HTTP endpoints, the entire pipeline — follow,
enqueue, drain, send — is drivable from an integration test. Prior art is
`reversal_reconciler_test.go`, which posts to `/api/v1/internal/reversals/drain` exactly this way.
Email assertions follow `payout_request_notice_test.go` and
`customer_sale_reversal_refused_notice_test.go`.

Scenarios covered at this seam:

- Following and unfollowing Tags and Organizations; idempotent repeat follows; the Follow list.
- Follow requires a Customer Session; a Follow intent is bound to the verifying session only.
- One digest per Customer per week regardless of Follow count; no digest for zero Follows; no
  digest when nothing matched.
- Section membership: new versus happening, New winning on overlap, soonest-first ordering.
- Cross-Follow dedupe: an Event matched by both a Tag and an Organization appears once.
- The cap, and carry-over across two consecutive weekly runs proving nothing is dropped.
- Anti-repetition: an Event shown once never returns as new.
- Eligibility: draft, non-discoverable, cancelled, ended and dateless Events all excluded.
- Externally registered Events show Register and are never marked as attending.
- Attendance marking, and that a reversed Ticket Sale removes it.
- Locale selection, and Preset Tag names rendered in the Customer's language.
- Unsubscribe silences the digest, leaves Follows intact, and leaves One-time Passcodes and Sale
  Confirmations unaffected; re-enabling resumes.
- Retry safety: a drain re-run after a simulated send failure produces exactly one email.
- Time-dependent behaviour driven by `WithClock`, including the seven-day boundary.

### Repository exception: concurrency

`docs/testing.md` reserves repository-level tests for concurrency and locking. One such test covers
concurrent drain claims against the pending-digest table, proving two overlapping ticks cannot both
claim the same row. It is paired with an HTTP test asserting the user-visible outcome — exactly one
email — as the rule requires.

### E2E: one thin journey

One Playwright journey in `e2e/tests/`, covering the round trip that exists only in the browser and
is invisible to API tests: an anonymous visitor presses Follow, completes sign-in, and lands back on
the page with the control showing "Following" and the Follow persisted. Deliberately thin — it
asserts the intent survived the round trip and nothing else, and duplicates no integration scenario.

## Out of Scope

- **Reminders for Events matching no Follow.** A Customer holding Tickets to something they found on
  their own gets no reminder; the Customer Area already lists their upcoming Events. A general
  "your upcoming events" mail is a separate feature.
- **Any per-Event or day-of email.** Considered and rejected: the weekly digest is the only outbound
  channel this feature creates.
- **A Following feed page.** Overflow links point at the existing explorer and Organization pages.
  The follow data will support a feed later if one earns its place.
- **Follower counts or analytics for Organizations**, and any public follower count as social proof.
  The Follow tables can answer these whenever the need is real.
- **Anti-Tag-stuffing controls.** `maxEventTags` stays at 20. The per-reader cap bounds the harm to
  relevance rather than volume, and per-Event tag policing would be inventing a moderation problem
  before having one. Matched-Follow attribution is recorded so it can be measured first.
- **Moderation or deletion of Custom Tags.** Still deferred, as ADR 0004 left it.
- **Following individual Events, Members, or other Customers.**
- **Per-Follow notification settings or digest frequency choice.** One global switch, one cadence.
- **HTML email.** The existing sender is plain text; making the digest the first HTML email is a
  separate change.
- **Localising any existing email.** Only the digest branches on language; One-time Passcodes and
  the rest stay as they are.
- **Push, SMS or in-app notifications.**

## Further Notes

This spec is the outcome of a grilling session, and several decisions are the *opposite* of the
original request. It was asked for as two per-Event emails — one a week before, one on the day —
and became a single weekly digest. The reasoning is worth keeping: per-Event mail scales with the
number of Events a Customer's Follows match, so following a busy Tag would produce an unbounded
stream, and the only defences would have been restricting what is followable or capping mail with
rules a reader cannot see. A weekly digest bounds volume structurally at one message, which is what
made it safe to let Customers Follow any Tag at all. The original reminder intent survives inside
the digest as "Happening this week", and the "You're going" marking is what preserves it for people
holding Tickets.

Two constraints found in the codebase shaped the design more than anything in the request:

- **Events have no `published_at`.** Nothing records when an Event became public, which is why
  novelty is defined per reader instead of per Event. Adding the column was considered and rejected:
  it would still have been wrong for Events published long before they were made discoverable, and
  for Events tagged after publication.
- **Transactional and marketing mail would have shared a sending domain.** This is the highest-risk
  detail in the whole feature and is unrelated to Follows as such. One-time Passcodes are
  login-critical, and complaint-driven reputation damage from a weekly marketing send would land on
  the same domain. The domain split is not optional polish.

Two things to watch after launch, neither worth building for yet: a Customer following several busy
Tags can sit permanently at the cap, so their carried overflow drains lazily and far-future Events
may be deferred for a long time; and Organizations now have an incentive to tag broadly, since Tags
carry email for the first time.
