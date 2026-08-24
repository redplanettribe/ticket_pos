package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Platform Operator's view of an Event's Ticket Questions, and the
// Revocation (#410, parent #404, ADR 0056).
//
// The Operator is the party answerable for what the platform collects, and an
// approval that could not be taken back would make that answerability theatre.
// A Revocation is a retirement with a reason: the question stops being asked
// from that moment, every Answer it was given stays, and review_status stays
// `approved` because the approval was real.

// RevocationMailer delivers the Revocation notice. The same narrow-interface
// seam the Assignment and No Longer Holding mails use.
type RevocationMailer interface {
	SendTicketQuestionRevoked(ctx context.Context, revoked platform.TicketQuestionRevoked) error
}

// RevocationRecipients is what the notice needs from identity: who the Org
// Admins of an Organization are, and the Staff Locale each is written in. Both
// keyed on the address, on the terms sales' StaffLocales seam set (ADR 0041).
type RevocationRecipients interface {
	OrgAdminEmails(ctx context.Context, orgID string) ([]string, error)
	StaffLocale(ctx context.Context, email string) (string, error)
}

// OperatorTicketQuestion is one question on the Operator's view of an Event:
// the staff payload, plus the Ticket Type it hangs off, since the Operator
// arrives by Event and the questions are the Ticket Type's.
type OperatorTicketQuestion struct {
	TicketQuestionView
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
}

// RevokeTicketQuestionInput is a validated Revocation: the reason already
// trimmed and bounded by the handler, the Operator taken from the session.
type RevokeTicketQuestionInput struct {
	Reason   string
	Operator string
}

// ListEventTicketQuestionsForOperator returns every Ticket Question of one
// Event, every review state and retired ones included, for the Platform
// Operator's view. Not scoped to an acting Member: the caller's authority is
// the operator allowlist (ADR 0015). An Event that does not exist is
// EVENT_NOT_FOUND, and the feature flag is read first for the reason the
// staff surface reads it first.
func (s *Service) ListEventTicketQuestionsForOperator(ctx context.Context, eventID string) ([]OperatorTicketQuestion, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if _, err := uuid.Parse(eventID); err != nil {
		return nil, catalog.ErrEventNotFound()
	}
	event, err := s.repo.GetEventByIDForOperator(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	rows, err := s.repo.ListTicketQuestionsByEventIDForOperator(ctx, eventID)
	if err != nil {
		return nil, err
	}
	out := make([]OperatorTicketQuestion, 0, len(rows))
	for i := range rows {
		options, err := s.repo.ListTicketQuestionOptions(ctx, rows[i].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, OperatorTicketQuestion{
			TicketQuestionView: toTicketQuestionView(&rows[i].TicketQuestion, options),
			TicketTypeID:       rows[i].TicketTypeID,
			TicketTypeName:     rows[i].TicketTypeName,
		})
	}
	return out, nil
}

// RevokeTicketQuestion takes an approval back.
//
// Only an approved, live question can be revoked: a draft, under-review or
// refused question has no approval to take back (TICKET_QUESTION_NOT_APPROVED),
// and one already retired — by the Organization or by an earlier Revocation —
// is TICKET_QUESTION_RETIRED. The guarded update underneath makes the second
// of those hold under a race too. The notice goes out AFTER the write and
// never fails it: a Revocation nobody was mailed about is still a Revocation,
// and the reason is on the staff editor regardless.
func (s *Service) RevokeTicketQuestion(ctx context.Context, questionID string, input RevokeTicketQuestionInput) (*TicketQuestionView, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if _, err := uuid.Parse(questionID); err != nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}
	existing, err := s.repo.GetTicketQuestionForOperator(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}
	if existing.ReviewStatus != string(catalog.TicketQuestionReviewApproved) {
		return nil, catalog.ErrTicketQuestionNotApproved()
	}
	if existing.RetiredAt.Valid {
		return nil, catalog.ErrTicketQuestionRetired()
	}

	revoked, err := s.repo.RevokeTicketQuestion(ctx, questionID, input.Reason, input.Operator, s.now())
	if err != nil {
		return nil, err
	}
	if revoked == nil {
		return nil, catalog.ErrTicketQuestionRetired()
	}

	options, err := s.repo.ListTicketQuestionOptions(ctx, questionID)
	if err != nil {
		return nil, err
	}

	s.notifyTicketQuestionRevoked(ctx, existing, input.Reason)

	view := toTicketQuestionView(revoked, options)
	return &view, nil
}

// notifyTicketQuestionRevoked mails every Org Admin of the Organization, each
// in their Staff Locale, on the terms the Payout Request notices set
// (ADR 0026, ADR 0041): every failure is swallowed and logged, because the
// write has committed and there is nothing a caller could do with a failure
// except lie about what happened. The reason is read off what was written.
func (s *Service) notifyTicketQuestionRevoked(ctx context.Context, question *repository.OperatorTicketQuestionContext, reason string) {
	if s.revocationMailer == nil || s.revocationRecipients == nil {
		return
	}
	recipients, err := s.revocationRecipients.OrgAdminEmails(ctx, question.OrganizationID)
	if err != nil {
		s.logger.Error("ticket question revoked notice: read org admins", "question_id", question.ID, "error", err)
		return
	}
	for _, to := range recipients {
		locale := platform.DefaultLocale
		if stored, err := s.revocationRecipients.StaffLocale(ctx, to); err != nil {
			s.logger.Error("ticket question revoked notice: read staff locale", "error", err)
		} else {
			locale = platform.ResolveStaffLocale(stored)
		}
		if err := s.revocationMailer.SendTicketQuestionRevoked(ctx, platform.TicketQuestionRevoked{
			To:               to,
			Locale:           locale,
			OrganizationName: question.OrganizationName,
			EventName:        question.EventName,
			QuestionLabel:    question.Label,
			Reason:           reason,
		}); err != nil {
			s.logger.Error("ticket question revoked notice: send", "question_id", question.ID, "error", err)
		}
	}
}
