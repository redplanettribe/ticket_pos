package sri

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// ConsultaComprobante: the SRI's status service (#744, spec #742, ADR 0078).
//
// It is the one thing the SRI tells a machine about an annulment: its
// consultarEstadoAutorizacionComprobante answers estadoAutorizacion, which
// the Ficha documents as AUTORIZADO, NO AUTORIZADO, PENDIENTE DE ANULAR or
// ANULADO. autorizacionComprobante cannot say it, since it returns the stored
// authorization record.
//
// ONLY A POSITIVE ANSWER IS EVIDENCE (#716). Probed from production on
// 2026-11-15, the service answered an authorized factura AUTORIZADO on 8
// calls in 10 and refused it on the other two with "fecha de emision fuera
// del rango permitido" - the same refusal it gives a clave it never held - and
// 6 consultas in 40 came back as an HTTP 302 to an HTML page. So every answer
// is classified into exactly three readings, and everything that is not
// estadoAutorizacion present and reading ANULADO or AUTORIZADO is no reading:
// any refusal, any other state, a redirect, a non-SOAP body, a reset, a
// timeout. ANULADO was first observed on 2026-11-16, for factura 238 annulled
// at the SRI portal, and reads exactly so; it is still matched case- and
// whitespace-insensitively.

const (
	// ConsultaPath is the ConsultaComprobante service path, on cel and celcer
	// alike.
	ConsultaPath = "/comprobantes-electronicos-ws/ConsultaComprobante"

	consultaNamespace = "http://ec.gob.sri.ws.consultas"

	// ConsultaRefused is the estadoConsulta of a refusal; it carries mensajes
	// and no estadoAutorizacion.
	ConsultaRefused = "RECHAZADA"
)

// The estadoAutorizacion words that are a reading.
const (
	consultaAnnulled   = "ANULADO"
	consultaAuthorized = "AUTORIZADO"
)

// ConsultaReading is one answer of consultarEstadoAutorizacionComprobante,
// classified.
type ConsultaReading struct {
	Reading invoicing.StatusReading
	// State is the estadoAutorizacion as sent, trimmed; "" on a refusal or
	// when no SOAP answer was read.
	State string
	// Messages are the SRI's mensajes, verbatim: on a refusal, its reason.
	Messages []Message
	// Problem says why there is no reading when the SRI gave no refusal of
	// its own: a non-200 status, a body that is not its SOAP, a fault, an
	// estadoAutorizacion it does not document as a reading. "" otherwise.
	Problem string
}

// BuildConsultarEstadoRequest renders the SOAP envelope of
// consultarEstadoAutorizacionComprobante for one clave de acceso, as the
// #716 probe sent it.
func BuildConsultarEstadoRequest(accessKey string) []byte {
	var b bytes.Buffer
	b.WriteString(xmlDeclaration)
	b.WriteString(`<soapenv:Envelope xmlns:soapenv="` + soapNamespace + `" xmlns:ec="` + consultaNamespace + `">`)
	b.WriteString(`<soapenv:Header/><soapenv:Body><ec:consultarEstadoAutorizacionComprobante><claveAcceso>`)
	xml.EscapeText(&b, []byte(accessKey))
	b.WriteString(`</claveAcceso></ec:consultarEstadoAutorizacionComprobante></soapenv:Body></soapenv:Envelope>`)
	return b.Bytes()
}

// consultaFields is EstadoAutorizacionComprobante as the WSDL defines it.
type consultaFields struct {
	EstadoConsulta     string    `xml:"estadoConsulta"`
	ClaveAcceso        string    `xml:"claveAcceso"`
	Mensajes           []Message `xml:"mensajes>mensaje"`
	EstadoAutorizacion *string   `xml:"estadoAutorizacion"`
	TipoComprobante    string    `xml:"tipoComprobante"`
	RucEmisor          string    `xml:"rucEmisor"`
	FechaAutorizacion  string    `xml:"fechaAutorizacion"`
}

type consultaEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Fault *struct {
			Code   string `xml:"faultcode"`
			String string `xml:"faultstring"`
		} `xml:"Fault"`
		Response *struct {
			// The fields sit in an element named after their WSDL type, not
			// in the <return> a JAX-WS answer usually has. Reading them
			// anywhere else is what made every answer read as empty until
			// 2026-11-16, so no other wrapper is accepted.
			Estado *consultaFields `xml:"EstadoAutorizacionComprobante"`
		} `xml:"consultarEstadoAutorizacionComprobanteResponse"`
	} `xml:"Body"`
}

// ClassifyConsulta reads one HTTP answer of the status service into its
// reading. It never fails: whatever cannot be read is StatusNone with the
// reason in Problem.
func ClassifyConsulta(statusCode int, body []byte) ConsultaReading {
	var env consultaEnvelope
	err := xml.Unmarshal(body, &env)
	if err == nil && env.Body.Fault != nil {
		return none((&SOAPFault{Code: env.Body.Fault.Code, String: env.Body.Fault.String}).Error())
	}
	if statusCode != http.StatusOK {
		return none(fmt.Sprintf("sri: http %d", statusCode))
	}
	if err != nil {
		return none(fmt.Sprintf("%v: not a SOAP answer: %v", ErrResponse, err))
	}
	if env.Body.Response == nil {
		return none(fmt.Sprintf("%v: no consultarEstadoAutorizacionComprobanteResponse", ErrResponse))
	}
	if env.Body.Response.Estado == nil {
		return none(fmt.Sprintf("%v: no EstadoAutorizacionComprobante in the response", ErrResponse))
	}
	fields := *env.Body.Response.Estado
	out := ConsultaReading{Reading: invoicing.StatusNone, Messages: trimMessages(fields.Mensajes)}
	if fields.EstadoAutorizacion == nil {
		if normalised(fields.EstadoConsulta) == ConsultaRefused {
			// The SRI's own refusal: its messages say why, and it is no
			// reading whatever they say.
			return out
		}
		out.Problem = fmt.Sprintf("%v: neither estadoAutorizacion nor a refusal (estadoConsulta %q)", ErrResponse, strings.TrimSpace(fields.EstadoConsulta))
		return out
	}
	out.State = strings.TrimSpace(*fields.EstadoAutorizacion)
	switch normalised(out.State) {
	case consultaAnnulled:
		out.Reading = invoicing.StatusAnnulled
	case consultaAuthorized:
		out.Reading = invoicing.StatusAuthorized
	default:
		out.Problem = fmt.Sprintf("sri: estadoAutorizacion %q is not a reading", out.State)
	}
	return out
}

// none is no reading, for the given reason.
func none(problem string) ConsultaReading {
	return ConsultaReading{Reading: invoicing.StatusNone, Problem: problem}
}

// normalised folds case and every run of whitespace, so "  anulado ",
// "Anulado" and "ANULADO" read alike, and "NO  AUTORIZADO" reads as
// "NO AUTORIZADO".
func normalised(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// ConsultarEstado asks the status service about one clave and classifies the
// answer. A transport failure (a reset, a timeout) is a reading too: none,
// with the error as its Problem. A redirect is never followed - the SRI's edge
// sends one to an HTML page - so it reads as its own status code.
func (c *Client) ConsultarEstado(ctx context.Context, accessKey string) ConsultaReading {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+ConsultaPath, bytes.NewReader(BuildConsultarEstadoRequest(accessKey)))
	if err != nil {
		return none(fmt.Sprintf("sri: request: %v", err))
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "")
	noRedirect := *c.http
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return none(fmt.Sprintf("sri: %v", err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil && !errors.Is(err, io.EOF) {
		return none(fmt.Sprintf("sri: read response: %v", err))
	}
	return ClassifyConsulta(resp.StatusCode, body)
}
