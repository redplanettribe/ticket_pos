package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// The Sale Invoice Drainer (#474, parent #471, ADR 0060): what turns an owed
// Sale Invoice into an authorized factura with nobody waiting — no buyer on
// the page, no operator pressing Issue.
//
// It is the Reversal Reconciler's shape (sales/service/reconciler.go, read
// that first): an authenticated internal endpoint a scheduler ticks and a
// human can curl, a queue that is a column on the feature's own table
// (invoicing_invoices.next_attempt_at), one document claimed at a time under
// a lease so that two drains never work the same one, and a run bounded by
// a budget of its own. What is written here is only where this drain
// differs.
//
// THE NUMBER IS CONSUMED HERE AND NOWHERE EARLIER. An owed row has no
// secuencial; signing it — inside one transaction that allocates the number,
// builds the factura from the row's own snapshot and signs it — is what
// consumes one, and everything that can refuse does so BEFORE that
// transaction, exactly as the manual issue refuses before its own. A
// document that cannot be signed (no Issuer, no certificate, an expired one,
// an Issuer the schema will not take) is parked needs_attention with no
// number consumed, so the sequence has no holes for documents that never
// existed (#471 story 23).
//
// A DOCUMENT THE AUTHORITY HOLDS IS POLLED, NEVER RESUBMITTED — AND ONLY A
// RECEIVED SUBMIT SAYS IT HOLDS IT (#515, parent #513, maintainer ruling
// 2026-08-29). Once some submit has come back "received" — RECIBIDA, or
// 43/70, "the clave is already registered / in processing" — every later
// round asks autorización and nothing else. A query answer never counts as
// acknowledgement, whatever it says: "received" from autorización is the
// authority describing a document it is processing, and "unknown" is the
// authority saying it has no record of the clave at all; neither is proof
// that anything ever reached it. So a document no submit ever landed — the
// submit died in transport, and the queries since have said one of those
// two things — is sent again on its next rung, byte for byte, under the
// same clave and secuencial (invoicing.AcknowledgedByAuthority reads the
// ledger for this). That can never make a duplicate: if the authority did
// hold it and only its RECIBIDA was lost, the resubmit is answered 43/70,
// which is itself an acknowledging submit (research §2.5).
//
// THE LADDER is 1 min, 5 min, 15 min, then hourly, measured from the
// signing instant (issued_at), for transport failures and for every answer
// that decides nothing alike — "received / en procesamiento", and "unknown",
// the authority reporting no record of the clave (#514). A definite refusal parks the document
// needs_attention and stops the ladder: the SRI's messages are the
// operator's to read, and Check status / Resend are the remedies. Twenty-four
// hours without a definite answer parks it needs_attention too, but the
// ladder goes on — hourly — so that a late AUTORIZADO heals it without
// anyone pressing anything. An unsignable document is retried hourly for the
// same reason: an operator who uploads the certificate has fixed it, and the
// next hour's tick proves so.
//
// A CREDIT NOTE RIDES THE SAME MACHINE (#476). Claimed only once the
// factura it credits is terminal (repository.ClaimDueInvoice), it is then
// one of two things: its factura is authorized, and it is signed under
// codDoc 04 from the factura's number and date, submitted, polled, parked
// and delivered exactly as a Sale Invoice is; or its factura died —
// withdrawn, annulled — and it is withdrawn with it, unsigned, no number
// consumed, nothing sent, because a nota de crédito can name only an
// authorized factura (#471 story 29).
//
// A CORRECTED FACTURA FOLLOWS ITS CREDIT NOTE (#483, ADR 0061). A Sale
// Invoice Reissue owes a Credit Note against the old factura and a fresh
// Sale Invoice that supersedes it, both due at once. The claim
// (repository.ClaimDueInvoice) hands out the Credit Note first and the
// corrected factura only once that Credit Note is authorized, so the SRI
// sees the cancellation before the replacement and a Sale never holds two
// authorized facturas; once claimed, the corrected factura is signed,
// submitted, polled and delivered exactly as the first one was. A Credit
// Note that dies instead — annulled at the portal — takes the corrected
// factura with it (#484): Mark annulled withdraws it in its own act, and
// the claim admits a corrected factura no live Credit Note precedes so that
// a round withdraws it unsigned as this round withdraws an owed Credit Note
// whose factura died. Either way the old factura is current again.
//
// ONE CREDIT NOTE PER FACTURA REACHES THE AUTHORITY (#484). A reversal
// during a reissue owes a second Credit Note against the old factura,
// which waits (repository.ClaimDueInvoice) while the reissue's is signed
// and undecided; claimed once that one is answered, it is withdrawn here
// if the sibling was authorized — the factura is credited already — and
// signed if the sibling died.
//
// DELIVERY IS THIS DRAINER'S LAST STEP (#475). An authorized document is
// mailed to the buyer in the same round that saw it authorized — under the
// same claim, which the authorization leaves in place — and, if the sender
// refused, is claimed again on the same ladder for delivery alone: an
// authorized document without a delivered_at is due work exactly as an
// owed one is, and the authorization is never touched by whatever the mail
// does. Delivery asks the authority nothing. See delivery.go.
//
// A CLAIM IS A LEASE, NOT A LOCK. A reversal may withdraw an owed document
// under the round that holds it (#476), and an operator may mark a signed
// one annulled (#477). Every write a round makes is therefore guarded on
// the state it read the row in (repository.Reschedule, ApplyOutcome), and
// a round that finds its row moved writes nothing more and reports the
// row as it now stands: a dead document is never resurrected onto the
// queue by a round that started before it died.
//
// LOG LINES COUNT DOCUMENTS AND NAME STATES, never buyers (#471 story 35).

// SaleInvoiceLadder is the delay between rounds after the first, second and
// third round without a definite answer, and every round after.
var SaleInvoiceLadder = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// SaleInvoiceAttentionAfter is how long a signed document may go without a
// definite answer before it is parked needs_attention (still polled).
const SaleInvoiceAttentionAfter = 24 * time.Hour

// saleInvoiceDrainBatch and saleInvoiceDrainBudget bound one run, on the
// reconciler's terms: the budget is the real bound and the count is the
// belt. Signing and submitting one owed document can cost the whole poll
// budget (DefaultPollBudget, ~15 s), so a run stops STARTING documents at
// the budget and the last one may still be paying that on top.
//
// The deadline chain, as the reconciler states it:
//
//	saleInvoiceDrainBudget  <  Cloud Scheduler's attempt_deadline  <  Cloud Run's request timeout
//	         60s            <              120s                    <           300s
//
// The middle term is terraform's sale_invoice_drainer_attempt_deadline_seconds;
// TestTheSaleInvoiceDrainBudgetIsStrictlyInsideTheSchedulerDeadline reads it
// rather than mirroring it.
const (
	saleInvoiceDrainBatch  = 50
	saleInvoiceDrainBudget = 60 * time.Second
)

// saleInvoiceClaimLease is how long a claimed document is hidden from other
// drains: the fallback for an instance that died between claiming and
// writing what it learned. An ordinary round overwrites it within seconds.
const saleInvoiceClaimLease = 5 * time.Minute

// saleInvoiceDrainerName is what a Sale Invoice's issued_by says: the
// platform's own hand, where a manual document names the operator.
const saleInvoiceDrainerName = "sale-invoice-drainer"

// platformMessageType is the type a message the PLATFORM puts on a document
// carries, beside the authority's ERROR / ADVERTENCIA: why an owed document
// could not be signed, in the operator's own vocabulary. The page shows it
// in the same list; the type says who is speaking.
const platformMessageType = "PLATFORM"

// SaleInvoiceDrainResult is what one run did, and what is standing
// afterwards — counts and states only.
type SaleInvoiceDrainResult struct {
	// Claimed is how many documents this run took up. Zero is the ordinary
	// answer: nothing was due.
	Claimed int `json:"claimed"`
	// Authorized, Pending and NeedsAttention are where those documents
	// ended: authorized this run; still undecided and due again on the
	// ladder; parked for an operator (refused, unsignable, or 24 h without an
	// answer). With Failed they sum to Claimed.
	Authorized     int `json:"authorized"`
	Pending        int `json:"pending"`
	NeedsAttention int `json:"needs_attention"`
	// Delivered is how many authorized documents this run mailed to their
	// buyers (#475). Each is counted under Authorized as well; an authorized
	// document the sender refused is not counted here and is due again.
	Delivered int `json:"delivered"`
	// Withdrawn is how many documents this run found dead under it: Credit
	// Notes it withdrew because the factura they would have credited died
	// (#476), and documents a reversal withdrew or an operator marked
	// annulled while the round was working them. Counted beside the three
	// above rather than under any of them.
	Withdrawn int `json:"withdrawn"`
	// Failed is documents whose round errored on the database itself. Each
	// logged its own line; the claim lease returns them to the queue. With
	// Authorized, Pending, NeedsAttention and Withdrawn it sums to Claimed.
	Failed int `json:"failed"`
	// Standing is how many Sale Invoices sit in each state once this run
	// finished, so two curls apart say whether a backlog is shrinking.
	Standing map[string]int `json:"standing"`
}

// DrainSaleInvoices works the Sale Invoices that are due, one at a time,
// until the queue is empty or the run reaches its bound. Safe to call by
// hand at any time; a no-op on an empty queue.
//
// It first works the Certificate Expiry Warning's ladder, ABOVE the flag
// (#503, ADR 0063 §1): a closed flag still answers SALE_INVOICING_UNAVAILABLE
// and signs nothing, but the certificate uploaded ahead of launch is watched
// from the day this deploys. See certificate_expiry_warning.go.
func (s *Service) DrainSaleInvoices(ctx context.Context) (*SaleInvoiceDrainResult, error) {
	s.warnOfCertificateExpiry(ctx)
	if !s.saleInvoicingEnabled {
		return nil, invoicing.ErrSaleInvoicingUnavailable()
	}
	return s.drainSaleInvoices(ctx, "")
}

// WithSaleInvoicingEnabled opens or closes Sale Invoicing on this service
// with SALE_INVOICING_ENABLED (#471, ADR 0060). Closed is how it ships, and
// closed means the Drainer's endpoint answers SALE_INVOICING_UNAVAILABLE and
// signs nothing: the incident switch for a document the platform must stop
// declaring. Whether a sale OWES a document is decided upstream — the sales
// module is handed this service as its seam only while the flag is open —
// so a closed flag leaves nothing new for the Drainer to find; what it
// leaves unworked is only what was owed before it closed, which waits, and
// which an operator can still Resend, Check or Mark annulled by hand.
func (s *Service) WithSaleInvoicingEnabled(enabled bool) *Service {
	s.saleInvoicingEnabled = enabled
	return s
}

// KickSaleInvoiceDrainer implements the sales module's seam for "the
// transaction that owed a document has committed": one immediate round over
// that Sale's documents, in the background, so the normal path completes in
// seconds while the checkout response has already gone out. The caller's
// context is detached, because the request it belongs to is finishing; the
// round bounds itself.
//
// It is fire-and-forget on purpose: nothing about the checkout waits on it
// and nothing about the checkout can fail because of it. Whatever it does
// not finish, the scheduled drain finds due exactly as it left it.
func (s *Service) KickSaleInvoiceDrainer(ctx context.Context, ticketSaleID string) {
	if !s.kick {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saleInvoiceDrainBudget)
		defer cancel()
		if _, err := s.drainSaleInvoices(ctx, ticketSaleID); err != nil {
			s.logger.Warn("invoicing: the post-commit kick could not drain; the scheduled drain will", "error", err)
		}
	}()
}

// WithSaleInvoiceKick turns the post-commit kick on or off. It is on in
// every deployment; the test harness turns it off so that a drain is
// something a test drives, and turns it on for the test that proves it.
func (s *Service) WithSaleInvoiceKick(enabled bool) *Service {
	s.kick = enabled
	return s
}

// WithSaleInvoiceDrainBatch narrows how many documents one run works, so a
// test can prove the bound is a bound.
func (s *Service) WithSaleInvoiceDrainBatch(batch int) *Service {
	s.drainBatch = batch
	return s
}

func (s *Service) saleInvoiceDrainBatch() int {
	if s.drainBatch > 0 {
		return s.drainBatch
	}
	return saleInvoiceDrainBatch
}

func (s *Service) drainSaleInvoices(ctx context.Context, ticketSaleID string) (*SaleInvoiceDrainResult, error) {
	out := &SaleInvoiceDrainResult{Standing: map[string]int{}}
	deadline := s.clock().Add(saleInvoiceDrainBudget)
	wall := time.Now()

	for out.Claimed < s.saleInvoiceDrainBatch() {
		if ctx.Err() != nil {
			break
		}
		now := s.clock()
		// The budget is measured on the wall as well as on the service clock:
		// the clock is fixed in tests, and a run that could not tell time had
		// passed would have no bound at all.
		if !now.Before(deadline) || time.Since(wall) >= saleInvoiceDrainBudget {
			break
		}
		row, err := s.repo.ClaimDueInvoice(ctx, now, now.Add(saleInvoiceClaimLease), ticketSaleID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			break
		}
		out.Claimed++
		status, delivered, err := s.workSaleInvoice(ctx, row)
		if err != nil {
			s.logger.Warn("invoicing: the drainer could not finish a document; it stays leased until the claim expires",
				"invoice_id", row.Invoice.ID, "error", err)
			out.Failed++
			continue
		}
		if delivered {
			out.Delivered++
		}
		switch status {
		case invoicing.InvoiceStatusAuthorized:
			out.Authorized++
		case invoicing.InvoiceStatusNeedsAttention:
			out.NeedsAttention++
		case invoicing.InvoiceStatusWithdrawn, invoicing.InvoiceStatusAnnulled:
			out.Withdrawn++
		default:
			out.Pending++
		}
	}

	standing, err := s.repo.CountSaleInvoicesByStatus(ctx)
	if err != nil {
		s.logger.Warn("invoicing: could not count the standing Sale Invoices for the drain response", "error", err)
	} else {
		out.Standing = standing
	}
	s.logger.Info("invoicing: sale invoices drained",
		"claimed", out.Claimed, "authorized", out.Authorized, "pending", out.Pending,
		"needs_attention", out.NeedsAttention, "delivered", out.Delivered, "withdrawn", out.Withdrawn, "failed", out.Failed)
	return out, nil
}

// workSaleInvoice does one round on one claimed document and reports the
// state it left it in, and whether this round delivered it to the buyer.
//
// A document claimed already authorized is here for delivery alone: an
// earlier round authorized it and the sender refused (#475). Nothing about
// it is asked of the authority again.
func (s *Service) workSaleInvoice(ctx context.Context, row *repository.InvoiceRow) (invoicing.InvoiceStatus, bool, error) {
	if row.Invoice.Status == invoicing.InvoiceStatusAuthorized {
		delivered, err := s.deliverDocument(ctx, row)
		return invoicing.InvoiceStatusAuthorized, delivered, err
	}
	if !row.Invoice.Signed() {
		if row.Invoice.Kind == invoicing.DocumentKindCreditNote {
			// Its factura is terminal and no sibling is undecided, or it
			// would not have been claimed. Dead factura, or a sibling that
			// credited the factura already: the Credit Note is withdrawn,
			// and there is nothing further to do this round.
			withdrawn, err := s.withdrawCreditNoteOfADeadFactura(ctx, row)
			if err != nil || withdrawn {
				return invoicing.InvoiceStatusWithdrawn, false, err
			}
			if withdrawn, err = s.withdrawRedundantCreditNote(ctx, row); err != nil || withdrawn {
				return invoicing.InvoiceStatusWithdrawn, false, err
			}
		}
		if row.Invoice.SupersedesInvoiceID != "" {
			// Its Credit Note is authorized or dead, or it would not have
			// been claimed. Dead: the corrected factura goes with it.
			withdrawn, err := s.withdrawCorrectedFacturaOfADeadCreditNote(ctx, row)
			if err != nil || withdrawn {
				return invoicing.InvoiceStatusWithdrawn, false, err
			}
		}
		signed, err := s.signOwedInvoice(ctx, row)
		if err != nil {
			return "", false, err
		}
		if signed == nil {
			// Parked unsignable, and signOwedInvoice wrote why — or a
			// reversal withdrew it under the claim and the park wrote
			// nothing; the row says which.
			status, err := s.settleRound(ctx, row.Invoice.ID)
			return status, false, err
		}
		row = signed
		s.submitAndPoll(ctx, row, nil)
	} else if invoicing.AcknowledgedByAuthority(row.Attempts) {
		// A submit came back received: the authority has the document, and
		// this round only asks what became of it.
		s.pollOnce(ctx, row)
	} else {
		// No submit ever landed — whatever the queries since have said
		// (#515, parent #513). Send the bytes on file again, under the same
		// clave and secuencial.
		s.submitAndPoll(ctx, row, nil)
	}
	status, err := s.settleRound(ctx, row.Invoice.ID)
	if err != nil || status != invoicing.InvoiceStatusAuthorized {
		return status, false, err
	}
	// Authorized this round: hand it over now rather than on the next tick,
	// so the buyer's second mail follows the receipt within seconds.
	authorized, err := s.repo.GetInvoice(ctx, row.Invoice.ID)
	if err != nil {
		return status, false, err
	}
	delivered, err := s.deliverDocument(ctx, authorized)
	return status, delivered, err
}

// withdrawCreditNoteOfADeadFactura reads the factura an owed Credit Note
// credits and, if that factura will never be authorized — withdrawn, or
// annulled by the operator at the portal — withdraws the Credit Note too,
// off the queue, with a platform message saying why, and reports so. An
// authorized factura reports false: the Credit Note is signed next. Any
// other state is a claim that should not have happened and is left
// leased to expire, reported as neither.
func (s *Service) withdrawCreditNoteOfADeadFactura(ctx context.Context, row *repository.InvoiceRow) (bool, error) {
	factura, err := s.repo.GetInvoice(ctx, row.Invoice.CreditsInvoiceID)
	if err != nil {
		return false, err
	}
	if factura == nil {
		return false, fmt.Errorf("invoicing: credit note %s credits invoice %s, which does not exist", row.Invoice.ID, row.Invoice.CreditsInvoiceID)
	}
	switch factura.Invoice.Status {
	case invoicing.InvoiceStatusAuthorized:
		return false, nil
	case invoicing.InvoiceStatusWithdrawn, invoicing.InvoiceStatusAnnulled:
	default:
		return false, fmt.Errorf("invoicing: credit note %s was claimed while its factura is %s", row.Invoice.ID, factura.Invoice.Status)
	}
	return s.withdrawUnsigned(ctx, row, creditNoteWithdrawnCode,
		fmt.Sprintf("The Sale Invoice this Credit Note would have credited is %s; nothing was sent to the SRI.", factura.Invoice.Status),
		"credit note withdrawn; the factura it credits died unauthorized")
}

// withdrawRedundantCreditNote reads the other Credit Notes against the
// factura an owed Credit Note credits (#484) and, if one of them is
// authorized — a reissue's, answered after the reversal owed this one —
// withdraws this one, off the queue, with a platform message saying why,
// and reports so. No authorized sibling reports false: the Credit Note is
// signed next. A sibling still signed and undecided is a claim that should
// not have happened and is left leased to expire.
func (s *Service) withdrawRedundantCreditNote(ctx context.Context, row *repository.InvoiceRow) (bool, error) {
	siblings, err := s.creditNotesAgainst(ctx, row.Invoice.TicketSaleID, row.Invoice.CreditsInvoiceID, row.Invoice.ID)
	if err != nil {
		return false, err
	}
	credited := false
	for _, sibling := range siblings {
		switch sibling.Status {
		case invoicing.InvoiceStatusAuthorized:
			credited = true
		case invoicing.InvoiceStatusPending, invoicing.InvoiceStatusNeedsAttention:
			return false, fmt.Errorf("invoicing: credit note %s was claimed while credit note %s against the same factura is %s", row.Invoice.ID, sibling.ID, sibling.Status)
		}
	}
	if !credited {
		return false, nil
	}
	return s.withdrawUnsigned(ctx, row, creditNoteRedundantCode,
		"The Sale Invoice this Credit Note would have credited is credited already by another Credit Note; nothing was sent to the SRI.",
		"credit note withdrawn; the factura it credits is credited already")
}

// withdrawCorrectedFacturaOfADeadCreditNote reads the Credit Notes against
// the factura an owed corrected Sale Invoice supersedes (#484) and, if none
// of them is live any more — the reissue's died, annulled at the portal —
// withdraws the corrected factura, unsigned, off the queue, with a
// platform message saying why, and reports so: the old factura stands
// current. An authorized Credit Note reports false: the corrected factura
// is signed next. One still owed or undecided is a claim that should not
// have happened and is left leased to expire.
func (s *Service) withdrawCorrectedFacturaOfADeadCreditNote(ctx context.Context, row *repository.InvoiceRow) (bool, error) {
	notes, err := s.creditNotesAgainst(ctx, row.Invoice.TicketSaleID, row.Invoice.SupersedesInvoiceID, "")
	if err != nil {
		return false, err
	}
	for _, note := range notes {
		switch note.Status {
		case invoicing.InvoiceStatusAuthorized:
			return false, nil
		case invoicing.InvoiceStatusOwed, invoicing.InvoiceStatusPending, invoicing.InvoiceStatusNeedsAttention:
			return false, fmt.Errorf("invoicing: corrected sale invoice %s was claimed while credit note %s against the factura it supersedes is %s", row.Invoice.ID, note.ID, note.Status)
		}
	}
	return s.withdrawUnsigned(ctx, row, correctedInvoiceWithdrawnCode, correctedInvoiceWithdrawnMessage,
		"corrected sale invoice withdrawn; the credit note it follows died unauthorized")
}

// creditNotesAgainst is the Sale's Credit Notes crediting one factura,
// except the one with excludeID.
func (s *Service) creditNotesAgainst(ctx context.Context, ticketSaleID, facturaID, excludeID string) ([]*invoicing.Invoice, error) {
	docs, err := s.repo.ListInvoicesBySale(ctx, ticketSaleID)
	if err != nil {
		return nil, err
	}
	var out []*invoicing.Invoice
	for i := range docs {
		inv := &docs[i].Invoice
		if inv.Kind == invoicing.DocumentKindCreditNote && inv.CreditsInvoiceID == facturaID && inv.ID != excludeID {
			out = append(out, inv)
		}
	}
	return out, nil
}

// withdrawUnsigned withdraws the claimed, unsigned document with one
// platform message, guarded on the state the round read it in, and reports
// true either way: written, or moved under the round by whoever got there
// first, in which case the row is theirs.
func (s *Service) withdrawUnsigned(ctx context.Context, row *repository.InvoiceRow, code, message, logLine string) (bool, error) {
	now := s.clock()
	msgs := []invoicing.AuthorityMessage{{Identifier: code, Message: message, Type: platformMessageType}}
	written, err := s.repo.Reschedule(context.WithoutCancel(ctx), row.Invoice.ID, row.Invoice.Status, invoicing.InvoiceStatusWithdrawn, msgs, nil, now)
	if err != nil {
		return false, err
	}
	if !written {
		s.logger.Info("invoicing: the document moved under the drainer; not withdrawn by this round", "invoice_id", row.Invoice.ID, "kind", row.Invoice.Kind, "read_as", row.Invoice.Status)
		return true, nil
	}
	s.logger.Info("invoicing: "+logLine, "invoice_id", row.Invoice.ID, "kind", row.Invoice.Kind)
	return true, nil
}

// creditNoteWithdrawnCode is the platform message a withdrawn Credit Note
// carries, in the operator's vocabulary beside the authority's codes.
const creditNoteWithdrawnCode = "CREDIT_NOTE_FACTURA_NOT_AUTHORIZED"

// creditNoteRedundantCode is the platform message a Credit Note withdrawn
// because another already credits its factura carries (#484).
const creditNoteRedundantCode = "CREDIT_NOTE_FACTURA_ALREADY_CREDITED"

// correctedInvoiceWithdrawnCode and its message are what a corrected Sale
// Invoice withdrawn because its Credit Note died carries (#484), whether
// Mark annulled withdrew it or the Drainer did.
const (
	correctedInvoiceWithdrawnCode    = "SALE_INVOICE_CREDIT_NOTE_NOT_AUTHORIZED"
	correctedInvoiceWithdrawnMessage = "The Credit Note this corrected Sale Invoice follows was not authorized; nothing was sent to the SRI, and the Sale Invoice it would have superseded stands."
)

// pollOnce asks autorización once and applies a definite answer; an
// undecided one only reschedules.
func (s *Service) pollOnce(ctx context.Context, row *repository.InvoiceRow) {
	callCtx, cancel := context.WithTimeout(ctx, s.pollBudget)
	defer cancel()
	authority := s.authority(row.Invoice.Environment)
	outcome, err := s.attempt(callCtx, row.Invoice.ID, invoicing.AttemptQuery, func() (invoicing.Outcome, error) {
		return authority.QueryOutcome(callCtx, row.Ecuador.AccessKey)
	})
	if err == nil {
		s.applyOutcome(ctx, row, outcome)
	}
}

// settleRound reads the document back and, if the round left it undecided
// without an answer to schedule from (a transport failure writes no
// outcome), puts it on the ladder itself. A refusal and an unsignable
// document are left exactly as applyOutcome and signOwedInvoice parked
// them, and a document that died under the round — withdrawn, annulled —
// exactly as whoever killed it left it. It returns the document's
// status as it then stands, and it is what the round reports.
func (s *Service) settleRound(ctx context.Context, id string) (invoicing.InvoiceStatus, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", invoicing.ErrInvoiceNotFound()
	}
	inv := &row.Invoice
	switch inv.Status {
	case invoicing.InvoiceStatusPending:
		return s.reschedule(ctx, row)
	case invoicing.InvoiceStatusNeedsAttention:
		if len(row.Attempts) == 0 {
			// Unsignable: parked with its hourly retry already written.
			return inv.Status, nil
		}
		switch row.Attempts[len(row.Attempts)-1].Outcome {
		case string(invoicing.OutcomeReceived), string(invoicing.OutcomeUnknown), invoicing.AttemptOutcomeError:
			// Past 24 h and still undecided — the authority is processing
			// it, has no record of the clave (#514), or could not be
			// reached: parked for the operator, and still worked. Which of
			// polling and resubmitting the next round does is the
			// Submit-only rule's to decide, not this park's.
			return s.reschedule(ctx, row)
		}
	}
	return inv.Status, nil
}

// reschedule puts an undecided document on the ladder from its signing
// instant, parking it needs_attention once 24 h have passed, and returns
// the status it wrote — or, when the row moved under the round and nothing
// was written, the status it now reads.
func (s *Service) reschedule(ctx context.Context, row *repository.InvoiceRow) (invoicing.InvoiceStatus, error) {
	now := s.clock()
	next := now.Add(ladderDelay(now.Sub(row.Invoice.IssuedAt)))
	status := undecidedStatus(&row.Invoice, now)
	written, err := s.repo.Reschedule(context.WithoutCancel(ctx), row.Invoice.ID, row.Invoice.Status, status, nil, &next, now)
	if err != nil {
		return "", err
	}
	if !written {
		s.logger.Info("invoicing: the document moved under the drainer; not rescheduled by this round", "invoice_id", row.Invoice.ID, "read_as", row.Invoice.Status)
		return s.currentStatus(ctx, row.Invoice.ID)
	}
	return status, nil
}

// currentStatus reads a document's status as it stands now.
func (s *Service) currentStatus(ctx context.Context, id string) (invoicing.InvoiceStatus, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", invoicing.ErrInvoiceNotFound()
	}
	return row.Invoice.Status, nil
}

// certificateExpiredMessage is the message a document parks under when the
// certificate in custody has expired (#501, ADR 0063): CERTIFICATE_EXPIRED
// with its usual text, and the certificate's not-after as an Ecuador calendar
// date in additional_info. Written at park time from the certificate that was
// in custody then, so a re-upload leaves the row saying what was true when it
// parked — the attention list renders additional_info as it is.
func certificateExpiredMessage(notAfter time.Time) invoicing.AuthorityMessage {
	e := invoicing.ErrCertificateExpired()
	return invoicing.AuthorityMessage{
		Identifier:     e.Code(),
		Message:        e.Message(),
		AdditionalInfo: platform.EcuadorDate(notAfter),
		Type:           platformMessageType,
	}
}

// signOwedInvoice turns an owed row into a signed, pending one, consuming a
// secuencial — or parks it needs_attention when it cannot be signed and
// returns nil. Everything that can refuse does so before the number is
// allocated.
func (s *Service) signOwedInvoice(ctx context.Context, row *repository.InvoiceRow) (*repository.InvoiceRow, error) {
	inv := &row.Invoice
	now := s.clock()
	parkWith := func(msg invoicing.AuthorityMessage) (*repository.InvoiceRow, error) {
		code := msg.Identifier
		next := now.Add(SaleInvoiceLadder[len(SaleInvoiceLadder)-1])
		msgs := []invoicing.AuthorityMessage{msg}
		// Guarded on the state the row was claimed in: a reversal that
		// withdrew it since (#476) wins, and the park writes nothing — a
		// withdrawn document parked needs_attention would be signed on the
		// next round, for a sale that no longer stands.
		written, err := s.repo.Reschedule(context.WithoutCancel(ctx), inv.ID, inv.Status, invoicing.InvoiceStatusNeedsAttention, msgs, &next, now)
		if err != nil {
			return nil, err
		}
		if !written {
			s.logger.Info("invoicing: the document moved under the drainer; not parked by this round", "invoice_id", inv.ID, "read_as", inv.Status, "reason", code)
			return nil, nil
		}
		s.logger.Warn("invoicing: sale invoice cannot be signed; parked needs_attention with no number consumed", "invoice_id", inv.ID, "reason", code)
		return nil, nil
	}
	park := func(code, message string) (*repository.InvoiceRow, error) {
		return parkWith(invoicing.AuthorityMessage{Identifier: code, Message: message, Type: platformMessageType})
	}

	issuer, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		return nil, err
	}
	if issuer == nil {
		e := invoicing.ErrIssuerNotFound()
		return park(e.Code(), e.Message())
	}
	cert, err := s.OpenEcuadorSigningKey(ctx)
	if err != nil {
		var domain apperror.DomainError
		if errors.As(err, &domain) {
			return park(domain.Code(), domain.Message())
		}
		return nil, err
	}
	if !cert.Metadata.NotAfter.After(now) {
		return parkWith(certificateExpiredMessage(cert.Metadata.NotAfter))
	}

	// The Issuer's CURRENT environment and details: an Issuer moved from
	// test to production signs still-owed documents in production.
	snapshot := invoicing.SnapshotOf(issuer.Details)
	env := issuer.Issuer.Environment
	base := sri.Factura{
		Environment:   sri.AmbienteFor(env),
		Issuer:        sri.IssuerFromSnapshot(snapshot),
		IssuedOn:      now,
		PaymentMethod: sri.PaymentMethod(inv.PaymentMethod),
	}
	parts, err := s.saleDocumentParts(ctx, base, row)
	if err != nil {
		e := invoicing.ErrInvoiceInvalid(reason(err))
		return park(e.Code(), e.Message())
	}
	if err := s.dryRun(base, probeRecipient, probeLines, nil); err != nil {
		e := invoicing.ErrIssuerIncomplete(reason(err))
		return park(e.Code(), e.Message())
	}
	if err := s.dryRunParts(parts); err != nil {
		e := invoicing.ErrInvoiceInvalid(reason(err))
		return park(e.Code(), e.Message())
	}

	signed, err := s.repo.SignOwedInvoice(ctx, inv.ID, repository.SignedInvoice{
		IssuerID:    issuer.Issuer.ID,
		Environment: env,
		IssuedOn:    guayaquilDate(now),
		IssuedAt:    now,
		IssuedBy:    saleInvoiceDrainerName,
		Snapshot:    snapshot,
		CodDoc:      parts.documentType(),
		Estab:       snapshot.Establecimiento,
		PtoEmi:      snapshot.PuntoEmision,
	}, numberAndSign(parts, cert, now))
	if err != nil {
		return nil, err
	}
	s.logger.Info("invoicing: sale document signed",
		"invoice_id", signed.Invoice.ID, "kind", inv.Kind, "cod_doc", parts.documentType(), "environment", env, "secuencial", signed.Ecuador.Secuencial)
	return signed, nil
}

// saleDocumentParts assembles the parts of an owed document by its kind: a
// Sale Invoice's factura, or a Credit Note's nota de crédito, which needs
// the factura it credits read for its number and date (#476).
func (s *Service) saleDocumentParts(ctx context.Context, base sri.Factura, row *repository.InvoiceRow) (facturaParts, error) {
	parts, err := saleFacturaParts(base, &row.Invoice)
	if err != nil {
		return facturaParts{}, err
	}
	if row.Invoice.Kind != invoicing.DocumentKindCreditNote {
		return parts, nil
	}
	factura, err := s.repo.GetInvoice(ctx, row.Invoice.CreditsInvoiceID)
	if err != nil {
		return facturaParts{}, err
	}
	return creditNoteParts(parts, &row.Invoice, factura)
}

// creditNoteParts turns a Credit Note's factura-shaped parts into a nota
// de crédito's: codDoc 04, the credited factura's printed number and
// emission date, and the Credit Note's reason as the motivo — a reversal
// route's words or the reissue's fixed correction text (#481). The credited
// factura must be signed and authorized; anything else is a document the
// Drainer should never have reached here with.
func creditNoteParts(parts facturaParts, note *invoicing.Invoice, factura *repository.InvoiceRow) (facturaParts, error) {
	if factura == nil || factura.Ecuador == nil || !factura.Invoice.Signed() {
		return facturaParts{}, fmt.Errorf("credit note %s credits an unsigned or missing invoice %s", note.ID, note.CreditsInvoiceID)
	}
	if factura.Invoice.Status != invoicing.InvoiceStatusAuthorized {
		return facturaParts{}, fmt.Errorf("credit note %s credits invoice %s, which is %s and not authorized", note.ID, note.CreditsInvoiceID, factura.Invoice.Status)
	}
	// The factura's emission date as the factura printed it. issued_on is
	// the Guayaquil calendar day stored as a date-only value (guayaquilDate),
	// so it is re-read as a day and NOT shifted through a zone again: a
	// midnight-UTC date moved to Guayaquil would print the day before.
	y, m, d := factura.Invoice.IssuedOn.Date()
	parts.docType = sri.DocumentTypeNotaCredito
	parts.modifies = sri.ModifiedDocument{
		DocumentType: factura.Ecuador.CodDoc,
		Number:       FormatNumber(factura.Ecuador.Estab, factura.Ecuador.PtoEmi, factura.Ecuador.Secuencial),
		IssuedOn:     time.Date(y, m, d, 12, 0, 0, 0, sri.Guayaquil),
	}
	parts.motivo = invoicing.CreditNoteMotivo(note.CreditNoteReason)
	return parts, nil
}

// saleFacturaParts assembles the factura from the owed row's own snapshot:
// the Recipient as the Sale transacted them, one IVA-inclusive line per
// stored line at the price paid, and no additional fields beyond the email
// the builder adds itself.
func saleFacturaParts(base sri.Factura, inv *invoicing.Invoice) (facturaParts, error) {
	recipient, err := sriRecipient(inv.Recipient)
	if err != nil {
		return facturaParts{}, err
	}
	lines := make([]sri.Line, 0, len(inv.Lines))
	for _, l := range inv.Lines {
		code, err := sri.IVACodeFor(l.IVARate)
		if err != nil {
			return facturaParts{}, err
		}
		lines = append(lines, sri.Line{
			Description:    l.Description,
			Quantity:       sri.Quantity(l.QuantityMillionths),
			UnitPriceCents: l.UnitPriceCents,
			IVA:            code,
			IVAInclusive:   true,
		})
	}
	return facturaParts{base: base, recipient: recipient, lines: lines, fields: nil}, nil
}

// undecidedStatus is where a signed document without a definite answer
// stands at now: pending, or needs_attention once 24 h have passed since it
// was signed.
func undecidedStatus(inv *invoicing.Invoice, now time.Time) invoicing.InvoiceStatus {
	if now.Sub(inv.IssuedAt) >= SaleInvoiceAttentionAfter {
		return invoicing.InvoiceStatusNeedsAttention
	}
	return invoicing.InvoiceStatusPending
}

// ladderDelay is the delay before the next round, from how long the
// document has been signed: the first rung until the first delay has
// elapsed, the second until the first two have, and so on; the last rung
// forever after. Read at the moments a healthy drain arrives, that is 1 min,
// 5 min, 15 min, then hourly.
func ladderDelay(elapsed time.Duration) time.Duration {
	var cumulative time.Duration
	for i, d := range SaleInvoiceLadder {
		if i == len(SaleInvoiceLadder)-1 || elapsed < cumulative+d {
			return d
		}
		cumulative += d
	}
	return SaleInvoiceLadder[len(SaleInvoiceLadder)-1]
}
