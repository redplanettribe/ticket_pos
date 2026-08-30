package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/exportfile"
)

// The HOLDER EXPORT (#529, parent #518, ADR 0065): the Holder List, as the
// reader is looking at it, handed over as an .xlsx of who is coming.
//
// IT IS THE SAME QUERY THE SCREEN RAN. The rows come from
// repository.ListHolderTickets with the same filters, the same sort and the same
// honoured-filter treatment as ListHolderList — literally the same call, with the
// page replaced by the cap — because "the file mirrors the view" is a promise
// only one code path can keep. A second query built for the file could be built
// differently, and the day it was, a person would forward a spreadsheet that
// disagreed with the screen they took it from and neither of them would say so.
//
// IT KNOWS NOTHING ABOUT DISCLOSURE. Which Holder a row may name is
// fillHolderListEntry's decision, made once for the screen and the file both, and
// this file routes every row through it rather than reading the repository's
// columns itself. See buildHolderExportRows.
//
// AND IT CARRIES NO MONEY. Not an amount, not Net Proceeds, not a currency — see
// the exportfile package's holders.go, where that ruling is argued at length. The
// Sales Export is the money and this is the people; two files that could be
// mistaken for one another would be worse than either.

// defaultHolderExportRowCap is how many Tickets one Holder Export may carry.
//
// IT IS ITS OWN CONSTANT WITH ITS OWN REASON, and referencing the Sales Export's
// defaultExportRowCap instead is the obvious wrong move (ADR 0065). That number
// is importfile.MaxRows — the Sale Import's row limit — and its reason is
// SYMMETRY: an export of sales must never hand back more rows than the importer
// would take back, so the system has one answer to how many sale rows travel in a
// file. NOTHING ABOUT THAT TRANSFERS HERE. There is no Holder Import; nobody
// uploads this file anywhere; a roster row is not a sale row and one Ticket Sale
// of forty tickets is one row there and forty here, so even the units differ.
//
// WHAT THIS NUMBER IS is a SYNCHRONOUS GENERATION CEILING. Generation happens
// inside the request and the workbook is buffered whole in memory, so an
// unbounded Event produces a request that hangs and then either times out at the
// proxy or takes the process down — on the busiest day of the Event, which is
// exactly when somebody reaches for this.
//
// IT IS A JUDGEMENT AND NOT A MEASUREMENT (ADR 0065's own consequence). It is
// contingent on a file of this size with the full Option column set generating
// inside the request timeout — Option columns make the sheet WIDE, and width
// costs more than height. If that benchmark does not hold, this number comes
// down, and it comes down here without touching the Sales Export.
const defaultHolderExportRowCap = 50_000

// WithHolderExportRowCap narrows how many Tickets a Holder Export may carry.
//
// It exists so a test can prove the cap is a cap. Reaching the deployed fifty
// thousand would mean seeding fifty thousand and one Tickets, which takes minutes
// and buys nothing: what has to hold is the behaviour AT the bound — the refusal,
// the count it names, and that exactly the cap still succeeds — and none of that
// is a property of the number. A value of zero or less keeps the default, so a
// misapplied override can never quietly mean "export nothing".
//
// Nothing in production calls it; the deployed cap is defaultHolderExportRowCap.
func (s *Service) WithHolderExportRowCap(rows int) *Service {
	if rows > 0 {
		s.holderExportRowCap = rows
	}
	return s
}

// HolderExportRowCap reports the Holder Export row cap currently in force.
func (s *Service) HolderExportRowCap() int {
	if s.holderExportRowCap > 0 {
		return s.holderExportRowCap
	}
	return defaultHolderExportRowCap
}

// WithLogger overrides where this service's audit lines go (tests).
//
// It exists for the Holder Export's audit line, which is the one thing on this
// service that a test must be able to READ BACK: "an audit line is written per
// generated file" is an acceptance criterion, and the only seam through which it
// can be asserted is the logger. The sales service carries the same override for
// the same reason.
func (s *Service) WithLogger(logger platform.Logger) *Service {
	s.logger = logger
	return s
}

// HolderExport is a built Holder Export: the .xlsx bytes and the filename the
// download carries. The filename is decided here rather than at the HTTP edge so
// there is one answer to what an exported file is called.
type HolderExport struct {
	Data     []byte
	Filename string
}

// ExportHolderList builds the Event's Holder List into an .xlsx, narrowed and
// ordered by exactly the filters the list was showing.
//
// It takes ListHolderListParams and honours every filter on it that this build
// honours on the screen, ignoring only Page and PageSize: pagination is a
// property of a screen, and a file that stopped at row 50 would be a quietly
// wrong answer. The caller's role is gated at the route — Org Admin and Event
// Owner, never Event Staff, the same gate the Holder List read and the Sales
// Export carry — because this file is the platform's densest concentration of
// attendee personal data and it is forwarded and kept.
//
// Above the row cap it builds nothing and returns field errors instead, which the
// handler writes as the standard VALIDATION_FAILED envelope. The answer is not
// "this failed" but "narrow your filters", and the filters are on screen beside
// the button; the refusal names how many Tickets matched, because that is how the
// person knows how much narrower to go.
func (s *Service) ExportHolderList(
	ctx context.Context,
	actor ActorContext,
	eventID string,
	params ListHolderListParams,
) (*HolderExport, []platform.FieldError, error) {
	event, err := s.holderListAvailable(ctx, actor, eventID)
	if err != nil {
		return nil, nil, err
	}

	// THE HONOURED FILTERS, AND EVERYTHING DOWNSTREAM READS THESE AND NOT
	// `params`. The query is built from them, the Info sheet's words are built
	// from them, and the audit line's fields are built from them — so a filter
	// this build ignored is a filter the file has never heard of, and cannot claim
	// to have applied. That is the whole mechanism; see honourHolderFilters.
	honoured := s.honourHolderFilters(params)

	loc := resolveEventLocation(event.Timezone.String)
	soldFrom, soldTo := dateRangeBounds(honoured.SoldFrom, honoured.SoldTo, loc)

	// ONE ROW PAST THE CAP IS ALL THAT IS EVER READ. The total the roster query
	// reports is its own COUNT(*), computed from the same WHERE and BEFORE the
	// LIMIT, so the true matched count is exact however far over the cap the Event
	// is — the refusal can name it without a second query, and an Event with two
	// hundred thousand Tickets never pulls two hundred thousand rows into this
	// process to be told so.
	rowCap := s.HolderExportRowCap()
	tickets, total, err := s.repo.ListHolderTickets(ctx, repository.ListHolderTicketsQuery{
		OrganizationID:  actor.OrganizationID,
		EventID:         eventID,
		OwingOnly:       honoured.OwingOnly,
		QuestionID:      honoured.QuestionID,
		Search:          honoured.Search,
		AssignmentState: honoured.AssignmentState,
		TicketTypeID:    honoured.TicketTypeID,
		Channel:         honoured.Channel,
		SoldFrom:        soldFrom,
		SoldTo:          soldTo,
		Sort:            honoured.Sort,
		Dir:             honoured.Dir,
		Limit:           rowCap + 1,
	})
	if err != nil {
		return nil, nil, err
	}
	if total > rowCap {
		// REFUSED, NEVER TRUNCATED. A file silently missing its last ten thousand
		// people is the one outcome nobody can detect from the file itself.
		return nil, []platform.FieldError{holderExportTooManyRows(total, rowCap)}, nil
	}

	// The Event's Ticket Questions, as the file's columns — the EVENT's, not the
	// ones the exported Tickets happen to have answered, which is what keeps the
	// shape of the sheet stable under the filters. Read only while the feature is
	// open: a dark feature owes nobody a column (ADR 0045).
	var questions []repository.EventTicketQuestion
	if s.ticketQuestionsEnabled {
		questions, err = s.repo.ListEventTicketQuestions(ctx, actor.OrganizationID, eventID)
		if err != nil {
			return nil, nil, err
		}
	}
	columns, fansOut, optionLabels := holderExportQuestionColumns(questions)

	// What those Tickets have ANSWERED. The screen reads what each still OWES,
	// which is the opposite half of the same fact and not a substitute for it: a
	// file of blanks where the answers should be would be the roster without its
	// point. Skipped entirely when nothing is asked, so an Event with no questions
	// makes no query at all.
	answersByTicket := map[string][]repository.TicketAnswer{}
	if len(questions) > 0 && len(tickets) > 0 {
		ticketIDs := make([]string, 0, len(tickets))
		for _, ticket := range tickets {
			ticketIDs = append(ticketIDs, ticket.ID)
		}
		answers, err := s.repo.ListTicketAnswers(ctx, ticketIDs)
		if err != nil {
			return nil, nil, err
		}
		for _, answer := range answers {
			answersByTicket[answer.TicketID] = append(answersByTicket[answer.TicketID], answer)
		}
	}

	rows := s.buildHolderExportRows(tickets, answersByTicket, fansOut, optionLabels)

	// Whether a free-text search was applied, computed once and read twice: the
	// Info sheet states it and the audit line records it, and both must say THAT
	// one happened without ever repeating what it was.
	searched := strings.TrimSpace(honoured.Search) != ""

	generatedAt := s.now()
	info := exportfile.HolderInfo{
		EventName:   event.Name,
		GeneratedAt: generatedAt,
		Filters: exportfile.HolderFilters{
			OwingOnly: honoured.OwingOnly,
			// BY NAME AND BY WORDING, resolved against what was just read. The
			// reader never saw an id, and an id that names nothing — a stale
			// bookmark's, another Event's — says nothing rather than being printed
			// at them.
			QuestionLabel:   holderExportQuestionLabel(questions, honoured.QuestionID),
			AssignmentState: honoured.AssignmentState,
			TicketTypeName:  s.holderExportTicketTypeName(ctx, actor.OrganizationID, eventID, honoured.TicketTypeID),
			Channel:         honoured.Channel,
			SoldFrom:        honoured.SoldFrom,
			SoldTo:          honoured.SoldTo,
			Sort:            honoured.Sort,
			Dir:             honoured.Dir,
			// THAT a search happened, never what it was: the term is routinely a
			// named person's address, and this file is forwarded. Same call the
			// audit line makes, from the same value.
			Searched: searched,
		},
	}

	data, err := exportfile.BuildHolderExport(exportfile.HolderRoster{
		Questions: columns,
		// THE FLAG AND NOT THE DATA decides whether the file carries the Holder
		// block, exactly as it decides whether the Sales Export's per-Ticket sheet
		// carries its four (ADR 0045). With assignment closed the columns are
		// absent on every Event, not merely empty.
		Assignment: s.ticketAssignmentEnabled,
		Tickets:    rows,
	}, loc, info)
	if err != nil {
		return nil, nil, err
	}

	// THE ONE RECORD THAT A COPY OF THIS EVENT'S ATTENDEES LEFT THE BUILDING.
	//
	// There is no audit table behind it — that implies a reading surface, a
	// retention policy and an access rule, and should be designed once across the
	// platform's personal-data reads rather than growing out of this feature. So
	// this line is the whole answer to "who pulled the guest list", and it is not
	// a question that can be answered retroactively: it is written here or it is
	// never written.
	//
	// It is logged AFTER the workbook exists, so the line claims a file that was
	// actually handed over rather than one whose build then failed. A refusal over
	// the cap returns above and logs nothing, because no file was taken.
	//
	// THE FILTERS ARE THE HONOURED ONES, for the reason the Info sheet's are: a
	// line naming `assignment_state=accepted` beside a row count that is the whole
	// Event would be a false record of what was taken, and an audit line that can
	// be wrong is worse than none.
	//
	// THE FREE-TEXT SEARCH IS A BOOLEAN AND NEVER ITS VALUE. It matches a buyer's
	// name and email address, the Sale Confirmation reference and an accepted
	// Holder's name and address, so a support lookup for one attendee puts that
	// attendee's address into the filter — and a log aggregator typically has
	// broader access and longer retention than the database it would be copied out
	// of. The structural filters below say what was asked for without saying
	// anything about any one person, which is exactly the line the Info sheet draws
	// in the file itself.
	s.logger.Info("holder export generated",
		"member_id", actor.MemberID,
		"organization_id", actor.OrganizationID,
		"event_id", eventID,
		"row_count", len(rows),
		"ticket_type_id", honoured.TicketTypeID,
		"channel", honoured.Channel,
		"assignment_state", honoured.AssignmentState,
		"sold_from", honoured.SoldFrom,
		"sold_to", honoured.SoldTo,
		"outstanding", honoured.OwingOnly,
		"question_id", honoured.QuestionID,
		"sort", honoured.Sort,
		"dir", honoured.Dir,
		"search", searched,
	)

	return &HolderExport{
		Data:     data,
		Filename: holderExportFilename(event.Slug, generatedAt.In(loc)),
	}, nil, nil
}

// buildHolderExportRows turns the roster's rows into the file's rows.
//
// EVERY ROW GOES THROUGH fillHolderListEntry, WHICH IS THE POINT OF THIS
// FUNCTION. The rule about what an Organization may see of a Holder — nothing at
// all before that Holder has accepted (ADR 0047) — is decided once, in that
// method, for the Holder List screen. This builds the same HolderTicketView the
// screen is drawn from and reads the file's columns off it, so the file cannot
// disclose one byte more than the screen does and a change to the rule is a
// change to both. Reading ticket.HolderEmail here instead would be a second copy
// of ADR 0047, and the copy nobody updates.
//
// It also means a PURGED Ticket reads exactly as it does on the screen —
// `assigned` with never_accepted beside it — rather than acquiring a fourth word
// nobody else uses.
func (s *Service) buildHolderExportRows(
	tickets []repository.HolderTicket,
	answersByTicket map[string][]repository.TicketAnswer,
	fansOut map[string]bool,
	optionLabels map[string]string,
) []exportfile.HolderRow {
	rows := make([]exportfile.HolderRow, 0, len(tickets))
	for _, ticket := range tickets {
		view := HolderTicketView{}
		s.fillHolderListEntry(&view, ticket)

		row := exportfile.HolderRow{
			ConfirmationRef: ticket.ConfirmationRef,
			SoldAt:          ticket.SoldAt,
			Channel:         ticket.Channel,
			TicketTypeName:  ticket.TicketTypeName,
			Ordinal:         ticket.Ordinal,
			// The buyer, straight off the row: they are the party of record for
			// the Sale and there is no disclosure question about them — this is
			// the Organization's own customer, already on the Sales list and in
			// the Sales Export.
			CustomerFirstName: ticket.CustomerFirstName,
			CustomerLastName:  ticket.CustomerLastName,
			CustomerEmail:     ticket.CustomerEmail,
			// The Holder, taken from the VIEW and never from the repository row.
			AssignmentState: view.AssignmentState,
			NeverAccepted:   view.NeverAccepted,
			HolderFirstName: view.HolderFirstName,
			HolderLastName:  view.HolderLastName,
			HolderEmail:     view.HolderEmail,
		}

		if answers := answersByTicket[ticket.ID]; len(answers) > 0 {
			row.Answers = make(map[string]exportfile.Answer, len(answers))
			for _, answer := range answers {
				row.Answers[answer.TicketQuestionID] = holderExportAnswer(
					answer, fansOut[answer.TicketQuestionID], optionLabels)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// holderExportQuestionColumns turns the Event's questions into the builder's
// column input, plus the two lookups a row needs to shape its Answers: which
// questions FAN OUT to one column per Option, and what every Option is currently
// called.
//
// NOTHING HERE DECIDES WHICH COLUMNS EXIST — exportfile.BuildQuestionColumns
// does, for this file and the Sales Export's per-Ticket sheet identically (#520).
// This only says which questions are multiple-choice, which is a fact about the
// question and not about a sheet.
//
// ONLY multi_choice FANS OUT. single_choice takes exactly one Option, so it fits
// in one cell, and spreading it over a TRUE/FALSE column per Option would cost a
// reader width to say what one word says. The Option labels are needed even for
// the questions that do not fan out, because a single_choice cell says what its
// chosen Option is called now.
func holderExportQuestionColumns(
	questions []repository.EventTicketQuestion,
) (columns []exportfile.QuestionColumn, fansOut map[string]bool, optionLabels map[string]string) {
	columns = make([]exportfile.QuestionColumn, 0, len(questions))
	fansOut = make(map[string]bool, len(questions))
	optionLabels = map[string]string{}
	for _, q := range questions {
		column := exportfile.QuestionColumn{ID: q.ID, Label: q.Label}
		for _, option := range q.Options {
			optionLabels[option.ID] = option.Label
		}
		if catalog.TicketQuestionKind(q.Kind) == catalog.TicketQuestionKindMultiChoice {
			fansOut[q.ID] = true
			for _, option := range q.Options {
				column.Options = append(column.Options, exportfile.OptionColumn{
					ID:    option.ID,
					Label: option.Label,
				})
			}
		}
		columns = append(columns, column)
	}
	return columns, fansOut, optionLabels
}

// holderExportAnswer turns one stored Answer into the shape the file writes.
//
// IT IS THE SIBLING OF sales/service.exportedAnswer AND NOT A DUPLICATE OF IT.
// The two read DIFFERENT REPOSITORY TYPES — this one the catalog's TicketAnswer,
// with its sql.Null columns and its Options carrying both the snapshot and the
// current label — and converting a stored row into a cell value is a fact about
// the row's own module. What must not be stated twice is which COLUMNS a question
// takes and where a Ticket's cells land among them, and that is not here: it is
// exportfile.BuildQuestionColumns, called by both files.
func holderExportAnswer(
	answer repository.TicketAnswer, fansOut bool, optionLabels map[string]string,
) exportfile.Answer {
	// A multiple-choice Answer is its chosen Options, BY IDENTITY: the file writes
	// TRUE under each of them and FALSE under the rest of the question's Options,
	// and no label is involved on this path at all — which is what makes a renamed
	// Option move a heading rather than fork a column.
	if fansOut {
		chosen := make([]string, 0, len(answer.Options))
		for _, option := range answer.Options {
			chosen = append(chosen, option.TicketQuestionOptionID)
		}
		return exportfile.Answer{Chosen: chosen}
	}

	// A single_choice Answer: one Option, written as the words it CURRENTLY reads
	// rather than the snapshot the Answer keeps of what its chooser read. The file
	// must not report wording that contradicts the screen it was downloaded from;
	// the snapshot answers a different question ("what did they actually agree
	// to"), and that one is answered on the Ticket.
	if len(answer.Options) > 0 {
		if label, ok := optionLabels[answer.Options[0].TicketQuestionOptionID]; ok {
			return exportfile.Answer{Text: &label}
		}
		return exportfile.Answer{}
	}

	out := exportfile.Answer{}
	if answer.Text.Valid {
		text := answer.Text.String
		out.Text = &text
	}
	if answer.Checked.Valid {
		checked := answer.Checked.Bool
		out.Checked = &checked
	}
	// The number arrives as the text the NUMERIC column holds, and becomes a real
	// number for the cell. A value too wide for a float64 is left BLANK rather than
	// written rounded: a cell that quietly disagrees with what somebody typed is
	// worse than one a reader can see is missing. Nothing catalog accepts can reach
	// here, so this is a guard rather than a case.
	if answer.Number.Valid {
		if parsed, err := strconv.ParseFloat(answer.Number.String, 64); err == nil {
			out.Number = &parsed
		}
	}
	// The date arrives as "YYYY-MM-DD" text out of a DATE column — read as text on
	// purpose, so no zone is ever attached to a calendar date — and is parsed back
	// to a bare date here. exportfile writes it at midnight UTC so a birthday
	// cannot come back a day early.
	if answer.Date.Valid {
		if parsed, err := time.Parse("2006-01-02", answer.Date.String); err == nil {
			out.Date = &parsed
		}
	}
	return out
}

// holderExportQuestionLabel resolves the named-question filter to the wording the
// Info sheet prints, or "" when the id names no question of this Event.
//
// A blank is the right answer for an id that names nothing: the filter narrowed
// the roster to nobody, the sheet says nothing about a question the reader cannot
// see, and printing a UUID at them would be worse than silence.
func holderExportQuestionLabel(questions []repository.EventTicketQuestion, questionID string) string {
	if questionID == "" {
		return ""
	}
	for _, q := range questions {
		if q.ID == questionID {
			return q.Label
		}
	}
	return ""
}

// holderExportTicketTypeName resolves the Ticket Type filter to the name the Info
// sheet prints, or "" when nothing was filtered or the id names no Ticket Type of
// this Event.
//
// IT COSTS A QUERY ONLY WHEN THE FILTER IS SET, which is why it is a method and
// not a lookup over a list read unconditionally: unlike the Sales Export, this
// file has no per-Ticket-Type columns and therefore no other reason to read the
// catalog at all. An unfiltered download should not pay for a read whose only
// output would be a line the sheet does not print.
//
// A FAILED READ IS NOT A FAILED EXPORT. The name is a sentence on a cover sheet;
// losing it costs the reader one line and losing the file costs them the roster.
func (s *Service) holderExportTicketTypeName(ctx context.Context, orgID, eventID, ticketTypeID string) string {
	if ticketTypeID == "" {
		return ""
	}
	types, err := s.repo.ListTicketTypesByEventID(ctx, orgID, eventID)
	if err != nil {
		return ""
	}
	for _, tt := range types {
		if tt.ID == ticketTypeID {
			return tt.Name
		}
	}
	return ""
}

// holderExportTooManyRows is the refusal a Holder Export over the row cap
// carries.
//
// It is a FIELD ERROR rather than a domain error because of what the reader is
// meant to do next: the filters that produced this request are on screen beside
// the button that sent it, and narrowing them is the fix. The staff app renders
// the message inline there, so the message IS the feature — it names how many
// Tickets matched (which is how the person knows how much narrower to go), how
// many may travel at once, and the lever to reach for.
//
// The field named is `filters` and not any one parameter: no single filter is at
// fault, and blaming sold_from would be wrong for somebody whose lever is the
// Ticket Type or the assignment state.
//
// IT NAMES TICKETS AND NOT SALES, which is the difference between this file and
// the Sales Export in one word: a person told "1,200 matching sales" over a
// roster of 4,000 Tickets cannot work out which number the cap applies to.
func holderExportTooManyRows(matched, rowCap int) platform.FieldError {
	return platform.FieldError{
		Field: "filters",
		Code:  platform.CodeTooManyItems,
		Message: fmt.Sprintf(
			"This view matches %s tickets; up to %s can be downloaded at once. Narrow the filters and try again.",
			holderExportGroupDigits(matched), holderExportGroupDigits(rowCap),
		),
	}
}

// holderExportGroupDigits renders a count with thousands separators, because
// these numbers are read by a person deciding how much to narrow a filter and
// "24,318" is legible at a glance where "24318" is not.
//
// A third copy of six lines of formatting, restated for the reason
// resolveEventLocation is: promoting it would make the catalog module depend on
// the sales module, or invent a package, to save the lines — and what matters is
// that the ANSWER agrees, which a test pins.
func holderExportGroupDigits(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return sign + b.String()
}

// holderExportFilename names a Holder Export after its Event and the day it was
// taken — "holders-summer-fest-2026-08-29.xlsx" — so a Downloads folder holding
// several stays navigable, and so it can never be confused with the Sales
// Export's "sales-…" sitting beside it. The day is drawn in the Event's own
// timezone, like every other date in the file.
func holderExportFilename(slug string, generatedAt time.Time) string {
	return "holders-" + slug + "-" + generatedAt.Format("2006-01-02") + ".xlsx"
}
