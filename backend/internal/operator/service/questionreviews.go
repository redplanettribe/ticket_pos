package service

import (
	"context"

	catalogsvc "github.com/peter/ticket_pos/backend/internal/catalog/service"
)

// The Question Review queue: what every Organization is asking to ask, and
// the Operator's answer (#407, ADR 0056).
//
// The operator surface's THIRD cross-Organization view, on the Payout
// Request queue's justification: the Review is the reason to open the
// dashboard, and an Operator arrives not knowing whose it is. Every payload
// here is catalog's, passed through: what a Review carries and what a
// verdict does to a question are catalog's rules, and this surface lends
// them the operator gate and nothing else.

// QuestionReviewQueue is the ADR-0006 nested envelope for the queue.
type QuestionReviewQueue struct {
	Data       []QuestionReview `json:"data"`
	Pagination PageInfo         `json:"pagination"`
}

// OutstandingQuestionReviewCount is the badge beside the Payout Requests':
// how many Reviews are waiting across the platform.
type OutstandingQuestionReviewCount struct {
	OutstandingCount int `json:"outstanding_count"`
}

// QuestionReviewVerdict is one validated ruling.
type QuestionReviewVerdict struct {
	ItemID  string
	Verdict string
	Reason  string
}

// AnswerQuestionReviewInput is the Operator's whole answer.
type AnswerQuestionReviewInput struct {
	Verdicts []QuestionReviewVerdict
	Operator string
}

// QuestionReviewQueue returns one page of every outstanding Review, oldest
// first.
func (s *Service) QuestionReviewQueue(ctx context.Context, page, pageSize int) (*QuestionReviewQueue, error) {
	reviews, total, err := s.events.ListOutstandingQuestionReviews(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	return &QuestionReviewQueue{
		Data: reviews,
		Pagination: PageInfo{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages(total, pageSize),
		},
	}, nil
}

// OutstandingQuestionReviewCount returns the queue as one number.
func (s *Service) OutstandingQuestionReviewCount(ctx context.Context) (*OutstandingQuestionReviewCount, error) {
	count, err := s.events.CountOutstandingQuestionReviews(ctx)
	if err != nil {
		return nil, err
	}
	return &OutstandingQuestionReviewCount{OutstandingCount: count}, nil
}

// GetQuestionReview returns one Review whole, in any state.
func (s *Service) GetQuestionReview(ctx context.Context, reviewID string) (*QuestionReview, error) {
	return s.events.GetQuestionReviewForOperator(ctx, reviewID)
}

// AnswerQuestionReview records the verdicts as one act.
func (s *Service) AnswerQuestionReview(ctx context.Context, reviewID string, input AnswerQuestionReviewInput) (*QuestionReview, error) {
	verdicts := make([]catalogsvc.QuestionReviewVerdictInput, 0, len(input.Verdicts))
	for _, v := range input.Verdicts {
		verdicts = append(verdicts, catalogsvc.QuestionReviewVerdictInput{ItemID: v.ItemID, Verdict: v.Verdict, Reason: v.Reason})
	}
	return s.events.AnswerQuestionReview(ctx, reviewID, catalogsvc.AnswerQuestionReviewInput{
		Verdicts: verdicts,
		Operator: input.Operator,
	})
}
