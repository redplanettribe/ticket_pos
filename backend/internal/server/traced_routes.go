package server

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Trace flows, the "flow" tag of a request span that is always traced.
const (
	flowCheckout     = "checkout"
	flowPayment      = "payment"
	flowSRIInvoicing = "sri_invoicing"
)

// alwaysTraced names, in this one place, the routes every request of which is
// traced, whatever the Next app decided (ADR 0079): the flows that matter
// most are never missing from the sample. Every other request is traced one
// in ten, or as the Next app that started its trace decided.
//
//   - checkout: beginning a Customer's online checkout, where the Payment
//     Provider is asked for a payment;
//   - payment: settling a Payment on the Payment Provider's return, and
//     reversing a Ticket Sale, which asks the Payment Provider for the money
//     back, by the Customer, staff, an operator and the Reversal Reconciler;
//   - sri_invoicing: the routes that send a Tax Document to the SRI or ask it
//     about one: the Sale Invoice Drainer, an operator issuing, checking,
//     reissuing, issuing again and abandoning, and the closing sweep run
//     again.
var alwaysTraced = map[string]string{
	"POST /api/v1/customer/organizations/{slug}/events/{eventSlug}/checkout": flowCheckout,

	"POST /api/v1/public/checkout/{clientTransactionId}/confirm": flowPayment,
	"POST /api/v1/customer/ticket-sales/{ticketSaleId}/reverse":  flowPayment,
	"POST /api/v1/staff/events/{id}/sales/{saleId}/reverse":      flowPayment,
	"POST /api/v1/operator/sales/{confirmationRef}/reverse":      flowPayment,
	"POST /api/v1/internal/reversals/drain":                      flowPayment,

	"POST /api/v1/internal/sale-invoices/drain":                 flowSRIInvoicing,
	"POST /api/v1/operator/invoicing/invoices":                  flowSRIInvoicing,
	"POST /api/v1/operator/invoicing/invoices/{id}/check":       flowSRIInvoicing,
	"POST /api/v1/operator/invoicing/invoices/{id}/reissue":     flowSRIInvoicing,
	"POST /api/v1/operator/invoicing/invoices/{id}/issue-again": flowSRIInvoicing,
	"POST /api/v1/operator/invoicing/invoices/{id}/abandon":     flowSRIInvoicing,
	"POST /api/v1/operator/invoicing/closing-sweep/sweep-again": flowSRIInvoicing,
}

// neverTraced names the routes never traced: the health check, which the
// platform probes, and which would spend the quota on nothing.
var neverTraced = map[string]bool{
	"GET /health": true,
}

// traceSampling is the API's sampling at the head of a trace. random is the
// draw for an ordinary request, nil for math/rand/v2's.
func traceSampling(random func() float64) platform.TraceSampling {
	return platform.TraceSampling{Always: alwaysTraced, Never: neverTraced, Random: random}
}

// recordingRouter records every pattern registered through it.
type recordingRouter struct {
	next     Router
	patterns map[string]bool
}

func (r *recordingRouter) Handle(pattern string, handler http.Handler) {
	r.patterns[pattern] = true
	r.next.Handle(pattern, handler)
}

func (r *recordingRouter) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	r.Handle(pattern, http.HandlerFunc(handler))
}

// requireTracedRoutesRegistered panics, at construction, when a route
// alwaysTraced or neverTraced names was not registered: a route renamed
// without its line here would otherwise fall silently to one in ten.
func requireTracedRoutesRegistered(registered map[string]bool) {
	var missing []string
	for pattern := range alwaysTraced {
		if !registered[pattern] {
			missing = append(missing, pattern)
		}
	}
	for pattern := range neverTraced {
		if !registered[pattern] {
			missing = append(missing, pattern)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		panic(fmt.Sprintf("server: traced routes not registered: %q; update alwaysTraced or neverTraced", missing))
	}
}
