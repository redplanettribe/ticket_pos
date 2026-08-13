package service

import (
	"context"

	customerssvc "github.com/peter/ticket_pos/backend/internal/customers/service"
)

// The Operator's Consent Withdrawal surface (#271, parent #265): the fourth
// module this service composes, and the first that is about a person rather
// than about money.
//
// It owns nothing, exactly as the rest of this package owns nothing. The
// Customer, the consent state, the write path and the confirmation mail are all
// the customers module's to answer for; this side is where the Operator
// Dashboard's payloads are assembled, so a new operator surface can arrive
// without customers learning that operators exist (ADR 0015).
//
// WHY CUSTOMER CONSENT IS AN OPERATOR SURFACE AND NEVER AN ORGANIZATION ONE.
// Customer identity on this platform is global and separate from staff (ADR
// 0010): the person who bought a ticket from one venue is the same Customer who
// bought from another, and their consents are the platform's relationship with
// them rather than any Organization's. A venue that could inspect or alter them
// would be reading the choices of people who never dealt with it. The gate is
// the namespace's — the operator allowlist, and no Membership at all — which is
// what makes this structural rather than remembered.

// Consents is what the operator surface needs from customers: one Customer
// found by the address on a posted form, and the recording of a withdrawal that
// arrived off the platform.
//
// Deliberately two methods and no more. There is no way to reach a Customer's
// profile, their purchases or anybody else's record, and there is NO WAY TO
// GRANT: the withdrawal input carries answers, and the refusal of an
// affirmative one lives in the consent module's single write path, so it holds
// for this caller and every other.
type Consents interface {
	// CustomerConsentForOperator answers CUSTOMER_NOT_FOUND for an address no
	// Customer holds, which the handler maps to 404.
	CustomerConsentForOperator(ctx context.Context, email string) (*customerssvc.OperatorCustomerConsentView, error)
	// RecordOperatorWithdrawal writes ONE Consent Record through the platform's
	// single consent-write path, carrying the prior state, the operator's email
	// and the artefact reference — and answers CONSENT_GRANT_NOT_PERMITTED to
	// anything that tried to grant.
	RecordOperatorWithdrawal(ctx context.Context, email string, input customerssvc.OperatorWithdrawalInput) (*customerssvc.OperatorCustomerConsentView, error)
}

// LookUpCustomerConsent finds a Customer by email address and reports their
// consent state, across every Organization on the platform — which is not a
// widening of anything, because a Customer was never scoped to one.
//
// Read-only. The withdrawal that hangs off this door is a separate operation,
// and nothing here writes.
func (s *Service) LookUpCustomerConsent(ctx context.Context, email string) (*customerssvc.OperatorCustomerConsentView, error) {
	return s.consents.CustomerConsentForOperator(ctx, email)
}

// RecordCustomerConsentWithdrawal records a Consent Withdrawal that arrived by
// email or on paper, attributed to the operator who entered it and referenced to
// the artefact it answers.
func (s *Service) RecordCustomerConsentWithdrawal(ctx context.Context, email string, input customerssvc.OperatorWithdrawalInput) (*customerssvc.OperatorCustomerConsentView, error) {
	return s.consents.RecordOperatorWithdrawal(ctx, email, input)
}
