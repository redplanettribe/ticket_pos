package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// The Sale Re-addressing an Operator records against a stranded Online Sale
// (#420, parent #419, ADR 0058): the address the buyer meant, mailed a
// Re-addressing Link. Nothing about the Sale moves here — that is the click,
// #421 — and nothing here calls a Payment Provider, ever.

// reAddressingLinkPath is the Storefront route a Re-addressing Link points at.
//
// A STOREFRONT URL AND NEVER AN API ONE, like every other link this platform
// mails (ADR 0008), written without a Locale prefix so the Storefront's
// middleware puts the reader into their own language and carries the query
// string with it. ITS OWN PAGE, and not the Assignment Link's /accept: the two
// are different tokens with different powers — one accepts a Ticket, this one
// accepts a whole purchase — and one address would invite one page to try both.
// #422 builds the page at this path.
const reAddressingLinkPath = "/re-addressing"

// SaleReAddressing is one Sale Re-addressing record as every Operator surface
// shows it: the recording's result, the lookup's pending card, and the lookup's
// accepted history.
//
// IT CARRIES NO TOKEN AND NO LINK, and there is no field for one. The
// Re-addressing Link is composed in exactly one place (mailSaleReAddressing) and
// goes to the corrected address alone; an Operator shown it could complete the
// acceptance themself, and the click would prove nothing (ADR 0058).
type SaleReAddressing struct {
	ID              string `json:"id"`
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Status is DERIVED at read time and never stored: pending, accepted,
	// withdrawn or expired (sales.DeriveReAddressingState).
	Status string `json:"status"`
	// PreviousEmail is the Sale's address when this was recorded — the wrong
	// one, kept as the evidence of what was corrected.
	PreviousEmail string `json:"previous_email"`
	// CorrectedEmail is the address the buyer meant, normalised as a Customer's
	// is. Null once #424's purge has taken it off an unaccepted record.
	CorrectedEmail *string `json:"corrected_email"`
	// Operator is the acting operator's email, from their Staff Session and
	// never from a request body.
	Operator    string     `json:"operator"`
	Note        *string    `json:"note"`
	RequestedAt time.Time  `json:"requested_at"`
	AcceptedAt  *time.Time `json:"accepted_at"`
	WithdrawnAt *time.Time `json:"withdrawn_at"`
}

// SaleReAddressingBlock is the `re_addressing` block on the Operator's Sale
// lookup: the one pending record, or null, and the history of accepted ones,
// oldest first. Withdrawn and expired records are not listed — they are
// corrections that never completed, and the lookup shows what stands and what
// happened, not every attempt.
type SaleReAddressingBlock struct {
	Pending *SaleReAddressing `json:"pending"`
	// Accepted is never null: an empty list says "nobody has accepted
	// anything" and a null would say the block was not computed.
	Accepted []SaleReAddressing `json:"accepted"`
}

// ReAddressSaleInput is one recording as the handler has validated it: the
// operator from the session, the corrected address already parsed and
// normalised (sales.ParseCorrectedEmail), the note trimmed and within bounds.
type ReAddressSaleInput struct {
	Operator       string
	CorrectedEmail string
	Note           *string
}

// WithReAddressingLinks gives this service the key it signs Re-addressing Links
// with (#420, ADR 0058). The secret is the DEPLOYMENT's link secret — the same
// value every other signed link is derived from — turned into this purpose's
// own key rather than used directly, so a Re-addressing Link never verifies as
// an Assignment Link or a Confirmation Link whatever its payload spells. A WithX
// on the catalog service's terms: an unwired service signs nothing, refuses to
// compose a linkless mail, and says so in the log.
func (s *Service) WithReAddressingLinks(secret []byte) *Service {
	s.reAddressingLinks = sales.NewReAddressingLinkSigner(secret)
	return s
}

// ReAddressSaleAsOperator records a Sale Re-addressing against the Ticket Sale a
// Sale Confirmation reference names, and mails the corrected address its
// Re-addressing Link (#420, ADR 0058).
//
// THE GUARDS RUN BEFORE ANYTHING IS WRITTEN, so a refusal is always a no-op: the
// Sale, the record table and the corrected address's inbox are exactly as they
// were. In order: the Sale must be an Online Sale (an imported one has Sale
// Correction, a door sale has no buyer surface); it must be active; its Event
// must not have started, read live from the Event as it stands now and in the
// Event's own timezone (the instant already carries it); and the corrected
// address must be somewhere other than where the Sale already goes.
//
// THE WRITE IS UNDER THE SALE'S ROW LOCK (repository.RecordSaleReAddressing),
// which re-reads the status, so a reversal committing in between is seen and
// two Operators recording at once are serialised. A RECORDING MADE WHILE ONE IS
// PENDING REPLACES IT in that same transaction (#423, ADR 0058): the pending
// record is withdrawn, the new one written, and the old link — bound to the
// old row — dies with the commit. Recording the same address again is how a
// lost mail is sent again: a new record, a new token, a new mail, and the
// previous token refused. The replaced address is told nothing: it was a typo,
// or it is about to get the new mail.
//
// THE MAIL IS BEST EFFORT AND NEVER UNDOES THE RECORD, on the Assignment mail's
// terms: the record is worth keeping even unmailed — it says who typed which
// address when — and "send again" is one click. The Payment Provider is not
// called and must never be: nothing about this moves money.
func (s *Service) ReAddressSaleAsOperator(ctx context.Context, confirmationRef string, in ReAddressSaleInput) (*SaleReAddressing, error) {
	row, err := s.repo.GetSaleByConfirmationRef(ctx, confirmationRef)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, sales.ErrTicketSaleRefNotFound(confirmationRef)
	}
	now := s.now()

	if row.Channel != platform.OnlineSalesChannel {
		return nil, sales.ErrSaleNotReAddressable(row.Channel)
	}
	if row.Status != platform.ActiveSaleStatus {
		return nil, sales.ErrSaleAlreadyReversed()
	}
	if sales.EventHasStarted(nullableTime(row.EventStartsAt), now) {
		return nil, sales.ErrReAddressingEventStarted()
	}
	// The Sale's own address is compared normalised too: every address stored
	// on this platform has been through NormalizeEmail, but the rule is stated
	// here rather than assumed of a column.
	if in.CorrectedEmail == platform.NormalizeEmail(row.CustomerEmail) {
		return nil, sales.ErrReAddressingSameAddress(in.CorrectedEmail)
	}

	result, err := s.repo.RecordSaleReAddressing(ctx, repository.RecordSaleReAddressingInput{
		TicketSaleID:   row.ID,
		OperatorEmail:  in.Operator,
		CorrectedEmail: in.CorrectedEmail,
		Note:           in.Note,
		Now:            now,
	})
	if err != nil {
		return nil, err
	}
	if !result.SaleActive {
		// Reversed between the read above and the lock: the buyer's own undo, or
		// a colleague's Operator Reversal. Nothing was written.
		return nil, sales.ErrSaleAlreadyReversed()
	}

	recorded := result.Recorded
	s.mailSaleReAddressing(ctx, row, recorded)

	view := s.toSaleReAddressing(*recorded, row.ConfirmationRef, row.Status, nullableTime(row.EventStartsAt), now)
	return &view, nil
}

// WithdrawSaleReAddressingAsOperator ends the pending Sale Re-addressing on
// the Ticket Sale a Sale Confirmation reference names (#423, ADR 0058): the
// record is stamped withdrawn, kept, and its Re-addressing Link stops opening.
// Nobody is mailed — the corrected address's link simply stops working, and
// the wrong address is told nothing, as ever.
//
// REFUSED WHEN NOTHING IS PENDING, judged under the Sale's row lock from the
// record's ends and the Sale and Event beside it: a record already accepted,
// already withdrawn, or expired beneath a reversal or the Event's start is not
// pending and is not touched. The Payment Provider is never called.
func (s *Service) WithdrawSaleReAddressingAsOperator(ctx context.Context, confirmationRef string) (*SaleReAddressing, error) {
	row, err := s.repo.GetSaleByConfirmationRef(ctx, confirmationRef)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, sales.ErrTicketSaleRefNotFound(confirmationRef)
	}
	now := s.now()
	withdrawn, err := s.repo.WithdrawSaleReAddressing(ctx, row.ID, now)
	if err != nil {
		return nil, err
	}
	if withdrawn == nil {
		return nil, sales.ErrReAddressingNothingPending()
	}
	view := s.toSaleReAddressing(*withdrawn, row.ConfirmationRef, row.Status, nullableTime(row.EventStartsAt), now)
	return &view, nil
}

// SaleReAddressings assembles the lookup's `re_addressing` block for one Sale:
// the record currently pending, if any, and every accepted one.
//
// PENDING IS JUDGED BY THE DERIVED STATE and not by the two null columns alone:
// a record nobody ended but whose Sale was reversed or whose Event has started
// reads `expired`, and the block shows null for it — there is no pending
// correction on such a Sale, whatever the row says, and the lever is not
// offered on it either.
func (s *Service) SaleReAddressings(ctx context.Context, sale *OperatorSale) (*SaleReAddressingBlock, error) {
	rows, err := s.repo.ListSaleReAddressings(ctx, sale.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	block := SaleReAddressingBlock{Accepted: []SaleReAddressing{}}
	for _, row := range rows {
		view := s.toSaleReAddressing(row, sale.ConfirmationRef, sale.Status, sale.Event.StartsAt, now)
		switch view.Status {
		case string(sales.ReAddressingPending):
			block.Pending = &view
		case string(sales.ReAddressingAccepted):
			block.Accepted = append(block.Accepted, view)
		}
	}
	return &block, nil
}

func (s *Service) toSaleReAddressing(row repository.SaleReAddressingRow, confirmationRef, saleStatus string, eventStartsAt *time.Time, now time.Time) SaleReAddressing {
	acceptedAt := nullableTime(row.AcceptedAt)
	withdrawnAt := nullableTime(row.WithdrawnAt)
	return SaleReAddressing{
		ID:              row.ID,
		TicketSaleID:    row.TicketSaleID,
		ConfirmationRef: confirmationRef,
		Status:          string(sales.DeriveReAddressingState(acceptedAt, withdrawnAt, saleStatus, eventStartsAt, now)),
		PreviousEmail:   row.PreviousEmail,
		CorrectedEmail:  nullableString(row.CorrectedEmail),
		Operator:        row.OperatorEmail,
		Note:            nullableString(row.Note),
		RequestedAt:     row.RequestedAt.UTC(),
		AcceptedAt:      utcOrNil(acceptedAt),
		WithdrawnAt:     utcOrNil(withdrawnAt),
	}
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// mailSaleReAddressing sends the one mail a recording produces, and is the ONLY
// PLACE A RE-ADDRESSING LINK IS EVER COMPOSED.
//
// IT REFUSES TO COMPOSE A LINKLESS MESSAGE. A mail telling somebody a purchase is
// theirs to accept and giving them no way to accept it is an instruction its
// reader cannot follow. An unconfigured signer sends nothing and says so in the
// log, where the failure can be counted.
//
// THE LANGUAGE IS THE SALE'S (ADR 0058: "mail follows the Sale's own locale, as
// the original Sale Confirmation did"), then the corrected address's remembered
// Mail Locale, then English — the ordinary chain, read through the same
// mailLocale every other message about a Sale uses. Reading the recipient's
// record here is not an oracle: nothing about the answer reaches a response
// body, and what changes with it is the language of a message sent to the
// address itself.
func (s *Service) mailSaleReAddressing(ctx context.Context, sale *repository.OperatorSaleRow, recorded *repository.SaleReAddressingRow) {
	token, ok := s.reAddressingLinks.Sign(recorded.ID, recorded.RequestedAt)
	if !ok {
		s.logger.Error("re-addressing link unavailable: no link secret configured; the corrected address was not mailed",
			"ticket_sale_id", sale.ID, "re_addressing_id", recorded.ID)
		return
	}
	to := recorded.CorrectedEmail.String
	err := s.email.SendSaleReAddressing(ctx, platform.SaleReAddressing{
		To:        to,
		EventName: sale.EventName,
		Reference: sale.ConfirmationRef,
		AcceptURL: s.storefrontBaseURL + reAddressingLinkPath + "?token=" + token,
		Locale:    s.mailLocale(ctx, sale.ID, sale.Locale.String, to),
	})
	if err != nil {
		// Neither the address nor the link is logged: the link is a credential
		// that mints an identity, and the address belongs to somebody who may
		// never have been here.
		s.logger.Error("re-addressing mail delivery failed; the record stands and can be sent again",
			"ticket_sale_id", sale.ID, "re_addressing_id", recorded.ID, "error", err)
	}
}
