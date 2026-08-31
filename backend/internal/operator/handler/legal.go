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
