package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
)

// importAllowanceKey is what a Purchase Limit is counted against inside one Sale
// Import: a (Customer, Ticket Type) pair. The Customer half is the normalised
// email the customers seam handed back (ADR 0010), never the cell as typed, so
// two rows spelling one person's address differently spend one allowance.
type importAllowanceKey struct {
	customer string
	typeID   string
}

// importAllowance is how much of a (Customer, Ticket Type) allowance the rows
// walked so far have already taken, and which rows took it — the file's own
// tally, on top of whatever that Customer held before the upload.
//
// The rows are kept, not just the total, because the complaint has to name them:
// "an earlier row in this file already took this" and "you already sold this
// person their ticket" are different problems with different fixes, and a
// staff member told only the second would go looking in the wrong place.
type importAllowance struct {
	taken int
	rows  []int
}

// refuseImportRowsOverPurchaseLimit rejects every Sale Import row that would take
// a Customer past a Ticket Type's Purchase Limit (ADR 0025), leaving every other
// row of the batch alone — the same cell-by-cell treatment the preview gives
// every other row problem.
//
// *** Rows in one file count against each other. *** The walk keeps a running
// per-(Customer, Ticket Type) tally, so with a limit of one, the first row for a
// Customer is accepted and the second is refused even though she held nothing
// when the file was uploaded. Without the tally a single upload of a hundred
// identical rows would defeat the limit outright and the refusal would be
// decorative. A rejected row adds nothing to the tally: it will not be recorded,
// so it takes no allowance from the rows after it.
//
// What a Customer already holds is read with CustomerEventHoldings — the same
// read begin-checkout refuses on — rather than a second query of this module's
// own, so the two channels can never disagree about what somebody holds. That
// read answers for ONE buyer, so this costs one resolve plus one holdings read
// per DISTINCT Customer named on a row targeting a rationed Ticket Type, both
// cached: nothing at all for the ordinary file, whose Ticket Types carry no
// Purchase Limit and which returns before any read at all.
//
// A row identifies its Customer by email and a Sale Import may legitimately carry
// no Tax ID (ADR 0016), which is irrelevant here — the allowance is keyed on the
// Customer, and the Tax ID was rejected as a key precisely because it is the
// weaker identity. Resolution goes through the customers seam: sales never
// lower-cases an email itself, or one person would silently become two.
//
// Only rows that already passed field validation are judged. An invalid row is
// not going to be recorded, so it neither takes allowance nor needs a second
// complaint stacked on top of the one the organizer is already fixing.
//
// excludeSaleID, when set, is one Ticket Sale left out of what the Customer
// already holds: the sale a Sale Correction is reversing (#351). The import
// passes "".
func (s *Service) refuseImportRowsOverPurchaseLimit(
	ctx context.Context,
	eventID string,
	types []importfile.TicketTypeRef,
	result *importfile.ValidateResult,
	excludeSaleID string,
) error {
	rationed := make(map[string]importfile.TicketTypeRef, len(types))
	for _, tt := range types {
		if tt.MaxPerCustomer != nil {
			rationed[tt.ID] = tt
		}
	}
	if len(rationed) == 0 {
		return nil
	}

	cutoff := sales.HoldCutoff(s.now())
	// identities maps a cell's verbatim email to the Customer it is; held maps
	// that Customer to what they hold per Ticket Type. Two maps rather than one
	// because several spellings resolve to a single Customer, whose holdings must
	// be read — and spent — once.
	identities := make(map[string]string)
	held := make(map[string]map[string]int)
	taken := make(map[importAllowanceKey]*importAllowance)

	for i := range result.Rows {
		row := result.Rows[i]
		ticketType, isRationed := rationed[row.TicketTypeID]
		if !row.Valid || !isRationed {
			continue
		}

		customer, resolved := identities[row.CustomerEmail]
		if !resolved {
			normalizedEmail, holdings, err := s.resolveCustomerEventHoldingsExcluding(ctx, eventID, row.CustomerEmail, cutoff, excludeSaleID)
			if err != nil {
				return err
			}
			customer = normalizedEmail
			identities[row.CustomerEmail] = customer
			if _, counted := held[customer]; !counted {
				held[customer] = holdings
			}
		}

		key := importAllowanceKey{customer: customer, typeID: row.TicketTypeID}
		allowance := taken[key]
		if allowance == nil {
			allowance = &importAllowance{}
			taken[key] = allowance
		}

		limit := *ticketType.MaxPerCustomer
		alreadyHeld := held[customer][row.TicketTypeID]
		if alreadyHeld+allowance.taken+row.Quantity <= limit {
			allowance.taken += row.Quantity
			allowance.rows = append(allowance.rows, row.Row)
			continue
		}

		result.RejectRow(i, importfile.RowError{
			Field:   importfile.ColQuantity,
			Message: purchaseLimitComplaint(ticketType.Name, limit, alreadyHeld, *allowance),
		})
	}
	return nil
}

// purchaseLimitComplaint states why a row was refused, in the terms of the
// conflict that actually caused it.
//
// The three cases are kept apart because the organizer's next move differs. A
// conflict with what the Customer ALREADY HOLDS is settled off the spreadsheet —
// the person has their ticket, or the limit is wrong for the history being
// imported. A conflict with an EARLIER ROW is fixed in the file itself, and the
// complaint names the row to fix so nobody has to hunt for it. A row that hits
// both says both, naming the file's own rows first because they are the part the
// organizer can edit.
//
// The third is the row that is simply TOO LARGE ON ITS OWN — nothing held, no
// earlier row, just a quantity above the limit. It needs its own wording because
// the other two would report "they already hold 0", sending the organizer to
// look for a Ticket Sale that does not exist. Stating a holding of zero as
// though it were the cause is the same false claim the Storefront's refusal copy
// takes care to avoid (ADR 0025).
func purchaseLimitComplaint(ticketTypeName string, limit, alreadyHeld int, allowance importAllowance) string {
	if len(allowance.rows) == 0 {
		if alreadyHeld == 0 {
			return fmt.Sprintf(
				"is over the Purchase Limit of %d on %s on its own",
				limit, ticketTypeName)
		}
		return fmt.Sprintf(
			"would take this Customer past the Purchase Limit of %d on %s: they already hold %d",
			limit, ticketTypeName, alreadyHeld)
	}
	complaint := fmt.Sprintf(
		"would take this Customer past the Purchase Limit of %d on %s: %s in this file already takes %d",
		limit, ticketTypeName, rowList(allowance.rows), allowance.taken)
	if alreadyHeld > 0 {
		complaint += fmt.Sprintf(", on top of the %d they already hold", alreadyHeld)
	}
	return complaint
}

// rowList names the earlier rows that spent the allowance, as the organizer sees
// them numbered in the spreadsheet ("row 4", "rows 4, 7").
func rowList(rows []int) string {
	numbers := make([]string, 0, len(rows))
	for _, n := range rows {
		numbers = append(numbers, strconv.Itoa(n))
	}
	if len(numbers) == 1 {
		return "row " + numbers[0]
	}
	return "rows " + strings.Join(numbers, ", ")
}
