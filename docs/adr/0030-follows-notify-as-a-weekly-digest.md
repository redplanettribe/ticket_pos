# Follows notify as one weekly Digest, and "new" is a fact about the reader

## Status

accepted

## Context and decision

Customers can **Follow** a Tag or an Organization (see CONTEXT.md). The feature was asked
for as two per-Event emails — one a week before an Event, one on the day. We are shipping
neither. Three decisions here will look wrong without this record:

- **One weekly Follow Digest, not per-Event notifications.** Every Customer with at least one
  Follow gets a single email a week covering all of them, capped, with overflow carried into
  later Digests rather than dropped. There is no day-of email and no per-Event email.
- **"New" is defined per reader, not per Event.** An Event is *New to You* when it has never
  appeared in a Digest sent to *that Customer*. This is backed by a ledger of (Customer, Event)
  pairs, written only for Events actually included in a sent Digest. We deliberately did not add
  a `published_at` column to `events`.
- **Digests send from a sending domain separate from transactional mail.** One-time Passcodes,
  Sale Confirmations and Payout notices keep the existing sender; the Digest gets its own
  subdomain, provider domain and DNS records.

Delivery follows the pattern ADR 0024 established for the Reversal Reconciler — Cloud Scheduler
with a dedicated service account and an OIDC token, calling an endpoint under `/api/v1/internal/`,
gated by Cloud Run IAM. A weekly job enqueues one pending Digest per Customer; a per-minute drain
composes and sends within the provider's rate limit, with `attempt_count` and `next_attempt_at`
as `sale_reversals` does.

## Why this way

- **A digest is what made Following any Tag safe.** Per-Event mail scales with how many Events a
  Customer's Follows match, so Following a busy Tag would produce an unbounded stream. The
  alternatives were both worse: restrict what is followable (ADR 0004's `curated` flag was the
  obvious lever, but it would have cost the most valuable case — a narrow interest like "Techno" —
  to protect against a problem of our own making), or cap per-Event mail with rules a reader
  cannot see and cannot predict. One email a week bounds volume *structurally*, at the only number
  a reader can verify. Every Tag stays followable because of it.
- **The original reminder intent survives inside the Digest.** "Happening this week" is the
  week-before reminder, and Events the Customer already holds Tickets to are marked as attending
  rather than advertised. What was lost is same-day timing, which a weekly cadence cannot give.
- **`published_at` would have been wrong, not merely insufficient.** Nothing timestamps the
  `discoverable` flip either, so an Event published in January and listed in March would have been
  three months stale on the day the public first saw it, and an Event tagged after publication
  would never have been new to that Tag's followers at all. Defining novelty on the reader is
  correct in both cases, and it is the only definition under which a Customer who Follows a busy
  Tag gets a useful first Digest instead of an empty one — the worst possible answer to the action
  we most want them to take.
- **One ledger, four jobs.** The same (Customer, Event) rows are the novelty test, the dedupe when
  an Event is matched by both a Tag and an Organization, the guard against repeating an Event week
  after week, and the idempotency that lets a failed send be retried without mailing twice. A
  cheaper novelty rule would have needed three further mechanisms beside it.
- **The domain split is not about Follows at all.** It is the highest-risk detail in the feature.
  Marketing mail attracts spam complaints in a way transactional mail never does, and complaint
  rates degrade domain reputation. Sharing `send.multiticketing.com` would let an annoying Digest
  impair delivery of the One-time Passcodes people need in order to sign in — a discovery feature
  taking down authentication. Separating the domains costs some DNS records and removes that
  coupling entirely.
- **Unsubscribing is a switch, not a purge.** The unsubscribe link cannot require signing in, and
  mail security scanners routinely prefetch links in messages. Had unsubscribe meant "Unfollow
  everything", corporate scanners would have silently wiped Customers' lists. It flips one
  reversible flag instead, and the link confirms with a POST so a prefetch alone does nothing.

## Consequences

- The system sends its first non-transactional email. Everything before this answered something the
  Customer had just done; this one arrives unbidden, which is why it is the only mail with an
  unsubscribe and the only mail on its own domain.
- A (Customer, Event) ledger grows with followers × Events. Rows are pruned once an Event has ended,
  and it is the only table in the system whose size is driven by reading rather than by selling.
- The ledger records what a person was *shown*, so it is independent of Follows: Unfollowing and
  Following again does not make previously shown Events new. A future reader expecting Follows to
  reset novelty will find they do not, deliberately.
- A Customer Following several busy Tags can sit permanently at the cap, so their carried overflow
  drains lazily and far-future Events may be deferred a long time. Accepted: they are still getting
  a full Digest every week, and the "+N more" links reach everything.
- **This is the second scheduled execution in the deployment**, after ADR 0024's reconciler, and the
  first that fans out to many recipients. The provider rate limit ADR 0009 flagged as unsolved for
  bulk sends is answered here for this path only — Sale Confirmations on import remain as they were.
- **ADR 0027 is amended, narrowly.** Preset Tag copy lives in the Storefront message catalogues, so
  the backend cannot render "Música". A Spanish display name is added to `tags` for `curated = true`
  rows only, seeded by migration. The Storefront catalogue is unchanged and Custom Tags stay
  untranslated, which ADR 0027 already holds correct. Twelve strings are duplicated in one
  direction; the decision is not reversed.
- Customers now carry a remembered Locale, because a Locale is otherwise a property of a page's
  address and mail has no address.
- **Tags carry email for the first time.** `maxEventTags` stays at 20 and no anti-stuffing control
  ships: the per-reader cap bounds the harm to relevance rather than volume, and policing tags per
  Event would invent a moderation problem before we have one. Which Follow matched each Digest entry
  is recorded so that stuffing can be measured before anything is legislated against it.
- Organizations get no follower counts and no analytics, and there is no public follower count. The
  Follow tables can answer those whenever the need is real.
