package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// ticketQuestionBody is the whole of a Ticket Question as an authoring form
// states it, used by both create and update.
//
// One body for both verbs because both are a full restatement: the editor holds
// the question in a form and sends the form back, so a field left out is a field
// cleared rather than a field left alone. Options are absent from it on purpose —
// they have their own endpoints, because adding one is allowed at any time while
// changing the question is not, and a single body would blur the two rules.
type ticketQuestionBody struct {
	// Label is the Organization's own words, stored as coined and read
	// identically in every Locale (ADR 0027).
	Label string `json:"label"`
	// Kind is one of the seven; see catalog.ParseTicketQuestionKind.
	Kind string `json:"kind"`
	// Required produces an Outstanding Answer and nothing more. Absent is false,
	// which is the right default: a question nobody said was required is not.
	Required bool `json:"required"`
	// Timing is 'at_checkout' or 'after_purchase'. Absent means at_checkout,
	// which is the only value v1 writes.
	Timing string `json:"timing"`
	// OptionLabels are the Options a choice question is created with. Read on
	// create only; update ignores it, because the Option endpoints own Options
	// once the question exists.
	OptionLabels []string `json:"option_labels"`
}

// ticketQuestionOptionBody is one Option's label, for adding and for renaming.
type ticketQuestionOptionBody struct {
	Label string `json:"label"`
}

// reorderTicketQuestionsBody states the whole running order rather than a move.
type reorderTicketQuestionsBody struct {
	QuestionIDs []string `json:"question_ids"`
}

// ticketQuestionRoute pulls the Event and Ticket Type every one of these routes
// hangs off, reporting the validation failure itself so each handler is left
// with its own body to worry about.
func ticketQuestionRoute(w http.ResponseWriter, r *http.Request, reqID string) (eventID, ticketTypeID string, ok bool) {
	eventID = strings.TrimSpace(r.PathValue("id"))
	ticketTypeID = strings.TrimSpace(r.PathValue("ticketTypeId"))

	var fields []platform.FieldError
	if eventID == "" {
		fields = append(fields, platform.FieldError{Field: "id", Code: platform.CodeRequired, Message: "is required"})
	}
	if ticketTypeID == "" {
		fields = append(fields, platform.FieldError{Field: "ticketTypeId", Code: platform.CodeRequired, Message: "is required"})
	}
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return "", "", false
	}
	return eventID, ticketTypeID, true
}

func pathValueRequired(w http.ResponseWriter, r *http.Request, reqID, name string) (string, bool) {
	value := strings.TrimSpace(r.PathValue(name))
	if value == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: name, Code: platform.CodeRequired, Message: "is required"},
		})
		return "", false
	}
	return value, true
}

// validateTicketQuestion checks a submitted question against the rules that can
// be read off the body alone: the label's shape, and whether the kind and timing
// are ones this platform knows.
//
// It does NOT check Options against the kind, or the twenty cap, or the kind
// freeze. Those are the service's, because each of them needs to know what is
// already stored — and a rule enforced in two places is a rule that will
// eventually be enforced differently in each.
func validateTicketQuestion(body ticketQuestionBody, withOptions bool) ([]platform.FieldError, catalog.TicketQuestionKind, catalog.TicketQuestionTiming) {
	var fields []platform.FieldError

	fields = append(fields, validateCoinedLabel("label", body.Label, catalog.MaxTicketQuestionLabelLength)...)

	kind, kindOK := catalog.ParseTicketQuestionKind(strings.TrimSpace(body.Kind))
	if !kindOK {
		fields = append(fields, platform.FieldError{
			Field:   "kind",
			Code:    platform.CodeInvalidEnum,
			Message: "must be one of short_text, long_text, single_choice, multi_choice, number, date, checkbox",
		})
	}

	// An absent timing is at_checkout, which is what v1 writes for every
	// question anyway. Only a value that was actually sent and is not one of the
	// two is refused.
	timing := catalog.TicketQuestionTimingAtCheckout
	if raw := strings.TrimSpace(body.Timing); raw != "" {
		parsed, timingOK := catalog.ParseTicketQuestionTiming(raw)
		if !timingOK {
			fields = append(fields, platform.FieldError{
				Field:   "timing",
				Code:    platform.CodeInvalidEnum,
				Message: "must be one of at_checkout, after_purchase",
			})
		} else {
			timing = parsed
		}
	}

	if withOptions {
		if len(body.OptionLabels) > catalog.MaxTicketQuestionOptions {
			fields = append(fields, platform.FieldError{
				Field:   "option_labels",
				Code:    platform.CodeTooManyItems,
				Message: fmt.Sprintf("must contain at most %d options", catalog.MaxTicketQuestionOptions),
			})
		}
		for _, label := range body.OptionLabels {
			fields = append(fields, validateCoinedLabel("option_labels", label, catalog.MaxTicketQuestionOptionLabelLength)...)
		}
	}

	return fields, kind, timing
}

// validateCoinedLabel is the shape check both a Ticket Question's label and an
// Option's label answer to: present once trimmed, and within its own cap.
//
// The cap counts CHARACTERS, matching catalog.NormalizeTicketQuestionLabel, so
// an Organization writing Spanish is not handed a shorter field than one writing
// English.
func validateCoinedLabel(field, raw string, maxLength int) []platform.FieldError {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []platform.FieldError{{Field: field, Code: platform.CodeRequired, Message: "is required"}}
	}
	if utf8.RuneCountInString(trimmed) > maxLength {
		return []platform.FieldError{{
			Field:   field,
			Code:    platform.CodeTooLong,
			Message: fmt.Sprintf("must be at most %d characters", maxLength),
		}}
	}
	return nil
}

// ListTicketQuestions returns a Ticket Type's Ticket Questions.
//
// @Summary      List ticket questions
// @Description  Lists the Ticket Questions defined on a ticket type, retired ones last. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string  true  "Event ID"
// @Param        ticketTypeId   path      string  true  "Ticket type ID"
// @Success      200  {object}  openapi.EnvelopeTicketQuestionList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions [get]
func (h *Handler) ListTicketQuestions(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}

	questions, err := h.svc.ListTicketQuestions(r.Context(), actorFromRequest(r), eventID, ticketTypeID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, questions)
}

// CreateTicketQuestion adds a Ticket Question to a Ticket Type.
//
// @Summary      Create ticket question
// @Description  Adds a Ticket Question to a ticket type, with its Options when the kind is a choice one. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string              true  "Event ID"
// @Param        ticketTypeId   path      string              true  "Ticket type ID"
// @Param        body           body      ticketQuestionBody  true  "Ticket question"
// @Success      201  {object}  openapi.EnvelopeTicketQuestionDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions [post]
func (h *Handler) CreateTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}

	var body ticketQuestionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	fields, kind, timing := validateTicketQuestion(body, true)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	created, err := h.svc.CreateTicketQuestion(r.Context(), actorFromRequest(r), eventID, ticketTypeID, service.CreateTicketQuestionInput{
		Label:        body.Label,
		Kind:         kind,
		Required:     body.Required,
		Timing:       timing,
		OptionLabels: body.OptionLabels,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, created)
}

// UpdateTicketQuestion renames a Ticket Question and restates its flags.
//
// @Summary      Update ticket question
// @Description  Restates a Ticket Question. The kind is refused when any Answer exists. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string              true  "Event ID"
// @Param        ticketTypeId   path      string              true  "Ticket type ID"
// @Param        questionId     path      string              true  "Ticket question ID"
// @Param        body           body      ticketQuestionBody  true  "Ticket question"
// @Success      200  {object}  openapi.EnvelopeTicketQuestionDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId} [patch]
func (h *Handler) UpdateTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	var body ticketQuestionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	fields, kind, timing := validateTicketQuestion(body, false)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	updated, err := h.svc.UpdateTicketQuestion(r.Context(), actorFromRequest(r), eventID, ticketTypeID, questionID, service.UpdateTicketQuestionInput{
		Label:    body.Label,
		Kind:     kind,
		Required: body.Required,
		Timing:   timing,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, updated)
}

// RetireTicketQuestion retires a Ticket Question.
//
// The verb is DELETE and the effect is retirement, deliberately: a question that
// any Ticket answered must keep reading on that Ticket and in the export, so
// nothing here can hard-delete. The response is the retired question rather than
// a message, so the editor can redraw it in its retired state.
//
// @Summary      Retire ticket question
// @Description  Retires a Ticket Question. Never deletes: what has been answered keeps reading. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string  true  "Event ID"
// @Param        ticketTypeId   path      string  true  "Ticket type ID"
// @Param        questionId     path      string  true  "Ticket question ID"
// @Success      200  {object}  openapi.EnvelopeTicketQuestionDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId} [delete]
func (h *Handler) RetireTicketQuestion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	retired, err := h.svc.RetireTicketQuestion(r.Context(), actorFromRequest(r), eventID, ticketTypeID, questionID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, retired)
}

// ReorderTicketQuestions puts a Ticket Type's live Ticket Questions in order.
//
// @Summary      Reorder ticket questions
// @Description  States the whole running order of a ticket type's live Ticket Questions. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string                      true  "Event ID"
// @Param        ticketTypeId   path      string                      true  "Ticket type ID"
// @Param        body           body      reorderTicketQuestionsBody  true  "The whole running order"
// @Success      200  {object}  openapi.EnvelopeTicketQuestionList
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/order [put]
func (h *Handler) ReorderTicketQuestions(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}

	var body reorderTicketQuestionsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if len(body.QuestionIDs) == 0 {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "question_ids", Code: platform.CodeEmptyCollection, Message: "must contain at least one question"},
		})
		return
	}

	questions, err := h.svc.ReorderTicketQuestions(r.Context(), actorFromRequest(r), eventID, ticketTypeID, body.QuestionIDs)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, questions)
}

// AddTicketQuestionOption adds one Option to a choice Ticket Question.
//
// @Summary      Add ticket question option
// @Description  Adds an Option to a choice Ticket Question, refusing a twenty-first. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string                    true  "Event ID"
// @Param        ticketTypeId   path      string                    true  "Ticket type ID"
// @Param        questionId     path      string                    true  "Ticket question ID"
// @Param        body           body      ticketQuestionOptionBody  true  "Option label"
// @Success      201  {object}  openapi.EnvelopeTicketQuestionDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options [post]
func (h *Handler) AddTicketQuestionOption(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}

	var body ticketQuestionOptionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateCoinedLabel("label", body.Label, catalog.MaxTicketQuestionOptionLabelLength); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	question, err := h.svc.AddTicketQuestionOption(r.Context(), actorFromRequest(r), eventID, ticketTypeID, questionID, body.Label)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, question)
}

// RenameTicketQuestionOption corrects one Option's wording.
//
// @Summary      Rename ticket question option
// @Description  Corrects an Option's label. The Option's identity is untouched, so Answers given under the old wording stay attached. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string                    true  "Event ID"
// @Param        ticketTypeId   path      string                    true  "Ticket type ID"
// @Param        questionId     path      string                    true  "Ticket question ID"
// @Param        optionId       path      string                    true  "Option ID"
// @Param        body           body      ticketQuestionOptionBody  true  "Option label"
// @Success      200  {object}  openapi.EnvelopeTicketQuestionDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options/{optionId} [patch]
func (h *Handler) RenameTicketQuestionOption(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}
	optionID, ok := pathValueRequired(w, r, reqID, "optionId")
	if !ok {
		return
	}

	var body ticketQuestionOptionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateCoinedLabel("label", body.Label, catalog.MaxTicketQuestionOptionLabelLength); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	question, err := h.svc.RenameTicketQuestionOption(r.Context(), actorFromRequest(r), eventID, ticketTypeID, questionID, optionID, body.Label)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, question)
}

// RetireTicketQuestionOption retires one Option.
//
// DELETE that never deletes, for the same reason its question-level sibling
// does not: a retired Option leaves new lists, stays on the Tickets that chose
// it, and keeps its column in the Sales Export.
//
// @Summary      Retire ticket question option
// @Description  Retires an Option: gone from new lists, kept on the Tickets that chose it. Refuses the last live one. Answers 404 while the Ticket Question feature flag is off.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string  true  "Event ID"
// @Param        ticketTypeId   path      string  true  "Ticket type ID"
// @Param        questionId     path      string  true  "Ticket question ID"
// @Param        optionId       path      string  true  "Option ID"
// @Success      200  {object}  openapi.EnvelopeTicketQuestionDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options/{optionId} [delete]
func (h *Handler) RetireTicketQuestionOption(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := ticketQuestionRoute(w, r, reqID)
	if !ok {
		return
	}
	questionID, ok := pathValueRequired(w, r, reqID, "questionId")
	if !ok {
		return
	}
	optionID, ok := pathValueRequired(w, r, reqID, "optionId")
	if !ok {
		return
	}

	question, err := h.svc.RetireTicketQuestionOption(r.Context(), actorFromRequest(r), eventID, ticketTypeID, questionID, optionID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, question)
}
