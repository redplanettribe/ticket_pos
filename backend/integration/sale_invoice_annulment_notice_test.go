package integration

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer's Annulment Notice (#748, spec #742, ADR 0078), at the HTTP seam:
// owed when Mark annulled is recorded on a Sale Invoice the buyer was sent,
// held while the Annulment Check is checking or at mismatch, sent by the
// Drainer's tick once it is confirmed or unconfirmed, at most once and
// retried on the delivery's ladder, to the factura's Recipient email in the
// Sale's Locale, and worded by the Ticket Sale as it stands at send time.
// Never owed for a manual Tax Invoice or a factura the buyer was never sent.
// The follow-up factura's mail says it replaces a factura annulled at the
// SRI, and the Customer Area lists the annulled factura without downloads.
//
// Everything is read back from the captured mail, the drain response, the
// factura's detail (`annulment.current.notice`) and the customer routes.
// Every factura is issued and annulled at fixedClock and the check walked by
// tickAt, as sale_invoice_annulment_followups_test.go walks it.

// annulmentNoticeView is `annulment.current.notice` on the detail.
type annulmentNoticeView struct {
	State  string  `json:"state"`
	SentAt *string `json:"sent_at"`
}

// noticeOf reads the standing annulment's notice.
func noticeOf(t *testing.T, sessionID, id string) annulmentNoticeView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view struct {
		Status    string `json:"status"`
		Annulment *struct {
			Current *struct {
				Notice *annulmentNoticeView `json:"notice"`
			} `json:"current"`
		} `json:"annulment"`
	}
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	if view.Annulment == nil || view.Annulment.Current == nil || view.Annulment.Current.Notice == nil {
		t.Fatalf("invoice %s (%s) has no annulment notice", id, view.Status)
	}
	return *view.Annulment.Current.Notice
}

// noticeDrain is the drain response's Annulment Notice counts.
type noticeDrain struct {
	AnnulmentNotice struct {
		Sent   int `json:"sent"`
		Failed int `json:"failed"`
	} `json:"annulment_notice"`
}

// tickNoticesAt moves the clock to at, ticks once and returns the notice
// counts.
func tickNoticesAt(t *testing.T, at time.Time) noticeDrain {
	t.Helper()
	atInvoicingClock(t, at)
	resp, body := sriEnv.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var result noticeDrain
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode drain result: %v", err)
	}
	return result
}

func noticesSent(env *testEnv) []platform.AnnulmentNotice {
	return env.email.AnnulmentNoticesSent()
}

func wantNotices(t *testing.T, what string, env *testEnv, n int) []platform.AnnulmentNotice {
	t.Helper()
	sent := noticesSent(env)
	if len(sent) != n {
		t.Fatalf("%s: %d annulment notices sent; want %d: %+v", what, len(sent), n, sent)
	}
	return sent
}

func wantNoticeState(t *testing.T, what, sessionID, id, state string) annulmentNoticeView {
	t.Helper()
	notice := noticeOf(t, sessionID, id)
	if notice.State != state {
		t.Fatalf("%s: notice = %+v; want %s", what, notice, state)
	}
	if (state == "sent") != (notice.SentAt != nil) {
		t.Fatalf("%s: notice = %+v; want sent_at exactly when sent", what, notice)
	}
	return notice
}

// numberOfInvoice is a factura's printed number from the detail.
func numberOfInvoice(t *testing.T, sessionID, id string) string {
	t.Helper()
	detail := getReissuedInvoice(t, sessionID, id)
	if detail.Number == nil {
		t.Fatalf("invoice %s has no number", id)
	}
	return *detail.Number
}

// TestAnnulmentNoticeIsHeldWhileCheckingAndSentOnceConfirmed: annulling a
// delivered Sale Invoice owes the notice and holds it while the check is
// checking; the tick whose ask reads ANULADO sends it, once, to the
// factura's Recipient email in the Sale's Locale, standing-Sale words, the
// number, the reference and the Customer Area link; later ticks send
// nothing more.
func TestAnnulmentNoticeIsHeldWhileCheckingAndSentOnceConfirmed(t *testing.T) {
	env, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
	saleID := saleIDByRef(t, env, ref)
	wantNoticeState(t, "just annulled", operatorSessionID, invoiceID, "held")

	sriStub.setConsulta(consultaRefused)
	if result := tickNoticesAt(t, fixedClock.Add(time.Minute)); result.AnnulmentNotice.Sent != 0 {
		t.Fatalf("tick while checking = %+v; want nothing sent", result)
	}
	wantNotices(t, "while checking", env, 0)
	wantNoticeState(t, "while checking", operatorSessionID, invoiceID, "held")

	sriStub.setConsulta(consultaAnnulled)
	if result := tickNoticesAt(t, fixedClock.Add(5*time.Minute)); result.AnnulmentNotice.Sent != 1 {
		t.Fatalf("tick reading ANULADO = %+v; want the notice sent", result)
	}
	notice := wantNotices(t, "once confirmed", env, 1)[0]
	if notice.To != "guest@example.com" || notice.Reference != ref || notice.EventName != "House Fest" || notice.SaleReversed {
		t.Fatalf("notice = %+v; want the buyer told of a standing Sale's factura", notice)
	}
	if notice.Locale != platform.LocaleEN {
		t.Fatalf("notice locale = %s; want en for a Sale made in English", notice.Locale)
	}
	wantLink := "http://storefront.example/tickets#sale-" + saleID
	if notice.CustomerAreaURL != wantLink {
		t.Fatalf("customer area link = %q; want %q", notice.CustomerAreaURL, wantLink)
	}
	text := notice.Text()
	for _, want := range []string{
		numberOfInvoice(t, operatorSessionID, invoiceID), ref, "annulled at the SRI", "no tax effect",
		"Your purchase and your tickets are unchanged", "A new tax invoice (factura) will follow by email", wantLink,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("notice text lacks %q:\n%s", want, text)
		}
	}
	sent := wantNoticeState(t, "once confirmed", operatorSessionID, invoiceID, "sent")
	if at := timeOf(t, sent.SentAt); !at.Equal(fixedClock.Add(5 * time.Minute)) {
		t.Fatalf("sent_at = %s; want the tick's instant", at)
	}

	for _, after := range []time.Duration{15 * time.Minute, 2 * time.Hour} {
		if result := tickNoticesAt(t, fixedClock.Add(after)); result.AnnulmentNotice.Sent != 0 {
			t.Fatalf("tick at +%s = %+v; want nothing more sent", after, result)
		}
	}
	wantNotices(t, "later ticks", env, 1)
}

// TestAnnulmentNoticeIsReleasedByUnconfirmedAndHeldAtMismatch: an hour of no
// readings releases the notice with the +60 min ask; two AUTORIZADO across
// thirty minutes keep holding it, and a later ANULADO releases it.
func TestAnnulmentNoticeIsReleasedByUnconfirmedAndHeldAtMismatch(t *testing.T) {
	t.Run("unconfirmed", func(t *testing.T) {
		env, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
		sriStub.setConsulta(consultaRefused)
		for _, after := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute} {
			tickAt(t, fixedClock.Add(after))
		}
		wantNotices(t, "before +60 min", env, 0)
		wantNoticeState(t, "before +60 min", operatorSessionID, invoiceID, "held")

		if result := tickNoticesAt(t, fixedClock.Add(time.Hour)); result.AnnulmentNotice.Sent != 1 {
			t.Fatalf("the +60 min tick = %+v; want the notice sent on unconfirmed", result)
		}
		wantNotices(t, "unconfirmed", env, 1)
		wantNoticeState(t, "unconfirmed", operatorSessionID, invoiceID, "sent")
	})

	t.Run("mismatch", func(t *testing.T) {
		env, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
		checkToMismatch(t, operatorSessionID, invoiceID)
		wantNotices(t, "at mismatch", env, 0)
		wantNoticeState(t, "at mismatch", operatorSessionID, invoiceID, "held")

		// Past the hour the rule would read unconfirmed with no AUTORIZADO;
		// a mismatch never returns to it, and the notice stays held.
		tickAt(t, fixedClock.Add(time.Hour))
		wantNotices(t, "at mismatch past the hour", env, 0)

		sriStub.setConsulta(consultaAnnulled)
		if result := tickNoticesAt(t, fixedClock.Add(7*time.Hour)); result.AnnulmentNotice.Sent != 1 {
			t.Fatalf("the ANULADO tick = %+v; want the held notice sent", result)
		}
		wantNotices(t, "after ANULADO", env, 1)
		wantNoticeState(t, "after ANULADO", operatorSessionID, invoiceID, "sent")
	})
}

// TestAHeldNoticeReleasedAfterAnOperatorReversalSaysNothingFollows: the
// notice is worded at send time, so one held while the Sale stood and sent
// after an Operator Reversal says nothing follows.
func TestAHeldNoticeReleasedAfterAnOperatorReversalSaysNothingFollows(t *testing.T) {
	env, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
	wantNoticeState(t, "standing", operatorSessionID, invoiceID, "held")
	reversalRoutes[1].reverse(t, env, operatorSessionID, ref)

	checkToConfirmed(t, operatorSessionID, invoiceID)
	notice := wantNotices(t, "after the reversal", env, 1)[0]
	if !notice.SaleReversed {
		t.Fatalf("notice = %+v; want the reversed Sale's words", notice)
	}
	text := notice.Text()
	if !strings.Contains(text, "Nothing else follows") || strings.Contains(text, "will follow by email") || strings.Contains(text, "unchanged") {
		t.Fatalf("notice text is not the reversed wording:\n%s", text)
	}
}

// TestAnnulmentNoticeIsInTheSalesLocaleAndRetriedOnASendFailure: a Spanish
// Sale's notice is written in Spanish; a sender that refuses it leaves it
// owed and due again on the ladder's first rung, a tick before then sends
// nothing, and the tick after the sender recovers sends it once.
func TestAnnulmentNoticeIsInTheSalesLocaleAndRetriedOnASendFailure(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)
	body := checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1))
	body["locale"] = "es"
	paidCheckoutApproved(t, "house-fest", body)
	issuerReady(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Delivered != 1 {
		t.Fatalf("setup drain = %+v; want delivered", result)
	}
	invoiceID := getSaleInvoiceList(t, operatorSessionID).Data[0].ID
	annulOK(t, operatorSessionID, invoiceID)

	env.email.FailWith(errors.New("resend returned 503"))
	t.Cleanup(func() { env.email.FailWith(nil) })
	sriStub.setConsulta(consultaAnnulled)
	if result := tickNoticesAt(t, fixedClock.Add(time.Minute)); result.AnnulmentNotice.Sent != 0 || result.AnnulmentNotice.Failed != 1 {
		t.Fatalf("tick with the sender down = %+v; want one failed send", result)
	}
	wantNotices(t, "sender down", env, 0)
	wantNoticeState(t, "sender down", operatorSessionID, invoiceID, "due")

	env.email.FailWith(nil)
	if result := tickNoticesAt(t, fixedClock.Add(90*time.Second)); result.AnnulmentNotice.Sent != 0 || result.AnnulmentNotice.Failed != 0 {
		t.Fatalf("tick before the rung = %+v; want nothing tried", result)
	}
	if result := tickNoticesAt(t, fixedClock.Add(2*time.Minute)); result.AnnulmentNotice.Sent != 1 {
		t.Fatalf("tick at the rung = %+v; want the notice sent", result)
	}
	notice := wantNotices(t, "after the retry", env, 1)[0]
	if notice.Locale != platform.LocaleES {
		t.Fatalf("notice locale = %s; want es for a Sale made in Spanish", notice.Locale)
	}
	if subject, text := notice.Subject(), notice.Text(); !strings.Contains(subject, "fue anulada en el SRI") ||
		!strings.Contains(text, "Su compra y sus entradas no cambian") || strings.Contains(text, "Hi ") {
		t.Fatalf("notice is not the Spanish standing wording:\n%s\n%s", subject, text)
	}
	tickNoticesAt(t, fixedClock.Add(time.Hour))
	wantNotices(t, "a later tick", env, 1)
}

// TestNoAnnulmentNoticeForAManualTaxInvoiceOrAnUndeliveredFactura: a manual
// Tax Invoice is never followed by a notice, and neither is a Sale Invoice
// the buyer was never sent - one annulled while parked, and one authorized
// whose delivery the sender refused.
func TestNoAnnulmentNoticeForAManualTaxInvoiceOrAnUndeliveredFactura(t *testing.T) {
	t.Run("a manual tax invoice", func(t *testing.T) {
		operatorSessionID, invoiceID := annulledManualInvoiceAt(t, fixedClock)
		wantNoticeState(t, "manual", operatorSessionID, invoiceID, "not_owed")
		sriStub.setConsulta(consultaAnnulled)
		tickNoticesAt(t, fixedClock.Add(time.Minute))
		wantNotices(t, "manual", sriEnv, 0)
	})

	t.Run("a parked sale invoice", func(t *testing.T) {
		env := setupTest(t)
		sriStub.reset()
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		issuerReady(t, operatorSessionID)
		sriStub.setAuthorization(func(accessKey string) (int, string) {
			return http.StatusOK, notAuthorizedSOAP(accessKey, sriMessage("65", "FECHA EMISION EXTEMPORANEA", "", "ERROR"))
		})
		if result := drainSaleInvoices(t); result.NeedsAttention != 1 {
			t.Fatalf("setup drain = %+v; want parked", result)
		}
		annulOK(t, operatorSessionID, invoiceID)
		wantNoticeState(t, "parked", operatorSessionID, invoiceID, "not_owed")
		checkToConfirmed(t, operatorSessionID, invoiceID)
		wantNotices(t, "parked", env, 0)
	})

	t.Run("an undelivered authorized sale invoice", func(t *testing.T) {
		env := setupTest(t)
		sriStub.reset()
		operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
		issuerReady(t, operatorSessionID)
		env.email.FailWith(errors.New("resend returned 503"))
		t.Cleanup(func() { env.email.FailWith(nil) })
		if result := drainSaleInvoices(t); result.Authorized != 1 || result.Delivered != 0 {
			t.Fatalf("setup drain = %+v; want authorized and undelivered", result)
		}
		env.email.FailWith(nil)
		annulOK(t, operatorSessionID, invoiceID)
		wantNoticeState(t, "undelivered", operatorSessionID, invoiceID, "not_owed")
		checkToConfirmed(t, operatorSessionID, invoiceID)
		wantNotices(t, "undelivered", env, 0)
		if n := len(deliveriesSent(t, env)); n != 0 {
			t.Fatalf("%d deliveries; want the annulled factura never mailed", n)
		}
		// The buyer could still have downloaded it from the Customer Area
		// while it stood authorized, so it stays listed, as annulled.
		ref := lastConfirmation(t, env).Reference
		saleID := saleIDByRef(t, env, ref)
		docs, _ := listCustomerDocuments(t, buyerSession(t, env, "guest@example.com"), saleID)
		if len(docs) != 1 || docs[0].Status != "annulled" {
			t.Fatalf("documents = %+v; want the authorized-then-annulled factura listed as annulled", docs)
		}
	})
}

// TestADeliveryThatRacesMarkAnnulledStillOwesTheNotice: the delivery mail is
// in flight when the operator records the annulment, so the annulment reads
// no delivery and owes nothing; the delivery's stamp then owes the notice,
// and the buyer, who did get the factura, is told of its annulment.
func TestADeliveryThatRacesMarkAnnulledStillOwesTheNotice(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	env.email.SlowTaxDocumentDeliveryBy(time.Second)
	t.Cleanup(func() { env.email.SlowTaxDocumentDeliveryBy(0) })

	done := make(chan saleInvoiceDrainResult, 1)
	go func() { done <- drainSaleInvoices(t) }()
	deadline := time.Now().Add(5 * time.Second)
	for getDrainedInvoice(t, operatorSessionID, invoiceID).Status != "authorized" {
		if time.Now().After(deadline) {
			t.Fatal("the factura was never authorized")
		}
		time.Sleep(20 * time.Millisecond)
	}
	annulOK(t, operatorSessionID, invoiceID)
	if result := <-done; result.Delivered != 1 {
		t.Fatalf("drain = %+v; want the in-flight delivery recorded", result)
	}
	env.email.SlowTaxDocumentDeliveryBy(0)

	wantNoticeState(t, "after the late stamp", operatorSessionID, invoiceID, "held")
	checkToConfirmed(t, operatorSessionID, invoiceID)
	wantNotices(t, "after the race", env, 1)
}

// TestTheFollowUpFacturaSaysItReplacesAnAnnulledOne: the factura Issue again
// or a Sale Invoice Reissue owes after an annulment is mailed with the
// annulled replaces line, never the credit note's.
func TestTheFollowUpFacturaSaysItReplacesAnAnnulledOne(t *testing.T) {
	follow := map[string]func(t *testing.T, sessionID, invoiceID string) string{
		"issue again": func(t *testing.T, sessionID, invoiceID string) string {
			return issueAgainOK(t, sessionID, invoiceID, nil).ID
		},
		"reissue": func(t *testing.T, sessionID, invoiceID string) string {
			return reissueOK(t, sessionID, invoiceID, companyRecipient()).ID
		},
	}
	for name, press := range follow {
		t.Run(name, func(t *testing.T) {
			env, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
			checkToConfirmed(t, operatorSessionID, invoiceID)
			successorID := press(t, operatorSessionID, invoiceID)
			sriStub.answerAsUsual()
			if result := drainSaleInvoices(t); result.Authorized != 1 || result.Delivered != 1 {
				t.Fatalf("drain = %+v; want the successor authorized and delivered", result)
			}
			sent := deliveriesSent(t, env)
			mail := sent[len(sent)-1]
			if mail.Kind != "sale" || mail.Replaces != platform.TaxDocumentReplacesAnnulled {
				t.Fatalf("successor %s mail kind %s replaces %q; want a factura replacing an annulled one", successorID, mail.Kind, mail.Replaces)
			}
			text := mail.Text()
			if !strings.Contains(text, "which was annulled at the SRI") || strings.Contains(strings.ToLower(text), "credit note") {
				t.Fatalf("successor mail lacks the annulled replaces line:\n%s", text)
			}
		})
	}
}

// TestTheCustomerAreaListsAnAnnulledFacturaWithoutDownloads: the buyer's own
// read lists the annulled factura with the status `annulled` and no
// download, and both its XML and RIDE routes refuse it.
func TestTheCustomerAreaListsAnAnnulledFacturaWithoutDownloads(t *testing.T) {
	env, _, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
	saleID := saleIDByRef(t, env, ref)
	buyer := buyerSession(t, env, "guest@example.com")

	docs, _ := listCustomerDocuments(t, buyer, saleID)
	if len(docs) != 1 || docs[0].ID != invoiceID || docs[0].Kind != "sale" || docs[0].Status != "annulled" {
		t.Fatalf("documents = %+v; want the annulled factura", docs)
	}
	assertCustomerDownloads(t, "annulled", docs[0], saleID, false)

	for _, path := range []string{customerDocumentXMLPath(saleID, invoiceID), customerDocumentRIDEPath(saleID, invoiceID)} {
		resp, body := sriEnv.getRaw(t, path, authHeader(buyer))
		if errEnv := decodeErrorEnvelope(t, body); resp.StatusCode != http.StatusNotFound || errEnv.Error == nil || errEnv.Error.Code != "INVOICE_NOT_FOUND" {
			t.Fatalf("%s: status=%d body=%.200s; want 404 INVOICE_NOT_FOUND", path, resp.StatusCode, body)
		}
	}
}
