package service

import (
	"context"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Assignment Link's own surface: mailing one, accepting through one, and
// what a Holder may do once they have (#325, parent #322, ADR 0046).
//
// EVERYTHING IN THIS FILE IS UNAUTHENTICATED IN THE ORDINARY SENSE AND PROVES AN
// IDENTITY ANYWAY. There is no ActorContext, no Customer Session, no passcode
// and no password anywhere below — and yet, unlike its Answer Link neighbour,
// this flow MINTS A VERIFIED CUSTOMER. The click is the proof, by the standard
// ADR 0035 set for the Consent Confirmation Link: "clicking from the inbox being
// itself proof of ownership".
//
// WHICH IS WHY THE TOKEN MUST BE DISTINCT FROM THE ANSWER LINK, and why that is
// the load-bearing property of this whole feature rather than a detail of the
// format. The Answer Link is copyable off the buyer's own sale page. If this
// flow accepted one, the buyer could accept on their friend's behalf, and the
// Verified Customer minted from it would be a fiction — the platform asserting
// that somebody proved an address when nobody did. ADR 0046: an Assignment Link
// appearing on a buyer surface or in an API response to the buyer is a defect of
// the same severity as leaking the token itself. Nothing in this file returns a
// token, and the only place one is composed is the mail below.
//
// THE WORD IS `accept`, NEVER `confirm`. The glossary already carries four
// confirmations — Sale Confirmation, Confirmation Link, Consent Confirmation
// Link, `confirmation_ref` — and ADR 0046 puts `confirm*` on the avoid list for
// this flow. This one is the only token that mints an identity, so conflating it
// with any of the four is worse than a naming slip.

// assignmentLinkPath is the Storefront route an Assignment Link points at.
//
// A STOREFRONT URL AND NEVER AN API ONE, like every other link this platform
// mails: the Holder must land on a page, and no browser addresses the Go API
// directly (ADR 0008).
//
// It is written WITHOUT a Locale prefix, so the Storefront's middleware puts the
// reader into their own language and carries the query string with it. That
// matters more here than for any other link: the reader is not the buyer, is not
// a Customer, and may not share the buyer's language at all — baking in the
// language the buyer paid in would hand a Spanish-speaking Holder an English
// page. The MAIL is written in the reader's Mail Locale by its own route (see
// assignmentMailLocale); the PAGE is chosen by the browser that opens it.
//
// It is deliberately NOT under /answer and NOT under /tickets. Not /answer,
// because the two links are different tokens with different powers and one
// address would invite one page to try both. Not /tickets — the Customer Area —
// because the Holder is not signed in when they arrive; they become a Customer
// by pressing the button on this page, and only then does the Area have anything
// to show them.
const assignmentLinkPath = "/accept"

// AssignmentLinkView is one Ticket as the Holder who accepted it may see it.
//
// THIS TYPE IS THE DISCLOSURE BOUNDARY, and it is ADR 0044's rule carried over
// unchanged and applied to a person who is now known to the platform. It is a
// SEPARATE TYPE assembled from a SEPARATE repository read that never fetches the
// forbidden columns — a field that was never selected is not there to be leaked
// by somebody adding a `json:` tag.
//
// WHAT IS ABSENT, AND WHY EACH ONE STAYS ABSENT: the buyer's name and email, the
// price, the Tax ID, the Sale Confirmation reference, the Ticket Sale's id, the
// ordinal, and every other Ticket of the Sale. The reasons are the retired Answer Link's (ADR 0044),
// and are not repeated here; what IS worth stating is that they hold even though
// this reader is a Verified Customer. Being a Customer of this platform buys
// nobody a fact about somebody else's purchase.
//
// AND THE HOLDER'S OWN ADDRESS IS NOT HERE EITHER. They know it — it is where
// the mail arrived — and printing it back would put a third party's address on a
// page whose URL may have been forwarded.
//
// NOR IS THERE A TICKET ID. Every write posts back with the TOKEN, so the page
// never needs one, and no Ticket id ever appears in a path on this route: the
// token names the Ticket, and a path segment naming it too would be a second,
// unsigned way to say which Ticket this is.
type AssignmentLinkView struct {
	// EventName and TicketTypeName are the two public facts the page shows, as
	// on the Answer Link's page and for the same reason: both are already
	// readable by anybody on the Event's Storefront page.
	EventName      string `json:"event_name"`
	TicketTypeName string `json:"ticket_type_name"`
	// HolderFirstName and HolderLastName are the name the platform currently
	// holds for this Customer, for the form to start from. Both are empty for
	// somebody it has never named.
	//
	// THIS IS THE PREFILL, AND IT EXISTS ONLY ON THIS SIDE OF THE CLICK. There is
	// no route anywhere that reports it before the accept: a page reachable
	// without the click that showed a known Customer's name would be an oracle
	// for whether an address is registered, which ADR 0035 is explicit about
	// avoiding. The click is what buys it, and the click is proof that the person
	// asking is the person asked about.
	//
	// SEPARATE HALVES, per ADR 0005, and written to the Customer as their current
	// asserted name. There is no Tax ID here and there is no field for one: a Tax
	// ID is a fact about the sale's BUYER and is never asked of an attendee.
	HolderFirstName string `json:"holder_first_name"`
	HolderLastName  string `json:"holder_last_name"`
	// Questions is this Ticket Type's Ticket Questions with whatever this Ticket
	// has already said — the Holder answering for themselves, which is the whole
	// reason the accept step is worth having.
	//
	// EMPTY WHILE TICKET_QUESTIONS_ENABLED IS CLOSED, rather than the route
	// refusing. The two flags are separate on purpose (ADR 0046), and a Ticket
	// Type that asks nothing is an ordinary Ticket Type: accepting is worth doing
	// for the guest list alone.
	Questions []TicketQuestionAnswerView `json:"questions"`
}

// HolderCustomers is what catalog needs from customers in order to turn a click
// into a person.
//
// A SERVICE SEAM DECLARED HERE AND SATISFIED THERE, exactly as CustomerHoldings
// is, and pointing the same way: catalog never imports customers/service.
// Writing a Customer — and `verified_at` above all — is that module's authority
// (ADR 0010), and a second module able to assert an identity is a second module
// that could assert one nobody proved.
//
// THREE BARE STRINGS OUT OF AcceptHolder rather than a struct, because a struct
// would need a package both modules import and there is no honest home for one.
// See the implementation in customers/service/holder.go.
//
// ITS ZERO VALUE IS nil, AND A SERVICE WITHOUT ONE ACCEPTS NOTHING. That is the
// safe way to be unwired: the accept route reports the link as unavailable — a
// deployment fault, a 500 — rather than accepting a Ticket and attaching it to
// nobody, which the database's own CHECK would refuse anyway.
type HolderCustomers interface {
	// AcceptHolder mints or matches the Customer for a proven address, marks
	// them Verified, and reports the name already on that record for the prefill.
	AcceptHolder(ctx context.Context, email string, now time.Time) (customerID, firstName, lastName string, err error)
	// NameHolder writes the name the Holder gave as the Customer's current
	// asserted name. Never a Tax ID and never a phone: neither is ever asked of
	// a Holder.
	NameHolder(ctx context.Context, customerID, firstName, lastName string) error
	// HolderMailLocale reports the Mail Locale remembered for an address, or ""
	// when no Customer holds it. Its only use is the language of the Assignment
	// mail; nothing about the answer ever reaches a response body, which is what
	// keeps it from being an oracle.
	HolderMailLocale(ctx context.Context, email string) (string, error)
}

// AssignmentMailer delivers the Assignment mail.
//
// A ONE-METHOD INTERFACE OVER platform.EmailSender rather than the whole sender,
// because this service has exactly one message to send and the narrower seam
// says so. It also means nothing in catalog can reach the Sale Confirmation, the
// passcode or the Follow Digest.
type AssignmentMailer interface {
	SendTicketAssignment(ctx context.Context, assignment platform.TicketAssignment) error
}

// AcceptAssignmentLink is the click: it accepts the Ticket Assignment the token
// names and returns the page the Holder lands on.
//
// ONE CALL DOES BOTH, and that is the decision. There is no "open" that reads
// without accepting, because a page that could be read without accepting would
// have to decide what to show a person who has not proved anything yet — and the
// honest answer is "nothing", which is not a page. The mail is what tells
// somebody what they have; this is what makes it theirs.
//
// ACCEPTING TWICE IS IDEMPOTENT, which is an acceptance criterion and is also
// simply what a link in an inbox demands: people click twice, mail clients
// prefetch, and a second click a week later must land on the Holder's own page
// rather than on a refusal. The first acceptance's instant stands (see
// repository.AcceptTicketAssignment).
//
// IT GRANTS NO CONSENT OF ANY KIND. Nothing below writes a consent row, and that
// absence is the feature: a Customer minted this way has agreed to nothing
// beyond holding a ticket (ADR 0046). Anyone adding a Marketing Consent here —
// "they clicked, so they must want our newsletter" — is manufacturing consent
// from an act that was about a t-shirt size.
func (s *Service) AcceptAssignmentLink(ctx context.Context, token string) (*AssignmentLinkView, error) {
	ticket, holder, err := s.acceptAssignment(ctx, token)
	if err != nil {
		return nil, err
	}
	return s.assignmentLinkView(ctx, ticket, holder)
}

// NameByAssignmentLink writes the Holder's own name, on the authority of the
// token alone.
//
// BOTH HALVES, STORED SEPARATELY (ADR 0005), AND NOTHING ELSE IS ASKED FOR. No
// Tax ID — it is a fact about the sale's buyer, never about an attendee — no
// phone, no password, no passcode. A Holder is doing the platform a favour by
// saying who they are, and every extra field turns a favour into a registration.
//
// IT ACCEPTS FIRST, through the same gate the click goes through, so that a name
// arriving without a preceding open is not a state anybody has to reason about.
// Holding the token is the proof either way.
func (s *Service) NameByAssignmentLink(
	ctx context.Context,
	token, firstName, lastName string,
) (*AssignmentLinkView, error) {
	ticket, holder, err := s.acceptAssignment(ctx, token)
	if err != nil {
		return nil, err
	}

	// The handler has already refused a blank or overlong half as
	// VALIDATION_FAILED (#336) — required-fields validation is the handler's,
	// and the token names the Ticket so there is no id for that 400 to leak.
	// ParseHolderName here is the domain's one definition of the trim and
	// bound, and reaching its refusal means a caller skipped the handler's
	// check: a programming error, not a Holder's, so it surfaces as a 500
	// rather than as a code any client is told to handle.
	first, last, ok := catalog.ParseHolderName(firstName, lastName)
	if !ok {
		return nil, fmt.Errorf("holder name reached the service unvalidated")
	}
	if err := s.holders.NameHolder(ctx, holder.CustomerID, first, last); err != nil {
		return nil, err
	}
	holder.FirstName, holder.LastName = first, last

	return s.assignmentLinkView(ctx, ticket, holder)
}

// AnswerByAssignmentLink writes one of this Ticket's Answers, given by the
// person the Ticket is actually for.
//
// IT IS THE POINT OF THE WHOLE FEATURE. Under ADR 0044 the buyer guessed their
// friends' t-shirt sizes or forwarded four indistinguishable links; here the
// person wearing the shirt says the size, and — because they have accepted —
// nobody still holding a forwarded Answer Link can overwrite it (see
// the retired Answer Link did, which stopped opening an accepted Ticket).
//
// It reuses answerTicketQuestion, which is where catalog.ParseAnswer, the
// retired-question rule and the Option resolution live. Four surfaces now answer
// the same question — staff, buyer, Answer Link, Holder — and one shared body is
// what keeps a NUMERIC column from holding something that is not a number.
//
// The Ticket Question flag is read here and not in the gate, because the flags
// are separate: a deployment may run assignment with questions dark, in which
// case a Holder accepts, gives their name, and is asked nothing.
func (s *Service) AnswerByAssignmentLink(
	ctx context.Context,
	token, questionID string,
	input AnswerInput,
) (*AssignmentLinkView, error) {
	ticket, holder, err := s.acceptAssignment(ctx, token)
	if err != nil {
		return nil, err
	}
	if !s.ticketQuestionsEnabled {
		return nil, catalog.ErrTicketQuestionsUnavailable()
	}
	if err := s.answerTicketQuestion(ctx, ticket.ID, ticket.TicketTypeID, questionID, input); err != nil {
		return nil, err
	}
	return s.assignmentLinkView(ctx, ticket, holder)
}

// acceptedHolder is who the click proved the reader to be.
type acceptedHolder struct {
	CustomerID string
	FirstName  string
	LastName   string
}

// acceptAssignment is the single gate every route in this file passes through:
// flag, configuration, signature, Ticket, window, freshness, then the write.
//
// ONE GATE AND NOT THREE, because the three routes must refuse identically. If
// opening a reassigned Ticket were invalid but answering through one were merely
// refused, the write would confirm to a previous Holder that the Ticket still
// exists after the read had told them nothing.
//
// THE ORDER OF THE REFUSALS IS THE DESIGN:
//
//  1. The FLAG, before anything is parsed or read, so a closed build answers
//     exactly as a build that never had the feature (ADR 0045). Its own flag —
//     a deployment that opened Ticket Questions has said nothing about
//     assignment.
//  2. The SIGNATURE, so a forged, truncated or cross-purpose token never reaches
//     a database lookup. An Answer Link token fails here, cryptographically,
//     because it was signed under a different key.
//  3. The TICKET, and its window, read LIVE: a Sale reversed or an Event moved
//     after the mail went out changes what this link does.
//  4. The FRESHNESS of the assignment, which is what makes a reassignment kill
//     every link mailed to the previous address.
//
// EVERY REFUSAL BUT ONE IS THE SAME REFUSAL. A bad signature, a Ticket that no
// longer exists, a REVERSED Sale and a REASSIGNED Ticket are all
// ASSIGNMENT_LINK_INVALID, because telling them apart would disclose what the
// buyer did — cancelled the purchase, gave the ticket to somebody else — to a
// person who is never told who the buyer is. Only "the Event has started" is
// told apart, and only because an Event's start is already published.
func (s *Service) acceptAssignment(
	ctx context.Context,
	token string,
) (*repository.AssignmentLinkTicket, acceptedHolder, error) {
	var holder acceptedHolder

	if !s.ticketAssignmentEnabled {
		return nil, holder, catalog.ErrTicketAssignmentUnavailable()
	}
	if !s.assignmentLinks.Configured() || s.holders == nil {
		// A deployment fault and not the reader's: no link secret, or a service
		// nobody wired a Customer writer into. Refusing beats accepting a Ticket
		// on behalf of nobody.
		return nil, holder, catalog.ErrAssignmentLinkUnavailable()
	}

	ticketID, signedAt, fingerprint, ok := s.assignmentLinks.Parse(token)
	if !ok {
		return nil, holder, catalog.ErrAssignmentLinkInvalid()
	}

	ticket, err := s.repo.GetAssignmentLinkTicket(ctx, ticketID)
	if err != nil {
		return nil, holder, err
	}
	if ticket == nil {
		return nil, holder, catalog.ErrAssignmentLinkInvalid()
	}

	switch catalog.AnswerWindow(ticket.SaleStatus, nullTimeOrNil(ticket.EventStartsAt), s.now()) {
	case catalog.AnswerRefusedSaleReversed:
		// NOT ErrAssignmentSaleReversed, which is what the BUYER hears. The buyer
		// is entitled to know their own sale was reversed; the Holder is not
		// entitled to learn anything about somebody else's purchase, so it is
		// indistinguishable from a forgery here.
		return nil, holder, catalog.ErrAssignmentLinkInvalid()
	case catalog.AnswerRefusedEventStarted:
		// THE ONE REFUSAL TOLD APART. The window closes at the doors, read from
		// the Event as it stands now — an Organization that moves its Event moves
		// every Assignment Link's deadline with it — and it is also when #322's
		// purge takes an unaccepted address.
		return nil, holder, catalog.ErrAssignmentLinkExpired()
	}

	// THE TOKEN MUST NAME THE ASSIGNMENT THE TICKET CURRENTLY CARRIES — the same
	// address, named at the same moment. Anything else is a link mailed to an
	// address that no longer holds this Ticket: the buyer reassigned it, or
	// corrected a typo. Its reader is told the link does not open, and is told
	// nothing about why.
	//
	// THE ADDRESS IS CHECKED AND NOT ONLY THE TIMESTAMP, because two assignments
	// can share an instant — a buyer correcting a typo the moment they made it —
	// and a token that only knew "when" would let the previous address accept a
	// Ticket somebody else now holds. That is the whole failure this feature
	// cannot have: a Verified Customer minted for a person who was handed
	// nothing.
	if !s.assignmentLinks.NamesAssignment(
		signedAt, fingerprint, nullTimeOrNil(ticket.AssignedAt), ticket.HolderEmail.String,
	) {
		return nil, holder, catalog.ErrAssignmentLinkInvalid()
	}

	// THE CLICK IS PROOF OF EMAIL OWNERSHIP (ADR 0035), so this is where a row
	// becomes a person: the Customer is minted or matched on the normalised
	// address the buyer typed, and marked Verified.
	customerID, firstName, lastName, err := s.holders.AcceptHolder(ctx, ticket.HolderEmail.String, s.now())
	if err != nil {
		return nil, holder, err
	}

	written, err := s.repo.AcceptTicketAssignment(ctx, ticket.ID, customerID, ticket.AssignedAt.Time, s.now())
	if err != nil {
		return nil, holder, err
	}
	if !written {
		// The buyer reassigned the Ticket between the read above and this write.
		// The write lost the race deliberately — see repository.AcceptTicket
		// Assignment — and the reader is told what every other stale link is told.
		return nil, holder, catalog.ErrAssignmentLinkInvalid()
	}

	return ticket, acceptedHolder{CustomerID: customerID, FirstName: firstName, LastName: lastName}, nil
}

// assignmentLinkView assembles the page: the two public facts, the Holder's own
// name, and this Ticket's questions.
//
// It reuses ticketQuestionViews and the Answer read, so retired questions and
// retired Options arrive here too — right for the same reason it is right
// everywhere else: an Answer against a retired Option must keep reading, and a
// question whose Answer vanished would look like the Holder's reply had been
// thrown away.
func (s *Service) assignmentLinkView(
	ctx context.Context,
	ticket *repository.AssignmentLinkTicket,
	holder acceptedHolder,
) (*AssignmentLinkView, error) {
	view := AssignmentLinkView{
		EventName:       ticket.EventName,
		TicketTypeName:  ticket.TicketTypeName,
		HolderFirstName: holder.FirstName,
		HolderLastName:  holder.LastName,
		// Never nil, so the page always has a list to draw and never has to tell
		// "no questions" apart from "the flag is closed" — which is exactly the
		// distinction ADR 0045 says no surface may make.
		Questions: []TicketQuestionAnswerView{},
	}
	if !s.ticketQuestionsEnabled {
		return &view, nil
	}

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
	for _, question := range questions {
		pair := TicketQuestionAnswerView{Question: question}
		if i, found := byQuestion[question.ID]; found {
			pair.Answer = toAnswerView(answers[i])
		}
		view.Questions = append(view.Questions, pair)
	}
	return &view, nil
}

// mailTicketAssignment sends the one mail an assignment produces, and is the
// ONLY place an Assignment Link is ever composed.
//
// IT IS BEST EFFORT AND NEVER UNDOES THE ASSIGNMENT. A provider outage must not
// cost the buyer the record of who they gave which ticket to — that record is
// useful to them even unmailed, and the buyer can correct the address, which
// sends a fresh mail. The failure is logged where a Platform Operator can see
// it, which is the only place it can be seen: nobody is watching an inbox that
// never received anything.
//
// IT REFUSES TO COMPOSE A LINKLESS MESSAGE. A mail telling somebody they have a
// ticket and giving them no way to accept it is an instruction its reader cannot
// follow, and it would spend the one chance this platform has to be believed by
// a stranger. An unconfigured signer sends nothing and says so in the log.
//
// THE ADDRESS IT WRITES TO IS THE ONE JUST STORED, normalised — never the raw
// string the buyer typed, which may differ in case and whitespace from the value
// the token was minted against.
func (s *Service) mailTicketAssignment(
	ctx context.Context,
	ticket *repository.AssignmentLinkTicket,
	holderEmail string,
	assignedAt time.Time,
) bool {
	if s.mailer == nil {
		// No sender wired: local tooling and every test that has no opinion about
		// mail. Silent by design — the same posture the media cleanup takes.
		return false
	}

	acceptURL := s.assignmentLinkURL(ticket.ID, assignedAt, holderEmail)
	if acceptURL == "" {
		s.logAssignmentMailFailure("assignment link unavailable: no link secret configured", nil)
		return false
	}

	mail := platform.TicketAssignment{
		To:             holderEmail,
		EventName:      ticket.EventName,
		TicketTypeName: ticket.TicketTypeName,
		AcceptURL:      acceptURL,
		Locale:         s.assignmentMailLocale(ctx, holderEmail, ticket.SaleLocale.String),
	}
	if err := s.mailer.SendTicketAssignment(ctx, mail); err != nil {
		s.logAssignmentMailFailure("assignment mail delivery failed", err)
		return false
	}
	// TRUE MEANS A PROVIDER ACCEPTED IT, which is the only claim the #332 ledger
	// row beside the caller is entitled to make.
	return true
}

// assignmentLinkURL composes the Assignment Link a Ticket's Holder opens, and
// is THE ONLY PLACE ON THIS PLATFORM WHERE ONE IS BUILT.
//
// TWO CALLERS AND THERE MUST NEVER BE A THIRD OUTSIDE THIS FILE'S MODULE: the
// Assignment mail above, which tells a stranger they have a ticket, and the
// Holder's Answer Reminder (#328), which tells somebody who accepted one that it
// still owes an Answer. Both are messages addressed to the Holder's own inbox at
// the address the token is signed against, and that is the whole set of places
// this URL may go. ADR 0046 rates a link appearing on a buyer surface or in an
// API response to the buyer as a defect of the same severity as leaking the
// signing key, so a third caller is a decision and never a convenience.
//
// IT IS MINTED FRESH EVERY TIME AND NEVER STORED. The signer is a pure function
// of the secret, the Ticket, the instant the address was named and the address
// itself, so the reminder's link and the Assignment mail's link are the same
// string without either being kept anywhere — and a reassignment invalidates
// both at once, because both name the assignment they were minted for.
//
// EMPTY MEANS UNCONFIGURED: a deployment with no link secret, which NewApp
// refuses to build in production. Every caller treats "" as "compose no message
// at all" rather than as "send it without the link", because a message whose
// link is its whole content is an instruction its reader cannot follow.
func (s *Service) assignmentLinkURL(ticketID string, assignedAt time.Time, holderEmail string) string {
	token, ok := s.assignmentLinks.Sign(ticketID, assignedAt, holderEmail)
	if !ok {
		return ""
	}
	return s.assignmentLinkBaseURL + assignmentLinkPath + "?token=" + token
}

// logAssignmentMailFailure records a mail that did not go out — WITHOUT the
// address and without the link.
//
// The link is a credential that mints an identity and the address belongs to
// somebody who never came here; a log aggregator is a wider audience than an
// inbox. What an operator needs from this line is that a Holder was not told,
// which the count of these lines gives them.
func (s *Service) logAssignmentMailFailure(message string, err error) {
	if s.logger == nil {
		return
	}
	if err != nil {
		s.logger.Error(message, "error", err)
		return
	}
	s.logger.Error(message)
}

// assignmentMailLocale decides what language the Assignment mail is written in,
// and it is the ONE PLACE ON THIS PLATFORM WHERE ADR 0033'S CHAIN IS READ IN THE
// OTHER ORDER.
//
// Every other message here is about a sale, addressed to the person who made it,
// so the Sale Locale wins: it was collected at the moment of the act the mail is
// about, from somebody who had just read a whole page in that language. THIS
// MAIL'S READER IS NOT THAT PERSON. They did not buy anything, were not on that
// page, and may not share the buyer's language at all — a Spanish-speaking
// Holder whose friend paid on the English site is exactly the case the feature
// exists to serve. So the recipient's OWN remembered Mail Locale outranks the
// sale's, and the sale's is kept only as the better-than-nothing fallback: a
// friend who bought in Spanish is more likely than chance to have Spanish-
// speaking friends.
//
// English is the floor, as always, and both candidates go through ParseLocale —
// so an unserved or malformed value is skipped rather than written in.
//
// READING THE RECIPIENT'S RECORD HERE IS NOT AN ORACLE. Nothing about the answer
// reaches any response body: the buyer is told only that their assignment was
// recorded, and what changes with the value is the language of a message sent to
// the address itself. That is the difference from the passcode route, which
// deliberately refuses to read the stored Locale — there the response goes back
// to the anonymous caller who named the address.
func (s *Service) assignmentMailLocale(ctx context.Context, holderEmail, saleLocale string) platform.Locale {
	remembered := ""
	if s.holders != nil {
		if locale, err := s.holders.HolderMailLocale(ctx, holderEmail); err == nil {
			remembered = locale
		}
		// An error is swallowed on purpose: a language is not worth failing an
		// assignment over, and the fallbacks below are both honest answers.
	}
	return platform.ResolveMailLocale(remembered, saleLocale)
}
