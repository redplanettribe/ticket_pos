package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// What the Sale Invoice drain reports at Error (#804, ADR 0079): a definite
// refusal once, as the moment the ladder ends; an undecided document
// crossing 24 h once, worded as what it is; and nothing above Info for the
// hourly polls of a document already parked. Read through the invoicing
// service's logger, the seam ADR 0079 reports Errors behind.

const (
	refusalReport   = "invoicing: the tax authority refused a document; it is parked needs_attention and only an operator moves it on"
	undecidedReport = "invoicing: no definite answer from the tax authority in 24 h; parked needs_attention, still polling"
)

// captureInvoicingLog points the SRI app's invoicing logger at a capture for
// the length of one test.
func captureInvoicingLog(t *testing.T) *captureLogger {
	t.Helper()
	capture := &captureLogger{}
	sriApp.InvoicingService.WithLogger(capture)
	t.Cleanup(func() { sriApp.InvoicingService.WithLogger(platform.NewSlogLogger(sriApp.Logger, sriApp.Reporter)) })
	return capture
}

// errorLines is every Error the capture holds.
func errorLines(l *captureLogger) []capturedLine {
	var out []capturedLine
	for _, line := range l.snapshot() {
		if line.level == "error" {
			out = append(out, line)
		}
	}
	return out
}

// logArg is the value logged under key, printed, and whether it was.
func logArg(line capturedLine, key string) (string, bool) {
	for i := 0; i+1 < len(line.args); i += 2 {
		if k, ok := line.args[i].(string); ok && k == key {
			return fmt.Sprint(line.args[i+1]), true
		}
	}
	return "", false
}

func requireOneReport(t *testing.T, l *captureLogger, msg string) capturedLine {
	t.Helper()
	errs := errorLines(l)
	if len(errs) != 1 || errs[0].msg != msg {
		t.Fatalf("errors logged = %+v; want exactly one %q", errs, msg)
	}
	return errs[0]
}

func requireNoReport(t *testing.T, l *captureLogger) {
	t.Helper()
	if errs := errorLines(l); len(errs) != 0 {
		t.Fatalf("errors logged = %+v; want none", errs)
	}
}

func TestSaleInvoiceDrainReportsARefusalOnceWithTheSRICodes(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		stub   func()
	}{
		{"not authorized", "not_authorized", func() {
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, notAuthorizedSOAP(accessKey,
					sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR")+sriMessage("56", "ESTABLECIMIENTO CERRADO", "", "ERROR"))
			})
		}},
		{"rejected", "rejected", func() {
			sriStub.setReception(func(accessKey string) (int, string) {
				return http.StatusOK, returnedSOAP(accessKey, sriMessage("35", "DOCUMENTO INVALIDO", "El XML no cumple el esquema", "ERROR"))
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
			issuerReady(t, operatorSessionID)
			tc.stub()
			capture := captureInvoicingLog(t)

			if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
				t.Fatalf("drain = %+v; want the document parked needs_attention", result)
			}
			report := requireOneReport(t, capture, refusalReport)
			if id, _ := logArg(report, "invoice_id"); id != invoiceID {
				t.Fatalf("invoice_id = %v; want %s", id, invoiceID)
			}
			if kind, _ := logArg(report, "kind"); kind != "sale" {
				t.Fatalf("kind = %v; want sale", kind)
			}
			if answer, ok := logArg(report, "answer"); !ok || answer != tc.answer {
				t.Fatalf("answer = %v; want %s", answer, tc.answer)
			}
			codes, _ := logArg(report, "codes")
			want := map[string]string{"not_authorized": "[65 56]", "rejected": "[35]"}[tc.answer]
			if codes != want {
				t.Fatalf("codes = %q; want %s, identifiers only", codes, want)
			}
			if rendered := capture.rendered(); containsAny(rendered, "FECHA EMISION", "DOCUMENTO INVALIDO", "no cumple") {
				t.Fatalf("the SRI's free text reached the log:\n%s", rendered)
			}

			// The operator's Check status on the refused document hears the
			// same refusal: the status does not change, and nothing new gave up.
			capture.reset()
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
			})
			checkInvoice(t, operatorSessionID, invoiceID)
			requireNoReport(t, capture)
		})
	}
}

func TestSaleInvoiceDrainReportsAnUndecidedParkOnceAndTruthfully(t *testing.T) {
	cases := []struct {
		name string
		soap func(string) string
	}{
		{"received", inProcessingSOAP},
		{"unknown", emptyAuthorizationSOAP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
			issuerReady(t, operatorSessionID)
			sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, tc.soap(accessKey) })
			capture := captureInvoicingLog(t)

			if result := drainSaleInvoices(t); result.Pending != 1 {
				t.Fatalf("first drain = %+v; want pending", result)
			}
			requireNoReport(t, capture)

			atInvoicingClock(t, fixedClock.Add(24*time.Hour))
			if late := drainSaleInvoices(t); late.NeedsAttention != 1 {
				t.Fatalf("drain at 24 h = %+v; want the document parked needs_attention", late)
			}
			report := requireOneReport(t, capture, undecidedReport)
			if id, _ := logArg(report, "invoice_id"); id != invoiceID {
				t.Fatalf("invoice_id = %v; want %s", id, invoiceID)
			}

			// Every later poll that still hears no verdict reports nothing.
			for hour := 25; hour <= 27; hour++ {
				capture.reset()
				atInvoicingClock(t, fixedClock.Add(time.Duration(hour)*time.Hour))
				if again := drainSaleInvoices(t); again.Claimed != 1 || again.NeedsAttention != 1 {
					t.Fatalf("drain at %d h = %+v; want the document polled and still needs_attention", hour, again)
				}
				requireNoReport(t, capture)
			}

			// A refusal heard while it is still polled ends the ladder: the
			// drain gives up on it then, and says so once.
			capture.reset()
			sriStub.setAuthorization(func(accessKey string) (int, string) {
				return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
			})
			atInvoicingClock(t, fixedClock.Add(28*time.Hour))
			if refused := drainSaleInvoices(t); refused.Claimed != 1 || refused.NeedsAttention != 1 {
				t.Fatalf("drain with a late refusal = %+v; want needs_attention", refused)
			}
			requireOneReport(t, capture, refusalReport)
			if detail := getDrainedInvoice(t, operatorSessionID, invoiceID); detail.NextAttemptAt != nil {
				t.Fatalf("next_attempt_at after a refusal = %v; want off the ladder", detail.NextAttemptAt)
			}
		})
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
