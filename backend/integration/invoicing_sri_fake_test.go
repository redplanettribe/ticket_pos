package integration

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/server"
)

// A fake SRI, wired the way the PayPhone stub is: a second app over the shared
// database with the base-URL override (SRI_BASE_URL) pointed at an httptest
// server that speaks the SRI's SOAP shapes and records what it received (#454).
//
// The real SRI is asynchronous — recepción takes a document, autorización is
// polled for the verdict — so the fake answers the two paths independently.
// Every scenario the ticket names is a closure a test installs: RECIBIDA,
// DEVUELTA with messages, AUTORIZADO with the authorization XML, NO AUTORIZADO
// with messages, EN PROCESAMIENTO, HTTP 500 and a hang past the budget.

var (
	sriStub *fakeSRI
	sriApp  *server.App
	sriSrv  *httptest.Server
	sriEnv  *testEnv
)

// The poll schedule the SRI app runs in tests: tiny delays and a short budget,
// so a "pending after the budget" scenario costs milliseconds and not 15 s.
var (
	testPollDelays = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}
	testPollBudget = 400 * time.Millisecond
)

type receivedComprobante struct {
	path      string
	signedXML []byte
	accessKey string
}

type fakeSRI struct {
	server *httptest.Server
	mu     sync.Mutex

	received []receivedComprobante

	// reception answers validarComprobante; authorization answers
	// autorizacionComprobante. Both may sleep before answering (the timeout
	// scenario). accessKey is the clave the request carried.
	reception     func(accessKey string) (int, string)
	authorization func(accessKey string) (int, string)
	sleep         time.Duration

	// A one-shot gate on the next autorización call (holdAuthorization):
	// the fake signals on holdArrived once the query is in, and answers
	// only once holdRelease is closed — so a test can do something to the
	// document while the Drainer is provably waiting on the SRI.
	holdArrived chan struct{}
	holdRelease chan struct{}
}

var claveInXML = regexp.MustCompile(`<claveAcceso>([0-9]{49})</claveAcceso>`)
var claveInQuery = regexp.MustCompile(`<claveAccesoComprobante>([0-9]{49})</claveAccesoComprobante>`)
var base64InReception = regexp.MustCompile(`(?s)<xml>(.*?)</xml>`)

func startSRIStub() *fakeSRI {
	f := &fakeSRI{}
	f.reception = func(accessKey string) (int, string) { return http.StatusOK, receivedSOAP(accessKey) }
	f.authorization = func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) }
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)

		f.mu.Lock()
		sleep := f.sleep
		var status int
		var out string
		switch {
		case strings.HasPrefix(r.URL.Path, sri.ReceptionPath):
			signed := decodeReception(body)
			key := ""
			if m := claveInXML.FindSubmatch(signed); m != nil {
				key = string(m[1])
			}
			f.received = append(f.received, receivedComprobante{path: r.URL.Path, signedXML: signed, accessKey: key})
			status, out = f.reception(key)
		case strings.HasPrefix(r.URL.Path, sri.AuthorizationPath):
			key := ""
			if m := claveInQuery.FindStringSubmatch(body); m != nil {
				key = m[1]
			}
			status, out = f.authorization(key)
		default:
			status, out = http.StatusNotFound, ""
		}
		var arrived, release chan struct{}
		if strings.HasPrefix(r.URL.Path, sri.AuthorizationPath) && f.holdRelease != nil {
			arrived, release = f.holdArrived, f.holdRelease
			f.holdArrived, f.holdRelease = nil, nil
		}
		f.mu.Unlock()

		if release != nil {
			close(arrived)
			<-release
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(out))
	}))
	return f
}

func decodeReception(body string) []byte {
	m := base64InReception.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m[1]))
	if err != nil {
		return nil
	}
	return decoded
}

// reset returns the stub to its default: RECIBIDA then AUTORIZADO, no sleep,
// nothing recorded.
func (f *fakeSRI) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = nil
	f.sleep = 0
	f.holdArrived, f.holdRelease = nil, nil
	f.reception = func(accessKey string) (int, string) { return http.StatusOK, receivedSOAP(accessKey) }
	f.authorization = func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) }
}

// holdAuthorization gates the NEXT autorización call: arrived is closed
// once the SRI has the query in hand, and the answer goes out only when
// release is called. One call, then the gate is gone. The caller must
// release within the SRI app's poll budget (testPollBudget) or the call
// times out on the platform's side and no outcome is applied at all.
func (f *fakeSRI) holdAuthorization() (arrived <-chan struct{}, release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, r := make(chan struct{}), make(chan struct{})
	f.holdArrived, f.holdRelease = a, r
	var once sync.Once
	return a, func() { once.Do(func() { close(r) }) }
}

// answerAsUsual restores the default answers — RECIBIDA then AUTORIZADO —
// without forgetting what was received, for a test that changes the SRI's
// mind mid-way (#474).
func (f *fakeSRI) answerAsUsual() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleep = 0
	f.reception = func(accessKey string) (int, string) { return http.StatusOK, receivedSOAP(accessKey) }
	f.authorization = func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) }
}

func (f *fakeSRI) setReception(fn func(accessKey string) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reception = fn
}

func (f *fakeSRI) setAuthorization(fn func(accessKey string) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authorization = fn
}

func (f *fakeSRI) setSleep(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleep = d
}

func (f *fakeSRI) lastReceived() (receivedComprobante, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.received) == 0 {
		return receivedComprobante{}, false
	}
	return f.received[len(f.received)-1], true
}

func (f *fakeSRI) receptionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

// ---- SOAP response builders (the SRI's shapes, ns2-prefixed like the live
// service) --------------------------------------------------------------

func receivedSOAP(accessKey string) string {
	return recepcionEnvelope(`<estado>RECIBIDA</estado><comprobantes/>`)
}

func returnedSOAP(accessKey string, messages string) string {
	return recepcionEnvelope(fmt.Sprintf(
		`<estado>DEVUELTA</estado><comprobantes><comprobante><claveAcceso>%s</claveAcceso><mensajes>%s</mensajes></comprobante></comprobantes>`,
		accessKey, messages))
}

// authorizedSOAP is AUTORIZADO as the test environment answers it: with
// advertencia 60 alone, the one every pruebas authorization carries.
func authorizedSOAP(accessKey string) string {
	return authorizedSOAPWithMessages(accessKey, testEnvironmentAdvertencia)
}

// testEnvironmentAdvertencia is the SRI's advertencia 60, on every document
// authorized in the pruebas environment.
const testEnvironmentAdvertencia = `<mensaje><identificador>60</identificador><mensaje>ESTE PROCESO FUE REALIZADO EN EL AMBIENTE DE PRUEBAS</mensaje><tipo>ADVERTENCIA</tipo></mensaje>`

// authorizedSOAPWithMessages is AUTORIZADO carrying the given mensajes —
// how the SRI authorizes a document and still warns about its Recipient
// (advertencias 59 / 62, #482).
func authorizedSOAPWithMessages(accessKey string, messages string) string {
	comprobante := "<![CDATA[<factura id=\"comprobante\" version=\"1.1.0\"/>]]>"
	return autorizacionEnvelope(accessKey, fmt.Sprintf(
		`<autorizaciones><autorizacion><estado>AUTORIZADO</estado><numeroAutorizacion>%s</numeroAutorizacion><fechaAutorizacion>2026-07-07T12:00:05-05:00</fechaAutorizacion><ambiente>PRUEBAS</ambiente><comprobante>%s</comprobante><mensajes>%s</mensajes></autorizacion></autorizaciones>`,
		accessKey, comprobante, messages))
}

func notAuthorizedSOAP(accessKey string, messages string) string {
	return autorizacionEnvelope(accessKey, fmt.Sprintf(
		`<autorizaciones><autorizacion><estado>NO AUTORIZADO</estado><fechaAutorizacion>2026-07-07T12:00:05-05:00</fechaAutorizacion><ambiente>PRUEBAS</ambiente><mensajes>%s</mensajes></autorizacion></autorizaciones>`,
		messages))
}

func inProcessingSOAP(accessKey string) string {
	return autorizacionEnvelope(accessKey,
		`<autorizaciones><autorizacion><estado>EN PROCESAMIENTO</estado><fechaAutorizacion></fechaAutorizacion><ambiente>PRUEBAS</ambiente><mensajes/></autorizacion></autorizaciones>`)
}

func emptyAuthorizationSOAP(accessKey string) string {
	return autorizacionEnvelope(accessKey, `<autorizaciones/>`)
}

func recepcionEnvelope(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:validarComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.recepcion"><RespuestaRecepcionComprobante>` +
		inner +
		`</RespuestaRecepcionComprobante></ns2:validarComprobanteResponse></soap:Body></soap:Envelope>`
}

func autorizacionEnvelope(accessKey, inner string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:autorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.autorizacion"><RespuestaAutorizacionComprobante><claveAccesoConsultada>%s</claveAccesoConsultada><numeroComprobantes>1</numeroComprobantes>%s</RespuestaAutorizacionComprobante></ns2:autorizacionComprobanteResponse></soap:Body></soap:Envelope>`, accessKey, inner)
}

func sriMessage(identifier, message, additional, kind string) string {
	return fmt.Sprintf(`<mensaje><identificador>%s</identificador><mensaje>%s</mensaje><informacionAdicional>%s</informacionAdicional><tipo>%s</tipo></mensaje>`,
		identifier, message, additional, kind)
}

// ---- the second app over the shared DB --------------------------------

func startSRIEnv(ctx context.Context, connStr string, email *platform.CaptureEmailSender) error {
	if sriStub == nil {
		sriStub = startSRIStub()
	}

	cfg := platform.Config{
		DatabaseURL:       connStr,
		RunMigrations:     false,
		StorefrontBaseURL: "http://storefront.example",
		StaffBaseURL:      "http://staff.example",
		Fees: platform.FeeConfig{
			FeeBasisPoints:    platform.DefaultPlatformFeeBasisPoints,
			FeeIVABasisPoints: platform.DefaultPlatformFeeIVABasisPoints,
		},
		// The same key the shared app keeps certificates under, so a
		// certificate uploaded through the shared app opens here for signing.
		InvoicingCertificateKey: sharedInvoicingKey(),
		// The single base-URL override points every SRI call at the fake.
		SRIBaseURL: sriStub.server.URL,
		// Sale Invoicing OPEN, which is not how it ships (#471, ADR 0060): the
		// suite proves the feature, and the one test of the closed flag boots
		// its own app without this line.
		SaleInvoicingEnabled: true,
	}

	app, err := server.NewApp(ctx, cfg,
		server.WithEmailSender(email),
		server.WithClock(func() time.Time { return fixedClock }),
		server.WithObjectStorage(&mockObjectStorage{}),
	)
	if err != nil {
		return fmt.Errorf("new sri app: %w", err)
	}
	// Milliseconds, not the production ~15 s: the tests prove the state
	// machine, not that Go can wait.
	app.InvoicingService.WithPollSchedule(testPollDelays, testPollBudget).WithSaleInvoiceKick(false)
	sriApp = app

	sriSrv = httptest.NewServer(server.NewHandler(app))
	sriEnv = &testEnv{
		server:     sriSrv,
		db:         app.DB.Pool,
		email:      email,
		fixedClock: fixedClock,
		service:    app.IdentityService,
	}
	return nil
}

func stopSRIEnv() {
	if sriSrv != nil {
		sriSrv.Close()
	}
	if sriApp != nil {
		_ = sriApp.Close()
	}
	if sriStub != nil {
		sriStub.server.Close()
	}
}

// allReceived is every document the fake took at recepción, in the order it
// took them: what a test asserts when the ORDER of two submissions is the
// rule under test (#483, ADR 0061).
func (f *fakeSRI) allReceived() []receivedComprobante {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]receivedComprobante, len(f.received))
	copy(out, f.received)
	return out
}
