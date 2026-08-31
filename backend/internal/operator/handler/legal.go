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
