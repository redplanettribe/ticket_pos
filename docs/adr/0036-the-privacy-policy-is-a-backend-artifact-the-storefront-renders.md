# The Privacy Policy is a backend artifact the Storefront renders

## Context

A Policy Version records the SHA-256 "of the exact rendered notice + policy text", so that a
compliance officer can prove what a person was shown when they accepted (#249). The obvious place
for the text was `apps/storefront/messages/{en,es}.json`, beside every other word on the
Storefront, and ADR 0027 points that way: the API stays Locale-unaware, and words this product
chooses live in the catalog.

It does not survive the hash. Copy in the message catalogs is compiled into a different runtime,
deployed on its own cadence, and reachable from no Go test — so the value on the row would have
been a hash of a file, computed by hand at some point, with nothing able to notice when the text
and the fingerprint drifted apart. The one property the column exists to have would have been
unenforceable, and unenforced properties about evidence are worse than absent ones.

## Decision

The Privacy Policy, its Short Notice and the three consent checkbox labels are embedded in the Go
binary, one directory per Locale, and hashed there. A public endpoint,
`GET /api/v1/public/privacy-policy/{locale}`, serves one Locale's whole artifact set in a single
payload, and the Storefront page renders it through the Next BFF (ADR 0008) like any other public
read.

The Policy Version's `content_hash` is computed over the served values of every published Locale
at once — one edition of the policy, not one per language. A Go test recomputes it from the
embedded artifacts and compares it against the seed in the migration, so editing the policy text
without publishing a new edition fails the build.

The page's own chrome — its heading, its effective-date label, the footer link's words — stays in
the message catalogs, worded per language like everything else.

## Consequences

- **This is a narrow exception to ADR 0027, and the boundary is the point.** That decision is
  about COPY, where the worst case of drift is an English chip on a Spanish page. This is
  EVIDENCE: a legal artifact whose integrity is the feature, published in editions rather than
  restyled, and quoted back in an audit. The rule that survives both is "the catalog holds words
  this app chooses"; nobody chose these.
- **The API gains its one localized route.** The Locale is a path segment rather than an
  `Accept-Language`, so the two languages are two addresses, each cacheable and each a 404 in its
  own right — and the rest of the API stays exactly as Locale-unaware as it was.
- **The policy and the checkboxes cannot come from different editions.** One payload carries the
  page's text and the capture surfaces' labels, so a deploy cannot leave a Customer accepting one
  wording while reading another.
- **Publishing an edition is a backend deploy.** A wording change is a Go commit, a new
  `policy_versions` row and a migration, not a copy edit merged by whoever owns the Storefront —
  which is the correct weight for an act that re-gates every Customer, and a real cost for
  typo-level corrections.
- **A Storefront that cannot reach the API has no Privacy Policy page.** It 404s rather than
  rendering an empty one. Every other public page degrades to less content; this one would be
  making a promise the platform was not keeping.
