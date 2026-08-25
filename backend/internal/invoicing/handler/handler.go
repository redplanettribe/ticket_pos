// Package handler exposes the Tax invoicing HTTP endpoints. Every route lives
// under /api/v1/operator/invoicing/* and is gated by the operator middleware:
// a Staff Session on the platform operator allowlist and nothing else (ADR
// 0015, ADR 0059). No Member, Integration Partner or Customer route exists.
package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Handler exposes HTTP endpoints for Tax invoicing.
type Handler struct {
	svc *service.Service
}

// New returns an invoicing HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// GetEcuadorIssuer returns the platform's Ecuador Issuer.
//
// @Summary      Get the platform's Ecuador Issuer
// @Description  Returns the platform's registration with the SRI (ADR 0059): the environment it points at (`test` = SRI pruebas, `production` = SRI producción) and the details every factura carries — RUC, razón social, nombre comercial, dirección matriz, dirección del establecimiento, establecimiento and punto de emisión codes, obligado a llevar contabilidad, régimen and the optional agente de retención resolution number. The data payload is `null` when no Issuer has been recorded, which is an ordinary state rather than an error. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeEcuadorIssuer
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/issuers/ec [get]
func (h *Handler) GetEcuadorIssuer(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	issuer, err := h.svc.GetEcuadorIssuer(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, issuer)
}

// ecuadorIssuerBody is the Ecuador Issuer as the operator states it.
type ecuadorIssuerBody struct {
	Environment              string  `json:"environment"`
	RUC                      string  `json:"ruc"`
	RazonSocial              string  `json:"razon_social"`
	NombreComercial          string  `json:"nombre_comercial"`
	DireccionMatriz          string  `json:"direccion_matriz"`
	DireccionEstablecimiento string  `json:"direccion_establecimiento"`
	Establecimiento          string  `json:"establecimiento"`
	PuntoEmision             string  `json:"punto_emision"`
	ObligadoContabilidad     bool    `json:"obligado_contabilidad"`
	Regimen                  string  `json:"regimen"`
	AgenteRetencion          *string `json:"agente_retencion"`
}

// PutEcuadorIssuer records the platform's Ecuador Issuer.
//
// @Summary      Record the platform's Ecuador Issuer
// @Description  Creates the Ecuador Issuer on the first call and replaces its details on every call after (ADR 0059). `environment` is `test` (SRI pruebas) or `production` (SRI producción) and may be switched freely in either direction. `ruc` is validated exactly as the platform validates a `ruc` Tax ID; `establecimiento` and `punto_emision` are three digits each; `regimen` is `general`, `rimpe_contribuyente` or `rimpe_negocio_popular`; `nombre_comercial` may be empty and `agente_retencion` may be null. Every failing field is named at once under VALIDATION_FAILED and nothing is stored when any fails. Returns the Issuer as stored. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  ecuadorIssuerBody  true  "The Issuer's environment and SRI details"
// @Success      200  {object}  openapi.EnvelopeEcuadorIssuer
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/issuers/ec [put]
func (h *Handler) PutEcuadorIssuer(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body ecuadorIssuerBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	input, fields := validateEcuadorIssuer(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	issuer, err := h.svc.SaveEcuadorIssuer(r.Context(), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, issuer)
}

// validateEcuadorIssuer checks the request's shape: the environment is one
// of the two, and the SRI details are well-formed by the domain's own rule.
// The environment is refused alongside the detail fields rather than before
// them, so one answer names everything wrong.
func validateEcuadorIssuer(body ecuadorIssuerBody) (service.SaveEcuadorIssuerInput, []platform.FieldError) {
	var fields []platform.FieldError

	environment := invoicing.Environment(strings.TrimSpace(body.Environment))
	switch {
	case environment == "":
		fields = append(fields, platform.FieldError{Field: "environment", Code: platform.CodeRequired, Message: "is required"})
	case !environment.Valid():
		fields = append(fields, platform.FieldError{
			Field:   "environment",
			Code:    platform.CodeInvalidEnum,
			Message: "must be " + string(invoicing.EnvironmentTest) + " or " + string(invoicing.EnvironmentProduction),
		})
	}

	details, detailErrs := invoicing.EcuadorIssuerDetails{
		RUC:                      body.RUC,
		RazonSocial:              body.RazonSocial,
		NombreComercial:          body.NombreComercial,
		DireccionMatriz:          body.DireccionMatriz,
		DireccionEstablecimiento: body.DireccionEstablecimiento,
		Establecimiento:          body.Establecimiento,
		PuntoEmision:             body.PuntoEmision,
		ObligadoContabilidad:     body.ObligadoContabilidad,
		Regimen:                  body.Regimen,
		AgenteRetencion:          body.AgenteRetencion,
	}.Normalize()
	fields = append(fields, detailErrs...)

	return service.SaveEcuadorIssuerInput{Environment: environment, Details: details}, fields
}
