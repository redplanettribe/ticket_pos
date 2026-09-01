package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers"
)

// The Operator's half of the Consent Withdrawal (#271, parent #265): recording
// a withdrawal that arrived off the platform.
//
// IT USED TO BEGIN BY FINDING A CUSTOMER FROM THE ADDRESS ON A FORM, and #566
// took that half away. The operator now reaches this act from the person's
// per-subject record in the Legal Center, having reached THAT from the
// acceptance browser's search — so the Customer is already resolved and arrives
// as an opaque id. What is gone with it is the last route on this platform that
// carried a data subject's email in a request line.
//
// WHY THIS LIVES IN THE CUSTOMERS MODULE rather than in the operator one. The
// operator module owns no tables and composes what other modules answer for
// (ADR 0015), and the two things this act needs are both here: the Customer the
// id resolves to, and confirmWithdrawal — the mechanism #267 built for telling
// somebody a withdrawal took something away and stamping the evidence that they
// were told. Building a second confirmation path for the one channel where the
// Customer is not the actor would be the surest way to end up with a channel
// that quietly does not confirm.
//
// WHAT IS DIFFERENT ABOUT THIS CHANNEL, and it is exactly two things: the act
// carries who recorded it and which artefact it answers. Everything else — the
// single write path, the prior state, the lockstep with the Follow Digest, the
// confirmation mail and its stamp — is identical to a Customer switching their
// own toggle, and it is identical because it is literally the same code.
//
// WHAT THIS SURFACE MAY NOT DO. It may not grant. The refusal is not written
// here: it lives in the consent module's write path, so it holds for every
// caller and not merely for the one that remembered (service.refuseGrant-
// OnOperatorRequest). Nothing below filters the answers it was handed, on
// purpose — a filter here would make the guarantee this module's discipline
// instead of the platform's rule, and would hide from the test the thing the
// test exists to prove.

// OperatorCustomerConsentView is what an Operator is shown about one Customer:
// enough to be sure they have the right person, and what a withdrawal would
// actually change.
//
// It is deliberately NOT the Customer's profile. An Operator honouring a
// withdrawal form needs to identify a human being and read three consent facts;
// they do not need a phone number, a Tax ID, an Avatar or a purchase history,
// and a payload that carried them would be a cross-Organization view of
// somebody's personal data justified by a consent ticket.
type OperatorCustomerConsentView struct {
	Customer OperatorCustomerIdentity `json:"customer"`
	Consent  OperatorConsentStateView `json:"consent"`
	// Withdrew is what the act on this request TOOK AWAY, and is null on the
	// lookup, which took nothing away because it performed nothing.
	//
	// It is reported rather than left for the caller to infer from the state
	// beside it, because it cannot be inferred: `denied` reads the same whether
	// somebody just gave something up or was already refusing. It is also the
	// answer to "was anybody written to?" — the confirmation mail is sent on
	// exactly this predicate.
	Withdrew *OperatorWithdrewView `json:"withdrew"`
}

// OperatorCustomerIdentity names the person the act is about.
type OperatorCustomerIdentity struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// FirstName and LastName as the platform holds them, so an Operator can
	// check the name on the form against the record before acting on it.
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// OperatorConsentStateView is the consent state on the wire.
//
// The two optional consents are POINTERS, and null means UNANSWERED — which is
// a different fact from denied and is published as the different fact it is. An
// Operator deciding what a form changes must not be shown a refusal the Customer
// never made, and a payload that spelled "never asked" as "denied" would be
// putting words in somebody's mouth in a compliance surface.
type OperatorConsentStateView struct {
	MarketingConsent  *string `json:"marketing_consent"`
	NetworkingConsent *string `json:"networking_consent"`
	// PolicyAcceptedAt is when this Customer last accepted a Policy Version,
	// RFC 3339, null where no acceptance was ever recorded.
	//
	// It is shown and is NOT actionable. Policy Acceptance is not withdrawable
	// (ADR 0038): it is absent from counsel's form, it gates the platform on a
	// basis other than consent, and clearing it would re-gate the person rather
	// than free them.
	PolicyAcceptedAt *string `json:"policy_accepted_at"`
}

// OperatorWithdrewView is which optional consents one recorded act took away.
type OperatorWithdrewView struct {
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
}

// OperatorWithdrawalInput is a withdrawal as an Operator states it, already
// validated for shape by the handler.
//
// The two consents are *bool carrying the same meaning they carry everywhere
// else in this feature: nil is THE BOX WAS NOT NAMED ON THIS FORM and false is
// an answer of No. True is a grant, and is not filtered out here — see the file
// comment above.
type OperatorWithdrawalInput struct {
	MarketingConsent  *bool
	NetworkingConsent *bool
	// RequestReference names the inbound artefact. Required, and required by the
	// handler rather than here because "the Operator must say what they are
	// holding" is a rule about the request.
	RequestReference string
	// RecordedBy is the acting operator's Staff Session email. Never a value the
	// request body could name.
	RecordedBy string
	// Evidence is the circumstances of the RECORDING — the Operator's own
	// browser and session, not the Customer's. That is the honest reading of the
	// technical proof on this channel: it corroborates who sat at a keyboard and
	// entered a form, which is the only technical fact there is when the act
	// itself happened on paper.
	Evidence consent.Evidence
}

// RecordOperatorWithdrawal records a Consent Withdrawal that arrived off the
// platform, on the Customer's behalf and under the Operator's name.
//
// IT IS ONE CAPTURE THROUGH THE ONE WRITE PATH, and every guarantee that path
// makes therefore holds here without being restated: the state and the immutable
// Consent Record commit together, the prior state is observed under the Customer
// row's lock, `digest_enabled` moves in lockstep with Marketing Consent (ADR
// 0034), and a grant is refused. There is no second withdrawal mechanism and no
// second state machine — the parent spec is explicit that a withdrawal is a
// capture act whose answers are false (ADR 0038).
//
// WHY THIS ACT IS RECORDED AS EmailProven DESPITE NOBODY PROVING ANYTHING IN
// THIS REQUEST, which is the one genuinely debatable decision here and is the
// same debate the unsubscribe link settled the same way (#256):
//
//   - PROOF OF OWNERSHIP IS WHAT THE ARTEFACT IS. The act answers a signed form
//     or an email from the address, examined by a human who is named on the
//     record and who has named where the paper is. `email_proven` is an EXPLICIT
//     INPUT and never inferred from the channel (migration 061) — this caller is
//     stating what it knows about this act, which is that the request came from
//     the person it is about, established off-platform rather than by a passcode.
//   - AND THE ANSWER IS ALWAYS NO. An unproven answer of No writes NOTHING at
//     all (consent/service.optionalStateWrite), so recording this as unproven
//     would silently make the entire feature a no-op for exactly the Customers it
//     exists for: everybody who had granted something. This surface can never
//     grant — the write path refuses it — so treating it as proven can only ever
//     take something away, never authorize anything.
//
// The confirmation mail and the stamp are #267's, reused exactly: sent only when
// something moved, to the Customer's OWN stored address rather than anything the
// request named, in their Mail Locale, and never allowed to fail the withdrawal.
// A person who wrote in learns their request was actioned; a person for whom
// nothing moved is not told about a change that did not happen.
func (s *Service) RecordOperatorWithdrawal(ctx context.Context, customerID string, input OperatorWithdrawalInput) (*OperatorCustomerConsentView, error) {
	// BY ID, AND NO LONGER BY ADDRESS (#566). The Operator reaches this act
	// from the per-subject record they are already reading, which they reached
	// by an opaque UUID from the acceptance browser's search — so the address
	// that used to key this route is gone from the request line, and with it
	// the last place on the platform a data subject's email travelled in a URL.
	// A person's address is still the thing an operator recognises them by; it
	// simply lives in the body of what they read, not in what they typed.
	customer, err := s.repo.GetCustomerByID(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		return nil, customers.ErrCustomerNotFound()
	}

	receipt, err := s.consent.Capture(ctx, consent.Capture{
		CustomerID: customer.ID,
		// The Customer's own stored address. The address the Operator typed is
		// how this Customer was FOUND; what the record is about is the person it
		// resolved to, and the two differ whenever somebody types a mixed-case or
		// otherwise unnormalised form of it.
		Email:   customer.Email,
		Channel: consent.ChannelOperatorRequest,
		// See the doc comment above.
		EmailProven: true,
		// Handed over exactly as they arrived. Policy Acceptance is absent and
		// always will be: it is not withdrawable, and an operator-recorded
		// acceptance would be a staff member asserting that somebody agreed to a
		// document.
		Answers: consent.Answers{
			MarketingConsent:  input.MarketingConsent,
			NetworkingConsent: input.NetworkingConsent,
		},
		Evidence:         input.Evidence,
		RecordedBy:       input.RecordedBy,
		RequestReference: input.RequestReference,
	})
	if err != nil {
		return nil, err
	}

	s.confirmWithdrawal(ctx, customer, receipt)

	return operatorConsentView(customer.ID, customer.Email, customer.FirstName, customer.LastName,
		consent.PrivacyState{
			// What is true AFTER the act, as the write itself reported it rather
			// than as a second read would find it: the transaction has committed
			// and the world is allowed to move on, so re-reading could report a
			// state this act did not produce.
			MarketingConsent:  receipt.MarketingConsent,
			NetworkingConsent: receipt.NetworkingConsent,
			// Untouched by a withdrawal: the capture showed no Policy Acceptance
			// box, so the standing acceptance stands. It is re-read because the
			// receipt does not carry it.
			PolicyAcceptedAt: s.policyAcceptedAt(ctx, customer.ID),
		},
		&OperatorWithdrewView{
			MarketingConsent:  receipt.Withdrawn.MarketingConsent,
			NetworkingConsent: receipt.Withdrawn.NetworkingConsent,
		}), nil
}

// policyAcceptedAt re-reads the one fact the receipt does not carry. A failed
// read is reported as "no acceptance recorded" rather than failing the response:
// the withdrawal has committed and is the thing that had to happen, and an
// Operator being shown one blank field is not a reason to tell them their act
// failed when it did not.
func (s *Service) policyAcceptedAt(ctx context.Context, customerID string) time.Time {
	state, err := s.consent.PrivacyState(ctx, customerID)
	if err != nil {
		s.logger.Error("operator consent view could not re-read the policy acceptance",
			"customer_id", customerID, "error", err)
		return time.Time{}
	}
	return state.PolicyAcceptedAt
}

// operatorConsentView assembles the payload both operations answer with, so the
// lookup and the withdrawal can only ever describe a Customer one way.
func operatorConsentView(id, email, firstName, lastName string, state consent.PrivacyState, withdrew *OperatorWithdrewView) *OperatorCustomerConsentView {
	return &OperatorCustomerConsentView{
		Customer: OperatorCustomerIdentity{
			ID:        id,
			Email:     email,
			FirstName: firstName,
			LastName:  lastName,
		},
		Consent: OperatorConsentStateView{
			MarketingConsent:  optionalState(state.MarketingConsent),
			NetworkingConsent: optionalState(state.NetworkingConsent),
			PolicyAcceptedAt:  optionalInstant(state.PolicyAcceptedAt),
		},
		Withdrew: withdrew,
	}
}

// optionalState renders one consent state, or null where it was never answered.
// The empty State is the absence of an answer and not a fourth value, so it
// becomes JSON null rather than an empty string a client might render as a word.
func optionalState(state consent.State) *string {
	if state == "" {
		return nil
	}
	value := string(state)
	return &value
}

// optionalInstant renders a timestamp, or null where there is none.
func optionalInstant(at time.Time) *string {
	if at.IsZero() {
		return nil
	}
	value := at.UTC().Format(time.RFC3339)
	return &value
}
