package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/exportfile"
)

// The HOLDER EXPORT (#529, parent #518, ADR 0065): the Holder List, as the
// reader is looking at it, handed over as an .xlsx of who is coming.
//
// IT IS THE SAME QUERY THE SCREEN RAN. The rows come from the statement
// repository.ListHolderTickets pages, with the same filters, the same sort and the
// same honoured-filter treatment as ListHolderList - read to the end through a
// cursor instead of a page at a time (ADR 0075) - because "the file mirrors the
// view" is a promise only one code path can keep. A second query built for the file could be built
// differently, and the day it was, a person would forward a spreadsheet that
// disagreed with the screen they took it from and neither of them would say so.
//
// IT KNOWS NOTHING ABOUT DISCLOSURE. Which Holder a row may name is
// catalog.DiscloseHolder's decision (through fillHolderListEntry), made once for
// the screen and the file both, and this file routes every row through it rather
// than reading the repository's columns itself. See buildHolderExportRows.
//
// AND IT CARRIES NO MONEY. Not an amount, not Net Proceeds, not a currency — see
// the exportfile package's holders.go, where that ruling is argued at length. The
// Sales Export is the money and this is the people; two files that could be
// mistaken for one another would be worse than either.

// holderExportDeadline is how long one Holder Export may stream.
//
// IT IS INSIDE THE REQUEST TIMEOUT ON PURPOSE (ADR 0075). With no cap the only
// ceiling left is Cloud Run's api_request_timeout_seconds, and a request the
// platform kills there is indistinguishable, from in here, from a client that
// went away - while its snapshot holds one of the instance's database
// connections to the last second. Expiring first means an export that would
// have run past the timeout ends as an aborted download for a reason this
// process can name, and gives its connection back while it still can.
// TestTheHolderExportDeadlineIsInsideTheRequestTimeout holds the relationship.
const holderExportDeadline = 290 * time.Second

// holderExportConcurrency is how many Holder Exports one API instance streams at
// once (ADR 0075).
//
// EACH ONE HOLDS A DATABASE CONNECTION for as long as its client downloads, up
// to holderExportDeadline, and the instance has ten (platform.OpenDB). Two
// leaves eight for every other request on the instance however slow the
// downloaders' phones are, and makes a second colleague downloading at the same
// moment an ordinary case rather than a refused one. A third is refused at once,
// before anything is read, rather than queued: a queued export would hold its
// request open with no bytes flowing, and the reader would see a spinner that
// means nothing.
const holderExportConcurrency = 2

// acquireHolderExport takes one of the instance's export slots, or reports that
// none is free. The caller releases it with releaseHolderExport.
func (s *Service) acquireHolderExport() bool {
	s.holderExports.Lock()
	defer s.holderExports.Unlock()
	if s.holderExports.streaming >= holderExportConcurrency {
		return false
	}
	s.holderExports.streaming++
	return true
}

func (s *Service) releaseHolderExport() {
	s.holderExports.Lock()
	defer s.holderExports.Unlock()
	s.holderExports.streaming--
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

// WithHolderExportPause installs a function every Holder Export calls once its
// file has begun streaming, and again after each batch of rows it writes, with
// how many rows are written so far (tests).
//
// IT EXISTS BECAUSE A TEST HAS TO HOLD AN EXPORT OPEN AT A KNOWN POINT, which
// TCP buffering alone cannot promise: to commit a sale in the middle of one, or
// to have two in flight at once. The pause blocks the export exactly where it
// stands, snapshot open and response started. Nil, the default, is no pause.
//
// Nothing in production calls it; no configuration reaches it.
func (s *Service) WithHolderExportPause(pause func(ctx context.Context, rowsWritten int)) *Service {
	s.holderExportPause = pause
	return s
}

// WithHolderExportDeadline overrides how long one Holder Export may stream
// (tests). Zero, the default, is holderExportDeadline.
//
// IT EXISTS BECAUSE THE DEADLINE'S BEHAVIOUR HAS TO BE SEEN, not assumed: a
// client that stops reading must be let go when it passes, and a test cannot
// wait 290 seconds to watch that happen.
func (s *Service) WithHolderExportDeadline(d time.Duration) *Service {
	s.holderExportDeadline = d
	return s
}

// ExportHolderList streams the Event's Holder List as an .xlsx, narrowed and
// ordered by exactly the filters the list was showing (ADR 0075).
//
// It takes ListHolderListParams and honours every filter on it that this build
// honours on the screen, ignoring only Page and PageSize: pagination is a
// property of a screen, and a file that stopped at row 50 would be a quietly
// wrong answer. The caller's role is gated at the route - Org Admin and Event
// Owner, never Event Staff, the same gate the Holder List read and the Sales
// Export carry - because this file is the platform's densest concentration of
// attendee personal data and it is forwarded and kept.
//
// THERE IS NO CAP. The roster is read through a cursor inside one snapshot and
// every row is written, compressed and handed to the response as it comes, so
// nothing here holds the whole roster or the whole file, however many Tickets
// match.
//
// start IS CALLED EXACTLY ONCE, AND ONLY WHEN THE FILE IS READY TO BEGIN: the
// snapshot is open and the roster's first batch has been read. It is given the
// filename and the moment the export's deadline passes, and returns where the
// bytes go. The deadline is the caller's to put on the connection, so a write
// blocked on a client that stopped reading gives up when the export does; if
// the caller cannot do that it returns an error, and the export is refused
// before its first byte rather than streamed without one. An error returned
// before start succeeded is an ordinary refusal and the caller answers it with
// the standard envelope; an error after it means the response is already a 200
// with a file part way down the wire, and the only honest thing left is to
// abort it. The file is never finished on a failure, so a cut download is bytes
// no spreadsheet program will open - never a short, well-formed roster.
func (s *Service) ExportHolderList(
	ctx context.Context,
	actor ActorContext,
	eventID string,
	params ListHolderListParams,
	start func(filename string, deadline time.Time) (io.Writer, error),
) (err error) {
	event, err := s.holderListAvailable(ctx, actor, eventID)
	if err != nil {
		return err
	}

	// THE HONOURED FILTERS, AND EVERYTHING DOWNSTREAM READS THESE AND NOT
	// `params`. The query is built from them, the Info sheet's words are built
	// from them, and the audit line's fields are built from them — so a filter
	// this build ignored is a filter the file has never heard of, and cannot claim
	// to have applied. That is the whole mechanism; see honourHolderFilters.
	honoured := s.honourHolderFilters(params)

	loc := resolveEventLocation(event.Timezone.String)
	soldFrom, soldTo := dateRangeBounds(honoured.SoldFrom, honoured.SoldTo, loc)

	// BUSY IS DECIDED AFTER ACCESS AND BEFORE ANY READ: somebody who may not
	// have the roster is told so rather than told to wait, and a refused export
	// has touched nothing and logs nothing.
	if !s.acquireHolderExport() {
		return catalog.ErrHolderExportBusy()
	}
	defer s.releaseHolderExport()

	ctx, cancel := context.WithTimeout(ctx, s.holderExportTimeout())
	defer cancel()
	deadline, _ := ctx.Deadline()

	// ONE SNAPSHOT FOR THE WHOLE FILE. The generation time is taken as it opens,
	// and it is what the Info sheet and the filename state: the file is the
	// roster at that moment, whatever is sold or reassigned while it downloads.
	snapshot, err := s.repo.OpenHolderRosterSnapshot(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = snapshot.Close() }()
	generatedAt := s.now()

	// The Event's Ticket Questions, as the file's columns — the EVENT's, not the
	// ones the exported Tickets happen to have answered, which is what keeps the
	// shape of the sheet stable under the filters. Read only while the feature is
	// open: a dark feature owes nobody a column (ADR 0045).
	var questions []repository.EventTicketQuestion
	if s.ticketQuestionsEnabled {
		questions, err = snapshot.ListEventTicketQuestions(ctx, actor.OrganizationID, eventID)
		if err != nil {
			return err
		}
	}
	columns, fansOut, optionLabels := holderExportQuestionColumns(questions)

	// The filtered Ticket Type's name, for the Info sheet, read inside the same
	// snapshot as everything else.
	ticketTypeName, err := holderExportTicketTypeName(ctx, snapshot, actor.OrganizationID, eventID, honoured.TicketTypeID)
	if err != nil {
		return err
	}

	// IT IS THE SAME QUERY THE SCREEN RAN, with no page on it.
	if err := snapshot.OpenRoster(ctx, repository.ListHolderTicketsQuery{
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
	}); err != nil {
		return err
	}
	batch, err := snapshot.NextTickets(ctx)
	if err != nil {
		return err
	}

	// Whether a free-text search was applied, computed once and read twice: the
	// Info sheet states it and the audit line records it, and both must say THAT
	// one happened without ever repeating what it was.
	searched := strings.TrimSpace(honoured.Search) != ""

	info := exportfile.HolderInfo{
		EventName:   event.Name,
		GeneratedAt: generatedAt,
		Filters: exportfile.HolderFilters{
			OwingOnly: honoured.OwingOnly,
			// BY NAME AND BY WORDING WHERE THEY CAN BE RESOLVED, AND BY ID WHERE
			// THEY CANNOT. The reader never saw an id, so the label is what the
			// sheet prints - but a stale bookmark's id, or another Event's, names
			// nothing here and resolves to no label at all.
			//
			// THE ID TRAVELS ALONGSIDE, ALWAYS, whenever the filter was honoured.
			// Passing only the label meant a blank label became a SILENT filter:
			// the roster really was narrowed, and the Info sheet said "No filters
			// were applied: this is every Ticket… on the Event." That is #529's
			// own failure mode inverted, and worse in this direction — a reader
			// takes a filtered roster for a whole one and concludes people are
			// missing from the Event. The builder prints the name when it has one
			// and the id when it does not; see exportfile.HolderFilters.
			//
			// A DROPPED filter still passes NEITHER, because `honoured` is blank
			// for it — which is the mechanism, untouched.
			QuestionLabel:   holderExportQuestionLabel(questions, honoured.QuestionID),
			QuestionID:      honoured.QuestionID,
			AssignmentState: honoured.AssignmentState,
			TicketTypeName:  ticketTypeName,
			TicketTypeID:    honoured.TicketTypeID,
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

	// THE RESPONSE BEGINS. Everything above could still be refused with an
	// envelope, and so can a caller that cannot hold the connection to the
	// deadline. The headers carry no personal data; the rows do, and none is
	// written before the started line below.
	out, err := start(holderExportFilename(event.Slug, generatedAt.In(loc)), deadline)
	if err != nil {
		return err
	}
	// The sink remembers whether a write to the response failed, which is how a
	// client that went away is told apart from a database that did.
	sink := &holderExportSink{w: out}

	// THE ONE RECORD THAT A COPY OF THIS EVENT'S ATTENDEES LEFT THE BUILDING,
	// now in two lines (ADR 0075).
	//
	// There is no audit table behind them - that implies a reading surface, a
	// retention policy and an access rule, and should be designed once across the
	// platform's personal-data reads rather than growing out of this feature. So
	// these lines are the whole answer to "who pulled the guest list", and it is
	// not a question that can be answered retroactively.
	//
	// "STARTED" IS WRITTEN BEFORE THE FIRST ROW, because personal data leaves
	// with the first chunk and not with the last, and it is the only line that
	// survives every way a stream can die - a deploy, a scale-down, the process
	// killed under it. A request refused before this point writes neither line,
	// because nothing was taken. "FINISHED" says how it ended and how many rows
	// were produced, and is written on every way out of here from this point on,
	// a panic included; see logHolderExportFinished.
	//
	// THE REQUEST ID IS ON BOTH, as the key that joins them to each other and to
	// the request line: two colleagues downloading the same Event at once are two
	// started lines that differ in nothing else.
	//
	// THE FILTERS ARE THE HONOURED ONES, for the reason the Info sheet's are: a
	// line naming `assignment_state=accepted` beside a roster that is the whole
	// Event would be a false record of what was taken, and an audit line that can
	// be wrong is worse than none.
	//
	// THE FREE-TEXT SEARCH IS A BOOLEAN AND NEVER ITS VALUE, on either line. It
	// matches a buyer's name and email address, the Sale Confirmation reference
	// and an accepted Holder's name and address, so a support lookup for one
	// attendee puts that attendee's address into the filter - and a log
	// aggregator typically has broader access and longer retention than the
	// database it would be copied out of.
	who := []any{
		"request_id", platform.RequestID(ctx),
		"member_id", actor.MemberID,
		"organization_id", actor.OrganizationID,
		"event_id", eventID,
	}
	s.logger.Info("holder export started", append(who,
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
	)...)

	var export *exportfile.HolderExport
	defer func() {
		// recover() only stops a panic when called directly by the deferred
		// function, so it is here and the logging is a call away. The panic is
		// raised again untouched: recording it is this function's business,
		// handling it is not.
		recovered := recover()
		rows := 0
		if export != nil {
			rows = export.Rows()
		}
		s.logHolderExportFinished(ctx, who, rows, err, sink.err, recovered)
		if recovered != nil {
			panic(recovered)
		}
	}()

	export, err = exportfile.BeginHolderExport(sink, exportfile.HolderColumns{
		Questions: columns,
		// THE FLAG AND NOT THE DATA decides whether the file carries the Holder
		// block, exactly as it decides whether the Sales Export's per-Ticket sheet
		// carries its four (ADR 0045). With assignment closed the columns are
		// absent on every Event, not merely empty.
		Assignment: s.ticketAssignmentEnabled,
	}, loc, generatedAt)
	if err != nil {
		return err
	}
	return s.streamHolderExport(ctx, snapshot, export, batch, len(questions) > 0, fansOut, optionLabels, info)
}

// holderExportTimeout is how long this service lets one Holder Export stream.
func (s *Service) holderExportTimeout() time.Duration {
	if s.holderExportDeadline > 0 {
		return s.holderExportDeadline
	}
	return holderExportDeadline
}

// logHolderExportFinished writes the "holder export finished" line for an
// export that had begun, however it ended: err is what it returned, writeErr
// the first write to the response that failed, and recovered a panic's value,
// or nil.
//
// A COMPLETED EXPORT IS INFO AND ANY OTHER IS WARN: a finished file is routine,
// and a cut one is somebody's failed download that may need explaining.
//
// row_count IS ROWS PRODUCED, NOT ROWS DELIVERED. It counts the rows handed to
// the file's encoder, and a row is in there before it has been compressed,
// flushed or read by anybody; on an abort some of the last of them never left
// this process. For a completed export the two are the same.
//
// AN ABORT RECORDS A REASON AND A CLASS, NEVER THE ERROR'S TEXT. The text of a
// failed write names the client's address and port, and an error wrapped
// further up could carry whatever its wrapper put in it - on the one line whose
// whole purpose is to be kept.
func (s *Service) logHolderExportFinished(
	ctx context.Context, who []any, rows int, err, writeErr error, recovered any,
) {
	if err == nil && recovered == nil {
		s.logger.Info("holder export finished", append(who,
			"outcome", "completed",
			"row_count", rows,
		)...)
		return
	}
	reason, class := holderExportAbortReason(ctx, writeErr), holderExportErrorClass(err)
	if recovered != nil {
		reason, class = holderExportAbortPanic, fmt.Sprintf("panic %T", recovered)
	}
	s.logger.Warn("holder export finished", append(who,
		"outcome", "aborted",
		"row_count", rows,
		"reason", reason,
		"error_class", class,
	)...)
}

// streamHolderExport writes the file from the roster's first batch to the end.
func (s *Service) streamHolderExport(
	ctx context.Context,
	snapshot *repository.HolderRosterSnapshot,
	export *exportfile.HolderExport,
	batch []repository.HolderTicket,
	asked bool,
	fansOut map[string]bool,
	optionLabels map[string]string,
	info exportfile.HolderInfo,
) error {
	s.pauseHolderExport(ctx, 0)

	for len(batch) > 0 {
		// What this batch's Tickets have ANSWERED, read at the snapshot's moment.
		// The screen reads what each still OWES, which is the opposite half of the
		// same fact and not a substitute for it. Skipped entirely when nothing is
		// asked, so an Event with no questions makes no such query at all.
		answersByTicket := map[string][]repository.TicketAnswer{}
		if asked {
			ticketIDs := make([]string, 0, len(batch))
			for _, ticket := range batch {
				ticketIDs = append(ticketIDs, ticket.ID)
			}
			answers, err := snapshot.ListTicketAnswers(ctx, ticketIDs)
			if err != nil {
				return err
			}
			for _, answer := range answers {
				answersByTicket[answer.TicketID] = append(answersByTicket[answer.TicketID], answer)
			}
		}
		for _, row := range s.buildHolderExportRows(batch, answersByTicket, fansOut, optionLabels) {
			if err := export.Append(row); err != nil {
				return err
			}
		}
		s.pauseHolderExport(ctx, export.Rows())
		var err error
		if batch, err = snapshot.NextTickets(ctx); err != nil {
			return err
		}
	}

	// Last, so the row count the Info sheet states is the rows written.
	return export.Finish(info)
}

// holderExportSink is the response, remembering the first write that failed.
type holderExportSink struct {
	w   io.Writer
	err error
}

func (s *holderExportSink) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil && s.err == nil {
		s.err = err
	}
	return n, err
}

// The reasons a "holder export finished" line gives for an abort.
const (
	holderExportAbortClientGone = "client_gone"
	holderExportAbortDeadline   = "deadline"
	holderExportAbortDatabase   = "database_error"
	holderExportAbortPanic      = "panic"
)

// holderExportAbortReason says why a stream that had begun did not finish,
// from the export's own context and the first failed write to the response.
//
// THE DEADLINE IS ASKED FIRST, because once it passes every read fails with
// it and the client may be gone too; it is the cause and they are symptoms. It
// arrives one of two ways: the export's context expiring, or a write to a
// client that stopped reading timing out on the connection's write deadline,
// which is set to the same moment. The second is not the context's deadline
// even then: a failed write cancels the request, so the context reads as
// cancelled, and only the write's own error says it was the clock.
//
// Then the client: a cancelled request is one the client abandoned, and a write
// that failed is one whose reader is no longer there. Anything else broke on
// the database side, which is the only other thing the stream reads from.
func holderExportAbortReason(ctx context.Context, writeErr error) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(writeErr, os.ErrDeadlineExceeded):
		return holderExportAbortDeadline
	case ctx.Err() != nil, writeErr != nil:
		return holderExportAbortClientGone
	default:
		return holderExportAbortDatabase
	}
}

// holderExportErrorClass names the kind of error an export ended on, in words
// that carry nothing of the error's own text: a context's end, a connection's
// deadline, reset or closed pipe, a Postgres SQLSTATE, or - for anything it does
// not recognise - the Go type of the innermost error, which is a name in this
// program's source and never data.
func holderExportErrorClass(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "write_deadline_exceeded"
	case errors.Is(err, syscall.EPIPE):
		return "broken_pipe"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.As(err, &pgErr):
		return "postgres " + pgErr.Code
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, sql.ErrConnDone):
		return "database_connection_lost"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "unexpected_eof"
	}
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return fmt.Sprintf("%T", err)
		}
		err = inner
	}
}

// pauseHolderExport calls the test-only pause, if one is installed.
func (s *Service) pauseHolderExport(ctx context.Context, rowsWritten int) {
	if s.holderExportPause != nil {
		s.holderExportPause(ctx, rowsWritten)
	}
}

// buildHolderExportRows turns the roster's rows into the file's rows.
//
// EVERY ROW GOES THROUGH fillHolderListEntry, WHICH IS THE POINT OF THIS
// FUNCTION. The rule about what an Organization may see of a Holder — nothing at
// all before that Holder has accepted (ADR 0047) — is decided once, in
// catalog.DiscloseHolder, which that method applies for the Holder List screen.
// This builds the same HolderTicketView the screen is drawn from and reads the
// file's columns off it, so the file cannot disclose one byte more than the
// screen does and a change to the rule is a change to both. Reading ticket.HolderEmail here instead would be a second copy
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
	// number for the cell. A value with more significant digits than a float64
	// holds - 12345678901234567890, say - is written ROUNDED to the nearest
	// float64, because that is all an Excel number cell can hold either: the
	// spreadsheet would round it on the way in whatever this wrote. Only a
	// magnitude past float64's range (about 1.8e308), which ParseFloat refuses,
	// is left blank.
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
// A BLANK HERE NO LONGER MEANS "SAY NOTHING". It used to, on the argument that
// printing a UUID at a reader is worse than silence — and that argument was
// answered by the wrong question. Silence about a HONOURED filter is the sheet
// claiming the file is the whole roster when it is not, which is the one thing
// #529 says the Info sheet must never do. So this function keeps its single job
// — resolve the wording or admit it could not — and the CALLER passes the id
// beside whatever comes back, leaving the builder to choose the sentence.
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
// IT COSTS A QUERY ONLY WHEN THE FILTER IS SET, which is why it is a function
// and not a lookup over a list read unconditionally: unlike the Sales Export,
// this file has no per-Ticket-Type columns and therefore no other reason to read
// the catalog at all. An unfiltered download should not pay for a read whose
// only output would be a line the sheet does not print.
//
// IT READS INSIDE THE SNAPSHOT, like every other read the file makes, so the
// name the cover states is the name at the moment the roster is (ADR 0075).
//
// A FAILED READ IS A FAILED EXPORT, which it was not before the snapshot. The
// name is only a sentence on a cover sheet, but a statement that fails inside a
// Postgres transaction aborts the transaction, and every read after it - the
// roster included - would fail too. It fails before the first byte, so it is
// an ordinary refusal. An id naming nothing on this Event is not a failure: it
// resolves to "", and the caller passes the id alongside, so the sheet says the
// file is narrowed even when it cannot say to what.
func holderExportTicketTypeName(
	ctx context.Context, snapshot *repository.HolderRosterSnapshot, orgID, eventID, ticketTypeID string,
) (string, error) {
	if ticketTypeID == "" {
		return "", nil
	}
	types, err := snapshot.ListTicketTypesByEventID(ctx, orgID, eventID)
	if err != nil {
		return "", err
	}
	for _, tt := range types {
		if tt.ID == ticketTypeID {
			return tt.Name, nil
		}
	}
	return "", nil
}

// holderExportFilename names a Holder Export after its Event and the day it was
// taken — "holders-summer-fest-2026-08-29.xlsx" — so a Downloads folder holding
// several stays navigable, and so it can never be confused with the Sales
// Export's "sales-…" sitting beside it. The day is drawn in the Event's own
// timezone, like every other date in the file.
func holderExportFilename(slug string, generatedAt time.Time) string {
	return "holders-" + slug + "-" + generatedAt.Format("2006-01-02") + ".xlsx"
}
