# Adulthood is declared on the Terms gate, never verified, and a refusal leaves no record

Specified as issue #584.
Extends [ADR 0066](./0066-the-terms-are-accepted-twice-as-attendee-on-the-storefront-as-organizer-before-a-staff-session.md),
whose two capacities it inherits whole, and is published through the machinery of
[ADR 0067](./0067-the-legal-text-lives-in-the-database-the-operator-publishes-it-and-revision-zero-is-the-whole-of-gating.md).
It adds no document, no gate and no table.

## Context

The Términos y Condiciones already carry the rule, in both published Locales:

> Only persons who have reached eighteen years of age may buy through Multiticketing. The user
> declares that they meet this requirement. Additional restrictions of age or capacity for an
> event, drink, prize draw, area or benefit are the organizer's responsibility and must be stated
> in the particular conditions and verified at the event.

So the obligation exists, the threshold is fixed at eighteen, everything event-specific is already
delegated to the organizer and checked at the door, and the contract already puts the declaration
in the person's mouth. What it does not do is evidence it separately: today the declaration is
bundled into accepting the document as a whole, and the only answer the platform can give to
"show me that this individual affirmed they were an adult" is an inference from a text's contents.

Nothing else in the system knows the concept. There is no age, date-of-birth, minor or adult
column, field, checkbox or validation anywhere in the schema, the Go packages, the API or either
frontend. The Privacy Policy meanwhile already names the idea in its own definition of a data
subject — "la persona natural, **mayor de edad** que pertenezca a una o varias de las siguientes
categorías" — so the vocabulary exists in the contract and nowhere in the code.

(The Policy separately still serves an unfilled placeholder, "personas menores de **[EDAD
MÍNIMA]**", in both languages. That is a defect of the Policy's text, corrected by its own
publish, and deliberately out of this decision's scope: the two documents version independently.)

## Decision

**An Adulthood Declaration is a declared fact about a person, not an acceptance of a document.**
It has no editions, no `generation`/`revision` lineage, no content hash of its own and no
satisfying set, because there is nothing about it that can change and therefore nothing to
re-ask. What *is* editioned is the wording shown — one Artifact, `label-adulthood-declaration`,
on the Terms edition, hashed into that edition's fingerprint exactly as `label-terms-acceptance`
is, so the words a person was shown stay reproducible for as long as the acceptance does.

**It rides the Terms gate and has no gate of its own.** It is introduced by publishing a Gating
Edition of the Terms, which the machinery compels rather than merely permits: adding an Artifact
changes the slug set, that is a structural change, and a Correction is refused over one. The
Gating Edition owes everybody — Customers and staff — a fresh acceptance, so the declaration is
collected from the entire population by the gate that already exists, at the sign-in consent
step, the checkout owed-boxes fallback and the staff interstitial. There is no backfill, no
migration over `customers`, no second Standing to compute and no second population to chase.

**Its own mandatory, un-premarked checkbox, in both capacities.** Not a clause folded into the
Terms box: a combined tick evidences that somebody accepted a document containing an age
sentence, which is the inference this decision exists to replace. Attendees tick it on the
Storefront and organizers tick it on the Staff platform, because ADR 0066's organizer acceptance
is the more contractual of the two and capacity to contract is precisely what is being declared.
The Artifact is therefore worded about *being of age* rather than about *buying*, so that it
reads true in the capacity that is not buying anything.

**A refusal is forgotten.** An untick is refused by the API with `ADULTHOOD_DECLARATION_REQUIRED`
— 400, before any `Capture`, exactly as `TERMS_ACCEPTANCE_REQUIRED` is — and nothing whatever is
written. The platform keeps **no record of anyone who says they are a minor**. Such a record would
be a permanent, unverified assertion that a named individual is a child, on a table that is never
edited and never deleted, about the one population the Privacy Policy promises not to knowingly
process; and it would go stale in the worst direction, since the sixteen-year-old it names turns
eighteen while the row does not, with no edit path to say so. The surface says plainly that a
person must be eighteen or older to use Multiticketing, which is what the contract says, rather
than reporting a failed field validation.

**The answer is stored, never derived.** `adulthood_declaration BOOLEAN` on `consent_records` and
on `staff_terms_acceptances`, beside the existing `terms_acceptance` and `capacity`. It is
genuinely redundant — a refusal writes nothing, so every acceptance of an Artifact-carrying
edition necessarily ticked both boxes, and the answer could be recovered by joining to the
edition's slug set. It is stored anyway because deriving it would make a fact about the past
depend on how rows are read in the present: a renamed slug, a mistyped slug in some later
publish, or a change to the slug-to-field mapping would silently rewrite what thousands of people
are recorded as having declared. That is the failure the content hash exists to prevent, and
`consent_records.terms_acceptance` — already implied by its paired `terms_version_id` — is the
precedent.

**Nothing is kept on `customers`.** The current-state columns there exist to answer which boxes a
person must still be *shown*, which is `Outstanding`'s question; and because the declaration is
welded to the Terms gate, nothing ever decides independently whether to show it. Every pair in
that block also names an edition of a document, and this names none.

**A declaration, never a verification.** Eighteen is a number in prose inside the Artifact, in
both languages — no setting, no column, no per-Organization override, and no per-Event rule,
those being the organizer's by the contract's own words. **No date of birth is collected,
anywhere.** A birthdate is a far more identifying datum, it invites a verification the platform
cannot perform, and it would need its own basis under the Policy. The declaration's whole legal
weight is that the person asserted it and the platform can prove what they were shown when they
did; a checkbox is not a step toward identity verification and is not intended as one.

**The Artifact is optional to the binary, though the checkbox is mandatory to the person.**
`terms.DocumentFrom` requires its known slugs and returns `false` when one is missing, so a
binary that *required* `label-adulthood-declaration` would refuse the current edition the instant
it deployed, taking down the public terms page, the sign-in gate and the staff interstitial until
an operator published. The label is therefore served when present and absent otherwise, and the
box is drawn iff the edition in effect carries it. Rendering derives from the edition — correct,
since what to show is a fact about now — while storage stays explicit, per the paragraph above.
The two are not in tension; they are different questions.

**No ratchet on removal.** A later edition may drop the Artifact and thereby stop collecting.
Nothing special guards it, because the general machinery already does: removal is `CellRemoved`,
which is structural, which forces a Gating Edition — dated at least tomorrow, cancellable
overnight, refused without a seen diff, counted on the publish summary, and re-gating the entire
population. A rule naming this one slug would be the publish path learning about one particular
checkbox, and the first crack in "an artifact is an artifact".

**It is visible in the evidence and not in the browsers.** The Consent Evidence Pack carries it —
that is the point of the feature, and "declared on such a date under such an edition" is a
finished fact that reads identically on any day, so the pack stays byte-reproducible. The
per-subject record shows it per act, an act captured under an earlier edition reading "never
asked" in that screen's existing vocabulary for a null. The acceptance browsers gain nothing: an
adulthood standing would be a near-copy of Terms standing, and those browsers are deliberately
not a segmentation tool. The Consent Access Log gains no act, reading a declaration being a
`subject_read` of the record that holds it.

## Considered options

**A third editioned document, "Age Attestation", with its own versions table and gate.** Rejected:
it inherits Legal Draft, Artifacts, Corrections, gating floors and a satisfying set to express a
sentence that cannot change and can never re-gate anybody, and it makes a third copy of the whole
Legal Center machinery.

**An independent Age Gate,** blocking on session establishment and checkout for anyone with no
declaration on file, regardless of Terms Standing. It would have bought a rollout on its own
timetable, independent of the Terms. Rejected: a second interstitial competing for the same
moment on the same screen, with its own Standing, its own Outstanding population and its own way
to disagree with the first gate about who has been asked — over an obligation the Terms already
cover.

**One combined checkbox** reading "acepto los Términos y declaro ser mayor de edad". Rejected for
the reason the separate box exists at all, and because it makes the meaningful state unsayable:
declining the Terms means "I do not agree", declining adulthood means "I am a child", and one
control cannot say both.

**Recording the negative** — a row with `adulthood_declaration = false`, and a known-minor state
thereafter. Rejected as above; it would also have been the first required box ever stored as
`false` in this system.

**Deriving the answer from the edition's artifact set,** storing no column. Rejected: it makes
finished facts re-derivable, and re-derivable evidence is not evidence.

**A migration seeding Terms edition `'2'`,** as migration 105 seeded `'1'`. It is atomic and needs
no operator. Rejected: it publishes legal text by deploy, which ADR 0067 reversed, and it cannot
carry the dated-for-tomorrow, cancellable-overnight ceremony that makes a gating publish safe.

**Collecting a date of birth** and computing adulthood. Rejected: see above.

## Consequences

- **The deploy strictly precedes the publish, by hand.** Step one ships a binary that knows
  `label-adulthood-declaration` and does not require it; step two is an operator authoring the
  Artifact in the Legal Draft and publishing a Gating Edition. Reversing the order publishes
  bytes inside a fingerprint that no reader is ever shown — tolerated by `DocumentFrom`'s
  `default: continue` and logged once per cache fill, but silent to the person at the keyboard.
  GitHub Actions has refused every run on billing since 2026-08-28 and production deploys
  manually, so nothing enforces this ordering but the runbook.
- **Everybody is re-gated, staff included and mid-work.** The staff gate binds on the next page
  navigation as a read-and-accept interstitial (ADR 0067 amending ADR 0066), so every Member and
  Platform Operator meets two boxes the morning the edition takes effect. `/api/` is not gated, so
  a sale in progress still commits.
- **The new Artifact takes an ordinal and shifts `terms` behind it,** changing the content hash's
  preimage order. Harmless — it is a new edition, and the ordinal lives on the row precisely so
  that adding an Artifact needs no deploy to change how an edition is hashed.
- **The checkout snapshot grows a column.** `payments` carries the answers a held payment will be
  committed with, so it gains `consent_adulthood_declaration` beside `consent_terms_acceptance`,
  under the same paired CHECK discipline.
- **A future edition can silently stop collecting,** by omitting the Artifact. Accepted: the act
  costs a full re-gating Gating Edition with a seen diff, which nobody performs by accident.
- **The Terms body may want rewording.** "The user declares that they meet this requirement" was
  written for a world in which accepting the document *was* the declaration, and now describes a
  checkbox. Whether it changes is counsel's call, not engineering's; if it does, it rides the same
  Gating Edition at no extra cost.
- **The Policy's `[EDAD MÍNIMA]` placeholder is untouched here** and is corrected by its own
  publish — a Correction, since replacing a word is not structural, taking effect at once and
  re-gating nobody.
