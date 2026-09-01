package sri

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// SRI web service hosts per environment (Ficha §7.2; live WSDLs).
const (
	BaseURLTest       = "https://celcer.sri.gob.ec"
	BaseURLProduction = "https://cel.sri.gob.ec"

	// ReceptionPath is the RecepcionComprobantesOffline service path.
	ReceptionPath = "/comprobantes-electronicos-ws/RecepcionComprobantesOffline"
	// AuthorizationPath is the AutorizacionComprobantesOffline service path.
	AuthorizationPath = "/comprobantes-electronicos-ws/AutorizacionComprobantesOffline"

	receptionNamespace     = "http://ec.gob.sri.ws.recepcion"
	authorizationNamespace = "http://ec.gob.sri.ws.autorizacion"
	soapNamespace          = "http://schemas.xmlsoap.org/soap/envelope/"

	// DefaultTimeout bounds one SRI call; the issue flow polls within a
	// budget of a few seconds, so a call must never hang for a minute.
	DefaultTimeout = 10 * time.Second

	// MaxComprobanteBytes is the SRI's size limit per comprobante (§7.5).
	MaxComprobanteBytes = 320 * 1024
)

// BaseURL returns the SRI host for an environment.
func BaseURL(env Environment) string {
	if env == EnvironmentProduction {
		return BaseURLProduction
	}
	return BaseURLTest
}

// Client talks SOAP 1.1 to the SRI's two offline web services.
type Client struct {
	baseURL string
	http    *http.Client
}

// ClientOption configures NewClient.
type ClientOption func(*Client)

// WithBaseURL points every call at another host — the fake SRI in tests,
// or a local stand-in — instead of the environment's real endpoint.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithHTTPClient replaces the HTTP client (timeouts, transport).
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) { c.http = h }
}

// NewClient builds a client for an environment. Production and test differ
// only in host; the SRI's TLS certificates are not pinned (Ficha §7.1.4).
func NewClient(env Environment, opts ...ClientOption) *Client {
	c := &Client{baseURL: BaseURL(env), http: &http.Client{Timeout: DefaultTimeout}}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Message is one SRI mensaje, returned verbatim.
type Message struct {
	// Identifier is the SRI code, e.g. "43", "70", "60".
	Identifier     string `xml:"identificador"`
	Message        string `xml:"mensaje"`
	AdditionalInfo string `xml:"informacionAdicional"`
	// Type is "ERROR" or "ADVERTENCIA".
	Type string `xml:"tipo"`
}

// SRI message identifiers the issue flow branches on (Ficha error table).
// The secuencial one is the core's constant under the SRI's own name: the
// code is the authority's vocabulary, but the predicate that reads it off a
// stored message belongs to the core (#576, ADR 0068), and one literal for
// one code means the two can never drift.
const (
	MessageAccessKeyRegistered   = "43" // clave de acceso registrada: already at the SRI — poll authorization
	MessageAccessKeyInProcessing = "70" // clave de acceso en procesamiento: do not resend — poll authorization
	MessageTestEnvironment       = "60" // advertencia: ambiente de pruebas
	// MessageSequenceRegistered: ERROR SECUENCIAL REGISTRADO — the SRI
	// refuses the document's NUMBER, so resending the same secuencial can
	// only earn the same answer.
	MessageSequenceRegistered = invoicing.AuthorityMessageSequenceRegistered // "45"
)

// ReceptionState is the estado of validarComprobante.
type ReceptionState string

const (
	ReceptionReceived ReceptionState = "RECIBIDA"
	ReceptionReturned ReceptionState = "DEVUELTA"
)

// ReceptionResult is the parsed RespuestaSolicitud of validarComprobante.
type ReceptionResult struct {
	State        ReceptionState
	Comprobantes []ReceivedComprobante
}

// ReceivedComprobante is one comprobante entry of a reception response.
type ReceivedComprobante struct {
	AccessKey string
	Messages  []Message
}

// Messages flattens every message across comprobantes.
func (r *ReceptionResult) Messages() []Message {
	var out []Message
	for _, c := range r.Comprobantes {
		out = append(out, c.Messages...)
	}
	return out
}

// HasMessage reports whether any message carries the identifier.
func (r *ReceptionResult) HasMessage(identifier string) bool {
	for _, m := range r.Messages() {
		if m.Identifier == identifier {
			return true
		}
	}
	return false
}

// AuthorizationState is the estado of one autorizacion.
type AuthorizationState string

const (
	Authorized    AuthorizationState = "AUTORIZADO"
	NotAuthorized AuthorizationState = "NO AUTORIZADO"
	// InProcessing covers both spellings the SRI uses, "EN PROCESO" and
	// "EN PROCESAMIENTO"; ParseAuthorizationState normalises them.
	InProcessing AuthorizationState = "EN PROCESAMIENTO"
)

// ParseAuthorizationState normalises the SRI's estado text.
func ParseAuthorizationState(s string) AuthorizationState {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "AUTORIZADO":
		return Authorized
	case "NO AUTORIZADO":
		return NotAuthorized
	case "EN PROCESO", "EN PROCESAMIENTO", "PPR":
		return InProcessing
	}
	return AuthorizationState(strings.ToUpper(strings.TrimSpace(s)))
}

// Authorization is one autorizacion entry of autorizacionComprobante.
type Authorization struct {
	State AuthorizationState
	// Number is the número de autorización — the clave de acceso in the
	// offline scheme; empty unless authorized.
	Number string
	// Date is the fechaAutorizacion; zero when absent or unparseable, in
	// which case RawDate holds the text.
	Date    time.Time
	RawDate string
	// Environment is the SRI's word: "PRUEBAS" or "PRODUCCIÓN".
	Environment string
	// Comprobante is the signed XML the SRI holds, as returned.
	Comprobante []byte
	Messages    []Message
	// XML is the <autorizacion> element as the SRI returned it, prefixed
	// with an XML declaration: the legal artifact to store for an
	// authorized document.
	XML []byte
}

// AuthorizationResult is the parsed RespuestaAutorizacionComprobante.
type AuthorizationResult struct {
	AccessKey      string
	Count          int
	Authorizations []Authorization
}

// Latest returns the last authorization entry, which for a document sent
// several times is the state the SRI reports (Ficha §5.11), or nil when the
// SRI knows nothing about the clave.
func (r *AuthorizationResult) Latest() *Authorization {
	if len(r.Authorizations) == 0 {
		return nil
	}
	return &r.Authorizations[len(r.Authorizations)-1]
}

// SOAPFault is a SOAP 1.1 Fault the SRI returned.
type SOAPFault struct {
	Code   string
	String string
}

func (f *SOAPFault) Error() string {
	return fmt.Sprintf("sri: soap fault %s: %s", f.Code, f.String)
}

// HTTPError is a non-200 answer from the SRI.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("sri: http %d", e.StatusCode)
}

// ErrResponse is wrapped when a 200 answer cannot be understood.
var ErrResponse = errors.New("sri: unexpected response")

// BuildValidarComprobanteRequest renders the SOAP envelope of
// validarComprobante, with the signed XML base64-encoded.
func BuildValidarComprobanteRequest(signedXML []byte) []byte {
	var b bytes.Buffer
	b.WriteString(xmlDeclaration)
	b.WriteString(`<soapenv:Envelope xmlns:soapenv="` + soapNamespace + `" xmlns:ec="` + receptionNamespace + `">`)
	b.WriteString(`<soapenv:Header/><soapenv:Body><ec:validarComprobante><xml>`)
	b.WriteString(base64.StdEncoding.EncodeToString(signedXML))
	b.WriteString(`</xml></ec:validarComprobante></soapenv:Body></soapenv:Envelope>`)
	return b.Bytes()
}

// BuildAutorizacionComprobanteRequest renders the SOAP envelope of
// autorizacionComprobante for one clave de acceso.
func BuildAutorizacionComprobanteRequest(accessKey string) []byte {
	var b bytes.Buffer
	b.WriteString(xmlDeclaration)
	b.WriteString(`<soapenv:Envelope xmlns:soapenv="` + soapNamespace + `" xmlns:ec="` + authorizationNamespace + `">`)
	b.WriteString(`<soapenv:Header/><soapenv:Body><ec:autorizacionComprobante><claveAccesoComprobante>`)
	xml.EscapeText(&b, []byte(accessKey))
	b.WriteString(`</claveAccesoComprobante></ec:autorizacionComprobante></soapenv:Body></soapenv:Envelope>`)
	return b.Bytes()
}

type soapEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Fault *struct {
			Code   string `xml:"faultcode"`
			String string `xml:"faultstring"`
		} `xml:"Fault"`
		Reception *struct {
			Respuesta struct {
				Estado       string `xml:"estado"`
				Comprobantes []struct {
					ClaveAcceso string    `xml:"claveAcceso"`
					Mensajes    []Message `xml:"mensajes>mensaje"`
				} `xml:"comprobantes>comprobante"`
			} `xml:"RespuestaRecepcionComprobante"`
		} `xml:"validarComprobanteResponse"`
		Authorization *struct {
			Respuesta struct {
				ClaveAcceso    string `xml:"claveAccesoConsultada"`
				Numero         string `xml:"numeroComprobantes"`
				Autorizaciones []struct {
					InnerXML           []byte    `xml:",innerxml"`
					Estado             string    `xml:"estado"`
					NumeroAutorizacion string    `xml:"numeroAutorizacion"`
					FechaAutorizacion  string    `xml:"fechaAutorizacion"`
					Ambiente           string    `xml:"ambiente"`
					Comprobante        string    `xml:"comprobante"`
					Mensajes           []Message `xml:"mensajes>mensaje"`
				} `xml:"autorizaciones>autorizacion"`
			} `xml:"RespuestaAutorizacionComprobante"`
		} `xml:"autorizacionComprobanteResponse"`
	} `xml:"Body"`
}

func decodeEnvelope(body []byte) (*soapEnvelope, error) {
	var env soapEnvelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrResponse, err)
	}
	if env.Body.Fault != nil {
		return nil, &SOAPFault{Code: env.Body.Fault.Code, String: env.Body.Fault.String}
	}
	return &env, nil
}

// ParseValidarComprobanteResponse parses a validarComprobante SOAP answer.
func ParseValidarComprobanteResponse(body []byte) (*ReceptionResult, error) {
	env, err := decodeEnvelope(body)
	if err != nil {
		return nil, err
	}
	if env.Body.Reception == nil {
		return nil, fmt.Errorf("%w: no validarComprobanteResponse", ErrResponse)
	}
	resp := env.Body.Reception.Respuesta
	state := ReceptionState(strings.ToUpper(strings.TrimSpace(resp.Estado)))
	if state != ReceptionReceived && state != ReceptionReturned {
		return nil, fmt.Errorf("%w: reception estado %q", ErrResponse, resp.Estado)
	}
	out := &ReceptionResult{State: state}
	for _, c := range resp.Comprobantes {
		out.Comprobantes = append(out.Comprobantes, ReceivedComprobante{AccessKey: c.ClaveAcceso, Messages: trimMessages(c.Mensajes)})
	}
	return out, nil
}

// ParseAutorizacionComprobanteResponse parses an autorizacionComprobante
// SOAP answer.
func ParseAutorizacionComprobanteResponse(body []byte) (*AuthorizationResult, error) {
	env, err := decodeEnvelope(body)
	if err != nil {
		return nil, err
	}
	if env.Body.Authorization == nil {
		return nil, fmt.Errorf("%w: no autorizacionComprobanteResponse", ErrResponse)
	}
	resp := env.Body.Authorization.Respuesta
	out := &AuthorizationResult{AccessKey: resp.ClaveAcceso}
	out.Count, _ = strconv.Atoi(strings.TrimSpace(resp.Numero))
	for _, a := range resp.Autorizaciones {
		auth := Authorization{
			State:       ParseAuthorizationState(a.Estado),
			Number:      strings.TrimSpace(a.NumeroAutorizacion),
			RawDate:     strings.TrimSpace(a.FechaAutorizacion),
			Environment: strings.TrimSpace(a.Ambiente),
			Messages:    trimMessages(a.Mensajes),
		}
		if c := strings.TrimSpace(a.Comprobante); c != "" {
			auth.Comprobante = []byte(c)
		}
		if auth.RawDate != "" {
			for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000-07:00", "2006-01-02T15:04:05"} {
				if t, err := time.ParseInLocation(layout, auth.RawDate, Guayaquil); err == nil {
					auth.Date = t
					break
				}
			}
		}
		auth.XML = append(append([]byte(xmlDeclaration+"<autorizacion>"), a.InnerXML...), []byte("</autorizacion>")...)
		out.Authorizations = append(out.Authorizations, auth)
	}
	return out, nil
}

func trimMessages(in []Message) []Message {
	out := make([]Message, 0, len(in))
	for _, m := range in {
		out = append(out, Message{
			Identifier:     strings.TrimSpace(m.Identifier),
			Message:        strings.TrimSpace(m.Message),
			AdditionalInfo: strings.TrimSpace(m.AdditionalInfo),
			Type:           strings.TrimSpace(m.Type),
		})
	}
	return out
}

// ValidateComprobante submits a signed comprobante to recepción.
// A DEVUELTA answer is a result, not an error; transport failures, non-200
// statuses (*HTTPError) and SOAP faults (*SOAPFault) are errors.
func (c *Client) ValidateComprobante(ctx context.Context, signedXML []byte) (*ReceptionResult, error) {
	if len(signedXML) > MaxComprobanteBytes {
		return nil, fmt.Errorf("sri: comprobante is %d bytes, the SRI accepts at most %d", len(signedXML), MaxComprobanteBytes)
	}
	body, err := c.post(ctx, ReceptionPath, BuildValidarComprobanteRequest(signedXML))
	if err != nil {
		return nil, err
	}
	return ParseValidarComprobanteResponse(body)
}

// AuthorizationComprobante asks autorización for the state of a clave.
func (c *Client) AuthorizationComprobante(ctx context.Context, accessKey string) (*AuthorizationResult, error) {
	body, err := c.post(ctx, AuthorizationPath, BuildAutorizacionComprobanteRequest(accessKey))
	if err != nil {
		return nil, err
	}
	return ParseAutorizacionComprobanteResponse(body)
}

func (c *Client) post(ctx context.Context, path string, envelope []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(envelope))
	if err != nil {
		return nil, fmt.Errorf("sri: request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sri: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("sri: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// A SOAP fault travels with status 500; surface it as such.
		var fault *SOAPFault
		if _, ferr := decodeEnvelope(body); errors.As(ferr, &fault) {
			return nil, fault
		}
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}
