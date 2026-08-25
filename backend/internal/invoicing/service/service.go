// Package service holds the invoicing module's business rules: today, the
// Ecuador Issuer as an operator records it; from #452 its certificate; from
// #453 what may no longer change once documents exist; from #454 the issue
// flow through the TaxAuthority seam.
package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Service is the invoicing module's business logic.
type Service struct {
	repo   *repository.Repository
	logger platform.Logger
}

// New builds the invoicing Service.
func New(repo *repository.Repository, logger platform.Logger) *Service {
	return &Service{repo: repo, logger: logger}
}

// EcuadorIssuer is the Ecuador Issuer as the operator surface reads it: the
// core row and the SRI details flattened into one payload, because there is
// exactly one of each and the page shows them as one form.
//
// Certificate metadata (#452) and the freeze flags the form greys fields on
// (#453) are added to this shape rather than served beside it, so the page
// keeps loading one thing.
type EcuadorIssuer struct {
	ID                       string    `json:"id"`
	Country                  string    `json:"country"`
	Environment              string    `json:"environment"`
	RUC                      string    `json:"ruc"`
	RazonSocial              string    `json:"razon_social"`
	NombreComercial          string    `json:"nombre_comercial"`
	DireccionMatriz          string    `json:"direccion_matriz"`
	DireccionEstablecimiento string    `json:"direccion_establecimiento"`
	Establecimiento          string    `json:"establecimiento"`
	PuntoEmision             string    `json:"punto_emision"`
	ObligadoContabilidad     bool      `json:"obligado_contabilidad"`
	Regimen                  string    `json:"regimen"`
	AgenteRetencion          *string   `json:"agente_retencion"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// SaveEcuadorIssuerInput is what a save carries: the environment on the core
// Issuer and the SRI details, already normalised by the handler.
type SaveEcuadorIssuerInput struct {
	Environment invoicing.Environment
	Details     invoicing.EcuadorIssuerDetails
}

// GetEcuadorIssuer returns the Ecuador Issuer, or nil when none has been
// recorded yet — an ordinary state, not an error.
func (s *Service) GetEcuadorIssuer(ctx context.Context) (*EcuadorIssuer, error) {
	row, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil || row == nil {
		return nil, err
	}
	return ecuadorIssuerView(row), nil
}

// SaveEcuadorIssuer records the Ecuador Issuer, creating it on the first save
// and updating it after, and returns it as stored.
//
// The input arrives validated: well-formedness is
// invoicing.EcuadorIssuerDetails.Normalize's verdict and the handler asks for
// it. What this method will add, with #453, is the question only the service
// can answer — whether the RUC may change given that Tax Invoices exist, and
// whether establecimiento / punto de emisión may change given that a
// sequence has started under them. Until then every save is unconditional.
func (s *Service) SaveEcuadorIssuer(ctx context.Context, input SaveEcuadorIssuerInput) (*EcuadorIssuer, error) {
	row, err := s.repo.SaveEcuadorIssuer(ctx, input.Environment, input.Details)
	if err != nil {
		return nil, err
	}
	return ecuadorIssuerView(row), nil
}

func ecuadorIssuerView(row *repository.EcuadorIssuerRow) *EcuadorIssuer {
	updatedAt := row.Issuer.UpdatedAt
	if row.DetailsUpdatedAt.After(updatedAt) {
		updatedAt = row.DetailsUpdatedAt
	}
	return &EcuadorIssuer{
		ID:                       row.Issuer.ID,
		Country:                  string(row.Issuer.Country),
		Environment:              string(row.Issuer.Environment),
		RUC:                      row.Details.RUC,
		RazonSocial:              row.Details.RazonSocial,
		NombreComercial:          row.Details.NombreComercial,
		DireccionMatriz:          row.Details.DireccionMatriz,
		DireccionEstablecimiento: row.Details.DireccionEstablecimiento,
		Establecimiento:          row.Details.Establecimiento,
		PuntoEmision:             row.Details.PuntoEmision,
		ObligadoContabilidad:     row.Details.ObligadoContabilidad,
		Regimen:                  row.Details.Regimen,
		AgenteRetencion:          row.Details.AgenteRetencion,
		CreatedAt:                row.Issuer.CreatedAt,
		UpdatedAt:                updatedAt,
	}
}
