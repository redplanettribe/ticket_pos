package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// TicketAnswersView is one Ticket with every Ticket Question its Ticket Type
// asks and whatever that Ticket has said in reply (#310).
//
// It is ONE payload rather than a Ticket read and a questions read, because
// there is no useful thing to do with half of it: a surface showing questions
// without their Answers cannot tell an answered one from an Outstanding one, and
// a surface showing Answers without their questions is a list of values with
// nothing to say what they are values of.
type TicketAnswersView struct {
	TicketID string `json:"ticket_id"`
	// Ordinal is which of its Ticket Sale Line's units this Ticket is,
	// 1..quantity. Internal and not a seat number — but it is the only thing
	// telling two Tickets of one line apart, which is what lets staff say "the
	// second of Ana's four".
	Ordinal        int    `json:"ordinal"`
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	TicketSaleID   string `json:"ticket_sale_id"`
	// ConfirmationRef is the buyer's own reference for the Ticket Sale, which is
	// how staff on the phone find the sale a caller is asking about.
	ConfirmationRef string `json:"confirmation_ref"`
	// Answerable is whether this Ticket's Answers may still be written. False
	// once the Event has started and false on a reversed Ticket Sale — and never
	// a reason to hide anything: everything below stays readable either way.
	Answerable bool `json:"answerable"`
	// AnswerableRefusal names WHY not, or is empty while it is answerable. The
	// staff app draws the reason beside the frozen fields rather than leaving
	// somebody to wonder why the form will not take.
	AnswerableRefusal string `json:"answerable_refusal"`
	// Questions carries the Ticket Type's questions in the order they are asked,
	// retired ones last, each with this Ticket's Answer or null.
	Questions []TicketQuestionAnswerView `json:"questions"`
}

// TicketQuestionAnswerView pairs one Ticket Question with one Ticket's reply.
type TicketQuestionAnswerView struct {
	Question TicketQuestionView `json:"question"`
	// Answer is null when this Ticket has not answered this question — which,
	// on a required question, is an Outstanding Answer. Null and never a blank
	// Answer: "not said" and "said nothing" are different facts, and only the
	// first of them is a debt an Organization can chase.
	Answer *AnswerView `json:"answer"`
}

// AnswerView is what one Ticket says in reply to one Ticket Question.
//
// EXACTLY ONE FIELD IS NON-NULL, decided by the question's kind, which the
// caller already has beside this in TicketQuestionAnswerView. Five nullable
// fields rather than one `value` of shifting type, so a reader in a typed
// language can tell a number from the text "3" without consulting the kind
// first — and so `false` on a checkbox is visibly an Answer rather than an
// absence.
type AnswerView struct {
	Text *string `json:"text"`
	// Number is a decimal string and not a JSON number, deliberately. JSON
	// numbers are IEEE doubles in most readers, and a value that survived a
	// NUMERIC column only to be rounded by the browser parsing it would defeat
	// the column. The staff app renders the digits; nothing arithmetic happens
	// on this side.
	Number *string `json:"number"`
	// Date is a calendar date, YYYY-MM-DD. Never an instant: it carries no time
	// and no zone, so nothing can shift it by a day.
	Date    *string `json:"date"`
	Checked *bool   `json:"checked"`
	// Options are the Options a choice Answer chose, in the order given. Empty
	// for the five kinds not answered by choosing.
	Options []AnswerOptionView `json:"options"`
	// UpdatedAt is when this Answer last changed. The whole of the history this
	// platform keeps: no versions, and no record of who changed it.
	UpdatedAt time.Time `json:"updated_at"`
}

// AnswerOptionView is one Option a choice Answer chose.
type AnswerOptionView struct {
	// OptionID is the Option's stable identity, which is what a form posts back
	// and what survives a rename.
	OptionID string `json:"option_id"`
	// Label is THE SNAPSHOT: the words this Option showed when it was chosen.
	// This is what the person actually read, and nothing ever rewrites it.
	Label string `json:"label"`
	// CurrentLabel is what the same Option reads NOW. It equals Label until
	// somebody corrects the Option's wording; after that the two differ, and
	// both are true statements about different moments.
	CurrentLabel string `json:"current_label"`
	// Retired is true for an Option kept only so that what chose it still reads.
	// An Answer against one persists and stays readable; what it may not do is
	// be chosen afresh.
	Retired bool `json:"retired"`
}

// AnswerInput is one Answer as a caller states it, before the Ticket Question's
// kind has been read against it. See catalog.SubmittedAnswer for why every field
// is optional and why absence is meaningful.
type AnswerInput struct {
	Text      *string
	Number    *string
	Date      *string
	Checked   *bool
	OptionIDs []string
}

// ListTicketSaleAnswers returns every Ticket of one Ticket Sale with its Ticket
// Questions and Answers.
//
// This is how staff REACH a Ticket at all. Nothing lists Tickets across an Event
// — a per-Event list of every unit of admission is the Outstanding Answers view
// (#313), which is a different question with a different shape — so the entry
// point is the Ticket Sale, which is the thing staff already have in front of
// them when somebody calls about their order.
func (s *Service) ListTicketSaleAnswers(ctx context.Context, actor ActorContext, eventID, ticketSaleID string) ([]TicketAnswersView, error) {
	if err := s.ticketAnswersAvailable(ctx, actor, eventID); err != nil {
		return nil, err
	}
	// A malformed Sale id names no Sale, so it gets the empty list a Sale this
	// Organization cannot see gets (below), rather than reaching the uuid
	// column as text Postgres refuses. It is checked here and not in the
	// handler because the Event is answered for first.
	if !catalog.IsUUID(ticketSaleID) {
		return []TicketAnswersView{}, nil
	}

	tickets, err := s.repo.ListAnswerableTicketsByTicketSale(ctx, actor.OrganizationID, eventID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	// An empty list rather than a 404 on a sale with no Tickets. A Ticket Sale
	// that this Organization cannot see resolves to no rows here for the same
	// reason it would 404 elsewhere — the query is scoped — and the two are
	// deliberately the same answer.
	if len(tickets) == 0 {
		return []TicketAnswersView{}, nil
	}
	return s.ticketAnswersViews(ctx, tickets)
}

// GetTicketAnswers returns one Ticket with its Ticket Questions and Answers.
func (s *Service) GetTicketAnswers(ctx context.Context, actor ActorContext, eventID, ticketID string) (*TicketAnswersView, error) {
	ticket, err := s.ticketForAnswers(ctx, actor, eventID, ticketID)
	if err != nil {
		return nil, err
	}
	views, err := s.ticketAnswersViews(ctx, []repository.AnswerableTicket{*ticket})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// AnswerTicketQuestion writes one Ticket's Answer to one Ticket Question,
// creating it or correcting what was there.
//
// The order of the checks is the order of the refusals a caller should hear
// first: the flag, then whether this Ticket is theirs, then whether it may be
// written at all, then whether the question is one that may still be answered,
// and only then whether the value fits. Validating the body before establishing
// that the Ticket exists would tell a stranger which of their guesses was a real
// Ticket by how the refusal changed.
func (s *Service) AnswerTicketQuestion(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketID, questionID string,
	input AnswerInput,
) (*TicketAnswersView, error) {
	ticket, err := s.ticketForAnswers(ctx, actor, eventID, ticketID)
	if err != nil {
		return nil, err
	}
	if err := s.answerWindowOpen(ticket); err != nil {
		return nil, err
	}

	if err := s.answerTicketQuestion(ctx, ticketID, ticket.TicketTypeID, questionID, input); err != nil {
		return nil, err
	}
	return s.GetTicketAnswers(ctx, actor, eventID, ticketID)
}

// answerTicketQuestion is the WRITE ITSELF, with no opinion about who is
// writing or how they came to be entitled to.
//
// EVERY ROUTE INTO AN ANSWER GOES THROUGH THIS ONE BODY: Event Staff above, and
// whoever holds the Answer Link (#312). Each is a different surface asking the
// same questions — does this question exist on this Ticket Type, is it retired,
// does the value fit its kind, which Options does it offer, and did anything
// actually change — and two answers to any of them would be two ways for a
// NUMERIC column to end up holding something that is not a number, or for
// updated_at to move when nothing did.
//
// WHAT IT DELIBERATELY DOES NOT DO is check the edit window or establish that
// the caller may reach this Ticket. Both are the CALLER's to establish, because
// the two callers establish them from entirely different evidence — a Staff
// Session scoped to an Organization, or a signed token naming one Ticket — and
// folding either in here would mean this function had to know which surface it
// was serving.
//
// It returns no view. The two callers need DIFFERENT views of the same write:
// staff get the whole Ticket including its Sale's reference, and the holder gets
// the Holder's own view (HeldTicketAnswersView), which shows none of that. Returning one of them here would put
// the wider one within reach of the surface that must never render it.
func (s *Service) answerTicketQuestion(
	ctx context.Context,
	ticketID, ticketTypeID, questionID string,
	input AnswerInput,
) error {
	// A malformed question id names no question, and is answered exactly as a
	// well-formed one that names nothing is (the path-id rule in the api-errors
	// skill and docs/technical-design.md, "Handler errors"), here after the
	// Ticket has been resolved so the Ticket's own not-found still comes first.
	// It must never reach the uuid column, where Postgres refuses it as a 500.
	if !catalog.IsUUID(questionID) {
		return catalog.ErrTicketQuestionNotFound()
	}
	question, err := s.repo.GetTicketQuestionByID(ctx, ticketTypeID, questionID)
	if err != nil {
		return err
	}
	if question == nil {
		return catalog.ErrTicketQuestionNotFound()
	}
	// A retired question is kept so that what has already been answered still
	// reads; it is not something new can be said about. The Answers under it
	// stay exactly where they are.
	if question.RetiredAt.Valid {
		return catalog.ErrTicketQuestionRetired()
	}
	// A question the Platform Operator has not approved was asked of nobody
	// (ADR 0056), and nothing can be said in reply to it — by staff any more
	// than by a Holder. It reads as not found rather than as a refusal with a
	// name, because on every surface that answers it does not exist.
	if question.ReviewStatus != string(catalog.TicketQuestionReviewApproved) {
		return catalog.ErrTicketQuestionNotFound()
	}

	kind := catalog.TicketQuestionKind(question.Kind)
	value, problem := catalog.ParseAnswer(kind, catalog.SubmittedAnswer{
		Text:      input.Text,
		Number:    input.Number,
		Date:      input.Date,
		Checked:   input.Checked,
		OptionIDs: input.OptionIDs,
	})
	if problem != catalog.AnswerOK {
		return invalidAnswerError(kind, problem)
	}

	existing, err := s.repo.GetTicketAnswer(ctx, ticketID, questionID)
	if err != nil {
		return err
	}

	chosen, err := s.resolveAnswerOptions(ctx, questionID, value.OptionIDs, existing)
	if err != nil {
		return err
	}

	// A RE-SUBMISSION OF THE SAME ANSWER IS NOT A CHANGE, and updated_at means
	// "when this Answer last changed". A form saved twice or a double-clicked
	// button must not move the timestamp, because the timestamp is the only
	// history this platform keeps and it should say something true.
	//
	// It is also what makes an Answer Link REPLACING a buyer's checkout guess
	// honest: writing the same words the buyer already wrote leaves the
	// timestamp where it was, and writing different ones moves it.
	if !answerChanged(existing, value, chosen) {
		return nil
	}

	_, err = s.repo.UpsertTicketAnswer(ctx, ticketID, questionID,
		upsertParams(value, chosen), s.now())
	return err
}

// RemoveTicketAnswer takes one Ticket's Answer to one Ticket Question away,
// putting the Ticket back where it was before anybody answered.
//
// The way to say "not said" — and the only way, because a blank Answer would be
// a row no later reader could tell from a real reply. On a required question
// that restores an Outstanding Answer, which is a debt an Organization can see
// and chase rather than a defect.
//
// It stands behind the same edit window as writing one: the doors have opened,
// or the Sale was reversed, and what is recorded stays recorded.
func (s *Service) RemoveTicketAnswer(
	ctx context.Context,
	actor ActorContext,
	eventID, ticketID, questionID string,
) (*TicketAnswersView, error) {
	ticket, err := s.ticketForAnswers(ctx, actor, eventID, ticketID)
	if err != nil {
		return nil, err
	}
	if err := s.answerWindowOpen(ticket); err != nil {
		return nil, err
	}

	removed, err := s.repo.DeleteTicketAnswer(ctx, ticketID, questionID)
	if err != nil {
		return nil, err
	}
	if !removed {
		return nil, catalog.ErrAnswerNotFound()
	}
	return s.GetTicketAnswers(ctx, actor, eventID, ticketID)
}

// ticketAnswersAvailable is the single place the feature flag and the Event
// scope are read for every Answer surface.
//
// The flag check comes FIRST, before the Event is looked at, for the reason
// ticketTypeForQuestions gives: a request while the feature is dark must not be
// distinguishable from one against a build that never had it — same status, same
// code, and no read whose timing could differ between a real Event and an
// invented one (ADR 0045).
func (s *Service) ticketAnswersAvailable(ctx context.Context, actor ActorContext, eventID string) error {
	if !s.ticketQuestionsEnabled {
		return catalog.ErrTicketQuestionsUnavailable()
	}
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if event == nil {
		return catalog.ErrEventNotFound()
	}
	return nil
}

// ticketForAnswers resolves one Ticket of one Event, scoped to the acting
// Organization.
func (s *Service) ticketForAnswers(ctx context.Context, actor ActorContext, eventID, ticketID string) (*repository.AnswerableTicket, error) {
	if err := s.ticketAnswersAvailable(ctx, actor, eventID); err != nil {
		return nil, err
	}
	ticket, err := s.repo.GetAnswerableTicketByID(ctx, actor.OrganizationID, eventID, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, catalog.ErrTicketNotFound()
	}
	return ticket, nil
}

// answerWindowOpen turns the domain's reading of the edit window into the
// refusal a caller hears. Only the WRITE paths consult it; every read stays open
// forever, because a Sale Reversal voids a sale rather than erasing what its
// Tickets answered.
func (s *Service) answerWindowOpen(ticket *repository.AnswerableTicket) error {
	switch catalog.AnswerWindow(ticket.SaleStatus, nullTimeOrNil(ticket.EventStartsAt), s.now()) {
	case catalog.AnswerRefusedSaleReversed:
		return catalog.ErrTicketSaleReversed()
	case catalog.AnswerRefusedEventStarted:
		return catalog.ErrEventStartedAnswersClosed()
	default:
		return nil
	}
}

// resolveAnswerOptions turns the Option ids a choice Answer named into the rows
// to write, each carrying the words its Option shows RIGHT NOW as its snapshot.
//
// THE SNAPSHOT IS TAKEN HERE, at the moment of answering, from the same read
// that validated the ids. That is what makes it a true statement: these are the
// words this surface was looking at, so the snapshot cannot describe a label the
// validation never saw.
//
// A RETIRED OPTION MAY BE KEPT BUT NOT CHOSEN AFRESH. A retired Option has left
// every new list, so naming one that this Answer had not already chosen is
// naming something that is not on offer. Naming one it HAD already chosen is
// exactly "kept on the Tickets that chose it" — which is what lets somebody tick
// a second box on a multi_choice Answer without the retired first one being
// refused out from under them.
func (s *Service) resolveAnswerOptions(
	ctx context.Context,
	questionID string,
	optionIDs []string,
	existing *repository.TicketAnswer,
) ([]repository.UpsertTicketAnswerOption, error) {
	if len(optionIDs) == 0 {
		return nil, nil
	}

	options, err := s.repo.ListTicketQuestionOptions(ctx, questionID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]repository.TicketQuestionOption, len(options))
	for _, option := range options {
		byID[option.ID] = option
	}

	alreadyChosen := make(map[string]bool)
	if existing != nil {
		for _, option := range existing.Options {
			alreadyChosen[option.TicketQuestionOptionID] = true
		}
	}

	chosen := make([]repository.UpsertTicketAnswerOption, 0, len(optionIDs))
	for _, id := range optionIDs {
		option, ok := byID[id]
		if !ok {
			// Either no such Option, or one belonging to another question. The
			// same refusal, deliberately: telling those apart would confirm that
			// somebody else's id exists.
			return nil, catalog.ErrAnswerOptionNotOffered()
		}
		// A retired Option, or one the Platform Operator has not approved (ADR
		// 0056), is offered to nobody; it can only stay chosen, never be chosen.
		notOffered := option.RetiredAt.Valid ||
			option.ReviewStatus != string(catalog.TicketQuestionReviewApproved)
		if notOffered && !alreadyChosen[id] {
			return nil, catalog.ErrAnswerOptionNotOffered()
		}
		chosen = append(chosen, repository.UpsertTicketAnswerOption{
			TicketQuestionOptionID: option.ID,
			LabelSnapshot:          option.Label,
		})
	}
	return chosen, nil
}

// ticketAnswersViews assembles the payload: the questions of each Ticket's
// Ticket Type, paired with that Ticket's Answers.
func (s *Service) ticketAnswersViews(ctx context.Context, tickets []repository.AnswerableTicket) ([]TicketAnswersView, error) {
	typed := make([]ticketOfType, 0, len(tickets))
	for _, ticket := range tickets {
		typed = append(typed, ticketOfType{ID: ticket.ID, TicketTypeID: ticket.TicketTypeID})
	}
	pairsByTicket, err := s.ticketQuestionAnswers(ctx, typed)
	if err != nil {
		return nil, err
	}

	now := s.now()
	views := make([]TicketAnswersView, 0, len(tickets))
	for _, ticket := range tickets {
		refusal := catalog.AnswerWindow(ticket.SaleStatus, nullTimeOrNil(ticket.EventStartsAt), now)
		views = append(views, TicketAnswersView{
			TicketID:          ticket.ID,
			Ordinal:           ticket.Ordinal,
			TicketTypeID:      ticket.TicketTypeID,
			TicketTypeName:    ticket.TicketTypeName,
			TicketSaleID:      ticket.TicketSaleID,
			ConfirmationRef:   ticket.ConfirmationRef,
			Answerable:        refusal == catalog.AnswerWindowOpen,
			AnswerableRefusal: answerRefusalToken(refusal),
			Questions:         pairsByTicket[ticket.ID],
		})
	}
	return views, nil
}

// ticketOfType is all ticketQuestionAnswers needs of a Ticket: which one, and
// which Ticket Type's questions it is asked.
type ticketOfType struct {
	ID           string
	TicketTypeID string
}

// ticketQuestionAnswers pairs each Ticket with every approved question its
// Ticket Type asks, in the order they are asked, and that Ticket's Answer or
// null — keyed by Ticket id, never nil for a Ticket passed in. It is the one
// statement of how an Answer reads to staff, shared by the Answers dialog and
// the Customer Dossier (#641).
//
// It reads the questions ONCE PER TICKET TYPE and the Answers ONCE FOR ALL
// TICKETS. A Ticket Sale is usually one line of several Tickets, so the loop is
// over one or two Ticket Types however many Tickets there are.
func (s *Service) ticketQuestionAnswers(ctx context.Context, tickets []ticketOfType) (map[string][]TicketQuestionAnswerView, error) {
	ticketIDs := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		ticketIDs = append(ticketIDs, ticket.ID)
	}
	answers, err := s.repo.ListTicketAnswers(ctx, ticketIDs)
	if err != nil {
		return nil, err
	}
	// Keyed on (ticket, question), which is the Answer's own identity — the
	// UNIQUE that says one Ticket says one thing in reply to one question.
	byTicketQuestion := make(map[string]map[string]repository.TicketAnswer, len(tickets))
	for _, answer := range answers {
		if byTicketQuestion[answer.TicketID] == nil {
			byTicketQuestion[answer.TicketID] = make(map[string]repository.TicketAnswer)
		}
		byTicketQuestion[answer.TicketID][answer.TicketQuestionID] = answer
	}

	questionsByType := make(map[string][]TicketQuestionView)
	pairsByTicket := make(map[string][]TicketQuestionAnswerView, len(tickets))
	for _, ticket := range tickets {
		questions, ok := questionsByType[ticket.TicketTypeID]
		if !ok {
			questions, err = s.ticketQuestionViews(ctx, ticket.TicketTypeID)
			if err != nil {
				return nil, err
			}
			questionsByType[ticket.TicketTypeID] = questions
		}
		pairs := make([]TicketQuestionAnswerView, 0, len(questions))
		for _, question := range questions {
			pair := TicketQuestionAnswerView{Question: question}
			if answer, found := byTicketQuestion[ticket.ID][question.ID]; found {
				pair.Answer = toAnswerView(answer)
			}
			pairs = append(pairs, pair)
		}
		pairsByTicket[ticket.ID] = pairs
	}
	return pairsByTicket, nil
}

// ticketQuestionViews reads one Ticket Type's APPROVED questions with their
// approved Options, retired ones included (catalog.ApprovedQuestionSQL).
//
// APPROVED ONLY, because a draft, under-review or refused question was asked of
// nobody (ADR 0056): there is no Answer to pair it with, and showing a Holder a
// question they were never put would be asking it. The same predicate the
// checkout, the Holder List, the export and the Answer Reminder read.
//
// RETIRED QUESTIONS AND RETIRED OPTIONS ARE BOTH RETURNED, because this surface
// has to show what a Ticket already answered. An Answer against a retired Option
// persists and stays readable; if the Option vanished from the payload there
// would be nothing to read its snapshot against, and a retired question whose
// Answer disappeared would look like data loss.
func (s *Service) ticketQuestionViews(ctx context.Context, ticketTypeID string) ([]TicketQuestionView, error) {
	questions, err := s.repo.ListApprovedTicketQuestionsByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}
	options, err := s.repo.ListApprovedTicketQuestionOptionsByTicketTypeID(ctx, ticketTypeID)
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

// answerChanged reports whether the submitted Answer says anything different
// from the one already stored — the guard that keeps updated_at meaning "last
// changed" rather than "last submitted".
//
// The Options are compared as an ORDERED list, so re-ticking the same two boxes
// in the other order counts as a change. That is the honest reading: the order
// is stored, it is what the Answer reads back in, and calling a reordering "no
// change" would leave the stored order disagreeing with what was last sent.
func answerChanged(existing *repository.TicketAnswer, value catalog.AnswerValue, chosen []repository.UpsertTicketAnswerOption) bool {
	if existing == nil {
		return true
	}
	if nullStringValue(existing.Text) != value.Text ||
		nullStringValue(existing.Number) != value.Number ||
		nullStringValue(existing.Date) != value.Date {
		return true
	}
	if existing.Checked.Valid != (value.Kind == catalog.TicketQuestionKindCheckbox) ||
		existing.Checked.Bool != value.Checked {
		return true
	}
	if len(existing.Options) != len(chosen) {
		return true
	}
	for i, option := range chosen {
		if existing.Options[i].TicketQuestionOptionID != option.TicketQuestionOptionID {
			return true
		}
	}
	return false
}

// upsertParams turns a validated AnswerValue into the nullable columns its kind
// occupies, leaving the other three NULL. The database CHECKs that at most one
// of them is set, which is the shape this function is the only producer of.
func upsertParams(value catalog.AnswerValue, chosen []repository.UpsertTicketAnswerOption) repository.UpsertTicketAnswerParams {
	params := repository.UpsertTicketAnswerParams{Options: chosen}
	switch value.Kind {
	case catalog.TicketQuestionKindShortText, catalog.TicketQuestionKindLongText:
		params.Text = sql.NullString{String: value.Text, Valid: true}
	case catalog.TicketQuestionKindNumber:
		params.Number = sql.NullString{String: value.Number, Valid: true}
	case catalog.TicketQuestionKindDate:
		params.Date = sql.NullString{String: value.Date, Valid: true}
	case catalog.TicketQuestionKindCheckbox:
		// Valid: true even when the value is false, because FALSE IS AN ANSWER.
		// A NULL here would be indistinguishable from a question nobody answered.
		params.Checked = sql.NullBool{Bool: value.Checked, Valid: true}
	}
	return params
}

func toAnswerView(answer repository.TicketAnswer) *AnswerView {
	view := AnswerView{
		Options:   make([]AnswerOptionView, 0, len(answer.Options)),
		UpdatedAt: answer.UpdatedAt,
	}
	if answer.Text.Valid {
		text := answer.Text.String
		view.Text = &text
	}
	if answer.Number.Valid {
		number := answer.Number.String
		view.Number = &number
	}
	if answer.Date.Valid {
		date := answer.Date.String
		view.Date = &date
	}
	if answer.Checked.Valid {
		checked := answer.Checked.Bool
		view.Checked = &checked
	}
	for _, option := range answer.Options {
		view.Options = append(view.Options, AnswerOptionView{
			OptionID:     option.TicketQuestionOptionID,
			Label:        option.LabelSnapshot,
			CurrentLabel: option.CurrentLabel,
			Retired:      option.Retired,
		})
	}
	return &view
}

// answerRefusalToken names a closed window for the wire.
//
// A TOKEN AND NEVER A SENTENCE: the staff app owns the words, in the reader's
// Staff Locale (ADR 0041), and a sentence composed here would be composed in
// whatever language this file happens to be written in.
func answerRefusalToken(refusal catalog.AnswerRefusal) string {
	switch refusal {
	case catalog.AnswerRefusedSaleReversed:
		return "sale_reversed"
	case catalog.AnswerRefusedEventStarted:
		return "event_started"
	default:
		return ""
	}
}

// invalidAnswerError gives each AnswerProblem its domain error.
//
// The two Option-shaped problems are told apart from the rest because the form
// control they point at is different — a set of checkboxes rather than a field —
// but they travel under the same code with a problem token, so the staff app
// maps tokens to sentences in one place rather than growing a case per kind.
func invalidAnswerError(kind catalog.TicketQuestionKind, problem catalog.AnswerProblem) error {
	token, extra := answerProblemToken(kind, problem)
	return catalog.ErrInvalidAnswer(string(kind), token, extra)
}

func answerProblemToken(kind catalog.TicketQuestionKind, problem catalog.AnswerProblem) (string, map[string]any) {
	switch problem {
	case catalog.AnswerMissing:
		return "missing", nil
	case catalog.AnswerWrongShape:
		return "wrong_shape", nil
	case catalog.AnswerTooLong:
		return "too_long", map[string]any{"max_length": answerMaxLength(kind)}
	case catalog.AnswerNotANumber:
		return "not_a_number", nil
	case catalog.AnswerNotADate:
		return "not_a_date", nil
	case catalog.AnswerOneOptionOnly:
		return "one_option_only", nil
	case catalog.AnswerDuplicateOption:
		return "duplicate_option", nil
	case catalog.AnswerTooManyOptions:
		return "too_many_options", map[string]any{"max": catalog.MaxTicketQuestionOptions}
	default:
		return "invalid", nil
	}
}

// answerMaxLength is the cap the refusal was measured against, so a form can say
// how much too long rather than only that it was.
func answerMaxLength(kind catalog.TicketQuestionKind) int {
	switch kind {
	case catalog.TicketQuestionKindShortText:
		return catalog.MaxShortTextAnswerLength
	case catalog.TicketQuestionKindLongText:
		return catalog.MaxLongTextAnswerLength
	case catalog.TicketQuestionKindNumber:
		return catalog.MaxNumberAnswerIntegerDigits
	default:
		return 0
	}
}

func nullTimeOrNil(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	instant := value.Time
	return &instant
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
