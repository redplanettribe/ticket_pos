package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/policy"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Publishing (#563, spec #556, ADR 0067): the one act in the Legal Center that
// a reader can see.
//
// TWO ACTS AND NOT ONE. A GATING EDITION takes the next generation, re-gates
// everybody standing below it, and waits a night. A CORRECTION takes the next
// revision within its generation, re-gates NOBODY, needs a typed reason, and
// takes effect at once. Neither mutates anything: a correction is a new row, and
// the old bytes stay exactly as the acceptances that fingerprint them expect.
//
// THE RAIL. The gating edition is the default and obvious act, because it is the
// SAFE one — it asks everybody again — and the correction is a quiet link
// beneath it, never an equal option, so "re-gate nobody" is never a coin toss.
// That is a decision about the screen; what this file owes it is the DATA the
// screen needs to be honest: the label each act would create, so choosing the
// correction visibly turns `2` into `1.2`, and the headcount the gating act
// would re-gate, so the consequence is shown before it is accepted.
//
// NO APPROVAL STEP, ON EITHER ACT. Production holds one platform_operators row
// with no roles and no second tier, so a two-person rule deadlocks on every act,
// and recruiting an approver would also hand them payouts, reversals and
// invoicing backfill — operator authority is one boolean. What is irreversible
// is not the text but the RE-GATING, so the overnight delay lands only on the
// act that moves the gating floor, and the substitute for review is proving the
// operator was SHOWN the consequence rather than that anybody agreed with it.
//
// A PUBLISH REVOKES NO SESSION, in either population, and there is no control
// for it even as an option — see #570. It also invalidates NO CACHE (#558): the
// legal text cache ages on its own, sixty seconds at a time, and a publication
// that reached into it would be the publication hook the whole scheduled-edition
// design is arranged to avoid.

// MinCorrectionReasonLength is what "a typed reason of meaningful length" is
// worth in characters.
//
// A bound and not a formality. The reason is the one thing that cannot be
// recovered from the bytes — the diff says what moved, the reason says what it
// was for — and "fix", "typo" or a stray keypress is not that. Ten characters is
// low enough that a real sentence never trips it and high enough that a reflex
// keystroke does.
const MinCorrectionReasonLength = 10

// PublishLegalEditionInput is one publication as the operator asked for it.
//
// THERE IS NO LABEL FIELD AND THERE NEVER WILL BE. Labels are system-generated
// from the lineage (legal.Lineage.Label) and migration 110 makes the database
// refuse anything that function would not have written, so there is no path —
// not this struct, not a migration, not a psql session — by which an edition
// acquires a name that lies about its descent.
//
// There is no content field either. What is published is the DRAFT, exactly as
// it was saved, previewed and diffed; a body that could restate the text would
// let the publication differ from the thing the operator was shown.
type PublishLegalEditionInput struct {
	// Kind is legal.PublishGating ("edition") or legal.PublishCorrection
	// ("correction"). No default: which act this is decides whether the entire
	// customer base is re-gated.
	Kind string
	// EffectiveDate is YYYY-MM-DD, read as 00:00 America/Guayaquil, and empty
	// means "now". REQUIRED AND AT LEAST TOMORROW on a gating publish, so an
	// irreversible re-gate has a night in which the operator can change their
	// mind; REFUSED on a correction, which takes effect immediately.
	EffectiveDate string
	// Reason is the typed correction reason. Required on a correction; a gating
	// edition is its own justification and has no column to keep one in
	// (migration 112).
	Reason string
	// By is the operator's email, taken from the Staff Session by the handler
	// and never from the body.
	By string
}

// OperatorLegalPublishPlan is what the publish step would do if it were pressed
// now: the labels, the consequence, and every reason it would refuse.
//
// IT IS PART OF THE WORKSPACE READ rather than an endpoint of its own, for the
// reason the workspace is one payload at all: the label a publication would
// take, the headcount it would re-gate and the diff it is a diff of are all
// statements about ONE published edition beside ONE draft, and a second read
// could straddle a publication and answer about neither.
//
// EVERY REFUSAL IS REPORTED, not just the first. The screen disables the
// correction link with a reason on it rather than letting an operator press a
// button to find out — but the refusals are enforced at the HTTP seam and this
// struct is not where they live. A precondition a client could decline to check
// is not a precondition.
type OperatorLegalPublishPlan struct {
	// GatingLabel is the label a NEW EDITION would take: the next generation, at
	// revision 0. It goes ON the publish button, so the operator reads the name
	// of the thing they are about to create.
	GatingLabel string `json:"gating_label"`
	// CorrectionLabel is the label a CORRECTION would take: the next revision
	// within the current generation, flat — never nested. Shown on the quiet
	// link, so choosing it visibly turns `2` into `1.2`.
	CorrectionLabel string `json:"correction_label"`
	// Headcount is how many people a gating publication would re-gate: those
	// standing on an edition that clears the gate today and would not clear it
	// after. It goes on the CONFIRM button ("Publish and re-gate 1,058"), and it
	// is stored on the version row exactly as it was shown.
	//
	// A CORRECTION'S HEADCOUNT IS ALWAYS ZERO and is not carried separately,
	// because "re-gate nobody" is not a number the platform computes — it is
	// what legal.GatingFloor's skipping of corrections makes true for free.
	Headcount int `json:"headcount"`

	// The three gates, reported apart so the editor can point at the one that is
	// missing: `can_publish` is exactly `complete && previewed_all && seen_diff`.
	CanPublish bool `json:"can_publish"`
	Complete   bool `json:"complete"`
	// Gaps names every hole, so an operator can finish rather than hunt.
	Gaps []legal.CellRef `json:"gaps"`

	// Why a CORRECTION is not available, each reported separately because each
	// is a different sentence about a different mistake.
	Structural       bool `json:"structural"`
	LocaleSetChanged bool `json:"locale_set_changed"`
	// EmptyDiff refuses a correction and permits a gating edition, deliberately:
	// re-gating over unchanged text is a real act, and correcting nothing is not.
	EmptyDiff bool `json:"empty_diff"`
	// CanCorrect is the three above, negated and combined with CanPublish.
	CanCorrect bool `json:"can_correct"`

	// ProtectedLocale is the language this document may not be published
	// without, as a CONSTANT of the document's own package and never a column
	// (policy.MandatoryLocale, terms.PrevailingLocale). Sent so the editor can
	// disable the toggle rather than hard-code a language; the two nouns those
	// constants are named for never reach a screen.
	ProtectedLocale string `json:"protected_locale"`
	// ProtectedLocaleKept is false when the draft would drop it — refused in
	// BOTH publish kinds.
	ProtectedLocaleKept bool `json:"protected_locale_kept"`

	// EarliestEffectiveDate is the first day a GATING edition may take effect:
	// the later of tomorrow and the day after the newest edition already
	// published. The editor uses it as the date input's minimum.
	EarliestEffectiveDate string `json:"earliest_effective_date"`
	// DiffSummary is the line that would be stored on the version row.
	DiffSummary string `json:"diff_summary"`
}

// PublishLegalEdition publishes the document's draft as a new edition or as a
// correction, and answers with the workspace as it now stands — whose draft is
// once again a copy of the published edition, because the draft became it.
func (s *Service) PublishLegalEdition(ctx context.Context, document string, input PublishLegalEditionInput) (*OperatorLegalWorkspace, error) {
	document, err := parseLegalDocument(document)
	if err != nil {
		return nil, err
	}

	kind, ok := legal.ParsePublishKind(input.Kind)
	if !ok {
		return nil, consent.ErrLegalPublishKindUnknown(input.Kind)
	}

	draft, err := s.repo.LegalDraft(ctx, document)
	if errors.Is(err, repository.ErrNoLegalDraft) {
		// Nothing to publish. The same refusal a preview gives, for the same
		// reason: what would be published is the SAVED draft, and there is none.
		return nil, consent.ErrLegalDraftNotStored()
	}
	if err != nil {
		return nil, err
	}

	publishedID, published, err := s.publishedArtifacts(ctx, document)
	if err != nil {
		return nil, err
	}

	// THE PROTECTED LOCALE IS CHECKED FIRST, before completeness and before
	// either kind's own rules, because it is the only refusal on this path that
	// a code change and a deploy are needed to lift. An operator who has dropped
	// Spanish should be told that and not told about a preview gap in a language
	// they were about to stop publishing.
	if !hasProtectedLocale(document, draft.PublishedLocales) {
		return nil, consent.ErrLegalProtectedLocaleRequired(
			document, string(protectedLocale(document)), protectedLocaleRefusal(document))
	}

	// The three gates, in the order the screen states them.
	gaps := legal.Completeness(draft.Artifacts, draft.PublishedLocales)
	if len(gaps) > 0 {
		return nil, consent.ErrLegalPublishIncomplete(gaps)
	}

	review, err := s.draftReview(ctx, document, draft, publishedID)
	if err != nil {
		return nil, err
	}
	if !review.PreviewedAll {
		return nil, consent.ErrLegalPublishNotPreviewed(review.PreviewGaps)
	}
	if !review.SeenDiff {
		// This is also where a publication underneath the draft is caught: the
		// seen-diff record names the edition it was taken against, so it lapses
		// the moment somebody else publishes. There is deliberately no separate
		// "your base is stale" refusal saying the same thing twice.
		return nil, consent.ErrLegalPublishDiffNotSeen()
	}

	editions, err := s.lineage(ctx, document)
	if err != nil {
		return nil, err
	}

	write := repository.PublishLegalEditionInput{
		Document:    document,
		Artifacts:   legal.PublishedIn(draft.Artifacts, draft.PublishedLocales),
		PublishedBy: input.By,
		PublishedAt: s.now(),
		DiffSummary: legal.DiffSummary(published, draft.Artifacts, draft.PublishedLocales),
	}
	write.ContentHash = legal.ContentHash(write.Artifacts)

	switch kind {
	case legal.PublishCorrection:
		if legal.IsStructural(published, draft.Artifacts, draft.PublishedLocales) {
			return nil, consent.ErrLegalCorrectionStructural()
		}
		if legal.LocaleSetChanged(published, draft.PublishedLocales) {
			return nil, consent.ErrLegalCorrectionLocaleSetChanged()
		}
		if legal.IsEmptyDiff(published, draft.Artifacts, draft.PublishedLocales) {
			return nil, consent.ErrLegalCorrectionEmptyDiff()
		}
		reason := strings.TrimSpace(input.Reason)
		if len([]rune(reason)) < MinCorrectionReasonLength {
			return nil, consent.ErrLegalCorrectionReasonRequired()
		}
		if strings.TrimSpace(input.EffectiveDate) != "" {
			return nil, consent.ErrLegalCorrectionEffectiveDateRefused()
		}

		lineage, err := legal.NextCorrection(editions, currentGeneration(editions, publishedID))
		if err != nil {
			return nil, err
		}
		write.Label = lineage.Label()
		write.Generation, write.Revision = lineage.Generation, lineage.Revision
		// IMMEDIATELY, so a typo fix does not wait overnight. A correction moves
		// no gating floor, so there is nothing for a night to protect against.
		//
		// "Immediately" means AS SOON AS THE EDITION IT CORRECTS IS CURRENT, which
		// is today for an edition already in force and that edition's own day for
		// one still scheduled. Plain "today" would be wrong in the second case and
		// silently so: every current-edition selector orders on effective_date
		// first, so a correction dated before the edition it corrects would sort
		// below it and never be served — a correction that corrected nothing
		// anybody could read.
		write.EffectiveDate = platform.StartOfEcuadorDay(s.now())
		if corrected := effectiveDateOf(editions, publishedID); corrected.After(write.EffectiveDate) {
			write.EffectiveDate = corrected
		}
		write.CorrectionReason = reason
		// RE-GATES NOBODY, and the zero is not a computation. GatingFloor skips
		// corrections, so nobody moves; migration 112 refuses any other value on
		// a correction row.
		write.RegatedHeadcount = 0

	default: // legal.PublishGating
		earliest := earliestGatingDate(editions, s.now())
		effective, err := parseEffectiveDate(input.EffectiveDate)
		if err != nil {
			return nil, err
		}
		// "Now" is refused: an empty date means today, and today is not a night
		// away. This is the whole of the overnight delay.
		if effective == "" || effective < earliest {
			return nil, consent.ErrLegalGatingEffectiveDateTooSoon(earliest)
		}
		day, _ := time.Parse("2006-01-02", effective)

		lineage := legal.NextGating(editions)
		write.Label = lineage.Label()
		write.Generation, write.Revision = lineage.Generation, lineage.Revision
		write.EffectiveDate = day
		// A reason has no column on a gating edition (migration 112 refuses
		// one): the edition IS the change, and its justification is its text.

		headcount, err := s.regatedHeadcount(ctx, document, editions)
		if err != nil {
			return nil, err
		}
		write.RegatedHeadcount = headcount
	}

	versionID, err := s.repo.PublishLegalEdition(ctx, write)
	if err != nil {
		return nil, err
	}

	s.logger.Info("legal edition published",
		"document", document,
		"kind", string(kind),
		"label", write.Label,
		"version_id", versionID,
		"effective_date", write.EffectiveDate.Format("2006-01-02"),
		"content_hash", write.ContentHash,
		"regated", write.RegatedHeadcount,
		"operator", input.By,
	)

	return s.LegalWorkspace(ctx, document)
}

// publishPlan is what the publish step would do, assembled for the workspace
// read. It performs the lineage read and the headcount count; it refuses
// nothing, because refusing is the publish call's job and a screen that could
// refuse would be a second implementation of every rule.
func (s *Service) publishPlan(
	ctx context.Context,
	document string,
	publishedID string,
	published []legal.Artifact,
	draft []legal.Artifact,
	draftLocales []platform.Locale,
	previewedAll, seenDiff bool,
) (OperatorLegalPublishPlan, []legal.Edition, error) {
	editions, err := s.lineage(ctx, document)
	if err != nil {
		return OperatorLegalPublishPlan{}, nil, err
	}

	gaps := legal.Completeness(draft, draftLocales)
	complete := len(gaps) == 0

	plan := OperatorLegalPublishPlan{
		GatingLabel:           legal.NextGating(editions).Label(),
		CanPublish:            complete && previewedAll && seenDiff,
		Complete:              complete,
		Gaps:                  gaps,
		Structural:            legal.IsStructural(published, draft, draftLocales),
		LocaleSetChanged:      legal.LocaleSetChanged(published, draftLocales),
		EmptyDiff:             legal.IsEmptyDiff(published, draft, draftLocales),
		ProtectedLocale:       string(protectedLocale(document)),
		ProtectedLocaleKept:   hasProtectedLocale(document, draftLocales),
		EarliestEffectiveDate: earliestGatingDate(editions, s.now()),
		DiffSummary:           legal.DiffSummary(published, draft, draftLocales),
	}
	if correction, err := legal.NextCorrection(editions, currentGeneration(editions, publishedID)); err == nil {
		plan.CorrectionLabel = correction.Label()
	}
	plan.CanCorrect = plan.CanPublish &&
		plan.ProtectedLocaleKept &&
		!plan.Structural && !plan.LocaleSetChanged && !plan.EmptyDiff

	headcount, err := s.regatedHeadcount(ctx, document, editions)
	if err != nil {
		return OperatorLegalPublishPlan{}, nil, err
	}
	plan.Headcount = headcount
	// The lineage travels back with the plan so the workspace can name the
	// scheduled editions (#564) from the SAME read: what a publication would do
	// and what one already did are two questions about one list of rows, and a
	// second query could straddle a publication or a cancellation.
	return plan, editions, nil
}

// regatedHeadcount is "who would this re-gate": the people standing on an
// edition that clears the gate TODAY and would not clear it once a new gating
// edition arrived.
//
// The Policy gate is the Customer base's alone. The Terms gate is BOTH
// populations' — every human on the Staff platform accepts the Términos y
// Condiciones before a Staff Session is minted (§3, ADR 0066) — so a Terms
// publication re-gates staff too and the button must say so.
//
// It is computed from this module's own repository rather than from the
// acceptance browsers (#565): the browsers page and search, this counts, and a
// count that went through a pager would be a count of a page.
func (s *Service) regatedHeadcount(ctx context.Context, document string, editions []legal.Edition) (int, error) {
	satisfying, ok := legal.Satisfying(editions)
	if !ok {
		// No gating edition has arrived, so nobody is standing anywhere and a
		// publication re-gates nobody. Migrations 060 and 105 make this
		// unreachable; answering 0 is the honest answer if it ever is not.
		return 0, nil
	}

	customers, err := s.repo.RegatedCustomerCount(ctx, document, satisfying.IDs())
	if err != nil {
		return 0, err
	}
	if document == LegalDocumentPolicy {
		return customers, nil
	}

	staff, err := s.repo.RegatedStaffCount(ctx, satisfying.IDs())
	if err != nil {
		return 0, err
	}
	return customers + staff, nil
}

// lineage reads one document's whole published lineage.
func (s *Service) lineage(ctx context.Context, document string) ([]legal.Edition, error) {
	if document == LegalDocumentPolicy {
		return s.repo.PolicyLineage(ctx)
	}
	return s.repo.TermsLineage(ctx)
}

// publishedArtifacts reads the current edition's id and every stored word of it,
// through the same repository calls every reader uses and NOT through the legal
// text cache: the cache serves a document that is at most sixty seconds stale,
// which is right for a Storefront page and wrong for the read a publication is
// about to be diffed against.
func (s *Service) publishedArtifacts(ctx context.Context, document string) (string, []legal.Artifact, error) {
	if document == LegalDocumentPolicy {
		edition, err := s.repo.CurrentPolicyEdition(ctx)
		if err != nil {
			return "", nil, err
		}
		return edition.Version.ID, edition.Artifacts, nil
	}
	edition, err := s.repo.CurrentTermsEdition(ctx)
	if err != nil {
		return "", nil, err
	}
	return edition.Version.ID, edition.Artifacts, nil
}

// currentGeneration is the generation a correction would descend from: the one
// the currently published edition belongs to.
//
// A correction corrects WHAT IS PUBLISHED, so it belongs to that edition's
// generation and not to the highest generation on record — which may be a
// future-dated edition nobody has read yet, and which a correction to today's
// text has nothing to say about.
func currentGeneration(editions []legal.Edition, publishedID string) int {
	for _, edition := range editions {
		if edition.ID == publishedID {
			return edition.Lineage.Generation
		}
	}
	return -1
}

// effectiveDateOf is one edition's day, or the zero time when the lineage does
// not name it.
func effectiveDateOf(editions []legal.Edition, id string) time.Time {
	for _, edition := range editions {
		if edition.ID == id {
			return edition.EffectiveDate
		}
	}
	return time.Time{}
}

// earliestGatingDate is the first day a gating edition may take effect, as
// YYYY-MM-DD in the platform's own wall clock (America/Guayaquil).
//
// TWO BOUNDS, AND THE LATER OF THEM WINS:
//
//   - TOMORROW, which is the ruled overnight delay: an irreversible re-gate gets
//     a night in which the operator can change their mind, and "now" is refused.
//   - THE DAY AFTER THE NEWEST EDITION ALREADY PUBLISHED. An edition effective
//     on or before one that already exists would sort below it in every
//     current-edition selector this codebase has, so it would become current
//     never and re-gate nobody, ever. A re-gating button whose most likely
//     failure is doing nothing silently is worse than one that refuses; this is
//     the ONE bound the ticket does not state, and it is stated here rather than
//     left to be discovered.
//
// A CANCELLED EDITION BINDS NEITHER (#564). It will never be current, so nothing
// would sort below it and nothing is silently defeated by taking its day back —
// and holding the floor above a withdrawn edition's date would be the
// cancellation still costing the operator something after it was undone. This is
// the mirror of legal.NextGating, which DOES count cancelled editions: the label
// stays spent because a name must mean one thing forever, and the day is
// released because a day is not a name.
func earliestGatingDate(editions []legal.Edition, now time.Time) string {
	earliest := platform.StartOfEcuadorDay(now).AddDate(0, 0, 1).Format("2006-01-02")
	for _, edition := range editions {
		if !edition.Counts() {
			continue
		}
		if after := edition.EffectiveDate.AddDate(0, 0, 1).Format("2006-01-02"); after > earliest {
			earliest = after
		}
	}
	return earliest
}

// parseEffectiveDate reads the requested day. Empty stays empty and means "now",
// which each publish kind answers for itself: a correction takes it, a gating
// edition refuses it.
//
// DATE-ONLY. An effective date is a legal fact stated on the document itself,
// published to the day, and a reader comparing the page to the row must see the
// same thing they read on it. It is read as 00:00 America/Guayaquil, which is
// what the column already means everywhere else.
func parseEffectiveDate(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	day, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return "", consent.ErrLegalEffectiveDateInvalid(raw)
	}
	// Round-tripped rather than trusted, so "2026-13-45" and "2026-2-3" are both
	// refused instead of silently normalised into a day nobody typed.
	if day.Format("2006-01-02") != raw {
		return "", consent.ErrLegalEffectiveDateInvalid(raw)
	}
	return raw, nil
}

// protectedLocale is the language a document may not be published without.
//
// TWO CONSTANTS, TWO PACKAGES, TWO FOOTINGS, and this switch is the ONLY place
// they meet. A single shared constant was refused because it would flatten the
// stronger footing into the weaker: the Policy's Spanish rests on the Ley
// Orgánica de Protección de Datos Personales' notice duty — statutory, and
// beyond anybody here to amend — while the Terms' rests on §37, a clause the
// Foundation wrote and could change. The two documents can publish different
// language sets; they stay genuinely independent (ADR 0066).
//
// CONSULTED ONLY AT PUBLISH. Nothing that reads a published edition asks this
// question, so a future change to either duty cannot retroactively invalidate an
// edition published honestly under the old one.
func protectedLocale(document string) platform.Locale {
	if document == LegalDocumentPolicy {
		return policy.MandatoryLocale
	}
	return terms.PrevailingLocale
}

func hasProtectedLocale(document string, locales []platform.Locale) bool {
	want := protectedLocale(document)
	for _, locale := range locales {
		if locale == want {
			return true
		}
	}
	return false
}

// protectedLocaleRefusal is the sentence the seam answers with, RULED VERBATIM
// (#563). Two documents, two sentences, two footings:
//
//   - The Policy cites the LOPDP BY NAME, WITH NO ARTICLE NUMBER, because
//     nothing in this repository derives the language of the notice from a
//     numbered article and an unchecked legal claim in the one refusal a
//     regulator reads would be exactly backwards. The law's name is a proper
//     noun and is not translated, here or in the staff app's Spanish catalog.
//   - The Terms cite §37, which is verifiable in the document's own text.
//
// The staff app carries the same two sentences as message keys, because the
// language toggle is disabled before this refusal can be reached; this is the
// backstop, and the two must not drift.
func protectedLocaleRefusal(document string) string {
	if document == LegalDocumentPolicy {
		return "Spanish cannot be unpublished. The Ley Orgánica de Protección de Datos Personales requires this notice to be given in Spanish."
	}
	return "Spanish cannot be unpublished. The Spanish text is the contract (§37); the English one is a translation of it."
}

// artifactsFromGrid turns the editor's grid back into storage rows, so the
// publish plan can be computed from the workspace's own two sides without a
// second read of either.
func artifactsFromGrid(grid []OperatorLegalArtifact) []legal.Artifact {
	rows := make([]legal.Artifact, 0, len(grid)*2)
	for _, entry := range grid {
		for token, body := range entry.Bodies {
			locale, ok := platform.ParseLocale(token)
			if !ok {
				continue
			}
			rows = append(rows, legal.Artifact{
				Locale:  locale,
				Slug:    entry.Slug,
				Ordinal: entry.Ordinal,
				Body:    body,
			})
		}
	}
	return rows
}

// parseLocaleTokens is localeTokens' inverse, for the draft's published-language
// set on its way from the view back into a rule. Unknown tokens are dropped
// rather than errored: the set was validated when it was saved, and a rule is
// not the place to discover otherwise.
func parseLocaleTokens(tokens []string) []platform.Locale {
	locales := make([]platform.Locale, 0, len(tokens))
	for _, token := range tokens {
		if locale, ok := platform.ParseLocale(token); ok {
			locales = append(locales, locale)
		}
	}
	return locales
}
