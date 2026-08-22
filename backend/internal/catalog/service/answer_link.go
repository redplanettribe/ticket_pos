package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The Answer Link's own surface: minting one, opening one, and answering
// through one (#312, ADR 0044).
//
// EVERYTHING IN THIS FILE IS UNAUTHENTICATED. There is no ActorContext anywhere
// below, and that is the decision rather than an oversight: no sign-in, no
// One-time Passcode, no Customer created and no session minted. ADR 0044 records
// why — requiring proof of identity would mean collecting an address from
// somebody who never came to this platform, which is the third-party collection
// problem the whole feature exists to avoid. A wrong Answer is cheap, and both
// the buyer and Event Staff can fix it.
//
// The authority is the signed token and its whole reach is ONE Ticket's Ticket
// Questions. What keeps that safe is not authentication but DISCLOSURE: see
// AnswerLinkView, which is the boundary this ticket is actually about.

// answerLinkPath is the Storefront route an Answer Link points at.
//
// A STOREFRONT URL AND NEVER AN API ONE, like every other link this platform
// mails or hands out: the holder must land on a page, and no browser may address
// the Go API directly (ADR 0008).
//
// It is written WITHOUT a Locale prefix. The Storefront's middleware redirects
// an unprefixed path into a language and carries the query string with it, which
// is how the Confirmation Link and the Consent Confirmation Link already work.
// That matters more here than it does for those two: this link is pasted into a
// group chat by hand, and baking the buyer's Locale into it would hand a
// Spanish-speaking holder the buyer's English page.
//
// It is deliberately NOT under /tickets, which is the Customer Area. Holding this
// link makes nobody a Customer, and an address implying otherwise would be the
// first step toward somebody adding a "your tickets" link to the page.
const answerLinkPath = "/answer"

// AnswerLinkView is one Ticket as the holder of its Answer Link may see it.
//
// THIS TYPE IS THE SECURITY BOUNDARY OF THIS FEATURE. An Answer Link is an
// unauthenticated URL that gets forwarded into group chats, and ADR 0044 is
// explicit that its safety rests entirely on disclosing nothing about the
// purchase. So this is a SEPARATE TYPE from TicketAnswersView rather than a
// subset of it, assembled from a SEPARATE repository read that does not fetch
// the forbidden columns at all — a field that was never selected is not there to
// be leaked by somebody adding a `json:` tag.
//
// WHAT IS ABSENT, AND WHY EACH ONE STAYS ABSENT:
//
//   - The buyer's name and email. The holder is not the buyer and is owed
//     nothing about them; often they are a friend, and sometimes they are six
//     friends in a group chat.
//   - The price. What somebody paid for a gift is between them and the person
//     they bought it for.
//   - The Tax ID. It is a national identity number in this market.
//   - The Sale Confirmation reference. It is the buyer's credential-adjacent
//     handle on the sale, quoted to support, and printed on the receipt.
//   - The Ticket Sale's id, and the ordinal. Both say something about the
//     purchase's SHAPE — "you are the third of four" tells a group chat how many
//     tickets were bought, and an id is a thing to try somewhere else.
//   - Every other Ticket of the Sale. One link, one Ticket.
//
// ADR 0044 names the failure mode directly: "Anyone widening what the page shows
// — adding the buyer's name 'for context', or the confirmation reference 'to
// help support' — is reversing this decision." There is an integration test that
// asserts each of the above is absent from the raw response body, and it exists
// so that widening this struct is a red test rather than a code review somebody
// might wave through.
//
// NOR IS THERE A TICKET ID. The form posts back with the TOKEN, so the page
// never needs one, and not sending it means the response carries no identifier
// at all that could be tried against another surface.
type AnswerLinkView struct {
	// EventName is one of the three things the page shows. The Event is public —
	// it has a Storefront page anybody can read — so naming it discloses nothing
	// that was not already published.
	EventName string `json:"event_name"`
	// TicketTypeName is the second. Also public, for the same reason: it is a
	// row on that same Event page, with its price beside it. What is NOT here is
	// the price this particular buyer paid, which a Promotion or an Affiliate
	// Link may have made different from the published one.
	TicketTypeName string `json:"ticket_type_name"`
	// Questions is the third: this Ticket Type's Ticket Questions with whatever
	// this Ticket has already said. Labels read AS COINED in every Locale, like
	// a Custom Tag — only the page's chrome follows the reader's Locale.
	//
	// The Answers are shown because THE HOLDER IS ENTITLED TO SEE WHAT WAS SAID
	// ABOUT THEM. An Answer given here replaces one the buyer guessed at
	// checkout, and somebody cannot correct a guess they cannot see. This is
	// data about the holder, not about the purchase.
	Questions []TicketQuestionAnswerView `json:"questions"`
}

// AnswerLinkURL returns the Answer Link for one Ticket.
//
// This is what #315's buyer-facing page copies to a clipboard. It is a pure
// function of the Ticket id and the signing key: no database read, no Event
// lookup, and no row written anywhere — a stateless token needs no table, which
// is the reason this ticket has no migration.
//
// It does NOT check that the Ticket exists, or that its window is open. Both are
// read at the moment the link is OPENED, which is the only moment at which the
// answer is still true; a check here would be a check against a state the link
// will outlive.
func (s *Service) AnswerLinkURL(ticketID string) (string, error) {
	token, ok := s.answerLinks.Sign(ticketID)
	if !ok {
		return "", catalog.ErrAnswerLinkUnavailable()
	}
	return s.answerLinkBaseURL + answerLinkPath + "?token=" + token, nil
}

// OpenAnswerLink turns a token into the one Ticket's questions it opens.
//
// THE ORDER OF THE REFUSALS IS THE DESIGN. The feature flag is read before the
// token is even parsed, so a dark build answers exactly as a build that never
// had the feature (ADR 0045). Then the signature, so a forged or truncated token
// never reaches a database lookup. Then the Ticket, then the window.
//
// EVERY REFUSAL BUT ONE IS THE SAME REFUSAL. A bad signature, a Ticket that no
// longer exists and a REVERSED Sale are all ANSWER_LINK_INVALID, because telling
// them apart would let whoever holds the link learn something about the
// purchase — that it existed, that it was refunded — which is the one thing this
// page must never do. Only "the Event has started" is told apart, and only
// because an Event's start is already published.
func (s *Service) OpenAnswerLink(ctx context.Context, token string) (*AnswerLinkView, error) {
	ticket, err := s.answerLinkTicket(ctx, token)
	if err != nil {
		return nil, err
	}
	return s.answerLinkView(ctx, ticket)
}

// AnswerByAnswerLink writes one Ticket's Answer to one of its Ticket Questions,
// on the authority of the token alone.
//
// AN ANSWER GIVEN HERE REPLACES ONE THE BUYER GAVE AT CHECKOUT. There is nothing
// special about that in the code and there should not be: an Answer belongs to
// the TICKET, so this is the same upsert through the same (ticket, question)
// UNIQUE that Event Staff go through, and "replaces" is what one row per pair
// means. Nothing records that the holder rather than the buyer wrote it —
// migration 073 has no author column on purpose.
//
// It reuses AnswerTicketQuestion's whole body through answerTicketQuestion,
// which is where catalog.ParseAnswer, the retired-question rule, the Option
// resolution and the "a re-submission is not a change" guard all live. Three
// surfaces answering the same question — is this a thing this Ticket Question
// can be answered with — would be three ways for a NUMERIC column to end up
// holding something that is not a number.
func (s *Service) AnswerByAnswerLink(
	ctx context.Context,
	token, questionID string,
	input AnswerInput,
) (*AnswerLinkView, error) {
	ticket, err := s.answerLinkTicket(ctx, token)
	if err != nil {
		return nil, err
	}

	if err := s.answerTicketQuestion(ctx, ticket.ID, ticket.TicketTypeID, questionID, input); err != nil {
		return nil, err
	}
	return s.answerLinkView(ctx, ticket)
}

// answerLinkTicket is the single gate every route in this file passes through:
// flag, signature, Ticket, window.
//
// ONE GATE AND NOT TWO, because the read and the write must refuse identically.
// If opening a link on a reversed Sale were invalid but answering through one
// were merely refused, the write would confirm the Ticket exists to somebody the
// read had just told nothing.
func (s *Service) answerLinkTicket(ctx context.Context, token string) (*repository.AnswerLinkTicket, error) {
	// The flag first, before anything is parsed or read. While the feature is
	// dark this address must be indistinguishable from one that was never
	// routed — same status, same code, and no read whose timing could differ
	// between a real Ticket and an invented one (ADR 0045).
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if !s.answerLinks.Configured() {
		return nil, catalog.ErrAnswerLinkUnavailable()
	}

	ticketID, ok := s.answerLinks.Parse(token)
	if !ok {
		return nil, catalog.ErrAnswerLinkInvalid()
	}

	ticket, err := s.repo.GetAnswerLinkTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		// Signed by us and naming nothing — a Ticket whose Event was deleted
		// while it was a draft, or whose Sale Import was undone. Reported as an
		// invalid link and never as a missing Ticket: the holder is not entitled
		// to learn which of the two it was.
		return nil, catalog.ErrAnswerLinkInvalid()
	}

	// THE ANSWER LINK STOPS OPENING ONCE A HOLDER HAS ACCEPTED (#325, ADR 0046).
	//
	// This is the one thing accepting takes away, and it is what makes accepting
	// mean anything. Both doors stand open while a Ticket is merely `assigned`,
	// so a mistyped address bricks nothing and an ignored mail degrades to
	// exactly ADR 0044's behaviour. The moment somebody PROVES the address, this
	// door is retired in their favour: a link still sitting in a group chat, or
	// forwarded on to six people, cannot overwrite the Holder's own answer about
	// their own body as a joke.
	//
	// IT IS ANSWER_LINK_INVALID AND NOT A NEW CODE. Whoever holds this link is
	// entitled to learn that it no longer works and nothing else — that somebody
	// else has claimed this Ticket is a fact about a person, and this page has
	// never disclosed one.
	//
	// THE BUYER AND EVENT STAFF KEEP THEIR ROUTES. ADR 0044's three-party rule
	// holds: this closes the unauthenticated door, not the two accountable ones,
	// so a wrong Answer stays fixable.
	if ticket.AcceptedAt.Valid {
		return nil, catalog.ErrAnswerLinkInvalid()
	}

	switch catalog.AnswerWindow(ticket.SaleStatus, nullTimeOrNil(ticket.EventStartsAt), s.now()) {
	case catalog.AnswerRefusedSaleReversed:
		// NOT ErrTicketSaleReversed, which is what Event Staff hear. Staff are
		// entitled to know a sale was reversed and to keep reading its Answers;
		// a group chat is entitled to neither. See catalog.ErrAnswerLinkInvalid.
		return nil, catalog.ErrAnswerLinkInvalid()
	case catalog.AnswerRefusedEventStarted:
		return nil, catalog.ErrAnswerLinkExpired()
	}
	return ticket, nil
}

// answerLinkView assembles the payload from the Ticket Type's questions and this
// Ticket's Answers.
//
// It reuses ticketQuestionViews and the Answer read, which is why retired
// questions and retired Options arrive here too. That is right for the same
// reason it is right on the staff surface: an Answer against a retired Option
// has to keep reading, and a question whose Answer vanished from the page would
// look like the holder's reply had been thrown away.
func (s *Service) answerLinkView(ctx context.Context, ticket *repository.AnswerLinkTicket) (*AnswerLinkView, error) {
	questions, err := s.ticketQuestionViews(ctx, ticket.TicketTypeID)
	if err != nil {
		return nil, err
	}
	answers, err := s.repo.ListTicketAnswers(ctx, []string{ticket.ID})
	if err != nil {
		return nil, err
	}
	byQuestion := make(map[string]int, len(answers))
	for i, answer := range answers {
		byQuestion[answer.TicketQuestionID] = i
	}

	view := AnswerLinkView{
		EventName:      ticket.EventName,
		TicketTypeName: ticket.TicketTypeName,
		Questions:      make([]TicketQuestionAnswerView, 0, len(questions)),
	}
	for _, question := range questions {
		pair := TicketQuestionAnswerView{Question: question}
		if i, found := byQuestion[question.ID]; found {
			pair.Answer = toAnswerView(answers[i])
		}
		view.Questions = append(view.Questions, pair)
	}
	return &view, nil
}
