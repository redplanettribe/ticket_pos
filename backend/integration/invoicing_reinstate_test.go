package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Reinstate (#749, spec #742, ADR 0078 section 5), at the HTTP seam: an
// annulled factura whose Annulment Check stands at mismatch once its
// Annulment Window has closed returns to authorized, and the platform's
// record follows the SRI's. Refused inside the window and on every other
// verdict. One rule decides the Credit Note it owes: the reversal's reason
// on a reversed Sale, the supersede reason where a successor stands,
// nothing otherwise, and nothing on a manual Tax Invoice. A factura
// annulled from pending fetches its authorization from autorizacion. The
// held Annulment Notice is dropped; one already sent owes the
// reinstatement notice with the RIDE and XML when no Credit Note follows.
// The annulment stays as history, and the factura can be annulled again
// under the late acknowledgement with a fresh check.
//
// Every factura is issued and annulled at fixedClock (2026-07-07), whose
// window closes at the end of 2026-08-07 in Guayaquil; the check is walked
// by tickAt as sale_invoice_annulment_followups_test.go walks it, and the
// Reinstate is pressed at afterTheWindow.

// afterTheWindow is a moment past fixedClock's Annulment Window.
var afterTheWindow = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

// annulmentRecordRow is one annulment on the detail's `annulment`.
type annulmentRecordRow struct {
	AnnulledBy          string               `json:"annulled_by"`
	ReinstatedAt        *string              `json:"reinstated_at"`
	ReinstatedBy        *string              `json:"reinstated_by"`
	ReinstateNote       *string              `json:"reinstate_note"`
	Notice              *annulmentNoticeView `json:"notice"`
	ReinstatementNotice *annulmentNoticeView `json:"reinstatement_notice"`
	CheckTrail          *struct {
		Verdict string `json:"verdict"`
		Asks    []struct {
			Reading string `json:"reading"`
		} `json:"asks"`
	} `json:"check_trail"`
}

// reinstatedView is the detail as these tests read it.
type reinstatedView struct {
	ID                  string  `json:"id"`
	Status              string  `json:"status"`
	AnnulledBy          *string `json:"annulled_by"`
	AnnulledAt          *string `json:"annulled_at"`
	CreditedByInvoiceID *string `json:"credited_by_invoice_id"`
	Annulment           *struct {
		Current   *annulmentRecordRow  `json:"current"`
		Past      []annulmentRecordRow `json:"past"`
		Check     *annulmentCheckView  `json:"check"`
		Reinstate *struct {
			Admitted bool `json:"admitted"`
			Refusal  *struct {
				Code string `json:"code"`
			} `json:"refusal"`
		} `json:"reinstate"`
	} `json:"annulment"`
}

func reinstatePath(id string) string { return invoicesPath + "/" + id + "/reinstate" }

// pressReinstate presses Reinstate with the body as given.
func pressReinstate(t *testing.T, sessionID, id string, body any) (*http.Response, envelope) {
	t.Helper()
	return sriEnv.post(t, reinstatePath(id), body, authHeader(sessionID))
}

// reinstateConfirm is the confirm the panel sends: the number typed in
// full and the note.
func reinstateConfirm(t *testing.T, sessionID, id, note string) map[string]any {
	t.Helper()
	return map[string]any{"number": numberOfInvoice(t, sessionID, id), "note": note}
}

// reinstateOK reinstates with the right number and returns the response.
func reinstateOK(t *testing.T, sessionID, id, note string) reinstatedView {
	t.Helper()
	resp, env := pressReinstate(t, sessionID, id, reinstateConfirm(t, sessionID, id, note))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reinstate status=%d error=%+v", resp.StatusCode, env.Error)
	}
	return decodeReinstated(t, env)
}

func decodeReinstated(t *testing.T, env envelope) reinstatedView {
	t.Helper()
	var view reinstatedView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return view
}

func getReinstated(t *testing.T, sessionID, id string) reinstatedView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	return decodeReinstated(t, env)
}

// wantReinstateRefusal presses Reinstate with the right number and asserts
// the refusal, and that the detail's `annulment.reinstate` says the same.
func wantReinstateRefusal(t *testing.T, what, sessionID, id, code string) {
	t.Helper()
	resp, body := pressReinstate(t, sessionID, id, reinstateConfirm(t, sessionID, id, ""))
	expectRefusal(t, what, resp, body, http.StatusConflict, code)
	view := getReinstated(t, sessionID, id)
	if view.Status != "annulled" {
		t.Fatalf("%s: the factura is %s after the refusal; want annulled", what, view.Status)
	}
	r := view.Annulment.Reinstate
	if r == nil || r.Admitted || r.Refusal == nil || r.Refusal.Code != code {
		t.Fatalf("%s: annulment.reinstate = %+v; want refused with %s", what, r, code)
	}
}

// mismatchedSaleInvoicePastItsWindow drives a delivered House Sale Invoice to a
// mismatch and moves the clock past its window: what Reinstate admits.
func mismatchedSaleInvoicePastItsWindow(t *testing.T) (env *testEnv, operatorSessionID, invoiceID, ref string) {
	t.Helper()
	env, operatorSessionID, invoiceID, ref = annulledAuthorizedSaleInvoice(t)
	checkToMismatch(t, operatorSessionID, invoiceID)
	atInvoicingClock(t, afterTheWindow)
	return env, operatorSessionID, invoiceID, ref
}

// TestReinstateIsRefusedInsideTheWindowAndOnEveryOtherVerdict: never an
// undo inside the window, even at mismatch; past it, refused while the check
// is checking, confirmed or unconfirmed. A wrong number and a factura that
// is not annulled are refused by their own codes.
func TestReinstateIsRefusedInsideTheWindowAndOnEveryOtherVerdict(t *testing.T) {
	t.Run("at mismatch inside the window", func(t *testing.T) {
		_, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
		checkToMismatch(t, operatorSessionID, invoiceID)
		atInvoicingClock(t, time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC))
		wantReinstateRefusal(t, "inside the window", operatorSessionID, invoiceID, "INVOICE_REINSTATE_WINDOW_OPEN")
		if item := needsAttentionItem(t, operatorSessionID, invoiceID); item == nil || item.ReinstateAdmitted {
			t.Fatalf("queue item inside the window = %+v; want it listed, Reinstate not offered", item)
		}
	})
	t.Run("confirmed", func(t *testing.T) {
		_, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
		checkToConfirmed(t, operatorSessionID, invoiceID)
		atInvoicingClock(t, afterTheWindow)
		wantReinstateRefusal(t, "confirmed", operatorSessionID, invoiceID, "INVOICE_REINSTATE_NOT_MISMATCH")
	})
	t.Run("unconfirmed", func(t *testing.T) {
		_, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
		checkToUnconfirmed(t, operatorSessionID, invoiceID)
		atInvoicingClock(t, afterTheWindow)
		wantReinstateRefusal(t, "unconfirmed", operatorSessionID, invoiceID, "INVOICE_REINSTATE_NOT_MISMATCH")
	})
	t.Run("checking", func(t *testing.T) {
		// Annulled late, past the window with the acknowledgement, and
		// pressed five minutes later, while the check is still checking.
		env := setupTest(t)
		sriStub.reset()
		operatorSessionID, invoiceID, _ := houseSaleAuthorized(t, env)
		atInvoicingClock(t, afterTheWindow)
		annulOK(t, operatorSessionID, invoiceID)
		atInvoicingClock(t, afterTheWindow.Add(5*time.Minute))
		if _, check := getAnnulmentCheck(t, operatorSessionID, invoiceID); check.Verdict != "checking" {
			t.Fatalf("setup: check = %+v; want checking", check)
		}
		wantReinstateRefusal(t, "checking", operatorSessionID, invoiceID, "INVOICE_REINSTATE_NOT_MISMATCH")
	})
	t.Run("a wrong number", func(t *testing.T) {
		_, operatorSessionID, invoiceID, _ := mismatchedSaleInvoicePastItsWindow(t)
		resp, body := pressReinstate(t, operatorSessionID, invoiceID, map[string]any{"number": "001-001-999999999"})
		expectRefusal(t, "a wrong number", resp, body, http.StatusConflict, "INVOICE_NUMBER_MISMATCH")
		resp, body = pressReinstate(t, operatorSessionID, invoiceID, nil)
		expectRefusal(t, "no number", resp, body, http.StatusConflict, "INVOICE_NUMBER_MISMATCH")
		if view := getReinstated(t, operatorSessionID, invoiceID); view.Status != "annulled" ||
			view.Annulment.Reinstate == nil || !view.Annulment.Reinstate.Admitted {
			t.Fatalf("after a mistyped press: %+v; want still annulled, Reinstate admitted", view)
		}
	})
	t.Run("not annulled", func(t *testing.T) {
		env := setupTest(t)
		sriStub.reset()
		operatorSessionID, invoiceID, _ := houseSaleAuthorized(t, env)
		atInvoicingClock(t, afterTheWindow)
		resp, body := pressReinstate(t, operatorSessionID, invoiceID, reinstateConfirm(t, operatorSessionID, invoiceID, ""))
		expectRefusal(t, "an authorized factura", resp, body, http.StatusConflict, "INVOICE_NOT_REINSTATABLE")
		if view := getReinstated(t, operatorSessionID, invoiceID); view.Annulment == nil || view.Annulment.Reinstate != nil {
			t.Fatalf("annulment.reinstate on an authorized factura = %+v; want null", view.Annulment)
		}
	})
}

// TestReinstateWithNothingAfterItIsTheSalesCurrentFacturaAgain: the held
// notice is dropped and never sent, no Credit Note is owed, the factura is
// current and authorized again everywhere - the detail, the queue, the
// Sale lookup and the Customer Area with its downloads - the annulment is
// kept as history with its check trail, and annulling it again needs the
// late acknowledgement and starts a fresh check.
func TestReinstateWithNothingAfterItIsTheSalesCurrentFacturaAgain(t *testing.T) {
	env, operatorSessionID, invoiceID, ref := mismatchedSaleInvoicePastItsWindow(t)
	wantNoticeState(t, "at mismatch", operatorSessionID, invoiceID, "held")
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count at mismatch = %d; want 1", n)
	}
	if item := needsAttentionItem(t, operatorSessionID, invoiceID); item == nil || item.AttentionReason != "annulment_mismatch" || !item.ReinstateAdmitted {
		t.Fatalf("queue item past the window = %+v; want the mismatch, Reinstate offered", item)
	}
	deliveries := len(deliveriesSent(t, env))

	view := reinstateOK(t, operatorSessionID, invoiceID, "the portal never took it")
	if view.Status != "authorized" || view.AnnulledBy != nil || view.AnnulledAt != nil {
		t.Fatalf("reinstated = %+v; want authorized with no annulled_by or annulled_at", view)
	}
	if view.Annulment == nil || view.Annulment.Current != nil || len(view.Annulment.Past) != 1 || view.Annulment.Reinstate != nil {
		t.Fatalf("annulment = %+v; want no current annulment, one past, no reinstate", view.Annulment)
	}
	past := view.Annulment.Past[0]
	if past.AnnulledBy != "operator@example.com" || deref(past.ReinstatedBy) != "operator@example.com" ||
		!timeOf(t, past.ReinstatedAt).Equal(afterTheWindow) || deref(past.ReinstateNote) != "the portal never took it" {
		t.Fatalf("past annulment = %+v; want who annulled, who reinstated, when and the note", past)
	}
	if past.Notice == nil || past.Notice.State != "dropped" || past.Notice.SentAt != nil {
		t.Fatalf("the held notice = %+v; want dropped", past.Notice)
	}
	if past.ReinstatementNotice == nil || past.ReinstatementNotice.State != "not_owed" {
		t.Fatalf("reinstatement notice = %+v; want not owed, the buyer was never told", past.ReinstatementNotice)
	}
	if past.CheckTrail == nil || past.CheckTrail.Verdict != "mismatch" || len(past.CheckTrail.Asks) != 4 {
		t.Fatalf("check trail = %+v; want the mismatch and its four asks kept", past.CheckTrail)
	}

	if notes := creditNotesOfSale(t, operatorSessionID, ref); len(notes) != 0 {
		t.Fatalf("the Sale's Credit Notes = %+v; want none, nothing came after it", notes)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("count after the Reinstate = %d; want 0", n)
	}
	docs := saleLookupDocuments(t, env, operatorSessionID, ref)
	if len(docs) != 1 || docs[0].ID != invoiceID || docs[0].Status != "authorized" || docs[0].Role != "current" {
		t.Fatalf("the Sale's documents = %+v; want the factura, current and authorized", docs)
	}

	// The next ticks send nothing: not the dropped notice, not a
	// reinstatement notice, not a second delivery, and ask nothing more.
	consultas := len(sriStub.consultaCalls())
	tickAt(t, afterTheWindow.Add(time.Minute))
	tickAt(t, afterTheWindow.Add(7*time.Hour))
	wantNotices(t, "after the Reinstate", env, 0)
	if n := len(deliveriesSent(t, env)); n != deliveries {
		t.Fatalf("%d deliveries after the Reinstate; want none more than %d", n, deliveries)
	}
	if n := len(sriStub.consultaCalls()); n != consultas {
		t.Fatalf("%d ConsultaComprobante calls after the Reinstate; want the check stopped", n-consultas)
	}

	// The buyer's own read lists it authorized, with both downloads again.
	saleID := saleIDByRef(t, env, ref)
	buyer := buyerSession(t, env, "guest@example.com")
	listed, _ := listCustomerDocuments(t, buyer, saleID)
	if len(listed) != 1 || listed[0].ID != invoiceID || listed[0].Status != "authorized" {
		t.Fatalf("customer documents = %+v; want the factura authorized", listed)
	}
	assertCustomerDownloads(t, "reinstated", listed[0], saleID, true)
	for _, path := range []string{customerDocumentXMLPath(saleID, invoiceID), customerDocumentRIDEPath(saleID, invoiceID)} {
		if resp, body := sriEnv.getRaw(t, path, authHeader(buyer)); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d body=%.200s; want the download", path, resp.StatusCode, body)
		}
	}

	// Annulled again: only with the late acknowledgement, and the check
	// starts fresh beside the past annulment.
	again := afterTheWindow.Add(time.Hour)
	atInvoicingClock(t, again)
	resp, body := pressAnnul(t, operatorSessionID, invoiceID, map[string]any{"number": numberOfInvoice(t, operatorSessionID, invoiceID)})
	expectRefusal(t, "annul again without the acknowledgement", resp, body, http.StatusConflict, "INVOICE_ANNULMENT_LATE")
	annulOK(t, operatorSessionID, invoiceID)
	annulledAgain := getReinstated(t, operatorSessionID, invoiceID)
	if annulledAgain.Status != "annulled" || annulledAgain.Annulment.Current == nil || len(annulledAgain.Annulment.Past) != 1 {
		t.Fatalf("annulled again = %+v; want a current annulment beside the past one", annulledAgain.Annulment)
	}
	check := annulledAgain.Annulment.Check
	if check == nil || check.Verdict != "checking" || len(check.Asks) != 0 {
		t.Fatalf("the fresh check = %+v; want checking with no asks", check)
	}
	wantNextAsk(t, *check, again.Add(time.Minute))
}

// TestReinstateOfAReversedSalesFacturaOwesACreditNoteWithTheReversalReason:
// an Operator Reversal after the annulment owed nothing; the Reinstate owes
// the Credit Note the reversal would have, with its route as the reason,
// worked by the Drainer's ordinary path.
func TestReinstateOfAReversedSalesFacturaOwesACreditNoteWithTheReversalReason(t *testing.T) {
	env, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
	checkToMismatch(t, operatorSessionID, invoiceID)
	operatorRoute := reversalRoutes[1]
	operatorRoute.reverse(t, env, operatorSessionID, ref)
	if notes := creditNotesOfSale(t, operatorSessionID, ref); len(notes) != 0 {
		t.Fatalf("setup: Credit Notes after the reversal = %+v; want none against an annulled factura", notes)
	}
	atInvoicingClock(t, afterTheWindow)

	reinstateOK(t, operatorSessionID, invoiceID, "")
	docs := saleLookupDocuments(t, env, operatorSessionID, ref)
	if len(docs) != 2 || docs[0].ID != invoiceID || docs[0].Status != "authorized" ||
		docs[1].Kind != "credit_note" || docs[1].Status != "owed" || deref(docs[1].CreditsInvoiceID) != invoiceID ||
		deref(docs[1].CreditNoteReason) != operatorRoute.reason {
		t.Fatalf("the Sale's documents = %+v; want the factura and an owed Credit Note against it, reason %s", docs, operatorRoute.reason)
	}

	sriStub.answerAsUsual()
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("drain = %+v; want the Credit Note authorized", result)
	}
	if factura := getReinstated(t, operatorSessionID, invoiceID); factura.Status != "authorized" || deref(factura.CreditedByInvoiceID) != docs[1].ID {
		t.Fatalf("the factura = %+v; want authorized, credited by %s", factura, docs[1].ID)
	}
}

// TestReinstateWithALiveSuccessorOwesACreditNoteWithTheSupersedeReason: an
// Issue again or Reissue successor authorized while the check was checking
// stands for the Sale, so the reinstated factura owes a Credit Note with
// the reissue's reason, and the pair takes a Reissue's shape: the factura
// superseded by its successor and credited once the Credit Note is
// authorized.
func TestReinstateWithALiveSuccessorOwesACreditNoteWithTheSupersedeReason(t *testing.T) {
	successors := map[string]func(t *testing.T, sessionID, id string) string{
		"issue again": func(t *testing.T, sessionID, id string) string { return issueAgainOK(t, sessionID, id, nil).ID },
		"reissue": func(t *testing.T, sessionID, id string) string {
			return reissueOK(t, sessionID, id, companyRecipient()).ID
		},
	}
	for name, follow := range successors {
		t.Run(name, func(t *testing.T) {
			env, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
			successorID := follow(t, operatorSessionID, invoiceID)
			sriStub.answerAsUsual()
			if result := drainSaleInvoices(t); result.Authorized != 1 {
				t.Fatalf("setup: drain = %+v; want the successor authorized", result)
			}
			checkToMismatch(t, operatorSessionID, invoiceID)
			atInvoicingClock(t, afterTheWindow)

			reinstateOK(t, operatorSessionID, invoiceID, "")
			notes := creditNotesOfSale(t, operatorSessionID, ref)
			if len(notes) != 1 || notes[0].Status != "owed" {
				t.Fatalf("the Sale's Credit Notes = %+v; want one owed", notes)
			}
			sriStub.answerAsUsual()
			if result := drainSaleInvoices(t); result.Authorized != 1 {
				t.Fatalf("drain = %+v; want the Credit Note authorized", result)
			}

			docs := saleLookupDocuments(t, env, operatorSessionID, ref)
			if len(docs) != 3 {
				t.Fatalf("the Sale's documents = %+v; want the factura, its Credit Note and the successor", docs)
			}
			factura, note, successor := docs[0], docs[1], docs[2]
			if factura.ID != invoiceID || factura.Status != "authorized" || factura.Role != "superseded" || deref(factura.SupersededByInvoiceID) != successorID {
				t.Fatalf("the reinstated factura = %+v; want authorized, superseded by %s", factura, successorID)
			}
			if note.Kind != "credit_note" || note.Status != "authorized" || deref(note.CreditsInvoiceID) != invoiceID || deref(note.CreditNoteReason) != "reissue" {
				t.Fatalf("the Credit Note = %+v; want authorized against the factura, reason reissue", note)
			}
			if successor.ID != successorID || successor.Status != "authorized" || successor.Role != "current" {
				t.Fatalf("the successor = %+v; want authorized and current", successor)
			}
		})
	}
}

// TestAnUnsignedIssueAgainSuccessorWaitsForTheReinstatedFacturasCreditNote:
// an Issue again replacement still owed when the factura it replaces is
// reinstated is no longer the successor of a terminally dead document, so
// the Drainer holds it until the Credit Note the Reinstate owed is
// authorized, as a Reissue's corrected factura waits (the shape
// sale_invoice_dead_successor_test.go drives). The check reaches mismatch
// by two Check status presses, so no tick signs the successor first.
func TestAnUnsignedIssueAgainSuccessorWaitsForTheReinstatedFacturasCreditNote(t *testing.T) {
	_, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
	successor := issueAgainOK(t, operatorSessionID, invoiceID, nil)
	sriStub.setConsulta(consultaAuthorized)
	atInvoicingClock(t, fixedClock.Add(time.Minute))
	checkAnnulledOK(t, operatorSessionID, invoiceID)
	atInvoicingClock(t, fixedClock.Add(31*time.Minute))
	checkAnnulledOK(t, operatorSessionID, invoiceID)
	if _, check := getAnnulmentCheck(t, operatorSessionID, invoiceID); check.Verdict != "mismatch" {
		t.Fatalf("setup: check = %+v; want mismatch", check)
	}
	if owed := getReissuedInvoice(t, operatorSessionID, successor.ID); owed.Status != "owed" {
		t.Fatalf("setup: the successor is %s; want still owed", owed.Status)
	}
	atInvoicingClock(t, afterTheWindow)
	reinstateOK(t, operatorSessionID, invoiceID, "")

	// The SRI holds the nota de crédito in processing: the Credit Note is
	// signed and pending, and the successor waits, unsigned.
	isCreditNote := func(accessKey string) bool { return len(accessKey) > 10 && accessKey[8:10] == "04" }
	sriStub.setAuthorization(func(accessKey string) (int, string) {
		if isCreditNote(accessKey) {
			return http.StatusOK, inProcessingSOAP(accessKey)
		}
		return http.StatusOK, authorizedSOAP(accessKey)
	})
	if result := drainSaleInvoices(t); result.Claimed != 1 || result.Pending != 1 {
		t.Fatalf("first drain = %+v; want the Credit Note alone claimed, pending", result)
	}
	if waiting := getReissuedInvoice(t, operatorSessionID, successor.ID); waiting.Status != "owed" || waiting.Number != nil {
		t.Fatalf("the successor is %s numbered %v; want owed and unsigned behind the Credit Note", waiting.Status, waiting.Number)
	}

	// Once the Credit Note is authorized, the successor is signed.
	sriStub.answerAsUsual()
	atInvoicingClock(t, afterTheWindow.Add(time.Hour))
	if result := drainSaleInvoices(t); result.Authorized != 2 {
		t.Fatalf("second drain = %+v; want the Credit Note and then the successor authorized", result)
	}
	notes := creditNotesOfSale(t, operatorSessionID, ref)
	if len(notes) != 1 || notes[0].Status != "authorized" {
		t.Fatalf("the Sale's Credit Notes = %+v; want one authorized", notes)
	}
	if signed := getReissuedInvoice(t, operatorSessionID, successor.ID); signed.Status != "authorized" {
		t.Fatalf("the successor is %s; want authorized", signed.Status)
	}
}

// TestReinstateOfAManualTaxInvoiceReturnsItToAuthorized: nothing is owed for
// a document the platform does not issue, and it leaves the queue.
func TestReinstateOfAManualTaxInvoiceReturnsItToAuthorized(t *testing.T) {
	operatorSessionID, invoiceID := annulledManualInvoiceAt(t, fixedClock)
	checkToMismatch(t, operatorSessionID, invoiceID)
	if item := needsAttentionItem(t, operatorSessionID, invoiceID); item == nil {
		t.Fatal("setup: the manual Tax Invoice at mismatch is not queued")
	}
	atInvoicingClock(t, afterTheWindow)

	view := reinstateOK(t, operatorSessionID, invoiceID, "")
	if view.Status != "authorized" || view.CreditedByInvoiceID != nil || len(view.Annulment.Past) != 1 {
		t.Fatalf("reinstated = %+v; want authorized, credited by nothing, the annulment past", view)
	}
	if past := view.Annulment.Past[0]; past.Notice == nil || past.Notice.State != "not_owed" ||
		past.ReinstatementNotice == nil || past.ReinstatementNotice.State != "not_owed" {
		t.Fatalf("past annulment = %+v; want no notice either way", past)
	}
	if item := needsAttentionItem(t, operatorSessionID, invoiceID); item != nil {
		t.Fatalf("the queue still lists %+v", item)
	}
	if result := drainSaleInvoices(t); result.Claimed != 0 {
		t.Fatalf("drain = %+v; want nothing owed", result)
	}
}

// TestReinstateOfAFacturaAnnulledFromPendingFetchesItsAuthorization: a
// factura annulled while pending never stored its authorization; Reinstate
// asks autorizacionComprobante for it and is refused until the SRI answers
// AUTORIZADO with the authorized XML, which it then stores. The buyer, who
// was never sent it, is delivered it as any authorized factura is.
func TestReinstateOfAFacturaAnnulledFromPendingFetchesItsAuthorization(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("setup: drain = %+v; want pending", result)
	}
	annulOK(t, operatorSessionID, invoiceID)
	checkToMismatch(t, operatorSessionID, invoiceID)
	atInvoicingClock(t, afterTheWindow)

	// The detail admits it: whether the SRI returns the authorization is
	// only known at the press.
	if view := getReinstated(t, operatorSessionID, invoiceID); view.Annulment.Reinstate == nil || !view.Annulment.Reinstate.Admitted {
		t.Fatalf("annulment.reinstate = %+v; want admitted", view.Annulment.Reinstate)
	}
	queries := sriStub.authorizationQueryCount()
	resp, body := pressReinstate(t, operatorSessionID, invoiceID, reinstateConfirm(t, operatorSessionID, invoiceID, ""))
	expectRefusal(t, "autorizacion in processing", resp, body, http.StatusConflict, "INVOICE_REINSTATE_UNAUTHORIZED")
	if view := getReinstated(t, operatorSessionID, invoiceID); view.Status != "annulled" {
		t.Fatalf("after the refusal the factura is %s; want annulled", view.Status)
	}
	if sriStub.authorizationQueryCount() != queries+1 {
		t.Fatalf("autorizacion asked %d times by the press; want once", sriStub.authorizationQueryCount()-queries)
	}
	if view := getReissuedInvoice(t, operatorSessionID, invoiceID); view.EcuadorFull == nil || view.EcuadorFull.AuthorizationNumber != nil {
		t.Fatalf("after the refusal: %+v; want no authorization stored", view.EcuadorFull)
	}

	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, authorizedSOAP(accessKey) })
	deliveries := len(deliveriesSent(t, env))
	if view := reinstateOK(t, operatorSessionID, invoiceID, ""); view.Status != "authorized" {
		t.Fatalf("reinstated = %s; want authorized", view.Status)
	}
	stored := getReissuedInvoice(t, operatorSessionID, invoiceID)
	if stored.EcuadorFull == nil || stored.EcuadorFull.AuthorizationNumber == nil || *stored.EcuadorFull.AuthorizationNumber != stored.EcuadorFull.AccessKey {
		t.Fatalf("after the Reinstate: %+v; want the authorization stored", stored.EcuadorFull)
	}
	if result := drainSaleInvoices(t); result.Delivered != 1 || len(deliveriesSent(t, env)) != deliveries+1 {
		t.Fatalf("drain = %+v; want the factura delivered to the buyer, never sent it before", result)
	}
}

// TestAReinstatementNoticeFollowsAnAnnulmentNoticeAlreadySent: a buyer told
// of the annulment (the check read unconfirmed, then mismatch) is mailed the
// factura again, worded as a reinstatement, with the XML and the RIDE, once;
// where a Credit Note follows, its own mail is what they hear and no
// reinstatement notice is owed.
func TestAReinstatementNoticeFollowsAnAnnulmentNoticeAlreadySent(t *testing.T) {
	sentThenMismatch := func(t *testing.T) (*testEnv, string, string, string) {
		t.Helper()
		env, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
		checkToUnconfirmed(t, operatorSessionID, invoiceID)
		wantNotices(t, "released by unconfirmed", env, 1)
		sriStub.setConsulta(consultaAuthorized)
		tickAt(t, fixedClock.Add(7*time.Hour))
		tickAt(t, fixedClock.Add(13*time.Hour))
		if _, check := getAnnulmentCheck(t, operatorSessionID, invoiceID); check.Verdict != "mismatch" {
			t.Fatalf("setup: check = %+v; want mismatch", check)
		}
		atInvoicingClock(t, afterTheWindow)
		return env, operatorSessionID, invoiceID, ref
	}

	t.Run("no credit note follows", func(t *testing.T) {
		env, operatorSessionID, invoiceID, ref := sentThenMismatch(t)
		deliveries := len(deliveriesSent(t, env))
		view := reinstateOK(t, operatorSessionID, invoiceID, "")
		past := view.Annulment.Past[0]
		if past.Notice == nil || past.Notice.State != "sent" || past.ReinstatementNotice == nil || past.ReinstatementNotice.State != "owed" {
			t.Fatalf("past annulment = %+v; want the notice sent and the reinstatement notice owed", past)
		}

		var result struct {
			ReinstatementNotice struct {
				Sent   int `json:"sent"`
				Failed int `json:"failed"`
			} `json:"reinstatement_notice"`
		}
		resp, body := sriEnv.post(t, saleInvoiceDrainPath, nil, nil)
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body.Data, &result) != nil || result.ReinstatementNotice.Sent != 1 {
			t.Fatalf("tick: status=%d result=%+v; want one reinstatement notice sent", resp.StatusCode, result)
		}
		sent := deliveriesSent(t, env)
		if len(sent) != deliveries+1 {
			t.Fatalf("%d mails after the tick; want one more than %d", len(sent), deliveries)
		}
		mail := sent[len(sent)-1]
		if !mail.Reinstated || mail.Kind != "sale" || mail.To != "guest@example.com" || mail.Reference != ref ||
			mail.Number != numberOfInvoice(t, operatorSessionID, invoiceID) || mail.Locale != platform.LocaleEN {
			t.Fatalf("reinstatement notice = %+v; want the factura to the buyer, worded as reinstated", mail)
		}
		if len(mail.Attachments) != 2 || mail.Attachments[0].ContentType != "application/xml; charset=utf-8" || mail.Attachments[1].ContentType != "application/pdf" {
			t.Fatalf("attachments = %+v; want the XML and the RIDE", mail.Attachments)
		}
		after := getReinstated(t, operatorSessionID, invoiceID).Annulment.Past[0].ReinstatementNotice
		if after == nil || after.State != "sent" || after.SentAt == nil {
			t.Fatalf("reinstatement notice = %+v; want sent", after)
		}
		tickAt(t, afterTheWindow.Add(time.Hour))
		if n := len(deliveriesSent(t, env)); n != deliveries+1 {
			t.Fatalf("%d mails after a second tick; want it sent once", n)
		}
	})

	t.Run("a credit note follows", func(t *testing.T) {
		env, operatorSessionID, invoiceID, ref := sentThenMismatch(t)
		reversalRoutes[1].reverse(t, env, operatorSessionID, ref)
		view := reinstateOK(t, operatorSessionID, invoiceID, "")
		if past := view.Annulment.Past[0]; past.ReinstatementNotice == nil || past.ReinstatementNotice.State != "not_owed" {
			t.Fatalf("past annulment = %+v; want no reinstatement notice, the Credit Note's mail speaks", past)
		}
		deliveries := len(deliveriesSent(t, env))
		sriStub.answerAsUsual()
		if result := drainSaleInvoices(t); result.Delivered != 1 {
			t.Fatalf("drain = %+v; want the Credit Note delivered", result)
		}
		sent := deliveriesSent(t, env)
		if len(sent) != deliveries+1 || sent[len(sent)-1].Kind != "credit_note" || sent[len(sent)-1].Reinstated {
			t.Fatalf("mails = %+v; want the Credit Note's own, alone", sent[deliveries:])
		}
	})
}
