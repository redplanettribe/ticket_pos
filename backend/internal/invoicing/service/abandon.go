package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// Abandon (#578, parent #575, ADR 0068): the Platform Operator's record that
// the Tax Authority never took this document and never will.
//
// WHY IT EXISTS. The SRI's 45, "ERROR SECUENCIAL REGISTRADO", is the one
// refusal no ordinary remedy can mend: Resend carries the same clave and
// secuencial (S1 §5.10), which is the entirety of the authority's objection,
// and #577 refuses it outright. That left the operator two buttons, one of
// which provably cannot work and one of which — Mark annulled — would write
// a true-looking record of a portal annulment that never happened, because
// the portal does not show the number. On 2026-08-30 production's
// 001-001-000000025 and 26 put a real operator in exactly that position.
//
// WHAT IT CLAIMS, AND WHAT IT DOES NOT. `abandoned` says the document was
// SENT, the authority never held it, and it was therefore never a legal
// document: nothing is owed at the portal and nothing is ever declared for
// it. That is a different claim from `annulled` — held, then disowned by
// hand, an obligation discharged there — and from `withdrawn`, which was
// never sent. Three deaths, told apart by what a reader may CONCLUDE.
//
// WHAT IT LEAVES ALONE. Everything. The number, the clave de acceso, the
// signed bytes and every attempt stand forever, and the secuencial stays
// consumed: the sequence only moves forward, and an abandoned number is
// never handed out again. If the authority holds a document at that number
// which is not ours, that is its affair. The act writes a status, an author,
// an instant and an optional note, and nothing else.
//
// ANY KIND. The number is dead whoever typed the document, so a manual Tax
// Invoice is abandoned as readily as a Sale Invoice — which is why the state
// gate admits `rejected` and `not_authorized` beside `needs_attention`: a
// manual document's refusals are recorded as those two and never parked.
// What a manual document does NOT get is a replacement the platform owes;
// it is typed again by hand, as it always was.
//
// NEVER THE DRAINER'S ACT. Abandoning declares that a document was never
// legal, which is a human claim with a human author. The Drainer never does
// it, and a late AUTORIZADO for an abandoned clave must not heal the row:
// applyOutcome writes only against the status the row was READ in, so an
// answer read against needs_attention cannot land on a row since abandoned.
// The answer goes to the attempts ledger and changes nothing. That guard is
// existing and general; nothing here duplicates it.
//
// ISSUE AGAIN (#580) is the other half of #575 and a SEPARATE press, not
// built here. Abandoning without reissuing must stay legal — a manual
// document, or a Sale the operator does not want reinvoiced — and the two
// claims may honestly be made days apart.

// AbandonCheckFreshness is how recently the authority must have been asked
// for its answer to still be the answer the abandonment rests on.
//
// WHAT "FRESH" MEANS HERE, in full, because ADR 0068 states the purpose and
// leaves the reading to the code: the LAST attempt on the document's ledger
// is a query, it carries an answer the authority actually gave, and it was
// started no longer than this ago. All three, and each for its own reason.
//
//   - The last attempt, not merely some recent one. Anything done after the
//     Check makes the Check history: a Resend in between changes what the
//     authority has been asked to consider, and "immediately before the act"
//     is the phrase the ADR uses.
//   - An answer, not an attempt: a query that failed in transport
//     (AttemptOutcomeError) is the platform failing to ask, and the ledger
//     must carry the AUTHORITY's word, not ours.
//   - A time bound, because an answer is only current for so long, and the
//     record the ledger leaves is meant to read as "asked, and then acted".
//     Fifteen minutes is one operator sitting at one page.
//
// WHAT IS DELIBERATELY NOT READ IS THE ANSWER'S VALUE. A decision — the
// authority authorizing or refusing the document — is applied by the Check
// itself, so it moves the status and the state gate answers instead; an
// authorized document is never abandonable, and that is where that case is
// caught. An undecided answer is the authority's current word, whatever it
// says, and the ledger is what carries which word it was. Reading the value
// here would mean a document the authority answers "still processing" about
// has no path at all and no different message for why, which is the shape
// of the very problem this feature exists to remove. ADR 0068 accepts the
// residual window this leaves — an authorization landing between the Check
// and the Abandon — on the strength of the status guard, which keeps the
// data consistent and leaves a reconcilable ledger either way.
const AbandonCheckFreshness = 15 * time.Minute

// AbandonInvoice records that the authority never took the document and
// never will, and returns it as it then stands.
//
// The refusals are decided on the row as read — the document, its ledger and
// the clock — and the write is guarded on the same three states, so a press
// that races a late AUTORIZADO finds no row to update and is told the
// document is not abandonable rather than abandoning an authorized artifact.
// This is Mark annulled's shape (attention.go), narrowed.
func (s *Service) AbandonInvoice(ctx context.Context, id, abandonedBy, note string) (*InvoiceDetail, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	now := s.clock()
	if err := abandonRefusal(&row.Invoice, row.Attempts, now); err != nil {
		return nil, err
	}
	done, err := s.repo.AbandonInvoice(ctx, id, abandonedBy, note, now)
	if err != nil {
		return nil, err
	}
	if !done {
		return nil, invoicing.ErrInvoiceNotAbandonable(row.Invoice.Status)
	}
	s.logger.Info("invoicing: document abandoned",
		"invoice_id", id, "kind", row.Invoice.Kind, "from_status", row.Invoice.Status, "abandoned_by", abandonedBy)
	return s.GetInvoice(ctx, id)
}

// abandonRefusal says whether Abandon may be pressed on the document as it
// stands, in the order a reader should think about it: what the document
// already is, then what the authority did to it, then what the ledger holds.
//
// KIND IS NOT AMONG THE QUESTIONS, deliberately. Every kind is abandonable
// (see the file comment), so the guard never asks.
//
//  1. The shared floor (actionableRefusal): authorized — a legal artifact is
//     never disowned this way — annulled, withdrawn, already abandoned, and
//     unsigned. Answered FIRST wherever more than one refusal applies, so
//     that what a finished document answers never changes because of what
//     the authority once said about its number.
//  2. The state: needs_attention, rejected or not_authorized — the three a
//     refusal leaves a document in. A pending document is still with the
//     authority and is CHECKED, not given up on.
//  3. The refusal by number, read through invoicing.RefusedByNumberIn over
//     the messages STORED ON THE ROW and never the Outcome an adapter built,
//     so the two production documents — refused long before this code — are
//     abandonable the moment it deploys, with no backfill. Whether a
//     document parked for any other reason may be abandoned is deliberately
//     undecided (ADR 0068).
//  4. The fresh Check (AbandonCheckFreshness).
//
// abandonableState is the three states a refusal leaves a document in, and
// so the three Abandon admits. Stated apart from abandonRefusal because
// Mark annulled's guard must ask the same question: it hands a code-45
// document to Abandon (ErrInvoiceAbandonInstead), and it may only do that
// where Abandon would actually take it — otherwise the narrowing invents a
// document with no escape at all rather than pointing at the right one.
func abandonableState(status invoicing.InvoiceStatus) bool {
	switch status {
	case invoicing.InvoiceStatusNeedsAttention, invoicing.InvoiceStatusRejected, invoicing.InvoiceStatusNotAuthorized:
		return true
	}
	return false
}

func abandonRefusal(inv *invoicing.Invoice, attempts []invoicing.Attempt, now time.Time) error {
	if err := actionableRefusal(inv); err != nil {
		return err
	}
	if !abandonableState(inv.Status) {
		return invoicing.ErrInvoiceNotAbandonable(inv.Status)
	}
	if !invoicing.RefusedByNumberIn(inv.Messages) {
		return invoicing.ErrInvoiceNotRefusedByNumber()
	}
	if !checkedFreshly(attempts, now) {
		return invoicing.ErrInvoiceCheckNotFresh()
	}
	return nil
}

// checkedFreshly reports whether the authority's own current answer stands
// at the end of the ledger, timestamped, ready to be the record of why this
// number was never declared. AbandonCheckFreshness states the rule in full
// and why each half of it is there.
func checkedFreshly(attempts []invoicing.Attempt, now time.Time) bool {
	if len(attempts) == 0 {
		return false
	}
	last := attempts[len(attempts)-1]
	if last.Operation != invoicing.AttemptQuery || last.Outcome == invoicing.AttemptOutcomeError {
		return false
	}
	return !last.StartedAt.Before(now.Add(-AbandonCheckFreshness))
}
