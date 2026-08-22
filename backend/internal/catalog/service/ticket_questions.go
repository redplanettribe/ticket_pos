package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// TicketQuestionView is one Ticket Question as the authoring surface reads it.
//
// The label is the Organization's own words and travels untranslated: the staff
// app and the Storefront both render it exactly as it is stored, in every
// Locale, as a Custom Tag is (ADR 0027). Only the chrome around it — the field
// labels, the kind names, the warning — follows the reader's language.
type TicketQuestionView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	// Required produces an Outstanding Answer and nothing else. It is not a
	// constraint, and no surface may treat it as one.
	Required bool `json:"required"`
	// Timing is 'at_checkout' on every row written so far; the field is here so
	// the Organization's choice can be honoured later without migrating Answers.
	Timing    string `json:"timing"`
	SortOrder int    `json:"sort_order"`
	// Retired is true for a question kept only so that what has already been
	// answered still reads. Retired questions are returned so the authoring
	// surface can show them rather than appearing to have lost them.
	Retired bool `json:"retired"`
	// Options is empty for the five kinds that are not answered by choosing.
	Options   []TicketQuestionOptionView `json:"options"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
}

// TicketQuestionOptionView is one selectable value of a choice Ticket Question.
type TicketQuestionOptionView struct {
	// ID is the Option's stable identity. A rename changes Label and never this,
	// which is what keeps the Answers given under the old wording attached.
	ID        string `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
	// Retired means gone from new lists, kept on the Tickets that chose it, and
	// still entitled to its column in the Sales Export.
	Retired bool `json:"retired"`
}

// CreateTicketQuestionInput states a whole new Ticket Question.
type CreateTicketQuestionInput struct {
	Label    string
	Kind     catalog.TicketQuestionKind
	Required bool
	Timing   catalog.TicketQuestionTiming
	// OptionLabels are the Options a choice question is created with, in order.
	// Must be empty for the five kinds that take none.
	OptionLabels []string
}

// UpdateTicketQuestionInput restates a Ticket Question's editable fields. The
// whole question is sent because a form sends a whole form; the kind among them
// is checked against the freeze rather than trusted.
type UpdateTicketQuestionInput struct {
	Label    string
	Kind     catalog.TicketQuestionKind
	Required bool
	Timing   catalog.TicketQuestionTiming
}

// ticketTypeForQuestions resolves the Ticket Type every Ticket Question
// operation hangs off, and is the single place the feature flag is read.
//
// The flag check comes FIRST, before the Event or the Ticket Type is looked at,
// so that a request while the feature is dark cannot be told apart from one
// against a build that never had it: same status, same code, and no read that
// could differ in timing between a real Ticket Type and an invented one.
func (s *Service) ticketTypeForQuestions(ctx context.Context, actor ActorContext, eventID, ticketTypeID string) (*repository.TicketType, error) {
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

	ticketType, err := s.repo.GetTicketTypeByID(ctx, actor.OrganizationID, eventID, ticketTypeID)
	if err != nil {
		return nil, err
	}
	if ticketType == nil {
		return nil, catalog.ErrTicketTypeNotFound()
	}
	return ticketType, nil
}

// ListTicketQuestions returns a Ticket Type's Ticket Questions, live ones first,
// each with its Options.
func (s *Service) ListTicketQuestions(ctx context.Context, actor ActorContext, eventID, ticketTypeID string) ([]TicketQuestionView, error) {
	if _, err := s.ticketTypeForQuestions(ctx, actor, eventID, ticketTypeID); err != nil {
		return nil, err
	}

	questions, err := s.repo.ListTicketQuestionsByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}
	options, err := s.repo.ListTicketQuestionOptionsByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}

	byQuestion := make(map[string][]repository.TicketQuestionOption, len(questions))
	for _, option := range options {
		byQuestion[option.TicketQuestionID] = append(byQuestion[option.TicketQuestionID], option)
	}

	views := make([]TicketQuestionView, 0, len(questions))
	for i := range questions {
		views = append(views, toTicketQuestionView(&questions[i], byQuestion[questions[i].ID]))
	}
	return views, nil
}

// CreateTicketQuestion adds a Ticket Question to a Ticket Type.
func (s *Service) CreateTicketQuestion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID string,
	input CreateTicketQuestionInput,
) (*TicketQuestionView, error) {
	if _, err := s.ticketTypeForQuestions(ctx, actor, eventID, ticketTypeID); err != nil {
		return nil, err
	}

	label, ok := catalog.NormalizeTicketQuestionLabel(input.Label)
	if !ok {
		return nil, catalog.ErrInvalidTicketQuestionLabel("label", catalog.MaxTicketQuestionLabelLength)
	}

	optionLabels, err := normalizeOptionLabels(input.Kind, input.OptionLabels)
	if err != nil {
		return nil, err
	}

	sortOrder, err := s.repo.NextTicketQuestionSortOrder(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}

	question, options, err := s.repo.CreateTicketQuestion(ctx, ticketTypeID, repository.CreateTicketQuestionParams{
		Label:        label,
		Kind:         string(input.Kind),
		Required:     input.Required,
		Timing:       string(input.Timing),
		SortOrder:    sortOrder,
		OptionLabels: optionLabels,
	}, s.now())
	if err != nil {
		return nil, err
	}

	view := toTicketQuestionView(question, options)
	return &view, nil
}

// UpdateTicketQuestion renames a Ticket Question and restates its flags.
//
// The kind is the one field with a rule behind it: it is frozen once any Ticket
// has answered this question, because the stored Answers ARE that kind. Since
// #310 landed the Answer, TicketQuestionHasAnswers is a real read and this
// refusal is reachable — the guard was written one ticket ahead of the data it
// governs, and that ticket only had to make the method tell the truth.
func (s *Service) UpdateTicketQuestion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID, questionID string,
	input UpdateTicketQuestionInput,
) (*TicketQuestionView, error) {
	if _, err := s.ticketTypeForQuestions(ctx, actor, eventID, ticketTypeID); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetTicketQuestionByID(ctx, ticketTypeID, questionID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}
	if existing.RetiredAt.Valid {
		return nil, catalog.ErrTicketQuestionRetired()
	}

	label, ok := catalog.NormalizeTicketQuestionLabel(input.Label)
	if !ok {
		return nil, catalog.ErrInvalidTicketQuestionLabel("label", catalog.MaxTicketQuestionLabelLength)
	}

	answersExist, err := s.repo.TicketQuestionHasAnswers(ctx, questionID)
	if err != nil {
		return nil, err
	}
	currentKind := catalog.TicketQuestionKind(existing.Kind)
	if catalog.TicketQuestionKindFrozen(currentKind, input.Kind, answersExist) {
		return nil, catalog.ErrTicketQuestionKindFrozen()
	}

	// A question changing INTO a choice kind must arrive with something to
	// choose from. It cannot, because Options are added through their own
	// endpoint — so the change is refused with the error that names the missing
	// piece rather than leaving an unanswerable question behind.
	if input.Kind.OffersOptions() && input.Kind != currentKind {
		liveOptions, countErr := s.repo.CountLiveTicketQuestionOptions(ctx, questionID)
		if countErr != nil {
			return nil, countErr
		}
		if liveOptions == 0 {
			return nil, catalog.ErrTicketQuestionOptionsRequired()
		}
	}

	updated, err := s.repo.UpdateTicketQuestion(ctx, ticketTypeID, questionID, repository.UpdateTicketQuestionParams{
		Label:     label,
		Kind:      string(input.Kind),
		Required:  input.Required,
		Timing:    string(input.Timing),
		SortOrder: existing.SortOrder,
	}, s.now())
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}

	options, err := s.repo.ListTicketQuestionOptions(ctx, questionID)
	if err != nil {
		return nil, err
	}

	view := toTicketQuestionView(updated, options)
	return &view, nil
}

// RetireTicketQuestion takes a Ticket Question out of every new list without
// deleting it. This is the DELETE verb's whole meaning here, and the reason
// there is no hard delete: an Organization that typed the wrong kind retires the
// question and adds another, and the Answers under the old one must survive that.
func (s *Service) RetireTicketQuestion(ctx context.Context, actor ActorContext, eventID, ticketTypeID, questionID string) (*TicketQuestionView, error) {
	if _, err := s.ticketTypeForQuestions(ctx, actor, eventID, ticketTypeID); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetTicketQuestionByID(ctx, ticketTypeID, questionID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}
	if existing.RetiredAt.Valid {
		return nil, catalog.ErrTicketQuestionRetired()
	}

	retired, err := s.repo.RetireTicketQuestion(ctx, ticketTypeID, questionID, s.now())
	if err != nil {
		return nil, err
	}
	if retired == nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}

	options, err := s.repo.ListTicketQuestionOptions(ctx, questionID)
	if err != nil {
		return nil, err
	}

	view := toTicketQuestionView(retired, options)
	return &view, nil
}

// ReorderTicketQuestions puts a Ticket Type's live Ticket Questions in the order
// given.
//
// The caller states the WHOLE order rather than moving one question by index.
// One request that says what the list is cannot leave two questions claiming the
// same place, and a reorder that raced another edit fails whole instead of
// applying half.
func (s *Service) ReorderTicketQuestions(ctx context.Context, actor ActorContext, eventID, ticketTypeID string, questionIDs []string) ([]TicketQuestionView, error) {
	if _, err := s.ticketTypeForQuestions(ctx, actor, eventID, ticketTypeID); err != nil {
		return nil, err
	}

	existing, err := s.repo.ListTicketQuestionsByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}

	// The submitted ids must be exactly the live questions, each once. A missing
	// one would silently keep a stale place and a stranger would name somebody
	// else's question, so anything but an exact match is refused rather than
	// partially honoured. Retired questions are not orderable: they are not in
	// any list to be ordered.
	live := make(map[string]bool)
	for _, q := range existing {
		if !q.RetiredAt.Valid {
			live[q.ID] = true
		}
	}
	if len(questionIDs) != len(live) {
		return nil, catalog.ErrTicketQuestionNotFound()
	}
	seen := make(map[string]bool, len(questionIDs))
	for _, id := range questionIDs {
		if !live[id] || seen[id] {
			return nil, catalog.ErrTicketQuestionNotFound()
		}
		seen[id] = true
	}

	if err := s.repo.ReorderTicketQuestions(ctx, ticketTypeID, questionIDs, s.now()); err != nil {
		return nil, err
	}
	return s.ListTicketQuestions(ctx, actor, eventID, ticketTypeID)
}

// AddTicketQuestionOption adds one Option to a choice Ticket Question.
//
// Options are added freely and at any time — Answers or not — which is the half
// of the editing rules that never needed a guard. The two that stand in the way
// are the kind (five of the seven are not answered by choosing) and the cap.
func (s *Service) AddTicketQuestionOption(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID, questionID, label string,
) (*TicketQuestionView, error) {
	question, err := s.liveTicketQuestion(ctx, actor, eventID, ticketTypeID, questionID)
	if err != nil {
		return nil, err
	}
	if !catalog.TicketQuestionKind(question.Kind).OffersOptions() {
		return nil, catalog.ErrTicketQuestionKindTakesNoOptions()
	}

	optionLabel, ok := catalog.NormalizeTicketQuestionOptionLabel(label)
	if !ok {
		return nil, catalog.ErrInvalidTicketQuestionLabel("label", catalog.MaxTicketQuestionOptionLabelLength)
	}

	liveOptions, err := s.repo.CountLiveTicketQuestionOptions(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if catalog.TicketQuestionOptionCapReached(liveOptions) {
		return nil, catalog.ErrTooManyTicketQuestionOptions(catalog.MaxTicketQuestionOptions)
	}

	sortOrder, err := s.repo.NextTicketQuestionOptionSortOrder(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.CreateTicketQuestionOption(ctx, questionID, optionLabel, sortOrder, s.now()); err != nil {
		return nil, err
	}
	return s.ticketQuestionView(ctx, ticketTypeID, questionID)
}

// RenameTicketQuestionOption corrects an Option's wording.
//
// Allowed at any time and with no guard whatsoever, which is the point of an
// Option having an identity independent of its label: the rename reaches the
// export header and every list, and forks nothing that was answered under the
// old words.
func (s *Service) RenameTicketQuestionOption(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID, questionID, optionID, label string,
) (*TicketQuestionView, error) {
	if _, err := s.liveTicketQuestion(ctx, actor, eventID, ticketTypeID, questionID); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetTicketQuestionOptionByID(ctx, questionID, optionID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrTicketQuestionOptionNotFound()
	}
	if existing.RetiredAt.Valid {
		return nil, catalog.ErrTicketQuestionOptionRetired()
	}

	optionLabel, ok := catalog.NormalizeTicketQuestionOptionLabel(label)
	if !ok {
		return nil, catalog.ErrInvalidTicketQuestionLabel("label", catalog.MaxTicketQuestionOptionLabelLength)
	}

	if _, err := s.repo.RenameTicketQuestionOption(ctx, questionID, optionID, optionLabel, s.now()); err != nil {
		return nil, err
	}
	return s.ticketQuestionView(ctx, ticketTypeID, questionID)
}

// RetireTicketQuestionOption takes an Option out of every new list. Never a
// delete: the Tickets that chose it keep reading, and it keeps its export
// column.
//
// The last live Option is refused, because a choice question with nothing to
// choose from is not a question. The way to close one down is to retire the
// question itself.
func (s *Service) RetireTicketQuestionOption(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID, questionID, optionID string,
) (*TicketQuestionView, error) {
	if _, err := s.liveTicketQuestion(ctx, actor, eventID, ticketTypeID, questionID); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetTicketQuestionOptionByID(ctx, questionID, optionID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrTicketQuestionOptionNotFound()
	}
	if existing.RetiredAt.Valid {
		return nil, catalog.ErrTicketQuestionOptionRetired()
	}

	liveOptions, err := s.repo.CountLiveTicketQuestionOptions(ctx, questionID)
	if err != nil {
		return nil, err
	}
	if liveOptions <= 1 {
		return nil, catalog.ErrTicketQuestionOptionsRequired()
	}

	if _, err := s.repo.RetireTicketQuestionOption(ctx, questionID, optionID, s.now()); err != nil {
		return nil, err
	}
	return s.ticketQuestionView(ctx, ticketTypeID, questionID)
}

// liveTicketQuestion resolves a Ticket Question that may still be edited: the
// flag is open, the Ticket Type is this Organization's, and the question has not
// been retired.
func (s *Service) liveTicketQuestion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketTypeID, questionID string,
) (*repository.TicketQuestion, error) {
	if _, err := s.ticketTypeForQuestions(ctx, actor, eventID, ticketTypeID); err != nil {
		return nil, err
	}
	question, err := s.repo.GetTicketQuestionByID(ctx, ticketTypeID, questionID)
	if err != nil {
		return nil, err
	}
	if question == nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}
	if question.RetiredAt.Valid {
		return nil, catalog.ErrTicketQuestionRetired()
	}
	return question, nil
}

// ticketQuestionView re-reads one question and its Options after a write, so
// every Option endpoint answers with the whole question rather than a fragment
// the caller would have to reassemble.
func (s *Service) ticketQuestionView(ctx context.Context, ticketTypeID, questionID string) (*TicketQuestionView, error) {
	question, err := s.repo.GetTicketQuestionByID(ctx, ticketTypeID, questionID)
	if err != nil {
		return nil, err
	}
	if question == nil {
		return nil, catalog.ErrTicketQuestionNotFound()
	}
	options, err := s.repo.ListTicketQuestionOptions(ctx, questionID)
	if err != nil {
		return nil, err
	}
	view := toTicketQuestionView(question, options)
	return &view, nil
}

// normalizeOptionLabels checks the Options a question is being created with
// against its kind and the cap.
//
// A choice question must arrive with at least one Option and no more than
// twenty; the five other kinds must arrive with none, rather than having stray
// Options quietly dropped — a caller that sent them believed something about the
// question that is not true, and silence would let it keep believing it.
func normalizeOptionLabels(kind catalog.TicketQuestionKind, raw []string) ([]string, error) {
	if !kind.OffersOptions() {
		if len(raw) > 0 {
			return nil, catalog.ErrTicketQuestionKindTakesNoOptions()
		}
		return nil, nil
	}
	if len(raw) == 0 {
		return nil, catalog.ErrTicketQuestionOptionsRequired()
	}
	if len(raw) > catalog.MaxTicketQuestionOptions {
		return nil, catalog.ErrTooManyTicketQuestionOptions(catalog.MaxTicketQuestionOptions)
	}

	labels := make([]string, 0, len(raw))
	for _, candidate := range raw {
		label, ok := catalog.NormalizeTicketQuestionOptionLabel(candidate)
		if !ok {
			return nil, catalog.ErrInvalidTicketQuestionLabel("options", catalog.MaxTicketQuestionOptionLabelLength)
		}
		labels = append(labels, label)
	}
	return labels, nil
}

func toTicketQuestionView(question *repository.TicketQuestion, options []repository.TicketQuestionOption) TicketQuestionView {
	views := make([]TicketQuestionOptionView, 0, len(options))
	for _, option := range options {
		views = append(views, TicketQuestionOptionView{
			ID:        option.ID,
			Label:     option.Label,
			SortOrder: option.SortOrder,
			Retired:   option.RetiredAt.Valid,
		})
	}
	return TicketQuestionView{
		ID:        question.ID,
		Label:     question.Label,
		Kind:      question.Kind,
		Required:  question.Required,
		Timing:    question.Timing,
		SortOrder: question.SortOrder,
		Retired:   question.RetiredAt.Valid,
		Options:   views,
		CreatedAt: question.CreatedAt,
		UpdatedAt: question.UpdatedAt,
	}
}
