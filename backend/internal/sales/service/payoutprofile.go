package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// PayoutProfile is where an Organization is paid, as its own Org Admin reads it
// back (ADR 0025).
//
// The account number is whole here rather than masked. Masking belongs to the
// screens that show many Organizations' details at once — the operator queue —
// and this is the Organization's own editor: an organizer correcting a digit
// needs the digits. Nothing on this surface reaches an Event's staff or any
// Customer-facing page.
type PayoutProfile struct {
	BankName          string `json:"bank_name"`
	AccountType       string `json:"account_type"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
	TaxIDType         string `json:"tax_id_type"`
	TaxIDNumber       string `json:"tax_id_number"`
	// UpdatedAt is when the details last changed, which is the fact an operator
	// about to transfer wants beyond the details themselves.
	UpdatedAt time.Time `json:"updated_at"`
}

// OrganizationPayoutProfile returns the acting Member's Organization's Payout
// Profile, or nil when it has never recorded one.
//
// Nil is an ordinary answer, not a 404: an Organization without a profile is
// every Organization on its first day, and the editor's job in that state is to
// show an empty form rather than an error.
func (s *Service) OrganizationPayoutProfile(ctx context.Context, actor ActorContext) (*PayoutProfile, error) {
	row, err := s.repo.GetPayoutProfile(ctx, actor.OrganizationID)
	if err != nil || row == nil {
		return nil, err
	}
	return payoutProfileView(row.Profile, row.UpdatedAt), nil
}

// SavePayoutProfile records where the acting Member's Organization is paid,
// replacing whatever was there, and returns the profile as stored.
//
// The profile arrives already validated and normalised: whether a set of bank
// details is acceptable is decided by sales.PayoutProfile.Normalize, which the
// handler calls, so this method neither re-checks the check digit nor re-strips
// the dashes. What it adds is the one thing the handler cannot know — which
// Organization is acting — and it takes that from the session rather than the
// body, so no Org Admin can redirect another Organization's settlement.
func (s *Service) SavePayoutProfile(ctx context.Context, actor ActorContext, profile sales.PayoutProfile) (*PayoutProfile, error) {
	row, err := s.repo.SavePayoutProfile(ctx, actor.OrganizationID, profile)
	if err != nil {
		return nil, err
	}
	return payoutProfileView(row.Profile, row.UpdatedAt), nil
}

// payoutProfileView renders a stored profile for the wire. Both entry points go
// through it so a read and a write can never describe the same row differently.
func payoutProfileView(profile sales.PayoutProfile, updatedAt time.Time) *PayoutProfile {
	return &PayoutProfile{
		BankName:          profile.BankName,
		AccountType:       profile.AccountType,
		AccountNumber:     profile.AccountNumber,
		AccountHolderName: profile.AccountHolderName,
		TaxIDType:         profile.TaxIDType,
		TaxIDNumber:       profile.TaxIDNumber,
		UpdatedAt:         updatedAt,
	}
}
