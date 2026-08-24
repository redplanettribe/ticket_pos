package service

import (
	"context"

	catalogsvc "github.com/peter/ticket_pos/backend/internal/catalog/service"
)

// The Operator's view of an Event's Ticket Questions, and the Revocation
// (#410, ADR 0056). Both are catalog's answers passed through: what a Ticket
// Question is and what a Revocation does are catalog's rules, and this surface
// only lends them the operator gate.

// RevokeTicketQuestionInput is a validated Revocation.
type RevokeTicketQuestionInput struct {
	Reason   string
	Operator string
}

// ListEventTicketQuestions returns every Ticket Question of one Event.
func (s *Service) ListEventTicketQuestions(ctx context.Context, eventID string) ([]TicketQuestion, error) {
	return s.events.ListEventTicketQuestionsForOperator(ctx, eventID)
}

// RevokeTicketQuestion takes an approval back, with a reason.
func (s *Service) RevokeTicketQuestion(ctx context.Context, questionID string, input RevokeTicketQuestionInput) (*RevokedTicketQuestion, error) {
	return s.events.RevokeTicketQuestion(ctx, questionID, catalogsvc.RevokeTicketQuestionInput{
		Reason:   input.Reason,
		Operator: input.Operator,
	})
}
