// Package repository provides hand-written SQL data access for the invoicing
// module: the Issuer today, the Tax Invoices and their sequences from #454.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Repository is the invoicing module's data access.
type Repository struct {
	db *platform.DB
}

// New builds a Repository over the shared connection pool.
func New(db *platform.DB) *Repository {
	return &Repository{db: db}
}

// EcuadorIssuerRow is the Ecuador Issuer as stored: the core row joined to its
// detail row.
type EcuadorIssuerRow struct {
	Issuer  invoicing.Issuer
	Details invoicing.EcuadorIssuerDetails
	// DetailsUpdatedAt is when the Ecuador details last changed, as distinct
	// from the core row's updated_at, which also moves on an environment flip.
	DetailsUpdatedAt time.Time
}

const ecuadorIssuerColumns = `
	i.id, i.country, i.environment, i.created_at, i.updated_at,
	e.ruc, e.razon_social, e.nombre_comercial, e.direccion_matriz, e.direccion_establecimiento,
	e.establecimiento, e.punto_emision, e.obligado_contabilidad, e.regimen, e.agente_retencion,
	e.updated_at`

// GetEcuadorIssuer reads the Ecuador Issuer, or nil when none has been
// recorded. Absence is an ordinary state and not an error: the Issuer page
// renders an empty form from it.
func (r *Repository) GetEcuadorIssuer(ctx context.Context) (*EcuadorIssuerRow, error) {
	row, err := scanEcuadorIssuer(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+ecuadorIssuerColumns+`
		FROM invoicing_issuers i
		JOIN invoicing_issuers_ec e ON e.issuer_id = i.id
		WHERE i.country = $1
	`, invoicing.CountryEcuador))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ecuador issuer: %w", err)
	}
	return row, nil
}

// SaveEcuadorIssuer records the Ecuador Issuer, creating it on the first save
// and replacing every detail after. Both rows are written in one transaction
// so that a core row without its details can never be observed.
//
// The details arrive already validated and normalised: whether a RUC is a RUC
// is decided by invoicing.EcuadorIssuerDetails.Normalize, which the handler
// calls, and whether it MAY change is the service's question (#453). This
// method only stores.
func (r *Repository) SaveEcuadorIssuer(ctx context.Context, environment invoicing.Environment, details invoicing.EcuadorIssuerDetails) (*EcuadorIssuerRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin save ecuador issuer: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var issuerID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO invoicing_issuers (country, environment)
		VALUES ($1, $2)
		ON CONFLICT (country) DO UPDATE SET
			environment = EXCLUDED.environment,
			updated_at = NOW()
		RETURNING id
	`, invoicing.CountryEcuador, environment).Scan(&issuerID); err != nil {
		return nil, fmt.Errorf("upsert issuer: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invoicing_issuers_ec
			(issuer_id, ruc, razon_social, nombre_comercial, direccion_matriz, direccion_establecimiento,
			 establecimiento, punto_emision, obligado_contabilidad, regimen, agente_retencion)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (issuer_id) DO UPDATE SET
			ruc = EXCLUDED.ruc,
			razon_social = EXCLUDED.razon_social,
			nombre_comercial = EXCLUDED.nombre_comercial,
			direccion_matriz = EXCLUDED.direccion_matriz,
			direccion_establecimiento = EXCLUDED.direccion_establecimiento,
			establecimiento = EXCLUDED.establecimiento,
			punto_emision = EXCLUDED.punto_emision,
			obligado_contabilidad = EXCLUDED.obligado_contabilidad,
			regimen = EXCLUDED.regimen,
			agente_retencion = EXCLUDED.agente_retencion,
			updated_at = NOW()
	`,
		issuerID,
		details.RUC,
		details.RazonSocial,
		details.NombreComercial,
		details.DireccionMatriz,
		details.DireccionEstablecimiento,
		details.Establecimiento,
		details.PuntoEmision,
		details.ObligadoContabilidad,
		details.Regimen,
		details.AgenteRetencion,
	); err != nil {
		return nil, fmt.Errorf("upsert ecuador issuer details: %w", err)
	}

	row, err := scanEcuadorIssuer(tx.QueryRowContext(ctx, `
		SELECT `+ecuadorIssuerColumns+`
		FROM invoicing_issuers i
		JOIN invoicing_issuers_ec e ON e.issuer_id = i.id
		WHERE i.id = $1
	`, issuerID))
	if err != nil {
		return nil, fmt.Errorf("reread ecuador issuer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit save ecuador issuer: %w", err)
	}
	return row, nil
}

func scanEcuadorIssuer(scanner interface{ Scan(dest ...any) error }) (*EcuadorIssuerRow, error) {
	var row EcuadorIssuerRow
	var agenteRetencion sql.NullString
	if err := scanner.Scan(
		&row.Issuer.ID, &row.Issuer.Country, &row.Issuer.Environment, &row.Issuer.CreatedAt, &row.Issuer.UpdatedAt,
		&row.Details.RUC, &row.Details.RazonSocial, &row.Details.NombreComercial,
		&row.Details.DireccionMatriz, &row.Details.DireccionEstablecimiento,
		&row.Details.Establecimiento, &row.Details.PuntoEmision, &row.Details.ObligadoContabilidad,
		&row.Details.Regimen, &agenteRetencion,
		&row.DetailsUpdatedAt,
	); err != nil {
		return nil, err
	}
	if agenteRetencion.Valid {
		row.Details.AgenteRetencion = &agenteRetencion.String
	}
	return &row, nil
}
