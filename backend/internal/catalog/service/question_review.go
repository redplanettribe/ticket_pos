package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Question Review (#406, parent #404, ADR 0056): the Organization submits
// an Event's drafts as one ask, and may withdraw it while it is outstanding.
// The Operator's half is #407.

// QuestionReviewView is one Review as the staff editor reads it.
type QuestionReviewView struct {
	ID      string `json:"id"`
	EventID string `json:"event_id"`
	// Status is outstanding, answered, withdrawn or lapsed.
	Status string  `json:"status"`
	Note   *string `json:"note"`
	// AcknowledgedAt is when the submitter affirmed what the Organization was
	// choosing to collect: the record ADR 0056 says the acknowledgement is.
	AcknowledgedAt time.Time `json:"acknowledged_at"`
	// SubmittedBy is the submitter's email, so the record outlives their
	// Membership.
	SubmittedBy string    `json:"submitted_by"`
	SubmittedAt time.Time `json:"submitted_at"`
	// AnsweredBy and AnsweredAt are how the Review ended, whichever way it did:
	// the Operator's verdict, a withdrawal, or the lapse.
	AnsweredBy *string                  `json:"answered_by"`
	AnsweredAt *time.Time               `json:"answered_at"`
	Items      []QuestionReviewItemView `json:"items"`
}

// QuestionReviewItemView is one thing a Review carries: a question, or an
// Option of one when TicketQuestionOptionID is set.
type QuestionReviewItemView struct {
	ID                     string  `json:"id"`
	TicketQuestionID       string  `json:"ticket_question_id"`
	TicketQuestionOptionID *string `json:"ticket_question_option_id"`
	// Verdict is `approved` or `refused` once the Operator has answered, with
	// the reason on a refusal; both absent until then.
	Verdict *string `json:"verdict"`
	Reason  *string `json:"reason"`
}

// SubmitQuestionReviewInput is what a submission states.
type SubmitQuestionReviewInput struct {
	Note string
	// Acknowledged is the Organization affirming what it is choosing to
	// collect. False refuses the submission; true is recorded with the instant.
	Acknowledged bool
	// SubmittedBy is the acting Member's email.
	SubmittedBy string
}

// PlatformOperators is what catalog needs from identity to address the
// submission notice: the allowlist, which is the whole of operator authority
// (ADR 0015).
type PlatformOperators interface {
	PlatformOperatorEmails(ctx context.Context) ([]string, error)
}

// StaffLocales is what catalog needs from identity to write each notice in
// its recipient's Staff Locale (ADR 0041).
type StaffLocales interface {
	StaffLocale(ctx context.Context, email string) (string, error)
}

// QuestionReviewMail is the one method of the email sender this flow reaches,
// so nothing in catalog can send a receipt, a passcode or the Digest.
type QuestionReviewMail interface {
	SendQuestionReviewSubmitted(ctx context.Context, submitted platform.QuestionReviewSubmitted) error
}

// WithQuestionReviewNotices supplies what the submission notice needs: the
// sender, the operator allowlist and the Staff Locale reader (#406, ADR 0056).
//
// Applied after construction on the terms sales' WithPlatformOperators is:
// it serves one notice on one path. Unset, a submission is recorded and
// nobody is told, which is what every test that builds this service by hand
// gets.
func (s *Service) WithQuestionReviewNotices(mail QuestionReviewMail, operators PlatformOperators, locales StaffLocales) *Service {
	s.questionReviewMail = mail
	s.operators = operators
	s.staffLocales = locales
	return s
}

// SubmitQuestionReview records one Review over the Event's draft and refused
// questions and tells every Platform Operator.
//
// Refused, each with its own code: without the acknowledgement; while one is
// outstanding; once the Event has started; and when there is nothing to carry.
// Event Staff never reach here — the route is gated to Org Admin and Event
// Owner. Nothing is written before every refusal has been checked except the
// one-outstanding rule, which is the database's and holds inside the same
// transaction that moves the questions.
func (s *Service) SubmitQuestionReview(ctx context.Context, actor ActorContext, eventID string, input SubmitQuestionReviewInput) (*QuestionReviewView, error) {
	event, err := s.eventForQuestionReview(ctx, actor, eventID)
	if err != nil {
		return nil, err
	}
	if !input.Acknowledged {
		return nil, catalog.ErrQuestionReviewAcknowledgementRequired()
	}
	// The Event's start is stored as an instant, so "started in the Event's
	// timezone" is a comparison of instants: a start typed as 20:00 in
	// America/Guayaquil is that moment wherever the clock reading it sits.
	now := s.now()
	if event.StartsAt.Valid && !now.Before(event.StartsAt.Time) {
		return nil, catalog.ErrQuestionReviewEventStarted()
	}

	note := sql.NullString{}
	if trimmed := strings.TrimSpace(input.Note); trimmed != "" {
		note = sql.NullString{String: trimmed, Valid: true}
	}
	row, err := s.repo.SubmitQuestionReview(ctx, repository.SubmitQuestionReviewParams{
		EventID:     eventID,
		Note:        note,
		SubmittedBy: input.SubmittedBy,
		Now:         now,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrQuestionReviewOutstanding):
			return nil, catalog.ErrQuestionReviewOutstanding()
		case errors.Is(err, repository.ErrNothingToReview):
			return nil, catalog.ErrQuestionReviewNothingToReview()
		}
		return nil, err
	}

	view, err := s.questionReviewView(ctx, row)
	if err != nil {
		return nil, err
	}
	s.notifyQuestionReviewSubmitted(ctx, actor, event, view)
	return view, nil
}

// WithdrawQuestionReview takes an outstanding Review back: the Review is
// marked withdrawn and its items return to draft.
func (s *Service) WithdrawQuestionReview(ctx context.Context, actor ActorContext, eventID, reviewID, withdrawnBy string) (*QuestionReviewView, error) {
	if _, err := s.eventForQuestionReview(ctx, actor, eventID); err != nil {
		return nil, err
	}
	row, err := s.repo.WithdrawQuestionReview(ctx, eventID, reviewID, withdrawnBy, s.now())
	if err != nil {
		return nil, err
	}
	if row == nil {
		// The compare-and-swap found nothing outstanding: either no such Review
		// on this Event, or one that has already ended. Name which.
		existing, err := s.repo.GetQuestionReview(ctx, eventID, reviewID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, catalog.ErrQuestionReviewNotFound()
		}
		return nil, catalog.ErrQuestionReviewNotOutstanding(existing.Status)
	}
	return s.questionReviewView(ctx, row)
}

// GetCurrentQuestionReview is the Event's outstanding Review, or the last one
// submitted, with its items — what the editor's banner is drawn from.
func (s *Service) GetCurrentQuestionReview(ctx context.Context, actor ActorContext, eventID string) (*QuestionReviewView, error) {
	if _, err := s.eventForQuestionReview(ctx, actor, eventID); err != nil {
		return nil, err
	}
	row, err := s.repo.GetCurrentQuestionReview(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, catalog.ErrQuestionReviewNotFound()
	}
	return s.questionReviewView(ctx, row)
}

// eventForQuestionReview is the gate every Review verb passes: the flag is
// open, and the Event is this Organization's.
func (s *Service) eventForQuestionReview(ctx context.Context, actor ActorContext, eventID string) (*repository.Event, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}
	return event, nil
}

func (s *Service) questionReviewView(ctx context.Context, row *repository.QuestionReviewRow) (*QuestionReviewView, error) {
	items, err := s.repo.ListQuestionReviewItems(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	views := make([]QuestionReviewItemView, 0, len(items))
	for _, item := range items {
		views = append(views, QuestionReviewItemView{
			ID:                     item.ID,
			TicketQuestionID:       item.QuestionID,
			TicketQuestionOptionID: nullStringOrNil(item.OptionID),
			Verdict:                nullStringOrNil(item.Verdict),
			Reason:                 nullStringOrNil(item.Reason),
		})
	}
	return &QuestionReviewView{
		ID:             row.ID,
		EventID:        row.EventID,
		Status:         row.Status,
		Note:           nullStringOrNil(row.Note),
		AcknowledgedAt: row.AcknowledgedAt,
		SubmittedBy:    row.SubmittedBy,
		SubmittedAt:    row.SubmittedAt,
		AnsweredBy:     nullStringOrNil(row.AnsweredBy),
		AnsweredAt:     nullTimeOrNil(row.AnsweredAt),
		Items:          views,
	}, nil
}

// notifyQuestionReviewSubmitted tells every Platform Operator, each in their
// own Staff Locale, that an Event's questions are waiting (#406, ADR 0056).
//
// EVERY FAILURE IS SWALLOWED, on the Payout Request notice's terms: the Review
// is committed, and nothing a caller could do with a failure here is better
// than an Operator who reads the queue. An unconfigured seam, an empty
// allowlist and a read that fails are all one outcome: nobody is told.
func (s *Service) notifyQuestionReviewSubmitted(ctx context.Context, actor ActorContext, event *repository.Event, review *QuestionReviewView) {
	if s.questionReviewMail == nil || s.operators == nil {
		return
	}
	recipients, err := s.operators.PlatformOperatorEmails(ctx)
	if err != nil {
		s.logger.Error("question review submitted notice: read operator allowlist", "review_id", review.ID, "error", err)
		return
	}
	if len(recipients) == 0 {
		return
	}
	orgName, err := s.repo.GetOrganizationName(ctx, actor.OrganizationID)
	if err != nil {
		s.logger.Error("question review submitted notice: read organization", "review_id", review.ID, "error", err)
		return
	}
	questionCount := 0
	for _, item := range review.Items {
		if item.TicketQuestionOptionID == nil {
			questionCount++
		}
	}
	note := ""
	if review.Note != nil {
		note = *review.Note
	}
	for _, to := range recipients {
		_ = s.questionReviewMail.SendQuestionReviewSubmitted(ctx, platform.QuestionReviewSubmitted{
			To:               to,
			Locale:           s.staffLocale(ctx, to),
			OrganizationName: orgName,
			EventName:        event.Name,
			SubmittedBy:      review.SubmittedBy,
			QuestionCount:    questionCount,
			Note:             note,
		})
	}
}

// staffLocale is the language one notice is written in: the Staff Locale
// stored against the address, and English underneath (ADR 0041).
func (s *Service) staffLocale(ctx context.Context, email string) platform.Locale {
	if s.staffLocales == nil {
		return platform.DefaultLocale
	}
	stored, err := s.staffLocales.StaffLocale(ctx, email)
	if err != nil {
		s.logger.Error("question review notice: read staff locale", "error", err)
		return platform.DefaultLocale
	}
	return platform.ResolveStaffLocale(stored)
}
