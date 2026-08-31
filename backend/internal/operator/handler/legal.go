package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Legal Center's three calls (#561, spec #556): read a document's
// workspace, save its draft, discard it. Gated by the operator allowlist and
// nothing else, on the Question Review queue's terms.
//
// NOTHING HERE PUBLISHES. Saving a draft changes no page a reader can see and
// re-gates nobody; that is #563, and it arrives as its own route.

// legalDraftBody is a whole draft as the editor holds it: the languages it
// intends to publish in, and the artifact list IN ORDER.
//
// THE CLIENT DOES NOT SEND ORDINALS. An artifact's ordinal is the fingerprint
// preimage's order (#541), and a body that could state one independently of the
// list's order would let the two disagree — publishing an edition hashed
// differently from the one on screen. The position in `artifacts` is the
// ordinal, and the service stamps it.
type legalDraftBody struct {
	PublishedLocales []string            `json:"published_locales"`
	Artifacts        []legalArtifactBody `json:"artifacts"`
}

// legalArtifactBody is one row of the editor's grid: a slug and its text in each
// language, keyed by language token.
type legalArtifactBody struct {
	Slug   string            `json:"slug"`
	Bodies map[string]string `json:"bodies"`
}

// GetLegalWorkspace returns one document's published edition and its draft.
//
// @Summary      Read a legal document's published edition and its draft
// @Description  Returns everything the Legal Center's editor needs for ONE document in one read (#561, spec #556): the currently published edition — version id, label, effective date, content hash, the languages it actually publishes and every artifact as {slug, ordinal, bodies-by-language} — the ONE MUTABLE DRAFT of that document, and `supported_locales`, the menu a draft's published-language set is bounded by (the platform's app locales; `platform.ParseLocale` is a closed switch). ONE PAYLOAD AND NOT THREE ENDPOINTS: the editor diffs the draft against the published edition cell by cell, and two reads could straddle a publication and produce a diff against text that was never on screen together. A document with no saved draft answers with a draft that is a COPY of the published edition, `stored: false` — the same answer a discard produces, so "never started" and "started again" are one state. `base_is_current` is false when somebody published underneath the draft since it was opened; that is a warning here and a refusal only at publish. `document` is `policy` or `terms`; anything else is 404 LEGAL_DOCUMENT_NOT_FOUND. Read-only. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string  true  "policy or terms"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalWorkspace
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/documents/{document} [get]
func (h *Handler) GetLegalWorkspace(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	workspace, err := h.svc.LegalWorkspace(r.Context(), strings.TrimSpace(r.PathValue("document")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, workspace)
}

// SaveLegalDraft replaces a document's draft with the one on screen.
//
// @Summary      Save a legal document's one mutable draft, whole
// @Description  Replaces the document's draft with the body, creating it if there is none (#561). ONE MUTABLE DRAFT PER DOCUMENT, held server-side so a closed tab does not lose an afternoon's drafting, and PUT rather than PATCH because the draft is replaced WHOLE: adding an artifact and removing one are both nothing more than saving a different list, so neither needs a verb of its own. The artifacts' ORDER IS THEIR ORDINAL — the fingerprint preimage's order (#541) — and the client sends no ordinals. `published_locales` is the EXPLICIT set of languages the draft intends to publish in, never inferred from which cells are filled: a half-translated language must be a draft that cannot publish, not a language quietly dropped by an empty textarea. Who saved it is taken from the Staff Session, never from the body. THIS PUBLISHES NOTHING: no version row, no artifact row, no fingerprint, no re-gating, and no cache invalidation, because nothing a reader can see has changed. A cell that is blank or absent is stored as ABSENT, so "emptied" and "never written" cannot drift apart. AN INCOMPLETE DRAFT IS SAVED HAPPILY — an artifact with no Spanish yet is somebody's afternoon, and completeness is refused at publish (#563). Refused: 400 LEGAL_DRAFT_LOCALES_REQUIRED for an empty language set, 400 LEGAL_DRAFT_LOCALE_UNSUPPORTED (with the token) for a language this platform does not serve, 400 LEGAL_DRAFT_SLUG_REQUIRED for an artifact with no slug, 400 LEGAL_DRAFT_DUPLICATE_SLUG (with the slug) for one carried twice, 404 LEGAL_DOCUMENT_NOT_FOUND for anything that is not `policy` or `terms`. Answers with the whole workspace, as the GET does. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string          true  "policy or terms"
// @Param        body      body  legalDraftBody  true  "The whole draft"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalWorkspace
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/documents/{document}/draft [put]
func (h *Handler) SaveLegalDraft(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body legalDraftBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	artifacts := make([]consentsvc.SaveLegalDraftArtifact, 0, len(body.Artifacts))
	for _, artifact := range body.Artifacts {
		artifacts = append(artifacts, consentsvc.SaveLegalDraftArtifact{
			Slug:   artifact.Slug,
			Bodies: artifact.Bodies,
		})
	}

	workspace, err := h.svc.SaveLegalDraft(r.Context(), strings.TrimSpace(r.PathValue("document")), consentsvc.SaveLegalDraftInput{
		PublishedLocales: body.PublishedLocales,
		Artifacts:        artifacts,
		UpdatedBy:        session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, workspace)
}

// DiscardLegalDraft throws a document's draft away.
//
// @Summary      Discard a legal document's draft, restoring it to the current edition
// @Description  Deletes the document's draft and its languages with it, so the editor reopens on the CURRENT PUBLISHED EDITION (#561) — which is what makes an experiment something other than a commitment. Discarding a document that has no draft is a SUCCESS and not a 404: what the caller asked for is exactly what they now have, and the response is identical either way. It publishes nothing and unpublishes nothing: the edition a reader sees is untouched, because a draft was never on any page. 404 LEGAL_DOCUMENT_NOT_FOUND for anything that is not `policy` or `terms`. Answers with the whole workspace, whose draft is now a copy of the published edition with `stored: false`. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string  true  "policy or terms"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalWorkspace
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/documents/{document}/draft [delete]
func (h *Handler) DiscardLegalDraft(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	workspace, err := h.svc.DiscardLegalDraft(r.Context(), strings.TrimSpace(r.PathValue("document")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, workspace)
}

// legalPreviewBody names the cell that was put on screen: one artifact in one
// language.
//
// IT CARRIES NO TEXT. What was previewed is whatever the draft says — the
// preview renders text the workspace read already handed over — and a body that
// could name its own text would let a client claim to have previewed something
// the draft does not contain.
type legalPreviewBody struct {
	Slug   string `json:"slug"`
	Locale string `json:"locale"`
}

// PreviewLegalDraftCell records that one artifact was seen rendered.
//
// @Summary      Record that a draft artifact was previewed
// @Description  Records that the operator has seen ONE artifact of the draft rendered in ONE language, as a reader will see it (#562, spec #556). THE RENDERING HAPPENS IN THE BROWSER, through the same `Markdown` component the Storefront's privacy-policy page uses, over text the workspace read already carried: there is no server-side render call and NO PREVIEW ROUTE ON THE PUBLIC SIDE, because the public route resolves what is current itself and refuses to be told which edition to serve. What this call records is only that somebody looked. A PREVIEW IS A LOG LINE AND NEVER AN AUDIT ROW (#545, amending #544): no `consent_access_log` row is written, now or ever, so every row in that table stays a touch of somebody's data — an operator reading the platform's own unpublished words has touched nobody's. Remembered by the DIGEST OF THE TEXT that was on screen, so previewing a paragraph and then rewriting it does not leave a preview standing over words nobody has seen; previewing the same cell twice is one fact. Every artifact of the draft, in every language it intends to publish, must be previewed before #563 will offer a publish button — `draft.previewed_all` on the response is that answer, and `draft.preview_gaps` names what is left. The body is `{slug, locale}` and carries no text. Refused: 409 LEGAL_DRAFT_NOT_STORED when the document has no saved draft (a preview promises a look at the text that will be published, and unsaved text will not be), 400 LEGAL_DRAFT_CELL_NOT_FOUND when the draft has no text for that artifact in that language, 400 LEGAL_DRAFT_LOCALE_UNSUPPORTED, 400 LEGAL_DRAFT_SLUG_REQUIRED, 404 LEGAL_DOCUMENT_NOT_FOUND. Answers with the whole workspace, as the other Legal Center calls do. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string            true  "policy or terms"
// @Param        body      body  legalPreviewBody  true  "The cell that was previewed"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalWorkspace
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/documents/{document}/draft/previews [post]
func (h *Handler) PreviewLegalDraftCell(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body legalPreviewBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	workspace, err := h.svc.PreviewLegalDraftCell(r.Context(), strings.TrimSpace(r.PathValue("document")), consentsvc.PreviewLegalDraftCellInput{
		Slug:   body.Slug,
		Locale: body.Locale,
		By:     session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, workspace)
}

// SeeLegalDraftDiff records that the diff against the current edition was shown.
//
// @Summary      Record that the draft's diff against the current edition was seen
// @Description  Records that the operator has been shown what this draft CHANGES about the currently published edition (#562, spec #556) — the second of the two things #563 requires before a publish button exists, so that no publication happens without its consequence having been displayed. THE DIFF IS COMPUTED IN THE BROWSER, from the published edition and the draft that the workspace read already carried together in one payload; this call records only that it was on screen, and takes no body. It is remembered against BOTH SIDES — the draft's text and the published version it was compared with — so a diff stops counting the moment either moves: rewrite a paragraph and it lapses, and so does somebody else publishing underneath the draft, which is the situation `base_is_current` already warns about. `draft.seen_diff` on the response is the answer. Like a preview, it writes a `platform.Logger` line and no `consent_access_log` row (#545). Refused: 409 LEGAL_DRAFT_NOT_STORED when the document has no saved draft, 404 LEGAL_DOCUMENT_NOT_FOUND for anything that is not `policy` or `terms`. Answers with the whole workspace. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string  true  "policy or terms"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalWorkspace
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/documents/{document}/draft/diff-seen [post]
func (h *Handler) SeeLegalDraftDiff(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	workspace, err := h.svc.SeeLegalDraftDiff(r.Context(), strings.TrimSpace(r.PathValue("document")), session.Email)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, workspace)
}

// legalPublishBody is one publication as the operator asked for it.
//
// IT CARRIES NO LABEL AND NO TEXT. The label is rendered from the lineage
// (legal.Lineage.Label) and migration 110's CHECK refuses anything else, so
// there is no path by which an edition acquires a name somebody typed; the text
// is the SAVED draft, exactly as it was previewed and diffed, so a body that
// could restate it would let the publication differ from what was on screen.
type legalPublishBody struct {
	// Kind is "edition" or "correction". No default: which act this is decides
	// whether the whole customer base is re-gated.
	Kind string `json:"kind"`
	// EffectiveDate is YYYY-MM-DD, 00:00 America/Guayaquil. Required and at
	// least tomorrow on an edition; refused on a correction, which is immediate.
	EffectiveDate string `json:"effective_date"`
	// Reason is the typed correction reason. Required on a correction.
	Reason string `json:"reason"`
}

// PublishLegalEdition publishes the draft: a new edition, or a correction.
//
// @Summary      Publish a legal document's draft as a new edition or as a correction
// @Description  Publishes the document's SAVED draft (#563, spec #556, ADR 0067). TWO ACTS AND NOT ONE. `kind: "edition"` is a GATING publication: it takes the next generation at revision 0, RE-GATES everybody standing below it, and must take effect NO EARLIER THAN TOMORROW — an irreversible re-gate gets a night in which the operator can change their mind, and "now" is refused. `kind: "correction"` takes the next revision within the published edition's generation, FLAT (`1.1`, `1.2`, never `1.1.1`), RE-GATES NOBODY, requires a typed reason, and TAKES EFFECT IMMEDIATELY so a typo fix does not wait overnight — it therefore names no effective date and is refused if it does. `correct now, publish a gating edition effective tomorrow` is an available path, so urgency never forces a choice between speed and honesty. LABELS ARE SYSTEM-GENERATED AND CANNOT BE TYPED, and the body carries no text: what is published is the draft as saved, previewed and diffed. NOTHING IS EVER MUTATED — a correction is a NEW ROW, and the old bytes stay exactly as the acceptances that fingerprint them expect. The version row records who published, when, the diff summary, the typed reason and THE HEADCOUNT AS IT STOOD ON THE BUTTON (migration 112), because provenance belongs on the immutable row the evidence already points at. THERE IS NO APPROVAL STEP on either act: production holds one Platform Operator, so a two-person rule would deadlock, and the substitute for review is the overnight delay plus proof that the consequence was displayed. PUBLISHING REVOKES NO SESSION in either population, and there is no control for it (#570). Refused: 409 LEGAL_DRAFT_NOT_STORED (no saved draft), 400 LEGAL_PUBLISH_KIND_UNKNOWN, 400 LEGAL_PUBLISH_INCOMPLETE with the gaps (an artifact missing in a published language, so no reader meets a document with a hole in it), 409 LEGAL_PUBLISH_NOT_PREVIEWED with the gaps, 409 LEGAL_PUBLISH_DIFF_NOT_SEEN (which also catches somebody publishing underneath the draft, since the seen diff names both sides), 400 LEGAL_CORRECTION_STRUCTURAL (a correction cannot add or remove an artifact — the one structural change the code can prove is not a typo), 400 LEGAL_CORRECTION_LOCALE_SET_CHANGED (that reshapes the hash preimage rather than the fingerprint), 400 LEGAL_CORRECTION_EMPTY_DIFF (a correction that corrects nothing cannot be recorded — an empty diff IS publishable as a gating edition), 400 LEGAL_CORRECTION_REASON_REQUIRED, 400 LEGAL_CORRECTION_EFFECTIVE_DATE_REFUSED, 400 LEGAL_EFFECTIVE_DATE_INVALID, 400 LEGAL_GATING_EFFECTIVE_DATE_TOO_SOON carrying `earliest_effective_date`, 400 LEGAL_PROTECTED_LOCALE_REQUIRED in BOTH kinds when the draft would stop publishing the language the document may not be published without (a CONSTANT per document and never a column, consulted only at publish), 404 LEGAL_DOCUMENT_NOT_FOUND. Answers with the whole workspace, whose draft is once again a copy of the published edition because the draft became it. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string            true  "policy or terms"
// @Param        body      body  legalPublishBody  true  "The publication"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalWorkspace
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/documents/{document}/publications [post]
func (h *Handler) PublishLegalEdition(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body legalPublishBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	workspace, err := h.svc.PublishLegalEdition(r.Context(), strings.TrimSpace(r.PathValue("document")), consentsvc.PublishLegalEditionInput{
		Kind:          body.Kind,
		EffectiveDate: body.EffectiveDate,
		Reason:        body.Reason,
		// Who published, from the Staff Session and never from the body: the
		// provenance on the version row is the platform's finding about who
		// pressed the button, not the client's assertion about it.
		By: session.Email,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, workspace)
}

// CancelLegalEdition withdraws a scheduled edition before its day.
//
// @Summary      Cancel a scheduled edition of a legal document
// @Description  Withdraws an edition that has been published but has NOT TAKEN EFFECT YET (#564, spec #556, ADR 0067) — the night the overnight delay buys. A gating edition cannot take effect the day it is published, so between the click and the midnight rollover there is a night in which the operator can change their mind, and this is the call that makes it usable. IT TAKES NO BODY. Cancelling is UNGATED AND IMMEDIATE: no reason, no delay, no confirmation ceremony and no approval step, because UNDOING IS ALWAYS CHEAPER THAN DOING — the publication it reverses re-gates every Customer and everybody on the Staff platform, and the reversal, while the edition is on nobody's screen, moves not one person. THE ROW IS RETAINED AND MARKED, NEVER DELETED: the edition survives in full with its artifacts, its fingerprint and its publish provenance, so the record of what was nearly published stays readable, its label stays spent, and a later publication cannot reuse the number (`cancelled_by`/`cancelled_at`, migration 113, in migration 112's whole-or-nothing house style). A CANCELLED EDITION NEVER BECOMES CURRENT, never lifts the gating floor and never enters the satisfying set — excluded by `cancelled_at IS NULL` in the SAME QUERY that answers `effective_date <= CURRENT_DATE`, so nothing fires at midnight, no job runs and no cache is invalidated. CANCELLING TWICE IS A SUCCESS, on the draft discard's terms — what the caller asked for is what they now have — and the FIRST cancellation's provenance is kept. Refused: 409 LEGAL_EDITION_ALREADY_EFFECTIVE carrying `effective_date` once the day has passed (the control is gone by then; this is the backstop for a page left open overnight, and the refusal is the UPDATE's own `effective_date > CURRENT_DATE` rather than a second opinion from the app's clock), 404 LEGAL_EDITION_NOT_FOUND for an id that names no edition of THIS document, 404 LEGAL_DOCUMENT_NOT_FOUND. Answers with the whole workspace, whose `scheduled` list no longer names the edition. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        document  path  string  true  "policy or terms"
// @Param        version   path  string  true  "The scheduled edition's version id"
// @Success      200  {object}  openapi.EnvelopeOperatorLegalWorkspace
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/legal/documents/{document}/publications/{version}/cancel [post]
func (h *Handler) CancelLegalEdition(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	// NO BODY IS READ, and there is none to read. Everything this act needs is
	// in the address and in the session: which document, which edition, and who
	// changed their mind. A body would be a place for a reason to grow, and a
	// reason is a gate on the cheap act.
	workspace, err := h.svc.CancelLegalEdition(r.Context(),
		strings.TrimSpace(r.PathValue("document")),
		strings.TrimSpace(r.PathValue("version")),
		// Who withdrew it, from the Staff Session and never from the body — the
		// mark on the version row is the platform's finding, exactly as
		// `published_by` beside it is.
		session.Email,
	)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, workspace)
}
