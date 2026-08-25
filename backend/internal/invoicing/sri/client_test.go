package sri

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const receivedResponse = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:validarComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.recepcion"><RespuestaRecepcionComprobante><estado>RECIBIDA</estado><comprobantes/></RespuestaRecepcionComprobante></ns2:validarComprobanteResponse></soap:Body></soap:Envelope>`

const returnedResponse = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:validarComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.recepcion"><RespuestaRecepcionComprobante><estado>DEVUELTA</estado><comprobantes><comprobante><claveAcceso>2508202601179001234500110010020000001231234567811</claveAcceso><mensajes><mensaje><identificador>43</identificador><mensaje>CLAVE ACCESO REGISTRADA</mensaje><informacionAdicional>La clave de acceso ya se encuentra registrada</informacionAdicional><tipo>ERROR</tipo></mensaje><mensaje><identificador>70</identificador><mensaje>CLAVE ACCESO EN PROCESAMIENTO</mensaje><tipo>ERROR</tipo></mensaje></mensajes></comprobante></comprobantes></RespuestaRecepcionComprobante></ns2:validarComprobanteResponse></soap:Body></soap:Envelope>`

const authorizedResponse = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:autorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.autorizacion"><RespuestaAutorizacionComprobante><claveAccesoConsultada>2508202601179001234500110010020000001231234567811</claveAccesoConsultada><numeroComprobantes>1</numeroComprobantes><autorizaciones><autorizacion><estado>AUTORIZADO</estado><numeroAutorizacion>2508202601179001234500110010020000001231234567811</numeroAutorizacion><fechaAutorizacion>2026-08-25T10:31:09-05:00</fechaAutorizacion><ambiente>PRUEBAS</ambiente><comprobante><![CDATA[<?xml version="1.0" encoding="UTF-8"?><factura id="comprobante" version="1.1.0"><infoTributaria><ambiente>1</ambiente></infoTributaria></factura>]]></comprobante><mensajes><mensaje><identificador>60</identificador><mensaje>ESTE PROCESO FUE REALIZADO EN EL AMBIENTE DE PRUEBAS</mensaje><tipo>ADVERTENCIA</tipo></mensaje></mensajes></autorizacion></autorizaciones></RespuestaAutorizacionComprobante></ns2:autorizacionComprobanteResponse></soap:Body></soap:Envelope>`

const notAuthorizedResponse = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:autorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.autorizacion"><RespuestaAutorizacionComprobante><claveAccesoConsultada>2508202601179001234500110010020000001231234567811</claveAccesoConsultada><numeroComprobantes>2</numeroComprobantes><autorizaciones><autorizacion><estado>NO AUTORIZADO</estado><fechaAutorizacion>2026-08-25T10:31:09-05:00</fechaAutorizacion><ambiente>PRUEBAS</ambiente><comprobante><![CDATA[<factura/>]]></comprobante><mensajes><mensaje><identificador>39</identificador><mensaje>FIRMA INVALIDA</mensaje><informacionAdicional>Error en la firma</informacionAdicional><tipo>ERROR</tipo></mensaje></mensajes></autorizacion><autorizacion><estado>EN PROCESO</estado><fechaAutorizacion></fechaAutorizacion><ambiente>PRUEBAS</ambiente><comprobante><![CDATA[<factura/>]]></comprobante><mensajes/></autorizacion></autorizaciones></RespuestaAutorizacionComprobante></ns2:autorizacionComprobanteResponse></soap:Body></soap:Envelope>`

const emptyAuthorizationResponse = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:autorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.autorizacion"><RespuestaAutorizacionComprobante><claveAccesoConsultada>2508202601179001234500110010020000001231234567811</claveAccesoConsultada><numeroComprobantes>0</numeroComprobantes><autorizaciones/></RespuestaAutorizacionComprobante></ns2:autorizacionComprobanteResponse></soap:Body></soap:Envelope>`

const faultResponse = `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault><faultcode>soap:Server</faultcode><faultstring>Error interno general</faultstring></soap:Fault></soap:Body></soap:Envelope>`

type fakeSRI struct {
	*httptest.Server
	received []recordedRequest
	respond  func(path string, body []byte) (int, string)
}

type recordedRequest struct {
	Path        string
	ContentType string
	Body        []byte
}

func newFakeSRI(t *testing.T, respond func(path string, body []byte) (int, string)) *fakeSRI {
	t.Helper()
	f := &fakeSRI{respond: respond}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.received = append(f.received, recordedRequest{Path: r.URL.Path, ContentType: r.Header.Get("Content-Type"), Body: body})
		status, resp := f.respond(r.URL.Path, body)
		w.Header().Set("Content-Type", "text/xml;charset=UTF-8")
		w.WriteHeader(status)
		io.WriteString(w, resp)
	}))
	t.Cleanup(f.Close)
	return f
}

func TestBaseURLPerEnvironment(t *testing.T) {
	if BaseURL(EnvironmentTest) != "https://celcer.sri.gob.ec" || BaseURL(EnvironmentProduction) != "https://cel.sri.gob.ec" {
		t.Fatal("hosts")
	}
	c := NewClient(EnvironmentProduction)
	if c.baseURL != "https://cel.sri.gob.ec" {
		t.Fatalf("default base url = %q", c.baseURL)
	}
	c = NewClient(EnvironmentProduction, WithBaseURL("http://127.0.0.1:9/"))
	if c.baseURL != "http://127.0.0.1:9" {
		t.Fatalf("override = %q", c.baseURL)
	}
}

func TestValidateComprobanteReceived(t *testing.T) {
	signed := signGolden(t)
	fake := newFakeSRI(t, func(path string, body []byte) (int, string) { return 200, receivedResponse })
	c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
	res, err := c.ValidateComprobante(context.Background(), signed)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != ReceptionReceived || len(res.Messages()) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if len(fake.received) != 1 {
		t.Fatalf("requests = %d", len(fake.received))
	}
	req := fake.received[0]
	if req.Path != ReceptionPath {
		t.Errorf("path = %q", req.Path)
	}
	if !strings.HasPrefix(req.ContentType, "text/xml") {
		t.Errorf("content type = %q", req.ContentType)
	}
	body := string(req.Body)
	if !strings.Contains(body, `xmlns:ec="http://ec.gob.sri.ws.recepcion"`) || !strings.Contains(body, "<ec:validarComprobante><xml>") {
		t.Errorf("envelope = %s", body)
	}
	// The fake decodes exactly the bytes that were signed.
	start := strings.Index(body, "<xml>") + len("<xml>")
	end := strings.Index(body, "</xml>")
	decoded, err := base64.StdEncoding.DecodeString(body[start:end])
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(signed) {
		t.Fatal("the SRI did not receive the signed bytes verbatim")
	}
	if _, err := Verify(decoded); err != nil {
		t.Fatalf("what the SRI received does not verify: %v", err)
	}
}

func TestValidateComprobanteReturnedWithMessages(t *testing.T) {
	fake := newFakeSRI(t, func(string, []byte) (int, string) { return 200, returnedResponse })
	c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
	res, err := c.ValidateComprobante(context.Background(), []byte("<factura/>"))
	if err != nil {
		t.Fatal(err)
	}
	if res.State != ReceptionReturned {
		t.Fatalf("state = %q", res.State)
	}
	if len(res.Comprobantes) != 1 || res.Comprobantes[0].AccessKey != "2508202601179001234500110010020000001231234567811" {
		t.Fatalf("comprobantes = %+v", res.Comprobantes)
	}
	msgs := res.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v", msgs)
	}
	want := Message{Identifier: "43", Message: "CLAVE ACCESO REGISTRADA", AdditionalInfo: "La clave de acceso ya se encuentra registrada", Type: "ERROR"}
	if msgs[0] != want {
		t.Fatalf("message = %+v, want %+v", msgs[0], want)
	}
	if !res.HasMessage(MessageAccessKeyRegistered) || !res.HasMessage(MessageAccessKeyInProcessing) || res.HasMessage("45") {
		t.Fatal("HasMessage")
	}
}

func TestValidateComprobanteRefusesOversize(t *testing.T) {
	c := NewClient(EnvironmentTest, WithBaseURL("http://127.0.0.1:9"))
	if _, err := c.ValidateComprobante(context.Background(), make([]byte, MaxComprobanteBytes+1)); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestAuthorizationComprobanteAuthorized(t *testing.T) {
	fake := newFakeSRI(t, func(string, []byte) (int, string) { return 200, authorizedResponse })
	c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
	key := "2508202601179001234500110010020000001231234567811"
	res, err := c.AuthorizationComprobante(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if fake.received[0].Path != AuthorizationPath {
		t.Errorf("path = %q", fake.received[0].Path)
	}
	if body := string(fake.received[0].Body); !strings.Contains(body, `xmlns:ec="http://ec.gob.sri.ws.autorizacion"`) ||
		!strings.Contains(body, "<ec:autorizacionComprobante><claveAccesoComprobante>"+key+"</claveAccesoComprobante>") {
		t.Errorf("envelope = %s", body)
	}
	if res.AccessKey != key || res.Count != 1 || len(res.Authorizations) != 1 {
		t.Fatalf("result = %+v", res)
	}
	a := res.Latest()
	if a.State != Authorized || a.Number != key || a.Environment != "PRUEBAS" {
		t.Fatalf("authorization = %+v", a)
	}
	if !a.Date.Equal(time.Date(2026, 8, 25, 10, 31, 9, 0, Guayaquil)) {
		t.Fatalf("date = %v (%q)", a.Date, a.RawDate)
	}
	if !strings.HasPrefix(string(a.Comprobante), `<?xml version="1.0" encoding="UTF-8"?><factura id="comprobante"`) {
		t.Fatalf("comprobante = %s", a.Comprobante)
	}
	if len(a.Messages) != 1 || a.Messages[0].Identifier != MessageTestEnvironment || a.Messages[0].Type != "ADVERTENCIA" {
		t.Fatalf("messages = %+v", a.Messages)
	}
	xml := string(a.XML)
	if !strings.HasPrefix(xml, `<?xml version="1.0" encoding="UTF-8"?><autorizacion><estado>AUTORIZADO</estado>`) ||
		!strings.Contains(xml, "<![CDATA[") || !strings.HasSuffix(xml, "</mensajes></autorizacion>") {
		t.Fatalf("authorization xml = %s", xml)
	}
}

func TestAuthorizationComprobanteNotAuthorizedAndInProcess(t *testing.T) {
	fake := newFakeSRI(t, func(string, []byte) (int, string) { return 200, notAuthorizedResponse })
	c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
	res, err := c.AuthorizationComprobante(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 2 || len(res.Authorizations) != 2 {
		t.Fatalf("result = %+v", res)
	}
	first := res.Authorizations[0]
	if first.State != NotAuthorized || first.Number != "" || len(first.Messages) != 1 || first.Messages[0].Identifier != "39" {
		t.Fatalf("first = %+v", first)
	}
	if last := res.Latest(); last.State != InProcessing || !last.Date.IsZero() {
		t.Fatalf("latest = %+v", last)
	}
	for in, want := range map[string]AuthorizationState{"EN PROCESO": InProcessing, "EN PROCESAMIENTO": InProcessing, "autorizado": Authorized, "NO AUTORIZADO": NotAuthorized, "RARO": "RARO"} {
		if got := ParseAuthorizationState(in); got != want {
			t.Errorf("ParseAuthorizationState(%q) = %q", in, got)
		}
	}
}

func TestAuthorizationComprobanteUnknownKey(t *testing.T) {
	fake := newFakeSRI(t, func(string, []byte) (int, string) { return 200, emptyAuthorizationResponse })
	c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
	res, err := c.AuthorizationComprobante(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 0 || res.Latest() != nil {
		t.Fatalf("result = %+v", res)
	}
}

func TestClientErrors(t *testing.T) {
	t.Run("soap fault with 500", func(t *testing.T) {
		fake := newFakeSRI(t, func(string, []byte) (int, string) { return 500, faultResponse })
		c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
		_, err := c.AuthorizationComprobante(context.Background(), "x")
		var fault *SOAPFault
		if !errors.As(err, &fault) || fault.String != "Error interno general" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("http 503", func(t *testing.T) {
		fake := newFakeSRI(t, func(string, []byte) (int, string) { return 503, "<html>down</html>" })
		c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
		_, err := c.ValidateComprobante(context.Background(), []byte("<x/>"))
		var he *HTTPError
		if !errors.As(err, &he) || he.StatusCode != 503 {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("garbage body", func(t *testing.T) {
		fake := newFakeSRI(t, func(string, []byte) (int, string) { return 200, "not xml" })
		c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
		if _, err := c.ValidateComprobante(context.Background(), []byte("<x/>")); !errors.Is(err, ErrResponse) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("wrong operation in body", func(t *testing.T) {
		fake := newFakeSRI(t, func(string, []byte) (int, string) { return 200, authorizedResponse })
		c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
		if _, err := c.ValidateComprobante(context.Background(), []byte("<x/>")); !errors.Is(err, ErrResponse) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		fake := newFakeSRI(t, func(string, []byte) (int, string) { time.Sleep(300 * time.Millisecond); return 200, receivedResponse })
		c := NewClient(EnvironmentTest, WithBaseURL(fake.URL), WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
		if _, err := c.ValidateComprobante(context.Background(), []byte("<x/>")); err == nil {
			t.Fatal("expected timeout")
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		fake := newFakeSRI(t, func(string, []byte) (int, string) { return 200, receivedResponse })
		c := NewClient(EnvironmentTest, WithBaseURL(fake.URL))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.ValidateComprobante(ctx, []byte("<x/>")); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unreachable host", func(t *testing.T) {
		c := NewClient(EnvironmentTest, WithBaseURL("http://127.0.0.1:1"))
		if _, err := c.AuthorizationComprobante(context.Background(), "x"); err == nil {
			t.Fatal("expected a transport error")
		}
	})
}

func TestBuildRequestsAreWellFormed(t *testing.T) {
	if !strings.HasPrefix(string(BuildValidarComprobanteRequest([]byte("<a/>"))), `<?xml version="1.0" encoding="UTF-8"?><soapenv:Envelope`) {
		t.Fatal("validar envelope")
	}
	req := string(BuildAutorizacionComprobanteRequest("a<b"))
	if !strings.Contains(req, "<claveAccesoComprobante>a&lt;b</claveAccesoComprobante>") {
		t.Fatalf("clave must be escaped: %s", req)
	}
}
