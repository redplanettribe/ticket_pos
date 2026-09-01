package sri

import (
	"context"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// Authority is the Ecuador adapter behind the core's TaxAuthority seam: it
// carries a prepared factura to the SRI's recepción and asks autorización
// what became of it, and translates the SRI's answers into the core's
// Outcome. It never touches a table, and the core never sees SOAP, XML or an
// SRI error code through it — only the messages, verbatim, as data.
type Authority struct {
	client *Client
}

// Compile-time proof that the adapter fits the seam.
var _ invoicing.TaxAuthority = (*Authority)(nil)

// NewAuthority builds the adapter for one of the core's environments: test
// is SRI pruebas (celcer), production is SRI producción (cel). Options such
// as WithBaseURL and WithHTTPClient reach the underlying Client.
func NewAuthority(env invoicing.Environment, opts ...ClientOption) *Authority {
	return &Authority{client: NewClient(AmbienteFor(env), opts...)}
}

// AuthorityFactory returns the per-environment constructor the core's
// service takes: the Issuer's environment is data and may flip, so the
// adapter is built for whichever one the invoice is issued under. The
// options apply to every environment — the single base-URL override that
// points local development and tests at a fake SRI.
func AuthorityFactory(opts ...ClientOption) func(invoicing.Environment) invoicing.TaxAuthority {
	return func(env invoicing.Environment) invoicing.TaxAuthority {
		return NewAuthority(env, opts...)
	}
}

// AmbienteFor translates the core's environment word into the SRI's digit.
func AmbienteFor(env invoicing.Environment) Environment {
	if env == invoicing.EnvironmentProduction {
		return EnvironmentProduction
	}
	return EnvironmentTest
}

// Submit hands the signed factura to recepción.
//
// RECIBIDA is OutcomeReceived. DEVUELTA is OutcomeRejected with the SRI's
// messages — except when those messages say the clave is already registered
// (43) or in processing (70): the SRI has the document, so the answer is
// "it is there, ask autorización", which is OutcomeReceived with the
// messages kept. That mapping is what makes a resend (#455) never produce a
// duplicate. Transport failures, non-200 answers and SOAP faults are errors.
//
// A DEVUELTA carrying 45 as an error is a refusal by NUMBER (#576, ADR 0068):
// still OutcomeRejected — the SRI did not take the document, and nothing about
// the invoice's status changes — but marked, because it is the one refusal a
// resend under the same secuencial can never mend.
func (a *Authority) Submit(ctx context.Context, doc invoicing.PreparedDocument) (invoicing.Outcome, error) {
	rec, err := a.client.ValidateComprobante(ctx, doc.Body)
	if err != nil {
		return invoicing.Outcome{}, err
	}
	messages := coreMessages(rec.Messages())
	switch rec.State {
	case ReceptionReceived:
		return invoicing.Outcome{State: invoicing.OutcomeReceived, Messages: messages}, nil
	case ReceptionReturned:
		if rec.HasMessage(MessageAccessKeyRegistered) || rec.HasMessage(MessageAccessKeyInProcessing) {
			return invoicing.Outcome{State: invoicing.OutcomeReceived, Messages: messages, AlreadyHeld: true}, nil
		}
		return invoicing.Outcome{State: invoicing.OutcomeRejected, Messages: messages}, nil
	}
	return invoicing.Outcome{}, fmt.Errorf("%w: reception state %q", ErrResponse, rec.State)
}

// QueryOutcome asks autorización about a clave de acceso.
//
// AUTORIZADO is OutcomeAuthorized with the number, date and the SRI's
// autorizacion XML; NO AUTORIZADO is OutcomeNotAuthorized with messages; EN
// PROCESAMIENTO (either spelling) is OutcomeReceived — the SRI holds it and
// is working on it.
//
// A CLAVE THE SRI REPORTS NOTHING ABOUT IS OutcomeUnknown (#514, parent
// #513). numeroComprobantes 0 with an empty autorizaciones list is not "not
// decided yet": it is the SRI saying it has never seen a document under this
// clave, which is what a recepción call that died in transport leaves
// behind. Reading it as received made the platform treat such a document as
// held by the SRI and poll a clave it had never heard of.
//
// For a document sent several times the SRI reports only the last state
// (Ficha §5.11), which is what Latest reads.
func (a *Authority) QueryOutcome(ctx context.Context, accessKey string) (invoicing.Outcome, error) {
	res, err := a.client.AuthorizationComprobante(ctx, accessKey)
	if err != nil {
		return invoicing.Outcome{}, err
	}
	latest := res.Latest()
	if latest == nil {
		return invoicing.Outcome{State: invoicing.OutcomeUnknown}, nil
	}
	messages := coreMessages(latest.Messages)
	switch latest.State {
	case Authorized:
		return invoicing.Outcome{
			State:               invoicing.OutcomeAuthorized,
			Messages:            messages,
			AuthorizationNumber: latest.Number,
			AuthorizationDate:   latest.Date,
			AuthorityXML:        latest.XML,
		}, nil
	case NotAuthorized:
		return invoicing.Outcome{State: invoicing.OutcomeNotAuthorized, Messages: messages}, nil
	case InProcessing:
		return invoicing.Outcome{State: invoicing.OutcomeReceived, Messages: messages}, nil
	}
	return invoicing.Outcome{}, fmt.Errorf("%w: authorization state %q", ErrResponse, latest.State)
}

func coreMessages(in []Message) []invoicing.AuthorityMessage {
	out := make([]invoicing.AuthorityMessage, 0, len(in))
	for _, m := range in {
		out = append(out, invoicing.AuthorityMessage{
			Identifier:     m.Identifier,
			Message:        m.Message,
			AdditionalInfo: m.AdditionalInfo,
			Type:           m.Type,
		})
	}
	return out
}

// IssuerFromSnapshot translates the core's Issuer snapshot into the factura
// builder's emisor: the régimen words the platform stores onto the ones the
// builder prints, and the optional resolution number onto its string.
func IssuerFromSnapshot(s invoicing.IssuerSnapshot) Issuer {
	regime := RegimeGeneral
	switch s.Regimen {
	case invoicing.RegimenRIMPEContribuyente:
		regime = RegimeRIMPE
	case invoicing.RegimenRIMPENegocioPopular:
		regime = RegimeRIMPENegocioPopular
	}
	agente := ""
	if s.AgenteRetencion != nil {
		agente = *s.AgenteRetencion
	}
	return Issuer{
		RUC:                  s.RUC,
		RazonSocial:          s.RazonSocial,
		NombreComercial:      s.NombreComercial,
		DirMatriz:            s.DireccionMatriz,
		DirEstablecimiento:   s.DireccionEstablecimiento,
		Establishment:        s.Establecimiento,
		EmissionPoint:        s.PuntoEmision,
		ObligadoContabilidad: s.ObligadoContabilidad,
		Regime:               regime,
		AgenteRetencion:      agente,
	}
}

// IVACodeFor translates the platform's IVA rate word into the SRI's
// codigoPorcentaje.
func IVACodeFor(rate invoicing.IVARate) (IVACode, error) {
	switch rate {
	case invoicing.IVARate15:
		return IVACode15, nil
	case invoicing.IVARateZero:
		return IVACodeZero, nil
	case invoicing.IVARateExento:
		return IVACodeExento, nil
	case invoicing.IVARateNoObjeto:
		return IVACodeNoObjeto, nil
	}
	return "", fmt.Errorf("%w: unknown IVA rate %q", ErrInvalidFactura, string(rate))
}
