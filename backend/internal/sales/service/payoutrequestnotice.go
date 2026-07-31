package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// The three emails a Payout Request sends (#179, ADR 0026), and the platform's
// first organizer-facing notification channel — ADR 0019 recorded that none
// existed.
//
// They close the three loops that would otherwise each become a support
// message: the operator allowlist learns an ask arrived, and the asker learns it
// was paid, or why it was not. They are three notices and not a notification
// system. There are no preferences, no digest and no inbox, and the next
// organizer-facing notice will find a path already cut; widening it into
// something general is a decision nobody has made (ADR 0026).
//
// FOUR RULES HOLD ACROSS ALL THREE.
//
// EVERY FAILURE IS SWALLOWED. Not one of these functions returns an error, and
// that is the interface rather than an oversight. Each is called AFTER the
// transaction that recorded the money has committed, so there is nothing left to
// roll back and nothing a caller could do with a failure except lie about what
// happened. A Resend outage that failed a fulfilment would tell an operator who
// had already wired money by hand that they had not — strictly worse than an
// organizer who never gets an email. The money record is the fact; the email is
// the courtesy (ADR 0019).
//
// THE RECIPIENT IS A RECORDED EMAIL. Both answers go to the request's
// `requested_by` — one request, one asker, one reply — and never to every Org
// Admin. It is an email rather than a member id exactly so it still resolves
// after that person's Membership ends.
//
// NO BANK DETAIL IS EVER PASSED IN. The notice structs have no field for one;
// what they carry is an amount, a name and a reason (see platform/email.go).
//
// DELIVERY IS SYNCHRONOUS, on the request that provoked it, as every other
// notice in this system is. The bound is the sender's own HTTP timeout, and the
// allowlist these fan out to is a handful of people rather than a mailing list.

// notifyPayoutRequestSubmitted tells every Platform Operator that an
// Organization has asked to be paid.
//
// This is the notice with the clearest reason to exist. The pending count on the
// operator navigation only works for somebody who already decided to look, and a
// Friday-evening request otherwise waits until somebody happens to click
// (ADR 0026).
//
// It is called only when a request was newly RECORDED. A repeated submission is
// handed the outstanding request back rather than writing a second one, and an
// organizer who pressed twice has not asked twice.
//
// An unconfigured allowlist reader, an empty allowlist and a read that fails are
// all the same outcome — nobody is told — and none of them is worth failing a
// recorded request over.
func (s *Service) notifyPayoutRequestSubmitted(ctx context.Context, request *repository.PayoutRequestRow) {
	if s.operators == nil {
		return
	}
	recipients, err := s.operators.PlatformOperatorEmails(ctx)
	if err != nil {
		s.logger.Error("payout request submitted notice: read operator allowlist", "request_id", request.ID, "error", err)
		return
	}
	if len(recipients) == 0 {
		return
	}

	org, ok := s.payoutNoticeOrganization(ctx, request, "submitted")
	if !ok {
		return
	}

	// The organizer's note is optional on the record and optional in the email:
	// an empty line labelled "Note" reads as a fault in the platform, and it is
	// the only part of the message an operator cannot get from the dashboard.
	note := ""
	if request.Note != nil {
		note = *request.Note
	}

	for _, to := range recipients {
		_ = s.email.SendPayoutRequestSubmitted(ctx, platform.PayoutRequestSubmitted{
			To:               to,
			OrganizationName: org.Name,
			AmountCents:      request.AmountCents,
			Currency:         org.Currency,
			RequestedBy:      request.RequestedBy,
			Note:             note,
		})
	}
}

// notifyPayoutRequestPaid tells the asker the transfer has been made, so they
// know to look at their bank.
//
// paidCents is what ACTUALLY moved and the request carries what was asked. The
// two are allowed to differ — an operator who transfers less records the smaller
// figure, and partial fulfilment is deliberately not modelled — so both are
// handed to the notice, which is the only place an organizer is ever told about
// the gap (ADR 0026).
func (s *Service) notifyPayoutRequestPaid(ctx context.Context, request *repository.PayoutRequestRow, paidCents int) {
	org, ok := s.payoutNoticeOrganization(ctx, request, "paid")
	if !ok {
		return
	}
	_ = s.email.SendPayoutRequestPaid(ctx, platform.PayoutRequestPaid{
		To:               request.RequestedBy,
		OrganizationName: org.Name,
		AmountCents:      paidCents,
		RequestedCents:   request.AmountCents,
		Currency:         org.Currency,
	})
}

// notifyPayoutRequestDeclined tells the asker the ask was refused, and why.
//
// The reason is read off the row that was just written rather than from the
// operator's input, so the email quotes what is stored: the two cannot disagree,
// and the organizer reading their payouts page and the organizer reading their
// inbox are reading the same sentence. A row that somehow carries none sends
// nothing — a decline notice with a blank reason is the outcome requiring a
// reason exists to prevent (ADR 0026).
func (s *Service) notifyPayoutRequestDeclined(ctx context.Context, request *repository.PayoutRequestRow) {
	if request.ResolutionReason == nil || *request.ResolutionReason == "" {
		s.logger.Error("payout request declined notice: no reason on the declined request", "request_id", request.ID)
		return
	}
	org, ok := s.payoutNoticeOrganization(ctx, request, "declined")
	if !ok {
		return
	}
	_ = s.email.SendPayoutRequestDeclined(ctx, platform.PayoutRequestDeclined{
		To:               request.RequestedBy,
		OrganizationName: org.Name,
		AmountCents:      request.AmountCents,
		Currency:         org.Currency,
		Reason:           *request.ResolutionReason,
	})
}

// payoutNoticeOrganization reads what all three notices need and none of them
// carries: the Organization's name, and the currency its money is stated in.
//
// A request's own row has neither — it names an Organization by id, and every
// figure on it is implicitly in that Organization's currency. The false return
// means "do not send", which is the correct answer to both failures it covers: a
// read that failed, and an id naming no Organization at all. Neither is worth
// failing a committed money record over, and an email quoting an amount with no
// currency would be worse than silence.
func (s *Service) payoutNoticeOrganization(ctx context.Context, request *repository.PayoutRequestRow, notice string) (repository.PayoutNoticeOrganization, bool) {
	org, err := s.repo.GetPayoutNoticeOrganization(ctx, request.OrganizationID)
	if err != nil {
		s.logger.Error("payout request notice: read organization", "notice", notice, "request_id", request.ID, "error", err)
		return repository.PayoutNoticeOrganization{}, false
	}
	if org == nil {
		s.logger.Error("payout request notice: no such organization", "notice", notice, "request_id", request.ID)
		return repository.PayoutNoticeOrganization{}, false
	}
	return *org, true
}
