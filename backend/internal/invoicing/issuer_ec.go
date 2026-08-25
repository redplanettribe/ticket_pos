package invoicing

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Ecuador Issuer's details: what the SRI expects in every factura's
// infoTributaria and infoFactura, as the operator records them from the RUC
// certificate.
//
// This is Ecuador's detail row in the core package rather than in the `sri`
// adapter for one reason only: the adapter is being built in parallel (#454's
// document kit) and this file must not collide with it. The type is
// self-contained and may be moved under `sri` without changing its shape.
//
// Field names are the SRI's Spanish. These are the SRI's fields, an operator
// reads them off a document written in that language, and translating them
// would put a second name on every one of them (CONTEXT.md "Issuer").

// The three régimenes the factura schema distinguishes. `general` prints
// nothing extra; the two RIMPE values print the SRI's contribuyenteRimpe
// legend on the document. These are the wire values and the stored ones; the
// CHECK constraint in migration 094 is the same set.
const (
	RegimenGeneral             = "general"
	RegimenRIMPEContribuyente  = "rimpe_contribuyente"
	RegimenRIMPENegocioPopular = "rimpe_negocio_popular"
)

// Regimenes lists the accepted régimen values, for messages and forms.
var Regimenes = []string{RegimenGeneral, RegimenRIMPEContribuyente, RegimenRIMPENegocioPopular}

// Length bounds. The SRI's own schema caps razón social and nombre comercial
// at 300 characters and each dirección at 300; the bound here is the SRI's so
// that a value the platform stores is one the factura can carry. The agente
// de retención resolution number is a short code (NAC-DNCRASC20-00000001);
// 50 is generous.
const (
	MaxRazonSocialLength     = 300
	MaxNombreComercialLength = 300
	MaxDireccionLength       = 300
	MaxAgenteRetencionLength = 50
)

// EcuadorIssuerDetails is the Ecuador Issuer as an operator states it: no id,
// no environment, no timestamps. Whose it is and when it was touched are the
// repository's business; whether it points at pruebas or producción is the
// core Issuer's.
type EcuadorIssuerDetails struct {
	RUC                      string
	RazonSocial              string
	NombreComercial          string
	DireccionMatriz          string
	DireccionEstablecimiento string
	Establecimiento          string
	PuntoEmision             string
	ObligadoContabilidad     bool
	Regimen                  string
	// AgenteRetencion is the resolución number when the Issuer is an agente de
	// retención, nil when it is not. Blank is normalised to nil: "not one" has
	// one spelling.
	AgenteRetencion *string
}

// Normalize returns the details in the form they must be stored in, or the
// field-level errors that stop them being stored at all.
//
// Every field is checked, not just the first bad one, so an operator who
// mistyped three things is told three times rather than made to save three
// times. The returned details are only meaningful when no errors come back.
//
// The RUC is judged exactly as the platform judges a `ruc` Tax ID
// (platform.ValidateTaxID): 13 digits, a real province, a check digit for a
// natural person's RUC, structure only for a company's. Establecimiento and
// punto de emisión are three digits each, as they appear in the número de
// comprobante. Nothing here knows about freezes — whether the RUC or the
// numbering MAY change is a question about what has been issued, which is
// the service's to ask (#453); this is only whether the value is well-formed.
func (d EcuadorIssuerDetails) Normalize() (EcuadorIssuerDetails, []platform.FieldError) {
	var errs []platform.FieldError

	n := EcuadorIssuerDetails{
		RUC:                      strings.TrimSpace(d.RUC),
		RazonSocial:              strings.TrimSpace(d.RazonSocial),
		NombreComercial:          strings.TrimSpace(d.NombreComercial),
		DireccionMatriz:          strings.TrimSpace(d.DireccionMatriz),
		DireccionEstablecimiento: strings.TrimSpace(d.DireccionEstablecimiento),
		Establecimiento:          strings.TrimSpace(d.Establecimiento),
		PuntoEmision:             strings.TrimSpace(d.PuntoEmision),
		ObligadoContabilidad:     d.ObligadoContabilidad,
		Regimen:                  strings.TrimSpace(d.Regimen),
	}

	if n.RUC == "" {
		errs = append(errs, required("ruc"))
	} else if ruc, err := platform.ValidateTaxID(platform.TaxIDTypeRUC, n.RUC); err != nil {
		errs = append(errs, platform.FieldError{
			Field:   "ruc",
			Code:    platform.CodeInvalidRUC,
			Message: platform.TaxIDNumberMessage(platform.TaxIDTypeRUC),
		})
	} else {
		n.RUC = ruc
	}

	errs = appendTextErrors(errs, "razon_social", n.RazonSocial, MaxRazonSocialLength, true)
	errs = appendTextErrors(errs, "nombre_comercial", n.NombreComercial, MaxNombreComercialLength, false)
	errs = appendTextErrors(errs, "direccion_matriz", n.DireccionMatriz, MaxDireccionLength, true)
	errs = appendTextErrors(errs, "direccion_establecimiento", n.DireccionEstablecimiento, MaxDireccionLength, true)
	errs = appendCodeErrors(errs, "establecimiento", n.Establecimiento)
	errs = appendCodeErrors(errs, "punto_emision", n.PuntoEmision)

	switch n.Regimen {
	case RegimenGeneral, RegimenRIMPEContribuyente, RegimenRIMPENegocioPopular:
	case "":
		errs = append(errs, required("regimen"))
	default:
		errs = append(errs, platform.FieldError{
			Field:   "regimen",
			Code:    platform.CodeInvalidEnum,
			Message: "must be one of " + strings.Join(Regimenes, ", "),
		})
	}

	if d.AgenteRetencion != nil {
		if trimmed := strings.TrimSpace(*d.AgenteRetencion); trimmed != "" {
			if utf8.RuneCountInString(trimmed) > MaxAgenteRetencionLength {
				errs = append(errs, tooLong("agente_retencion", MaxAgenteRetencionLength))
			}
			n.AgenteRetencion = &trimmed
		}
	}

	return n, errs
}

func required(field string) platform.FieldError {
	return platform.FieldError{Field: field, Code: platform.CodeRequired, Message: "is required"}
}

func tooLong(field string, max int) platform.FieldError {
	return platform.FieldError{
		Field:   field,
		Code:    platform.CodeTooLong,
		Message: "must be at most " + strconv.Itoa(max) + " characters",
	}
}

func appendTextErrors(errs []platform.FieldError, field, value string, max int, isRequired bool) []platform.FieldError {
	switch {
	case value == "" && isRequired:
		return append(errs, required(field))
	case utf8.RuneCountInString(value) > max:
		return append(errs, tooLong(field, max))
	}
	return errs
}

// appendCodeErrors judges a three-digit SRI code — establecimiento or punto de
// emisión. ASCII digits only, for the reason platform/taxid.go gives: a code
// typed with any other digits is not one the SRI will accept.
func appendCodeErrors(errs []platform.FieldError, field, value string) []platform.FieldError {
	if value == "" {
		return append(errs, required(field))
	}
	if !isThreeASCIIDigits(value) {
		return append(errs, platform.FieldError{
			Field:   field,
			Code:    platform.CodeInvalidSRICode,
			Message: "must be exactly 3 digits",
		})
	}
	return errs
}

func isThreeASCIIDigits(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
