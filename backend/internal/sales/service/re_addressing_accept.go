package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// The click on a Re-addressing Link (#421, parent #419, ADR 0058): the
// corrected address opens the link, sees what it is being asked to accept, and
// accepts — at which moment the Sale becomes theirs.
//
// EVERYTHING IN THIS FILE IS UNAUTHENTICATED IN THE ORDINARY SENSE AND PROVES AN
// IDENTITY ANYWAY, on the Assignment Link's terms: the token is the whole
// authority, pressing a link that only ever travelled to this address is Proof
// of Email Ownership (ADR 0035), and the Verified Customer it mints is real
// because the token never appeared anywhere but that inbox (#420).
//
// THE VERB IS ACCEPT AND THE ACT IS RE-ADDRESS; "claim" and "transfer" are
// barred (CONTEXT.md).

// ReAddressingLinkView is the public view of one Re-addressing Link: what the
// page shows above the button (#422).
//
// THIS TYPE IS THE DISCLOSURE BOUNDARY. It names the Event, the Sale
// Confirmation reference and the corrected address — the three facts the mail
// already carried to this inbox — and nothing else: not the wrong address, not
// the buyer's snapshot name, not the money, not the Operator, not the Sale's
// id. Before the click the reader has proved nothing, and a page reachable by
// URL must not tell a forwarded link's holder more than its mail did.
type ReAddressingLinkView struct {
	EventName       string `json:"event_name"`
	ConfirmationRef string `json:"confirmation_ref"`
	CorrectedEmail  string `json:"corrected_email"`
	// AcceptedAt is set once the record has been accepted, so a page opened a
	// second time can say the purchase is already theirs and still offer the
	// button — the accept is idempotent and lands them signed in.
	AcceptedAt *time.Time `json:"accepted_at"`
}

// ReAddressingAccepted is what the click returns before the handler adds the
// sign-in: the view, plus the Sale the buyer now owns — disclosed here and
// only here, because after the click they are its party of record.
type ReAddressingAccepted struct {
	ReAddressingLinkView
	TicketSaleID string `json:"ticket_sale_id"`
	// CorrectedEmail on the embedded view is the address the sign-in is for;
	// the handler hands it to the customers module to mint the session.
}

// ViewReAddressingLink opens a Re-addressing Link without accepting it: the
// page the reader sees before pressing the button.
//
// A READ THAT WRITES NOTHING, unlike the Assignment Link, whose open IS the
// accept. The difference is what is being accepted: a Ticket somebody chose
// to give the reader, versus a whole paid purchase that a support thread says
// is theirs. "Here is the Event and the reference — is this yours?" is a
// question worth asking before moving money's owner, and the mail already told
// them everything this view repeats.
func (s *Service) ViewReAddressingLink(ctx context.Context, token string) (*ReAddressingLinkView, error) {
	record, err := s.openReAddressingLink(ctx, token)
	if err != nil {
		return nil, err
	}
	view := reAddressingLinkView(record)
	return &view, nil
}

// AcceptReAddressingLink is the click: the Sale the token's record names moves
// to the corrected address, in one transaction, and a fresh Sale Confirmation
// goes to the corrected inbox. The handler then mints the session.
//
// ACCEPTING TWICE IS IDEMPOTENT: a second click finds the record accepted,
// rewrites nothing — not accepted_at, not the Sale, not the Ticket — sends no
// second Confirmation, and returns the same Sale so the page can land the
// reader on it signed in. People click twice and mail clients prefetch.
//
// THE MAIL IS SENT AFTER THE COMMIT AND IS BEST EFFORT, as every Sale
// Confirmation is: the Sale has moved either way, and the buyer's tickets do
// not depend on a receipt arriving. It is the existing Sale Confirmation
// template, in the Sale's own locale, with the Confirmation Link and the
// Outstanding Answer line this address never got.
//
// IT GRANTS NO CONSENT OF ANY KIND. Nothing here writes a consent row; the
// session the handler mints afterwards goes through the consent gate as a
// passcode sign-in does, which is the proof of that absence.
//
// THE PAYMENT PROVIDER IS NOT CALLED AND MUST NEVER BE.
func (s *Service) AcceptReAddressingLink(ctx context.Context, token string) (*ReAddressingAccepted, error) {
	record, err := s.openReAddressingLink(ctx, token)
	if err != nil {
		return nil, err
	}

	result, err := s.repo.AcceptSaleReAddressing(ctx, repository.AcceptSaleReAddressingInput{
		RecordID:     record.ID,
		Now:          s.now(),
		MintCustomer: s.customers.AcceptReAddressedSale,
	})
	if err != nil {
		return nil, err
	}
	if result.State != sales.ReAddressingAccepted {
		// The world moved between the open and the lock: a reversal, an Event
		// start, a withdrawal. Nothing was written, and the reader hears what
		// the open would have told them a moment later.
		return nil, reAddressingLinkEnded(result.State, result.Record)
	}
	if result.Written {
		s.sendReAddressedSaleConfirmation(ctx, result.Record)
	}

	return &ReAddressingAccepted{
		ReAddressingLinkView: reAddressingLinkView(result.Record),
		TicketSaleID:         result.Record.TicketSaleID,
	}, nil
}

// openReAddressingLink is the single gate both routes pass through:
// configuration, signature, record, instant, then state.
//
// THE ORDER OF THE REFUSALS IS THE DESIGN:
//
//  1. CONFIGURATION, so an unwired deployment refuses before it reads
//     anything: a deployment fault, reported as one.
//  2. THE SIGNATURE, so a forged, truncated or cross-purpose token never
//     reaches a database lookup. An Assignment Link token fails here,
//     cryptographically, because it was signed under a different key.
//  3. THE RECORD, and THE INSTANT it was minted for: a token for a record that
//     does not exist, or that was recorded at another moment, is a link that
//     never was — invalid, on the same code as a forgery.
//  4. THE STATE, read LIVE off the record's ends and the Sale and Event beside
//     it (sales.DeriveReAddressingState): accepted opens (idempotent);
//     pending opens; anything else is a link that was real once and is not
//     now, told apart from a forgery because its reader is the buyer.
func (s *Service) openReAddressingLink(ctx context.Context, token string) (*repository.ReAddressingLinkRecord, error) {
	if !s.reAddressingLinks.Configured() {
		return nil, sales.ErrReAddressingLinkUnavailable()
	}
	recordID, signedMicros, ok := s.reAddressingLinks.Parse(token)
	if !ok {
		return nil, sales.ErrReAddressingLinkInvalid()
	}
	record, err := s.repo.GetReAddressingLinkRecord(ctx, recordID)
	if err != nil {
		return nil, err
	}
	if record == nil || !s.reAddressingLinks.NamesRecord(signedMicros, record.RequestedAt) {
		return nil, sales.ErrReAddressingLinkInvalid()
	}

	state := sales.DeriveReAddressingState(
		nullableTime(record.AcceptedAt), nullableTime(record.WithdrawnAt),
		record.SaleStatus, nullableTime(record.EventStartsAt), s.now(),
	)
	switch state {
	case sales.ReAddressingAccepted, sales.ReAddressingPending:
		if !record.CorrectedEmail.Valid {
			// Purged (#424): pending in name only. Nothing to show and
			// nothing to accept.
			return nil, sales.ErrReAddressingLinkNoLongerValid(sales.ReAddressingLinkEventStarted)
		}
		return record, nil
	default:
		return nil, reAddressingLinkEnded(state, record)
	}
}

// reAddressingLinkEnded is the refusal a genuine link earns once its record
// has ended unaccepted, with the reason the page keys its sentence on.
func reAddressingLinkEnded(state sales.ReAddressingState, record *repository.ReAddressingLinkRecord) error {
	switch state {
	case sales.ReAddressingWithdrawn:
		return sales.ErrReAddressingLinkNoLongerValid(sales.ReAddressingLinkWithdrawn)
	case sales.ReAddressingExpired:
		if record != nil && record.SaleStatus != platform.ActiveSaleStatus {
			return sales.ErrReAddressingLinkNoLongerValid(sales.ReAddressingLinkSaleReversed)
		}
		return sales.ErrReAddressingLinkNoLongerValid(sales.ReAddressingLinkEventStarted)
	default:
		// A record that vanished between the open and the lock reads as
		// withdrawn: from the reader's side that is what it is.
		return sales.ErrReAddressingLinkNoLongerValid(sales.ReAddressingLinkWithdrawn)
	}
}

func reAddressingLinkView(record *repository.ReAddressingLinkRecord) ReAddressingLinkView {
	return ReAddressingLinkView{
		EventName:       record.EventName,
		ConfirmationRef: record.ConfirmationRef,
		CorrectedEmail:  record.CorrectedEmail.String,
		AcceptedAt:      utcOrNil(nullableTime(record.AcceptedAt)),
	}
}

// sendReAddressedSaleConfirmation mails the corrected address the receipt the
// Sale always had, now that the Sale is theirs — the fifth caller of
// SendSaleConfirmation and the first that is not a commit.
//
// It reads the Sale back after the commit rather than carrying it from the
// transaction, because the Confirmation's lines — the Outstanding Answer
// sentence, the Consent Confirmation Link — are read from state, and the
// state that matters is the one the commit produced. It is built by the same
// function the import channel's receipt is, addressed to the Sale's CURRENT
// address, which the commit just made the corrected one: the wrong address
// is never written to, here or anywhere.
func (s *Service) sendReAddressedSaleConfirmation(ctx context.Context, record *repository.ReAddressingLinkRecord) {
	sale, err := s.repo.GetRecordedSale(ctx, record.TicketSaleID)
	if err != nil || sale == nil {
		s.logger.Error("re-addressed sale could not be read back for its Sale Confirmation; the Sale has moved and no receipt went out",
			"ticket_sale_id", record.TicketSaleID, "error", err)
		return
	}
	event, ok, err := s.repo.GetEventImportContext(ctx, record.OrganizationID, record.EventID)
	if err != nil || !ok {
		s.logger.Error("re-addressed sale's Event could not be read for its Sale Confirmation; the Sale has moved and no receipt went out",
			"ticket_sale_id", record.TicketSaleID, "error", err)
		return
	}
	if err := s.email.SendSaleConfirmation(ctx, s.importedSaleConfirmation(ctx, event, *sale)); err != nil {
		s.logger.Error("re-addressed sale's Sale Confirmation could not be sent; the Sale has moved",
			"ticket_sale_id", record.TicketSaleID, "error", err)
	}
}
