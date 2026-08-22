package service

import (
	"context"
	"strconv"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/sales/exportfile"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// The Sales Export's per-Ticket sheet (#314): one row per Ticket, the Event's
// Ticket Questions as its columns, so an Organization can pivot "how many larges
// do I order".
//
// This file is where the two halves are put together — catalog's questions,
// which decide the columns, and sales' Tickets, which fill the rows — and it is
// the only place in the export that knows a Ticket Question exists.

// exportAnswers builds the per-Ticket sheet for the sales the file already
// carries, or the zero value when there is no sheet to build.
//
// rows are the EXPORTED sales, in the order the data sheet lists them, and they
// are the entire input for which Tickets appear: the Tickets are looked up by
// those sales' ids, so the sheet respects whatever filters the Sales list was
// showing without ever re-running a filter of its own. A filter this sheet
// re-ran itself could re-run it differently, and two sheets in one workbook
// disagreeing about which sales it is about is worse than no sheet.
//
// It returns the zero value when NEITHER feature gives the sheet a reason to
// exist, and the two reasons are separate on purpose (#333). Ticket Questions
// asked give it its question columns; Ticket Assignment being open gives it its
// Holder columns, on every Event and regardless of questions — an Organization
// that assigns 80 tickets and asks nothing came to this file for "who is
// coming", and a workbook that answered only when something was asked would be
// the Holder List's gap (#333) written into a spreadsheet. A flag being off is
// that feature not shipping at all (ADR 0045); an Event asking nothing while
// assignment is also closed is the ordinary state of almost every Event, and a
// workbook with an empty extra sheet in it would be a workbook whose reader
// wonders what they were supposed to find there.
func (s *Service) exportAnswers(ctx context.Context, orgID, eventID string, rows []repository.SaleRow) (exportfile.Answers, error) {
	var questions []repository.EventTicketQuestion
	if s.ticketQuestionsEnabled {
		var err error
		questions, err = s.repo.ListEventTicketQuestions(ctx, orgID, eventID)
		if err != nil {
			return exportfile.Answers{}, err
		}
	}
	if len(questions) == 0 && !s.ticketAssignmentEnabled {
		return exportfile.Answers{}, nil
	}

	columns := make([]exportfile.QuestionColumn, 0, len(questions))
	// Which questions fan out into one column per Option, and what every Option
	// is currently called. The labels are needed even for the questions that do
	// NOT fan out: a single_choice Answer is one Option in one cell, and the cell
	// says what that Option is called now.
	fansOut := make(map[string]bool, len(questions))
	optionLabels := map[string]string{}
	for _, q := range questions {
		column := exportfile.QuestionColumn{ID: q.ID, Label: q.Label}
		for _, option := range q.Options {
			optionLabels[option.ID] = option.Label
		}
		// ONLY multi_choice fans out. single_choice takes exactly one Option, so
		// it fits in one cell and spreading it over a TRUE/FALSE column per
		// Option would cost a reader width to say what one word says.
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

	saleIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		saleIDs = append(saleIDs, row.ID)
	}
	bySale, err := s.repo.ListTicketsForSales(ctx, saleIDs)
	if err != nil {
		return exportfile.Answers{}, err
	}

	var tickets []exportfile.TicketRow
	for _, row := range rows {
		for _, ticket := range bySale[row.ID] {
			// The CONFIRMATION REF and not the Ticket Sale's id: the id is not
			// something anybody has ever been shown, and the reference is what a
			// reader pastes back into the product and what the other sheet is
			// keyed by.
			out := exportfile.TicketRow{
				ConfirmationRef: row.ConfirmationRef,
				TicketTypeName:  ticket.TicketTypeName,
				// The Holder, carried through unexamined (#330, ADR 0047). The
				// repository has already decided what may be seen — the name and
				// address come off a joined Customer row that only an acceptance
				// can produce — so there is nothing to filter here, and a filter
				// here would be a second copy of that rule.
				AssignmentState: string(ticket.AssignmentState),
				HolderFirstName: ticket.HolderFirstName,
				HolderLastName:  ticket.HolderLastName,
				HolderEmail:     ticket.HolderEmail,
			}
			if len(ticket.Answers) > 0 {
				out.Answers = make(map[string]exportfile.Answer, len(ticket.Answers))
				for _, answer := range ticket.Answers {
					out.Answers[answer.TicketQuestionID] = exportedAnswer(
						answer, fansOut[answer.TicketQuestionID], optionLabels)
				}
			}
			tickets = append(tickets, out)
		}
	}

	return exportfile.Answers{
		Questions: columns,
		Tickets:   tickets,
		// The SECOND flag, read separately (#330). The sheet exists because the
		// Event asks something; its Holder columns exist because assignment is
		// open. An Event can be in either state without the other, and folding
		// the two tests together would make one flag silently gate the other.
		Assignment: s.ticketAssignmentEnabled,
	}, nil
}

// exportedAnswer turns one stored Answer into the shape the file writes.
func exportedAnswer(answer repository.ExportedAnswer, fansOut bool, optionLabels map[string]string) exportfile.Answer {
	// A multiple-choice Answer is its chosen Options, by identity: the file
	// writes TRUE under each of them and FALSE under the rest of the question's
	// Options, and no label is involved on this path at all — which is what makes
	// a renamed Option move a heading rather than fork a column.
	if fansOut {
		return exportfile.Answer{Chosen: answer.ChosenOptionIDs}
	}

	// A single_choice Answer: one Option, written as the words it currently
	// reads. The CURRENT label rather than the snapshot the Answer keeps of what
	// its chooser read, for the same reason the headings are current — the file
	// must not report wording that contradicts the screen it was downloaded from.
	// The snapshot is a support question ("what did they actually agree to"), and
	// it is answered on the Ticket rather than in this file.
	if len(answer.ChosenOptionIDs) > 0 {
		// The write path refuses a second Option on a single_choice question, so
		// there is exactly one; the first is taken rather than the list joined,
		// because a joined cell is the un-pivotable thing this sheet exists to
		// avoid.
		if label, ok := optionLabels[answer.ChosenOptionIDs[0]]; ok {
			return exportfile.Answer{Text: &label}
		}
		return exportfile.Answer{}
	}

	out := exportfile.Answer{
		Text:    answer.Text,
		Date:    answer.Date,
		Checked: answer.Checked,
	}
	// The number arrives as the text the NUMERIC column holds, and becomes a real
	// number for the cell. A value too wide for a float64 is left BLANK rather
	// than written rounded: a cell that quietly disagrees with what somebody
	// typed is worse than one a reader can see is missing, and the Answer itself
	// is still readable on the Ticket. Nothing catalog accepts can reach here —
	// MaxNumberAnswerIntegerDigits and MaxNumberAnswerFractionDigits bound it
	// well inside a float64 — so this is a guard rather than a case.
	if answer.Number != nil {
		if parsed, err := strconv.ParseFloat(*answer.Number, 64); err == nil {
			out.Number = &parsed
		}
	}
	return out
}
