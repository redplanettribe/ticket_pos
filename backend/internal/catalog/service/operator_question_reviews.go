package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Operator's half of the Question Review (#407, parent #404, ADR 0056):
// the queue across every Organization, one Review whole, the answer — one act,
// a verdict per item — and the lapse.

// OperatorQuestionReview is one Review as the Operator Dashboard reads it:
// the Review, and the Organization and Event it belongs to beside it, on the
// terms the Payout Request queue row is shaped.
type OperatorQuestionReview struct {
	Review       OperatorQuestionReviewView `json:"review"`
	Organization QuestionReviewOrganization `json:"organization"`
	Event        QuestionReviewEvent        `json:"event"`
}

// OperatorQuestionReviewView is the staff Review payload plus the two things
// the Operator needs that the editor already knows: how many questions ride
// it, and — on the detail — each item's whole question or Option.
type OperatorQuestionReviewView struct {
	QuestionReviewView
	QuestionCount int `json:"question_count"`
	// Items REPLACES the embedded payload's items with the same rows carrying
	// their question and Option shapes; empty on a queue row, full on the
	// detail.
	Items []OperatorQuestionReviewItemView `json:"items"`
}

// OperatorQuestionReviewItemView is one item with what it names: the whole
// question (with every Option, so an Option item is read under its question's
// words) and, for an Option item, the Option itself.
type OperatorQuestionReviewItemView struct {
	QuestionReviewItemView
	Question *OperatorTicketQuestion   `json:"question"`
	Option   *TicketQuestionOptionView `json:"option"`
}

// QuestionReviewOrganization is whose the Review is.
type QuestionReviewOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// QuestionReviewEvent is which Event, and when it starts — the instant the
// Review lapses.
type QuestionReviewEvent struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	StartsAt *time.Time `json:"starts_at"`
	Timezone string     `json:"timezone"`
}

// QuestionReviewVerdictInput is one ruling as the body states it.
type QuestionReviewVerdictInput struct {
	ItemID  string
	Verdict string
	Reason  string
}

// AnswerQuestionReviewInput is the Operator's whole answer: every item ruled
// on, and who ruled.
type AnswerQuestionReviewInput struct {
	Verdicts []QuestionReviewVerdictInput
	Operator string
}

// ListOutstandingQuestionReviews returns one page of every outstanding Review
// on the platform, oldest first, plus the unpaginated total. Reviews whose
// Event has started are lapsed first, so the queue never lists one.
func (s *Service) ListOutstandingQuestionReviews(ctx context.Context, page, pageSize int) ([]OperatorQuestionReview, int, error) {
	if !s.ticketQuestionsEnabled {
		return nil, 0, catalog.ErrTicketQuestionsUnavailable()
	}
	if err := s.repo.LapseStartedQuestionReviews(ctx, s.now()); err != nil {
		return nil, 0, err
	}
	rows, total, err := s.repo.ListOutstandingQuestionReviews(ctx, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]OperatorQuestionReview, 0, len(rows))
	for i := range rows {
		view, err := s.operatorQuestionReview(ctx, &rows[i], false)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *view)
	}
	return out, total, nil
}

// CountOutstandingQuestionReviews is the queue as one number, for the badge.
func (s *Service) CountOutstandingQuestionReviews(ctx context.Context) (int, error) {
	if !s.ticketQuestionsEnabled {
		return 0, catalog.ErrTicketQuestionsUnavailable()
	}
	if err := s.repo.LapseStartedQuestionReviews(ctx, s.now()); err != nil {
		return 0, err
	}
	return s.repo.CountOutstandingQuestionReviews(ctx)
}

// GetQuestionReviewForOperator returns one Review whole, in any state, with
// each item's question and Option shape.
func (s *Service) GetQuestionReviewForOperator(ctx context.Context, reviewID string) (*OperatorQuestionReview, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if _, err := uuid.Parse(reviewID); err != nil {
		return nil, catalog.ErrQuestionReviewNotFound()
	}
	if err := s.repo.LapseStartedQuestionReviews(ctx, s.now()); err != nil {
		return nil, err
	}
	row, err := s.repo.GetQuestionReviewForOperator(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, catalog.ErrQuestionReviewNotFound()
	}
	return s.operatorQuestionReview(ctx, row, true)
}

// AnswerQuestionReview records the verdicts as one act and tells the
// submitter.
//
// THE BODY IS CHECKED WHOLE BEFORE ANYTHING IS WRITTEN: every item of the
// Review needs a verdict, every refusal a reason, and no verdict may name an
// item the Review does not carry — each refusal names the item. Then the
// Review must still be outstanding, which the lapse is run first to decide:
// an answer that arrives after the Event started reaches a lapsed Review and
// is refused with that state.
func (s *Service) AnswerQuestionReview(ctx context.Context, reviewID string, input AnswerQuestionReviewInput) (*OperatorQuestionReview, error) {
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if _, err := uuid.Parse(reviewID); err != nil {
		return nil, catalog.ErrQuestionReviewNotFound()
	}
	now := s.now()
	if err := s.repo.LapseStartedQuestionReviews(ctx, now); err != nil {
		return nil, err
	}
	row, err := s.repo.GetQuestionReviewForOperator(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, catalog.ErrQuestionReviewNotFound()
	}
	if row.Status != "outstanding" {
		return nil, catalog.ErrQuestionReviewNotOutstanding(row.Status)
	}
	items, err := s.repo.ListQuestionReviewItems(ctx, reviewID)
	if err != nil {
		return nil, err
	}

	verdicts, err := validateQuestionReviewVerdicts(items, input.Verdicts)
	if err != nil {
		return nil, err
	}

	answered, err := s.repo.AnswerQuestionReview(ctx, reviewID, verdicts, input.Operator, now)
	if err != nil {
		return nil, err
	}
	if answered == nil {
		// Somebody answered, or the Event started, between the read and the
		// write. Name the state it reached.
		latest, err := s.repo.GetQuestionReview(ctx, row.EventID, reviewID)
		if err != nil {
			return nil, err
		}
		status := "answered"
		if latest != nil {
			status = latest.Status
		}
		return nil, catalog.ErrQuestionReviewNotOutstanding(status)
	}

	row.QuestionReviewRow = *answered
	view, err := s.operatorQuestionReview(ctx, row, true)
	if err != nil {
		return nil, err
	}
	s.notifyQuestionReviewAnswered(ctx, row, view)
	return view, nil
}

// validateQuestionReviewVerdicts turns the body into what the repository
// writes, refusing the first hole it finds in the order the items are listed.
func validateQuestionReviewVerdicts(items []repository.QuestionReviewItemRow, inputs []QuestionReviewVerdictInput) ([]repository.QuestionReviewVerdict, error) {
	carried := make(map[string]bool, len(items))
	for _, item := range items {
		carried[item.ID] = true
	}
	given := make(map[string]QuestionReviewVerdictInput, len(inputs))
	for _, v := range inputs {
		if !carried[v.ItemID] {
			return nil, catalog.ErrQuestionReviewUnknownItem(v.ItemID)
		}
		given[v.ItemID] = v
	}
	out := make([]repository.QuestionReviewVerdict, 0, len(items))
	for _, item := range items {
		v, ok := given[item.ID]
		if !ok {
			return nil, catalog.ErrQuestionReviewVerdictRequired(item.ID)
		}
		reason := strings.TrimSpace(v.Reason)
		switch v.Verdict {
		case "approved":
			out = append(out, repository.QuestionReviewVerdict{ItemID: item.ID, Verdict: "approved"})
		case "refused":
			if reason == "" {
				return nil, catalog.ErrQuestionReviewReasonRequired(item.ID)
			}
			out = append(out, repository.QuestionReviewVerdict{ItemID: item.ID, Verdict: "refused", Reason: sql.NullString{String: reason, Valid: true}})
		default:
			return nil, catalog.ErrQuestionReviewVerdictRequired(item.ID)
		}
	}
	return out, nil
}

// operatorQuestionReview assembles one row's payload; withItems reads every
// question of the Event once and pairs each item with its shape.
func (s *Service) operatorQuestionReview(ctx context.Context, row *repository.OperatorQuestionReviewRow, withItems bool) (*OperatorQuestionReview, error) {
	base, err := s.questionReviewView(ctx, &row.QuestionReviewRow)
	if err != nil {
		return nil, err
	}
	view := &OperatorQuestionReview{
		Review: OperatorQuestionReviewView{
			QuestionReviewView: *base,
			QuestionCount:      row.QuestionCount,
			Items:              []OperatorQuestionReviewItemView{},
		},
		Organization: QuestionReviewOrganization{ID: row.OrganizationID, Name: row.OrganizationName, Slug: row.OrganizationSlug},
		Event: QuestionReviewEvent{
			ID:       row.EventID,
			Name:     row.EventName,
			StartsAt: nullTimeOrNil(row.EventStartsAt),
			Timezone: row.EventTimezone,
		},
	}
	if !withItems {
		return view, nil
	}

	questions, err := s.repo.ListTicketQuestionsByEventIDForOperator(ctx, row.EventID)
	if err != nil {
		return nil, err
	}
	byQuestion := make(map[string]*OperatorTicketQuestion, len(questions))
	for i := range questions {
		options, err := s.repo.ListTicketQuestionOptions(ctx, questions[i].ID)
		if err != nil {
			return nil, err
		}
		q := OperatorTicketQuestion{
			TicketQuestionView: toTicketQuestionView(&questions[i].TicketQuestion, options),
			TicketTypeID:       questions[i].TicketTypeID,
			TicketTypeName:     questions[i].TicketTypeName,
		}
		byQuestion[q.ID] = &q
	}
	items := make([]OperatorQuestionReviewItemView, 0, len(base.Items))
	for _, item := range base.Items {
		out := OperatorQuestionReviewItemView{QuestionReviewItemView: item, Question: byQuestion[item.TicketQuestionID]}
		if item.TicketQuestionOptionID != nil && out.Question != nil {
			for i := range out.Question.Options {
				if out.Question.Options[i].ID == *item.TicketQuestionOptionID {
					option := out.Question.Options[i]
					out.Option = &option
				}
			}
		}
		items = append(items, out)
	}
	view.Review.Items = items
	return view, nil
}

// notifyQuestionReviewAnswered mails the submitter, in their Staff Locale,
// each item's verdict (#407, ADR 0056). Every failure is swallowed on the
// submission notice's terms: the verdicts are committed and readable in the
// editor, and only the telling can be missing.
func (s *Service) notifyQuestionReviewAnswered(ctx context.Context, row *repository.OperatorQuestionReviewRow, view *OperatorQuestionReview) {
	if s.questionReviewMail == nil {
		return
	}
	items := make([]platform.QuestionReviewAnsweredItem, 0, len(view.Review.Items))
	for _, item := range view.Review.Items {
		if item.Question == nil {
			continue
		}
		out := platform.QuestionReviewAnsweredItem{QuestionLabel: item.Question.Label}
		if item.Option != nil {
			out.OptionLabel = item.Option.Label
		}
		if item.Verdict != nil {
			out.Verdict = *item.Verdict
		}
		if item.Reason != nil {
			out.Reason = *item.Reason
		}
		items = append(items, out)
	}
	if err := s.questionReviewMail.SendQuestionReviewAnswered(ctx, platform.QuestionReviewAnswered{
		To:               row.SubmittedBy,
		Locale:           s.staffLocale(ctx, row.SubmittedBy),
		OrganizationName: row.OrganizationName,
		EventName:        row.EventName,
		AnsweredBy:       row.AnsweredBy.String,
		Items:            items,
	}); err != nil {
		s.logger.Error("question review answered notice: send", "review_id", row.ID, "error", err)
	}
}
