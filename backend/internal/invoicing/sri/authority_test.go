package sri

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// What the adapter makes of autorización's answers (#514, parent #513).
//
// THE TWO UNDECIDED ANSWERS ARE NOT THE SAME ANSWER. "EN PROCESAMIENTO" is
// the SRI saying it holds the document and has not finished with it; an
// answer with no autorizacion at all under the clave is the SRI saying it
// has never heard of it — which is what a Submit that died in transport
// leaves behind. The first is received, the second is unknown, and the
// platform's Drainer and staff hints read the difference (#515, #516).

func processingResponse(state string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><ns2:autorizacionComprobanteResponse xmlns:ns2="http://ec.gob.sri.ws.autorizacion"><RespuestaAutorizacionComprobante><claveAccesoConsultada>x</claveAccesoConsultada><numeroComprobantes>1</numeroComprobantes><autorizaciones><autorizacion><estado>%s</estado><fechaAutorizacion></fechaAutorizacion><ambiente>PRUEBAS</ambiente><comprobante><![CDATA[<factura/>]]></comprobante><mensajes/></autorizacion></autorizaciones></RespuestaAutorizacionComprobante></ns2:autorizacionComprobanteResponse></soap:Body></soap:Envelope>`, state)
}

func authorityAgainst(t *testing.T, response string) *Authority {
	t.Helper()
	fake := newFakeSRI(t, func(string, []byte) (int, string) { return http.StatusOK, response })
	return NewAuthority(invoicing.EnvironmentTest, WithBaseURL(fake.URL))
}

// TestQueryOutcomeUnknownWhenNothingIsKnown: numeroComprobantes 0 and an
// empty autorizaciones list is the authority having no record of the
// reference — its own outcome, never "received".
func TestQueryOutcomeUnknownWhenNothingIsKnown(t *testing.T) {
	a := authorityAgainst(t, emptyAuthorizationResponse)

	outcome, err := a.QueryOutcome(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != invoicing.OutcomeUnknown {
		t.Fatalf("state = %q, want %q", outcome.State, invoicing.OutcomeUnknown)
	}
	if outcome.AlreadyHeld {
		t.Fatal("a document the authority has no record of is not held by it")
	}
}

// TestQueryOutcomeInProcessingIsReceived: both spellings the SRI uses for
// "still working on it" stay received — the authority does hold it.
func TestQueryOutcomeInProcessingIsReceived(t *testing.T) {
	for _, state := range []string{"EN PROCESO", "EN PROCESAMIENTO"} {
		t.Run(state, func(t *testing.T) {
			a := authorityAgainst(t, processingResponse(state))

			outcome, err := a.QueryOutcome(context.Background(), "x")
			if err != nil {
				t.Fatal(err)
			}
			if outcome.State != invoicing.OutcomeReceived {
				t.Fatalf("state = %q, want %q", outcome.State, invoicing.OutcomeReceived)
			}
		})
	}
}

// TestQueryOutcomeDefiniteAnswers: the two verdicts that settle a document
// are unchanged by the new outcome word.
func TestQueryOutcomeDefiniteAnswers(t *testing.T) {
	a := authorityAgainst(t, authorizedResponse)
	outcome, err := a.QueryOutcome(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != invoicing.OutcomeAuthorized || outcome.AuthorizationNumber == "" || len(outcome.AuthorityXML) == 0 {
		t.Fatalf("outcome = %+v, want authorized with its number and XML", outcome)
	}

	a = authorityAgainst(t, notAuthorizedResponse)
	outcome, err = a.QueryOutcome(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's last entry is EN PROCESO, and only the last is read.
	if outcome.State != invoicing.OutcomeReceived {
		t.Fatalf("state = %q, want the last entry's %q", outcome.State, invoicing.OutcomeReceived)
	}
}
