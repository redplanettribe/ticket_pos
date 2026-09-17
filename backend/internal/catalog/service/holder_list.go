package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

// The Holder List (#333, rulings of 2026-08-22; formerly the Outstanding
// Answers surface of #313): every Ticket of an Event, each saying who is coming
// on it and — where the Event asks Ticket Questions — what it still owes.
//
// EVERY TICKET, NOT EVERY TICKET THAT OWES. The roster is the point and the
// questions are a column on it: a fully answered Ticket stays on the list, and
// an Event that asks no questions still has one, because "who is coming to my
// Event" is the headline value of Ticket Assignment and must not depend on
// anything having been asked. Outstanding Answers is a FILTER of this list —
// the owingOnly parameter — never its definition.
//
// THE DEBT IS STILL DERIVED, NEVER STORED. The rule is stated once in
// catalog.IsOutstandingAnswer and implemented once in SQL beside
// repository.outstandingAnswerWhere; nothing here re-decides it. This file's
// job is to page the roster and shape it for a screen.

// holderSortOwes is the one of the five sorts that belongs to a feature flag
// (#527): ordering by what a Ticket owes only means anything where the questions
// side of the list exists, exactly like the Owes column it ranks.
//
// SPELLED HERE AND IN THE REPOSITORY, which is a second copy of one word and is
// worth it for the reason holderAssignmentStates in the handler is a second copy
// of four: this one decides whether the sort survives the flag, and the
// repository's turns a key into an ORDER BY. They cannot silently disagree — a
// key this layer let through and the repository did not recognise falls back to
// the default order, which is the same answer this layer would have given.
const holderSortOwes = "owes"

// HolderListPage is one page of the Event's Holder List.
type HolderListPage struct {
	Data       []HolderTicketView    `json:"data"`
	Pagination OutstandingPagination `json:"pagination"`
	// OutstandingCount is how many Outstanding Answers the Event carries in ALL
	// — debts, not Tickets, so a Ticket owing three counts three. It is the
	// whole Event and never the page, because "how much don't I know yet" is a
	// question about the Event. It is unaffected by the owingOnly filter, for
	// the same reason.
	//
	// A POINTER, ABSENT WHILE TICKET_QUESTIONS_ENABLED IS CLOSED. This list is
	// readable on assignment alone (#333), and a build in that state must not
	// speak of debts a dark feature cannot define (ADR 0045).
	OutstandingCount *int `json:"outstanding_count,omitempty"`
}

// OutstandingPagination is the page metadata, in the shape every other paged
// staff list on this platform uses (ADR-0006).
type OutstandingPagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	// Total is how many Tickets the current view holds across the whole Event —
	// the roster, or the Tickets that owe when the filter is on. A page past
	// the last still reports it truthfully, so a surface can say how many there
	// are rather than appearing to have emptied.
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// HolderTicketView is one Ticket of the Event: its assignment, its buyer, and
// what it still owes.
type HolderTicketView struct {
	TicketID string `json:"ticket_id"`
	// Ordinal is which of its Ticket Sale Line's units this Ticket is,
	// 1..quantity. Internal and not a seat number, but the only thing telling
	// two Tickets of one line apart — which is what lets staff say "the second
	// of Ana's four".
	Ordinal        int    `json:"ordinal"`
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	// TicketSaleID and ConfirmationRef are how this row is ACTED ON. The staff
	// Answers dialog is keyed on a Ticket Sale and names itself after the
	// buyer's reference, so a row carrying neither would be a complaint nobody
	// could act on. This is the jump from the list to answering the Ticket.
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Channel is 'online', 'in_person' or 'import', and it EXPLAINS the row
	// rather than filtering it. A door sale and a Sale Import start out owing
	// every question because nobody ever put the questions to those buyers —
	// there is no checkout form on either. They stand here beside the online
	// ones, and the channel is what stops that reading as lost data.
	Channel string `json:"channel"`
	// The buyer: the party of record for the Sale, and the person to chase for
	// any Ticket nobody has accepted. They are no longer the only one — an
	// accepted Ticket names its Holder below — but they remain here on every row,
	// because a Holder is the named person a Ticket was handed to and never its
	// owner, and the Sale stays whole with the buyer either way.
	//
	// The two name parts stay APART, as they are on the Sales list and in the
	// column they are read from. Joining them here would mean choosing an order
	// for them, and which part leads a person's name is the reader's question and
	// not this payload's.
	CustomerFirstName string `json:"customer_first_name"`
	CustomerLastName  string `json:"customer_last_name"`
	CustomerEmail     string `json:"customer_email"`
	// CustomerID is the buyer's Customer, which the buyer's name links a
	// Customer Dossier by (#640).
	CustomerID string    `json:"customer_id"`
	SoldAt     time.Time `json:"sold_at"`
	// THE HOLDER (#329, parent #322, ADR 0047). Who is coming on this Ticket.
	//
	// EVERY FIELD IS `omitempty`, AND THAT IS THE FLAG'S DOING, exactly as it is
	// on the buyer's row. With TICKET_ASSIGNMENT_ENABLED closed the service fills
	// none of them and this payload is byte-identical to the one a build without
	// the feature sends (ADR 0045).

	// AssignmentState is `unassigned`, `assigned` or `accepted`, derived by
	// catalog.AssignmentState and never stored.
	//
	// IT IS THE FIELD THAT MAKES THE REST READABLE, and the reason it is on the
	// wire at all: a name arrives only with acceptance, so without the state an
	// `assigned` Ticket whose Holder never clicked would be indistinguishable
	// from one nobody was ever named for — and those are opposite facts to an
	// Organizer deciding whether to chase.
	//
	// THREE VALUES AND NEVER FOUR. A Ticket whose unaccepted address the
	// retention purge has taken (migration 081) reads `assigned` here, with
	// NeverAccepted set beside it — see fillHolderListEntry.
	AssignmentState string `json:"assignment_state,omitempty"`
	// NeverAccepted marks a Ticket whose assignment the retention purge closed:
	// somebody was named, nobody ever accepted, and the address is gone by
	// definition (#334, migration 081).
	//
	// A PRESENTATION-LEVEL INDICATOR DERIVED AT READ TIME from
	// holder_address_purged_at — deliberately NOT a fourth value in
	// catalog.AssignmentState, which #331 rightly rejected. It exists because
	// after the Event starts every unaccepted assignment otherwise reads
	// `unassigned`, and the morning-after sheet could not distinguish "nobody
	// was named" from "named and never claimed". It discloses nothing personal.
	NeverAccepted bool `json:"never_accepted,omitempty"`
	// HolderFirstName, HolderLastName and HolderEmail are the person a Ticket was
	// handed to, and they are filled ONLY once that person has ACCEPTED.
	//
	// THE DISCLOSURE RULE IS DECIDED HERE AND NOWHERE ELSE — see
	// fillHolderListEntry, which is the one place to change if it is ever
	// revisited. The address is disclosed deliberately and at a stated cost (ADR
	// 0047): an Organizer needs a way to reach the people attending its Event,
	// and a name it cannot write to leaves it routing through buyers by hand,
	// which is the problem assignment was built to end.
	HolderFirstName string `json:"holder_first_name,omitempty"`
	HolderLastName  string `json:"holder_last_name,omitempty"`
	HolderEmail     string `json:"holder_email,omitempty"`
	// HolderCustomerID is the accepted Holder's Customer, which their name
	// links a Customer Dossier by (#640). Filled on exactly the name's terms,
	// through fillHolderListEntry: an unaccepted assignment offers no link.
	HolderCustomerID string `json:"holder_customer_id,omitempty"`
	// Outstanding names the required questions this Ticket has not answered, in
	// the order they are asked. Empty on a Ticket that owes nothing — which
	// since #333 is an ordinary row of this list, not an absent one.
	//
	// A POINTER, ABSENT WHILE TICKET_QUESTIONS_ENABLED IS CLOSED, for
	// OutstandingCount's reason — and a pointer to a slice rather than an
	// `omitempty` slice so that "owes nothing" still reads as `[]` on the wire:
	// a typed reader that has to check for null before iterating is a reader
	// that will one day forget.
	Outstanding *[]OutstandingQuestionView `json:"outstanding,omitempty"`
}

// OutstandingQuestionView is one Outstanding Answer: a required Ticket Question
// this Ticket has not answered.
//
// It carries NO Answer field, and that absence is the point — there is no Answer,
// which is the entire fact being reported.
type OutstandingQuestionView struct {
	QuestionID string `json:"question_id"`
	// Label is the Organization's own words, read AS COINED in every Locale (ADR
	// 0027). Only the chrome around it follows the reader's Staff Locale.
	Label string `json:"label"`
	// Kind is the shape the Answer will take when it arrives, so the list can
	// show what is being asked for without a second read of the question.
	Kind      string `json:"kind"`
	SortOrder int    `json:"sort_order"`
}

// ListHolderListParams is one reading of the Holder List: the page wanted and
// how the roster is narrowed.
//
// A STRUCT AND NOT POSITIONAL ARGUMENTS (#523), in the shape of the Sales list's
// service.ListSalesParams, and for repository.ListHolderTicketsQuery's reason
// one layer down: three of these filters are strings, a caller that transposed
// the Ticket Type id and the Sales Channel would compile and be silently wrong,
// and #524–#527 add four more. A field is added without touching a single
// existing call site; a seventh positional argument is not.
//
// THE DATES ARE STILL "YYYY-MM-DD" HERE, not absolute times. They are CALENDAR
// DAYS until this service resolves them against the Event's own timezone
// (dateRangeBounds below) — the handler has no business knowing what a day is
// worth in Quito, and the repository has no business knowing there are days at
// all. Same division of labour the Sales list uses.
type ListHolderListParams struct {
	Page     int
	PageSize int

	// OwingOnly is the Outstanding Answers filter — one narrowing of the roster
	// and never its definition (#333). Closed below while Ticket Questions are
	// dark.
	OwingOnly bool
	// QuestionID narrows the roster to the Tickets owing ONE NAMED Ticket
	// Question (#525): "who still hasn't told me their shirt size", which on an
	// Event asking several questions is a different chase from "who owes
	// anything at all".
	//
	// IT COMPOSES WITH OwingOnly AND REPLACES NOTHING. `outstanding=true` with
	// a question named means what the question named alone means — owing this
	// implies owing something — and the checkbox is untouched.
	//
	// NOTHING HERE DECIDES WHAT IS OUTSTANDING; see
	// repository.holderRosterOwingQuestion, which is the debt's own SQL with
	// one clause added. Belongs to Ticket Questions and is dropped below while
	// that feature is dark, beside OwingOnly.
	QuestionID string
	// Search finds one person on the roster: a case-insensitive substring over
	// the BUYER's name and address, the Sale Confirmation reference, and — for
	// an ACCEPTED Holder only — that Holder's name and address (#526).
	//
	// SEARCHABLE IF AND ONLY IF DISPLAYABLE (ADR 0065). An address a buyer typed
	// and its owner never accepted matches NOTHING, and neither does a purged
	// one. That rule is enforced in the predicate itself — see
	// repository.holderRosterSearch, which carries the acceptance test inside
	// the Holder branch of its OR — and deliberately NOT here: a service that
	// post-filtered rows would leave the page's total describing a wider view,
	// and the leak would show in `pagination.total` even where no row was drawn.
	//
	// IT BELONGS TO NO FEATURE FLAG, unlike the two filters above and the state
	// filter below, and is passed through untouched on every build. A buyer's
	// name, a buyer's address and a Sale Confirmation reference exist on a plain
	// roster with both features dark; the Holder branch of the predicate simply
	// never matches there, because with Ticket Assignment closed no Ticket has
	// ever been accepted.
	//
	// IT IS NEVER LOGGED. It matches customer addresses, and a log aggregator is
	// a wider audience than the database; #529's audit line will record only
	// THAT a search was applied. Nothing on this path logs it today — see the
	// note on repository.ListHolderTicketsQuery.Search.
	Search string
	// TicketTypeID is one Ticket Type's roster: the VIP list apart from general
	// admission. A Ticket belongs to exactly one.
	TicketTypeID string
	// AssignmentState is where a Ticket stands with its Holder: `unassigned`,
	// `assigned`, `accepted` — or `never_accepted`, "who did I name who never
	// claimed their ticket", which is the morning-after question and a
	// SELECTABLE VALUE OF THIS FILTER AND NOT A FOURTH STATE (#524, ADR 0065).
	// catalog.AssignmentState still knows three. Belongs to Ticket Assignment
	// and is dropped below while that feature is dark.
	AssignmentState string
	// Channel is 'online', 'in_person' or 'import' — the buyers who came through
	// the door, or through an import and were therefore never asked anything.
	Channel string
	// SoldFrom/SoldTo are calendar days, INCLUSIVE OF BOTH ENDS as the reader
	// means them, read in the EVENT's timezone so "sold in January" is January
	// where the Event is and not where the reader is standing. Either may be
	// blank for an open end.
	SoldFrom string
	SoldTo   string
	// Sort and Dir are the order the reader asked for (#527, ADR 0065): one of
	// `sold_at`, `buyer`, `holder`, `ticket_type` or `owes`, and `asc` or
	// `desc`. Blank means the default, which is `sold_at` ascending — OLDEST
	// SALE FIRST, and unchanged: this screen is already in use and reordering
	// it under its readers is a change nobody asked for.
	//
	// ALREADY ALLOWLISTED BY THE HANDLER and allowlisted AGAIN in the
	// repository, which is the one place on this list where a parameter reaches
	// SQL as a fragment rather than as a bound argument. Neither layer refuses
	// an unknown value; both answer it with the default order.
	//
	// `owes` BELONGS TO TICKET QUESTIONS and is dropped below while that
	// feature is dark, beside OwingOnly and QuestionID.
	Sort string
	Dir  string
}

// ListHolderList returns a page of the Event's Holder List: every Ticket of
// every live Ticket Sale, oldest sale first, narrowed by the supplied filters.
//
// READABLE ON EITHER FLAG (#333). The list rides where the Outstanding Answers
// read always was, but it is the Holder List now, and an Organization that
// assigns tickets and asks nothing must still have one — so the gate opens on
// TICKET_ASSIGNMENT_ENABLED as well as TICKET_QUESTIONS_ENABLED, and only a
// build with both dark answers 404, exactly as a build without either feature
// would (ADR 0045).
//
// THE FILTERS NARROW THE PAGE AND THE TOTAL TOGETHER, because the repository
// builds both queries from one WHERE. A total that counted the whole roster
// under a filtered page would put "1 of 12 pages" over four rows, and an
// Organizer would go looking for the other eight.
//
// THE LIST'S DEBTS EMPTY BY THEMSELVES. Nothing here is invalidated or swept
// when an Answer arrives, because there is nothing to invalidate: what a Ticket
// owes is derived on every read, so an Answer written by Event Staff, by the
// checkout capture or through an Answer Link clears its row's debt on the next
// load, and a Sale Reversal removes all of that sale's rows at once.
func (s *Service) ListHolderList(
	ctx context.Context,
	actor ActorContext,
	eventID string,
	params ListHolderListParams,
) (*HolderListPage, error) {
	event, err := s.holderListAvailable(ctx, actor, eventID)
	if err != nil {
		return nil, err
	}
	honoured := s.honourHolderFilters(params)

	// The Event's own zone, so a date bound means the day it meant to whoever
	// typed it into the filter bar. The Event was already read by the gate
	// above, so this costs no query of its own.
	loc := resolveEventLocation(event.Timezone.String)
	soldFrom, soldTo := dateRangeBounds(honoured.SoldFrom, honoured.SoldTo, loc)

	page, pageSize := params.Page, params.PageSize
	tickets, total, err := s.repo.ListHolderTickets(ctx, repository.ListHolderTicketsQuery{
		OrganizationID: actor.OrganizationID,
		EventID:        eventID,
		OwingOnly:      honoured.OwingOnly,
		QuestionID:     honoured.QuestionID,
		// Passed through on every build: search belongs to no flag, and the
		// disclosure rule it is bounded by lives in the predicate, not here.
		Search:          honoured.Search,
		AssignmentState: honoured.AssignmentState,
		TicketTypeID:    honoured.TicketTypeID,
		Channel:         honoured.Channel,
		SoldFrom:        soldFrom,
		SoldTo:          soldTo,
		Sort:            honoured.Sort,
		Dir:             honoured.Dir,
		Limit:           pageSize,
		Offset:          (page - 1) * pageSize,
	})
	if err != nil {
		return nil, err
	}

	result := &HolderListPage{
		Data: make([]HolderTicketView, 0, len(tickets)),
		Pagination: OutstandingPagination{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages(total, pageSize),
		},
	}

	if s.ticketQuestionsEnabled {
		outstandingCount, err := s.repo.CountOutstandingAnswers(ctx, actor.OrganizationID, eventID)
		if err != nil {
			return nil, err
		}
		result.OutstandingCount = &outstandingCount
	}

	// Bucketed by Ticket, preserving the query's order — which is the order the
	// questions are ASKED in, so the list reads the way the form does. Only
	// read at all while the questions feature is open; a dark feature owes
	// nobody anything.
	byTicket := map[string][]OutstandingQuestionView{}
	if s.ticketQuestionsEnabled && len(tickets) > 0 {
		ticketIDs := make([]string, 0, len(tickets))
		for _, ticket := range tickets {
			ticketIDs = append(ticketIDs, ticket.ID)
		}
		if byTicket, err = s.outstandingQuestionsByTicket(ctx, actor.OrganizationID, eventID, ticketIDs); err != nil {
			return nil, err
		}
	}

	for _, ticket := range tickets {
		view := HolderTicketView{
			TicketID:          ticket.ID,
			Ordinal:           ticket.Ordinal,
			TicketTypeID:      ticket.TicketTypeID,
			TicketTypeName:    ticket.TicketTypeName,
			TicketSaleID:      ticket.TicketSaleID,
			ConfirmationRef:   ticket.ConfirmationRef,
			Channel:           ticket.Channel,
			CustomerFirstName: ticket.CustomerFirstName,
			CustomerLastName:  ticket.CustomerLastName,
			CustomerEmail:     ticket.CustomerEmail,
			CustomerID:        ticket.CustomerID,
			SoldAt:            ticket.SoldAt,
		}
		if s.ticketQuestionsEnabled {
			outstanding := outstandingOrEmpty(byTicket[ticket.ID])
			view.Outstanding = &outstanding
		}
		s.fillHolderListEntry(&view, ticket)
		result.Data = append(result.Data, view)
	}
	return result, nil
}

// honourHolderFilters answers which of a request's filters this build actually
// APPLIES, by returning the parameters with every filter belonging to a dark
// feature blanked out.
//
// IT IS THE ONE PLACE THAT DECISION IS MADE, and it is a function rather than a
// paragraph inline because it has TWO callers now (#529): the Holder List read
// above, and the Holder Export, whose Info sheet and audit line must describe
// ONLY THE FILTERS ACTUALLY HONOURED. A second copy of these four rules would be
// a file that claims a narrowing the query never performed — see below.
//
// A FILTER BELONGING TO A DARK FEATURE IS SILENTLY DROPPED HERE, NEVER
// REFUSED — and this is the general rule for every such filter on this
// list, stated once in this paragraph because #525's named-question filter
// and anything after it must follow it rather than re-decide it (ADR 0065,
// "a dark feature's filter: ignored, chosen; refused with a 400" —
// rejected).
//
// THREE REASONS, IN THE ORDER THEY MATTER:
//
//   - A REFUSAL WOULD TURN A STALE BOOKMARK INTO AN ERROR PAGE. The view
//     lives in the URL (#522), so a link shared before a flag closed — or
//     one pasted from a deployment where it is open — must keep working. It
//     comes back WIDER than it was, which the filter bar shows by drawing
//     that control empty or not at all.
//
//   - A REFUSAL WOULD FORCE THE CLIENT TO KNOW THE FLAG, which is precisely
//     what ADR 0045 exists to prevent: the staff app holds no copy of a
//     deployment flag and decides which controls exist from what the payload
//     CONTAINS. A 400 would make it either hold a second copy of the flag or
//     show a control that errors.
//
//   - AND A FILTERED READ MUST NOT BE A SIDE CHANNEL. A build with the
//     feature dark has to answer exactly as a build without the feature
//     would; a refusal naming `assignment_state` would admit the parameter
//     exists, which is a tell about unshipped work.
//
// The cost is stated and accepted: the reader sees more rows than they
// asked for. On a READ that is the right way round — a roster wider than
// intended is visibly wide, while a 400 hides the roster entirely.
//
// AND IT IS WHY THIS FUNCTION RETURNS ITS ANSWER RATHER THAN APPLYING IT. On the
// HOLDER EXPORT the cost above is NOT acceptable on its own: a file whose Info
// sheet claims "only Tickets whose holder has accepted" over a complete roster
// would have somebody reading every attendee of the Event believing they were
// reading a filtered few, and acting on the difference. The export therefore
// describes ITS RETURN VALUE and never the request — the words on the cover sheet
// and the fields in the audit line are built from what this function honoured,
// so a dropped filter is a filter the file has never heard of.
//
// OWING and the NAMED QUESTION belong to Ticket Questions, and ASSIGNMENT
// STATE to Ticket Assignment; the two flags are separate, so each filter is
// closed by its own. The three structural filters need no such treatment — a Ticket Type,
// a Sales Channel and a sale date exist on every build. NEITHER DOES SEARCH
// (#526): a buyer's name, a buyer's address and a Sale Confirmation
// reference are on every roster, and the one part of it that belongs to
// Ticket Assignment — an accepted Holder's name and address — is bounded by
// the predicate rather than by a flag, so on a build with assignment dark
// that branch matches nothing because nobody has ever accepted.
func (s *Service) honourHolderFilters(params ListHolderListParams) ListHolderListParams {
	honoured := params
	honoured.OwingOnly = params.OwingOnly && s.ticketQuestionsEnabled
	// The named-question filter (#525) is Ticket Questions' too, and is dropped
	// HERE, on the rule stated above and not on an argument of its own: with the
	// feature dark there are no questions to name, the control does not exist,
	// and a URL carrying `question_id` comes back as the whole roster with a
	// 200. Everything the three bullets above say about `assignment_state`
	// applies to it word for word.
	if !s.ticketQuestionsEnabled {
		honoured.QuestionID = ""
	}
	if !s.ticketAssignmentEnabled {
		honoured.AssignmentState = ""
	}
	// THE `owes` SORT IS TICKET QUESTIONS' TOO, and is dropped here on the rule
	// stated above rather than on an argument of its own (#527). It is offered
	// only where the questions side of the list exists at all, exactly as the
	// Owes column it orders on is: with the feature dark there are no debts to
	// rank, the control does not exist, and a URL carrying `sort=owes` comes
	// back as the roster in the DEFAULT ORDER with a 200 — ignored, never
	// refused. Everything the three bullets above say applies to it word for
	// word.
	//
	// THE DIRECTION GOES WITH IT, and that is the part worth stating. Dropping
	// the field alone would leave `sort=owes&dir=desc` reading as the default
	// FIELD in the reader's direction — newest sale first, which is neither
	// what they asked for nor the order this list defaults to. Half a sort is
	// not a sort, on this side of the wire as much as in the URL.
	//
	// THE OTHER FOUR SORTS BELONG TO NO FLAG, for the reason search does not
	// (#526): a sale's date, a buyer, a Ticket Type and an accepted Holder's
	// name exist on every build. The `holder` sort simply finds every row blank
	// on a build with Ticket Assignment closed, and its blanks-last rule puts
	// them where they already were.
	if params.Sort == holderSortOwes && !s.ticketQuestionsEnabled {
		honoured.Sort, honoured.Dir = "", ""
	}
	return honoured
}

// holderListAvailable is the Holder List's own gate: the read is available when
// EITHER Ticket Assignment or Ticket Questions is open (#333), and the Event
// must be the acting Organization's.
//
// IT RETURNS THE EVENT IT HAD TO READ ANYWAY (#523). The date filters must be
// interpreted in the Event's timezone, and the gate has the Event in hand — so
// the caller takes it from here rather than asking for the same row twice. The
// gate is still the gate: an unauthorised or unknown Event never gets past this
// function, whatever the caller wanted the row for.
//
// The flag check comes FIRST, before the Event is looked at, for the reason
// ticketAnswersAvailable gives: a request while both features are dark must not
// be distinguishable from one against a build that never had them — same
// status, same code, and no read whose timing could differ between a real Event
// and an invented one (ADR 0045). The code stays TICKET_QUESTIONS_UNAVAILABLE,
// which is what this route has answered since #313; a dark build changing its
// refusal would itself be a tell.
func (s *Service) holderListAvailable(
	ctx context.Context, actor ActorContext, eventID string,
) (*repository.Event, error) {
	if !s.ticketQuestionsEnabled && !s.ticketAssignmentEnabled {
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

// resolveEventLocation resolves an Event timezone name to a *time.Location,
// defaulting to UTC when the timezone is unset or unrecognised.
//
// A THIRD COPY, DELIBERATELY (#523). The same three lines live in
// sales/service/service.go and in affiliates/service/trends.go, both unexported,
// and the affiliates one already states the precedent this follows: restated
// rather than imported so the modules stay uncoupled over one line of policy.
// Promoting it to a shared package would make the catalog module depend on the
// sales module — or invent a fourth package for a fallback — to save six lines,
// and would put a change to one surface's timezone handling in the blast radius
// of all three. What matters is that the ANSWER agrees, and it does: unset or
// unrecognised means UTC everywhere, which is the property the tests assert.
func resolveEventLocation(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}

// dateRangeBounds turns "YYYY-MM-DD" sold-at bounds into a half-open absolute
// interval [from, to) read in the Event's zone: `from` is local midnight of the
// start day (inclusive) and `to` is local midnight of the day AFTER the end day
// (exclusive), so the END DAY IS INCLUDED WHOLE. Either bound may be blank,
// yielding a nil (open) end.
//
// THE HALF-OPEN SHAPE IS THE POINT, and it is copied from the Sales list's
// helper of the same name rather than reinvented. "Sold to the 3rd" means
// through the last instant of the 3rd, and the alternative — `<= end day
// 23:59:59` — has to choose a precision and is wrong for every sale recorded in
// the second it excludes. AddDate also gets the awkward cases right by
// construction: a day that is 23 or 25 hours long because the zone changed
// offset overnight still ends exactly when the next one begins.
//
// AN UNPARSEABLE VALUE YIELDS NIL — an OPEN bound, which is the unfiltered
// answer and not a silently empty one. The handler has already discarded
// anything malformed (see holderDateParam), so this is belt and braces; it is
// stated here because a future caller passing raw input must not be able to
// turn a typo into a roster with nobody on it.
func dateRangeBounds(from, to string, loc *time.Location) (*time.Time, *time.Time) {
	var fromT, toT *time.Time
	if d, err := time.ParseInLocation("2006-01-02", from, loc); from != "" && err == nil {
		start := d
		fromT = &start
	}
	if d, err := time.ParseInLocation("2006-01-02", to, loc); to != "" && err == nil {
		end := d.AddDate(0, 0, 1)
		toT = &end
	}
	return fromT, toT
}

// fillHolderListEntry puts one Ticket's assignment onto the Organization's row,
// or leaves the row exactly as it was while the flag is closed (#329, ADR 0047).
//
// IT DECIDES NOTHING. What a Ticket discloses about its Holder — the state, the
// never-accepted presentation (#334), and the name and address only once
// accepted — is catalog.DiscloseHolder, the one statement of ADR 0047's rule,
// shared with every other Organization-facing read of a Holder (#637). This
// only adapts the repository's NULL-able row to that rule's plain input and
// copies its answer onto the view. The early return is the flag's whole effect
// here: every field it would set is `omitempty`, so a closed build sends the
// bytes a build without the feature sends (ADR 0045). DiscloseHolder answers
// the same for a closed flag; the guard keeps the row literally untouched.
func (s *Service) fillHolderListEntry(view *HolderTicketView, ticket repository.HolderTicket) {
	if !s.ticketAssignmentEnabled {
		return
	}
	disclosure := catalog.DiscloseHolder(s.ticketAssignmentEnabled, catalog.HolderAssignment{
		HolderEmail:           ticket.HolderEmail.String,
		HolderFirstName:       ticket.HolderFirstName.String,
		HolderLastName:        ticket.HolderLastName.String,
		HolderCustomerID:      ticket.HolderCustomerID.String,
		AssignedAt:            nullTimeOrNil(ticket.AssignedAt),
		AcceptedAt:            nullTimeOrNil(ticket.AcceptedAt),
		HolderAddressPurgedAt: nullTimeOrNil(ticket.HolderAddressPurgedAt),
	})
	view.AssignmentState = string(disclosure.State)
	view.NeverAccepted = disclosure.NeverAccepted
	view.HolderFirstName = disclosure.HolderFirstName
	view.HolderLastName = disclosure.HolderLastName
	view.HolderEmail = disclosure.HolderEmail
	view.HolderCustomerID = disclosure.HolderCustomerID
}

// outstandingQuestionsByTicket names which required questions each of the given
// Tickets owes, bucketed by Ticket in the order the questions are asked. The
// one reading of the debt for a set of Tickets, shared by the Holder List and
// the Customer Dossier (#641), so the two can never disagree about a Ticket.
func (s *Service) outstandingQuestionsByTicket(
	ctx context.Context, organizationID, eventID string, ticketIDs []string,
) (map[string][]OutstandingQuestionView, error) {
	questions, err := s.repo.ListOutstandingQuestionsForTickets(ctx, organizationID, eventID, ticketIDs)
	if err != nil {
		return nil, err
	}
	byTicket := make(map[string][]OutstandingQuestionView)
	for _, question := range questions {
		byTicket[question.TicketID] = append(byTicket[question.TicketID], OutstandingQuestionView{
			QuestionID: question.QuestionID,
			Label:      question.Label,
			Kind:       question.Kind,
			SortOrder:  question.SortOrder,
		})
	}
	return byTicket, nil
}

// outstandingOrEmpty keeps the field an ARRAY on the wire rather than null. A
// typed reader that has to check for null before iterating is a reader that will
// one day forget, and there is no meaning here for null that empty does not
// already carry — a Ticket owing nothing simply owes nothing.
func outstandingOrEmpty(questions []OutstandingQuestionView) []OutstandingQuestionView {
	if questions == nil {
		return []OutstandingQuestionView{}
	}
	return questions
}

// totalPages is the page count for a total, and 0 for an empty list — never 1,
// because "page 1 of 1" over nothing invites a reader to go looking for a page
// that has nothing on it.
func totalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}
