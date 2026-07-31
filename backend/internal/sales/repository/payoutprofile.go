package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
)

// PayoutProfileRow is an Organization's stored Payout Profile: the details
// themselves plus when they last changed. UpdatedAt is what tells an operator
// about to transfer that the account was edited this morning, so it is read back
// with the profile rather than left in the table.
type PayoutProfileRow struct {
	Profile   sales.PayoutProfile
	UpdatedAt time.Time
}

// GetPayoutProfile returns the Organization's Payout Profile, or a nil row when
// it has never recorded one.
//
// Absence is an ordinary answer and not an error: every Organization starts
// without a profile, and the editor's whole job is to be shown an empty form in
// that state. Callers distinguish the two by the nil, so no sentinel error is
// needed.
func (r *Repository) GetPayoutProfile(ctx context.Context, orgID string) (*PayoutProfileRow, error) {
	var row PayoutProfileRow
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT bank_name, account_type, account_number, account_holder_name,
		       tax_id_type, tax_id_number, updated_at
		FROM organization_payout_profiles
		WHERE organization_id = $1
	`, orgID).Scan(
		&row.Profile.BankName,
		&row.Profile.AccountType,
		&row.Profile.AccountNumber,
		&row.Profile.AccountHolderName,
		&row.Profile.TaxIDType,
		&row.Profile.TaxIDNumber,
		&row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SavePayoutProfile stores where the Organization is paid and returns the row as
// stored, so the caller renders what the database holds rather than what it was
// handed.
//
// It is an upsert onto the unique organization_id, which is what makes "the
// Organization's bank account" a phrase with one referent: an organizer changing
// banks corrects their profile in place, and there is never a second row to
// choose between. Every column is replaced — the profile is stated whole or not
// at all (ADR 0026) — and created_at deliberately is not, so the row keeps
// saying when this Organization first told the platform where to pay it.
func (r *Repository) SavePayoutProfile(ctx context.Context, orgID string, profile sales.PayoutProfile) (*PayoutProfileRow, error) {
	var row PayoutProfileRow
	err := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO organization_payout_profiles
			(organization_id, bank_name, account_type, account_number, account_holder_name, tax_id_type, tax_id_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (organization_id) DO UPDATE SET
			bank_name = EXCLUDED.bank_name,
			account_type = EXCLUDED.account_type,
			account_number = EXCLUDED.account_number,
			account_holder_name = EXCLUDED.account_holder_name,
			tax_id_type = EXCLUDED.tax_id_type,
			tax_id_number = EXCLUDED.tax_id_number,
			updated_at = NOW()
		RETURNING bank_name, account_type, account_number, account_holder_name,
		          tax_id_type, tax_id_number, updated_at
	`,
		orgID,
		profile.BankName,
		profile.AccountType,
		profile.AccountNumber,
		profile.AccountHolderName,
		profile.TaxIDType,
		profile.TaxIDNumber,
	).Scan(
		&row.Profile.BankName,
		&row.Profile.AccountType,
		&row.Profile.AccountNumber,
		&row.Profile.AccountHolderName,
		&row.Profile.TaxIDType,
		&row.Profile.TaxIDNumber,
		&row.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}
