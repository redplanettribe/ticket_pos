package sri

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What the adapter makes of ConsultaComprobante's answers (#744, spec #742,
// ADR 0078). The fixtures are the shapes the #716 probe recorded from
// production on 2026-11-15: a success carries estadoAutorizacion and no
// estadoConsulta, a refusal carries estadoConsulta RECHAZADA, mensajes and no
// estadoAutorizacion, both as HTTP 200; and 6 consultas in 40 were an HTTP
// 302 to an HTML page. ANULADO was first observed on 2026-11-16, for factura
// 238; that answer and two of factura 225's are kept verbatim below.

// consultaFixtureClave is a well-formed clave, not a real document's.
const consultaFixtureClave = "1809202601179322846800120010010000002251234567812"

func consultaSuccessSOAP(estado string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:consultarEstadoAutorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.consultas"><EstadoAutorizacionComprobante><claveAcceso>` + consultaFixtureClave + `</claveAcceso><mensajes/><estadoAutorizacion>` + estado + `</estadoAutorizacion><tipoComprobante>Factura</tipoComprobante><rucEmisor>1793228468001</rucEmisor><fechaAutorizacion>2026-09-18T13:07:57-05:00</fechaAutorizacion></EstadoAutorizacionComprobante></ns2:consultarEstadoAutorizacionComprobanteResponse></soap:Body></soap:Envelope>`
}

func consultaRefusalSOAP(estadoConsulta, identificador, mensaje string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:consultarEstadoAutorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.consultas"><EstadoAutorizacionComprobante><estadoConsulta>` + estadoConsulta + `</estadoConsulta><claveAcceso>` + consultaFixtureClave + `</claveAcceso><mensajes><mensaje><identificador>` + identificador + `</identificador><mensaje>` + mensaje + `</mensaje><tipo>ERROR</tipo></mensaje></mensajes></EstadoAutorizacionComprobante></ns2:consultarEstadoAutorizacionComprobanteResponse></soap:Body></soap:Envelope>`
}

// The status service's answers kept byte for byte from production's egress
// path on 2026-11-16 (scripts/probe-sri-status.sh): factura 238, annulled at
// the SRI portal, and factura 225, authorized, with the wrong negative it
// also got. Note there is no XML declaration and the fields sit in
// <EstadoAutorizacionComprobante>.
const (
	consultaAnnulled238   = `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:consultarEstadoAutorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.consultas"><EstadoAutorizacionComprobante><claveAcceso>2710202601179322846800120010010000002387161147214</claveAcceso><mensajes/><estadoAutorizacion>ANULADO</estadoAutorizacion><tipoComprobante>Factura</tipoComprobante><rucEmisor>1793228468001</rucEmisor><fechaAutorizacion>2026-10-27T18:03:18-05:00</fechaAutorizacion></EstadoAutorizacionComprobante></ns2:consultarEstadoAutorizacionComprobanteResponse></soap:Body></soap:Envelope>`
	consultaAuthorized225 = `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:consultarEstadoAutorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.consultas"><EstadoAutorizacionComprobante><claveAcceso>1809202601179322846800120010010000002250312603419</claveAcceso><mensajes/><estadoAutorizacion>AUTORIZADO</estadoAutorizacion><tipoComprobante>Factura</tipoComprobante><rucEmisor>1793228468001</rucEmisor><fechaAutorizacion>2026-09-18T13:07:57-05:00</fechaAutorizacion></EstadoAutorizacionComprobante></ns2:consultarEstadoAutorizacionComprobanteResponse></soap:Body></soap:Envelope>`
	consultaRefused225    = `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:consultarEstadoAutorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.consultas"><EstadoAutorizacionComprobante><estadoConsulta>RECHAZADA</estadoConsulta><claveAcceso>1809202601179322846800120010010000002250312603419</claveAcceso><mensajes><mensaje><identificador>99</identificador><mensaje>ERROR AL CONSULTAR DATOS DEL SERVICIO WEB</mensaje><informacionAdicional>No es posible validar la clave de acceso ya que la fecha de emision esta fuera del rango permitido.</informacionAdicional><tipo>ERROR</tipo></mensaje></mensajes></EstadoAutorizacionComprobante></ns2:consultarEstadoAutorizacionComprobanteResponse></soap:Body></soap:Envelope>`
)

const (
	consultaOutOfRange = "No es posible validar la clave de acceso ya que la fecha de emision esta fuera del rango permitido."
	consultaMalformed  = "Error en el formato de la clave acceso."
	consultaFoundHTML  = `<!DOCTYPE HTML PUBLIC "-//IETF//DTD HTML 2.0//EN"><html><head><title>302 Found</title></head><body><h1>Found</h1><p>The document has moved <a href="https://181.113.227.222">here</a>.</p></body></html>`
)

func TestClassifyConsulta(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		want      invoicing.StatusReading
		messages  int
		problem   bool
		wantState string
	}{
		// ---- the two readings -------------------------------------------
		{"AUTORIZADO, as factura 225 answered on 8 calls in 10", http.StatusOK, consultaSuccessSOAP("AUTORIZADO"), invoicing.StatusAuthorized, 0, false, "AUTORIZADO"},
		{"ANULADO", http.StatusOK, consultaSuccessSOAP("ANULADO"), invoicing.StatusAnnulled, 0, false, "ANULADO"},
		{"anulado in lower case", http.StatusOK, consultaSuccessSOAP("anulado"), invoicing.StatusAnnulled, 0, false, "anulado"},
		{"Anulado padded with whitespace", http.StatusOK, consultaSuccessSOAP("\n  Anulado \t"), invoicing.StatusAnnulled, 0, false, "Anulado"},
		{"autorizado in mixed case", http.StatusOK, consultaSuccessSOAP(" Autorizado"), invoicing.StatusAuthorized, 0, false, "Autorizado"},
		{"factura 238's answer from production on 2026-11-16, verbatim", http.StatusOK, consultaAnnulled238, invoicing.StatusAnnulled, 0, false, "ANULADO"},
		{"factura 225's answer from production on 2026-11-16, verbatim", http.StatusOK, consultaAuthorized225, invoicing.StatusAuthorized, 0, false, "AUTORIZADO"},

		// ---- refusals: the SRI's own no, with its reason -----------------
		{"factura 225's wrong negative from production on 2026-11-16, verbatim", http.StatusOK, consultaRefused225, invoicing.StatusNone, 1, false, ""},
		{"RECHAZADA 99 fuera del rango permitido, the wrong negative", http.StatusOK, consultaRefusalSOAP("RECHAZADA", "99", consultaOutOfRange), invoicing.StatusNone, 1, false, ""},
		{"RECHAZADA 99 error en el formato", http.StatusOK, consultaRefusalSOAP("RECHAZADA", "99", consultaMalformed), invoicing.StatusNone, 1, false, ""},
		{"rechazada in lower case", http.StatusOK, consultaRefusalSOAP(" rechazada ", "99", consultaOutOfRange), invoicing.StatusNone, 1, false, ""},

		// ---- states that are not a reading, logged ----------------------
		{"NO AUTORIZADO", http.StatusOK, consultaSuccessSOAP("NO AUTORIZADO"), invoicing.StatusNone, 0, true, "NO AUTORIZADO"},
		{"PENDIENTE DE ANULAR", http.StatusOK, consultaSuccessSOAP("PENDIENTE DE ANULAR"), invoicing.StatusNone, 0, true, "PENDIENTE DE ANULAR"},
		{"an undocumented state", http.StatusOK, consultaSuccessSOAP("EN REVISION"), invoicing.StatusNone, 0, true, "EN REVISION"},
		{"an empty estadoAutorizacion", http.StatusOK, consultaSuccessSOAP(""), invoicing.StatusNone, 0, true, ""},
		{"neither a state nor a refusal", http.StatusOK, consultaRefusalSOAP("", "99", consultaOutOfRange), invoicing.StatusNone, 1, true, ""},

		// ---- answers that are not SOAP ----------------------------------
		{"HTTP 302 to the HTML Found page", http.StatusFound, consultaFoundHTML, invoicing.StatusNone, 0, true, ""},
		{"an HTML page answered 200", http.StatusOK, consultaFoundHTML, invoicing.StatusNone, 0, true, ""},
		{"an empty body", http.StatusOK, "", invoicing.StatusNone, 0, true, ""},
		{"HTTP 503", http.StatusServiceUnavailable, "Service Unavailable", invoicing.StatusNone, 0, true, ""},
		{"a SOAP fault", http.StatusInternalServerError, `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault><faultcode>soap:Server</faultcode><faultstring>Could not find xsd null</faultstring></soap:Fault></soap:Body></soap:Envelope>`, invoicing.StatusNone, 0, true, ""},
		{"the fields under <return>, the wrapper guessed before any answer was kept", http.StatusOK,
			strings.NewReplacer("<EstadoAutorizacionComprobante>", "<return>", "</EstadoAutorizacionComprobante>", "</return>").Replace(consultaSuccessSOAP("ANULADO")), invoicing.StatusNone, 0, true, ""},
		{"a response with nothing in it", http.StatusOK, `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:consultarEstadoAutorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.consultas"/></soap:Body></soap:Envelope>`, invoicing.StatusNone, 0, true, ""},
		{"another service's SOAP", http.StatusOK, authorizedResponse, invoicing.StatusNone, 0, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyConsulta(c.status, []byte(c.body))
			if got.Reading != c.want {
				t.Fatalf("reading = %q, want %q (%+v)", got.Reading, c.want, got)
			}
			if len(got.Messages) != c.messages {
				t.Fatalf("messages = %+v, want %d", got.Messages, c.messages)
			}
			if (got.Problem != "") != c.problem {
				t.Fatalf("problem = %q, want one: %v", got.Problem, c.problem)
			}
			if got.State != c.wantState {
				t.Fatalf("state = %q, want %q", got.State, c.wantState)
			}
		})
	}
}

// TestClassifyConsultaKeepsTheRefusalVerbatim: the refusal's mensaje is what
// the attempts ledger shows the operator on a no reading.
func TestClassifyConsultaKeepsTheRefusalVerbatim(t *testing.T) {
	got := ClassifyConsulta(http.StatusOK, []byte(consultaRefusalSOAP("RECHAZADA", "99", consultaOutOfRange)))
	if len(got.Messages) != 1 || got.Messages[0].Identifier != "99" || got.Messages[0].Message != consultaOutOfRange || got.Messages[0].Type != "ERROR" {
		t.Fatalf("messages = %+v; want the SRI's 99 verbatim", got.Messages)
	}
}

func TestBuildConsultarEstadoRequest(t *testing.T) {
	got := string(BuildConsultarEstadoRequest(consultaFixtureClave))
	for _, want := range []string{
		`xmlns:ec="http://ec.gob.sri.ws.consultas"`,
		`<ec:consultarEstadoAutorizacionComprobante><claveAcceso>` + consultaFixtureClave + `</claveAcceso></ec:consultarEstadoAutorizacionComprobante>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("request %s\nlacks %s", got, want)
		}
	}
}

// recordingTransport answers every request with an AUTORIZADO consulta and
// records where it was sent, so a test can see which host an environment
// asks without reaching the SRI.
type recordingTransport struct{ requests []*http.Request }

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, req)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/xml"}},
		Body:       io.NopCloser(strings.NewReader(consultaSuccessSOAP("AUTORIZADO"))),
		Request:    req,
	}, nil
}

// TestQueryStatusAsksTheEnvironmentsHost: a test-environment factura is
// asked of celcer, a production one of cel, at the ConsultaComprobante path
// with soapAction "".
func TestQueryStatusAsksTheEnvironmentsHost(t *testing.T) {
	for env, host := range map[invoicing.Environment]string{
		invoicing.EnvironmentTest:       "celcer.sri.gob.ec",
		invoicing.EnvironmentProduction: "cel.sri.gob.ec",
	} {
		t.Run(string(env), func(t *testing.T) {
			rt := &recordingTransport{}
			a := NewAuthority(env, WithHTTPClient(&http.Client{Transport: rt}))

			answer := a.QueryStatus(context.Background(), consultaFixtureClave)
			if answer.Reading != invoicing.StatusAuthorized {
				t.Fatalf("answer = %+v; want authorized", answer)
			}
			if len(rt.requests) != 1 {
				t.Fatalf("requests = %d; want one", len(rt.requests))
			}
			req := rt.requests[0]
			if req.URL.Scheme != "https" || req.URL.Host != host || req.URL.Path != ConsultaPath {
				t.Fatalf("asked %s; want https://%s%s", req.URL, host, ConsultaPath)
			}
			if sa, ok := req.Header["Soapaction"]; !ok || len(sa) != 1 || sa[0] != "" {
				t.Fatalf("SOAPAction = %v; want the empty string", req.Header.Values("SOAPAction"))
			}
		})
	}
}

// TestQueryStatusDoesNotFollowARedirect: the SRI's edge answers a consulta
// with a 302 to an HTML page; the adapter reads the 302 itself as no
// reading and never fetches the page.
func TestQueryStatusDoesNotFollowARedirect(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.URL.Path == ConsultaPath {
			http.Redirect(w, r, "/portal", http.StatusFound)
			return
		}
		io.WriteString(w, consultaSuccessSOAP("ANULADO"))
	}))
	t.Cleanup(srv.Close)
	a := NewAuthority(invoicing.EnvironmentTest, WithBaseURL(srv.URL))

	answer := a.QueryStatus(context.Background(), consultaFixtureClave)
	if answer.Reading != invoicing.StatusNone || !strings.Contains(answer.Error, "302") {
		t.Fatalf("answer = %+v; want no reading naming the 302", answer)
	}
	if len(seen) != 1 || seen[0] != "POST "+ConsultaPath {
		t.Fatalf("requests = %v; want the one consulta POST and no redirect followed", seen)
	}
}

// TestQueryStatusResetAndTimeoutAreNoReading: a connection the SRI resets
// and one that never answers are each no reading with the transport error,
// never a failure.
func TestQueryStatusResetAndTimeoutAreNoReading(t *testing.T) {
	reset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		conn.Close()
	}))
	t.Cleanup(reset.Close)
	answer := NewAuthority(invoicing.EnvironmentTest, WithBaseURL(reset.URL)).QueryStatus(context.Background(), consultaFixtureClave)
	if answer.Reading != invoicing.StatusNone || answer.Error == "" {
		t.Fatalf("reset: answer = %+v; want no reading with the error", answer)
	}

	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(release) })
	answer = NewAuthority(invoicing.EnvironmentTest, WithBaseURL(hang.URL), WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond})).
		QueryStatus(context.Background(), consultaFixtureClave)
	if answer.Reading != invoicing.StatusNone || answer.Error == "" {
		t.Fatalf("timeout: answer = %+v; want no reading with the error", answer)
	}
}
