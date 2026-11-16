package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The follow-ups after an annulment (#747, spec #742, ADR 0078), at the HTTP
// seam: an annulled Sale Invoice of a standing Sale sits in the
// needs-attention queue until Issue again or Reissue owes its replacement;
// both are refused while the Annulment Check stands at mismatch, whatever
// state the factura was annulled from, and admitted on confirmed and
// unconfirmed; a Reissue of an annulled factura owes the corrected factura
// alone, signed on the next tick; an Operator Reversal after an annulment
// owes no Credit Note; and the detail's follow-up chooser lists the cards
// for a standing Sale, a reversed one and a manual Tax Invoice.
//
// Every factura is issued and annulled at fixedClock; the check is walked
// by tickAt with the fake SRI's ConsultaComprobante scripted per test, as
// invoicing_annulment_check_test.go walks it.

// followUpView is one card of `annulment.follow_ups`.
type followUpView struct {
	Kind     string `json:"kind"`
	Admitted bool   `json:"admitted"`
	Refusal  *struct {
		Code string `json:"code"`
	} `json:"refusal"`
}

// followUpsOf reads the detail's follow-up chooser.
func followUpsOf(t *testing.T, sessionID, id string) []followUpView {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view struct {
		Status    string `json:"status"`
		Annulment *struct {
			FollowUps []followUpView `json:"follow_ups"`
		} `json:"annulment"`
	}
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	if view.Annulment == nil || view.Annulment.FollowUps == nil {
		t.Fatalf("invoice %s (%s) has no follow-up chooser", id, view.Status)
	}
	return view.Annulment.FollowUps
}

// wantFollowUps asserts the chooser's cards, in order, as "kind" when
// admitted and "kind:CODE" when refused.
func wantFollowUps(t *testing.T, what string, got []followUpView, want ...string) {
	t.Helper()
	var read []string
	for _, f := range got {
		switch {
		case f.Admitted && f.Refusal == nil:
			read = append(read, f.Kind)
		case !f.Admitted && f.Refusal != nil:
			read = append(read, f.Kind+":"+f.Refusal.Code)
		default:
			t.Fatalf("%s: card %+v is neither admitted nor refused", what, f)
		}
	}
	if len(read) != len(want) {
		t.Fatalf("%s: follow-ups = %v; want %v", what, read, want)
	}
	for i := range want {
		if read[i] != want[i] {
			t.Fatalf("%s: follow-ups = %v; want %v", what, read, want)
		}
	}
}

// annulledAuthorizedSaleInvoice issues a House Sale's Sale Invoice, authorized, and
// records its annulment, all at fixedClock. It returns the harness, the
// operator's session, the factura and the Sale's Confirmation reference.
func annulledAuthorizedSaleInvoice(t *testing.T) (env *testEnv, operatorSessionID, invoiceID, ref string) {
	t.Helper()
	env = setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID, _ = houseSaleAuthorized(t, env)
	ref = lastConfirmation(t, env).Reference
	if view := annulOK(t, operatorSessionID, invoiceID); view.Status != "annulled" {
		t.Fatalf("setup: annul left the factura %s", view.Status)
	}
	return env, operatorSessionID, invoiceID, ref
}

// checkToMismatch walks the check through +1, +5, +15 and +30 min with every
// ask reading AUTORIZADO, which settles a mismatch at +30 min.
func checkToMismatch(t *testing.T, sessionID, invoiceID string) {
	t.Helper()
	sriStub.setConsulta(consultaAuthorized)
	for _, after := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute} {
		tickAt(t, fixedClock.Add(after))
	}
	if _, check := getAnnulmentCheck(t, sessionID, invoiceID); check.Verdict != "mismatch" {
		t.Fatalf("setup: check = %+v; want mismatch", check)
	}
}

// checkToUnconfirmed walks the check through the first hour with every call
// refused, which reads unconfirmed after the +60 min ask.
func checkToUnconfirmed(t *testing.T, sessionID, invoiceID string) {
	t.Helper()
	sriStub.setConsulta(consultaRefused)
	for _, after := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 60 * time.Minute} {
		tickAt(t, fixedClock.Add(after))
	}
	if _, check := getAnnulmentCheck(t, sessionID, invoiceID); check.Verdict != "unconfirmed" {
		t.Fatalf("setup: check = %+v; want unconfirmed", check)
	}
}

// checkToConfirmed asks once at +1 min and reads ANULADO.
func checkToConfirmed(t *testing.T, sessionID, invoiceID string) {
	t.Helper()
	sriStub.setConsulta(consultaAnnulled)
	tickAt(t, fixedClock.Add(time.Minute))
	if _, check := getAnnulmentCheck(t, sessionID, invoiceID); check.Verdict != "confirmed" {
		t.Fatalf("setup: check = %+v; want confirmed", check)
	}
}

// TestAnAnnulledSaleInvoiceStaysInTheQueueUntilItIsReplaced: Mark annulled
// on a standing Sale's factura leaves the Sale with no factura, and the
// needs-attention list and count say so, with the reason `annulled`, while
// the check is still checking and once it is confirmed; Issue again or
// Reissue owing the replacement clears it.
func TestAnAnnulledSaleInvoiceStaysInTheQueueUntilItIsReplaced(t *testing.T) {
	replacements := map[string]func(t *testing.T, sessionID, id string) string{
		"issue again": func(t *testing.T, sessionID, id string) string {
			return issueAgainOK(t, sessionID, id, nil).ID
		},
		"reissue": func(t *testing.T, sessionID, id string) string {
			return reissueOK(t, sessionID, id, companyRecipient()).ID
		},
	}
	for name, replace := range replacements {
		t.Run(name, func(t *testing.T) {
			env := setupTest(t)
			sriStub.reset()
			operatorSessionID, invoiceID, _ := houseSaleAuthorized(t, env)
			if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
				t.Fatalf("count before the annulment = %d; want 0", n)
			}
			annulOK(t, operatorSessionID, invoiceID)

			if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
				t.Fatalf("count after the annulment = %d; want 1", n)
			}
			item := needsAttentionItem(t, operatorSessionID, invoiceID)
			if item == nil || item.Status != "annulled" || item.AttentionReason != "annulled" || item.MismatchSince != nil ||
				!timeOf(t, item.AnnulledAt).Equal(fixedClock) {
				t.Fatalf("queue item = %+v; want the annulled factura with the reason annulled, since it was annulled", item)
			}

			checkToConfirmed(t, operatorSessionID, invoiceID)
			if item := needsAttentionItem(t, operatorSessionID, invoiceID); item == nil || item.AttentionReason != "annulled" {
				t.Fatalf("queue item once confirmed = %+v; want it still there, the Sale has no factura", item)
			}

			replacementID := replace(t, operatorSessionID, invoiceID)
			if item := needsAttentionItem(t, operatorSessionID, invoiceID); item != nil {
				t.Fatalf("queue after the replacement lists %+v; want it gone", item)
			}
			if item := needsAttentionItem(t, operatorSessionID, replacementID); item != nil {
				t.Fatalf("queue lists the owed replacement %+v", item)
			}
			if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
				t.Fatalf("count after the replacement = %d; want 0", n)
			}
		})
	}
}

// TestIssueAgainAndReissueAreRefusedAtMismatch: an annulled factura the SRI
// still reports authorized is replaced by neither, each refused with
// INVOICE_ANNULMENT_MISMATCH. The queue holds it once, by the mismatch's
// reason though it comes in by both doors, and the chooser lists both
// refused beside Reverse the Sale. An ANULADO releases it.
func TestIssueAgainAndReissueAreRefusedAtMismatch(t *testing.T) {
	_, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
	checkToMismatch(t, operatorSessionID, invoiceID)

	resp, body := issueAgain(t, operatorSessionID, invoiceID, nil)
	expectRefusal(t, "issue again at mismatch", resp, body, http.StatusConflict, "INVOICE_ANNULMENT_MISMATCH")
	resp, body = reissueInvoice(t, operatorSessionID, invoiceID, companyRecipient())
	expectRefusal(t, "reissue at mismatch", resp, body, http.StatusConflict, "INVOICE_ANNULMENT_MISMATCH")

	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("count at mismatch = %d; want the factura once", n)
	}
	item := needsAttentionItem(t, operatorSessionID, invoiceID)
	if item == nil || item.AttentionReason != "annulment_mismatch" || !timeOf(t, item.MismatchSince).Equal(fixedClock.Add(30*time.Minute)) {
		t.Fatalf("queue item = %+v; want the mismatch's reason since +30 min", item)
	}
	wantFollowUps(t, "at mismatch", followUpsOf(t, operatorSessionID, invoiceID),
		"issue_again:INVOICE_ANNULMENT_MISMATCH", "reissue:INVOICE_ANNULMENT_MISMATCH", "reverse_sale")

	// The six-hourly ask reads ANULADO: the warning lifts, and the factura
	// is replaced as any annulled one is.
	sriStub.setConsulta(consultaAnnulled)
	tickAt(t, fixedClock.Add(60*time.Minute))
	if _, check := getAnnulmentCheck(t, operatorSessionID, invoiceID); check.Verdict != "confirmed" {
		t.Fatalf("check after ANULADO = %+v; want confirmed", check)
	}
	wantFollowUps(t, "once confirmed", followUpsOf(t, operatorSessionID, invoiceID), "issue_again", "reissue", "reverse_sale")
	issueAgainOK(t, operatorSessionID, invoiceID, nil)
}

// TestAPendingFacturaAnnulledAndFoundAuthorizedIsRefusedTheSameWay: the gate
// is keyed on the verdict, never on the state the factura was annulled
// from. A factura annulled while pending, which the SRI then reports
// authorized on two asks, is refused exactly as an authorized one.
func TestAPendingFacturaAnnulledAndFoundAuthorizedIsRefusedTheSameWay(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID := houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	sriStub.setAuthorization(func(accessKey string) (int, string) { return http.StatusOK, inProcessingSOAP(accessKey) })
	if result := drainSaleInvoices(t); result.Pending != 1 {
		t.Fatalf("setup: drain = %+v; want pending", result)
	}
	if view := annulOK(t, operatorSessionID, invoiceID); view.Status != "annulled" {
		t.Fatalf("setup: annul left the factura %s", view.Status)
	}
	checkToMismatch(t, operatorSessionID, invoiceID)

	resp, body := issueAgain(t, operatorSessionID, invoiceID, nil)
	expectRefusal(t, "issue again on a pending factura at mismatch", resp, body, http.StatusConflict, "INVOICE_ANNULMENT_MISMATCH")
	resp, body = reissueInvoice(t, operatorSessionID, invoiceID, companyRecipient())
	expectRefusal(t, "reissue on a pending factura at mismatch", resp, body, http.StatusConflict, "INVOICE_ANNULMENT_MISMATCH")
}

// TestIssueAgainAndReissueAreAdmittedOnConfirmedAndUnconfirmed: an ANULADO,
// or the SRI's silence through the first hour, admits both follow-ups.
func TestIssueAgainAndReissueAreAdmittedOnConfirmedAndUnconfirmed(t *testing.T) {
	verdicts := map[string]func(t *testing.T, sessionID, id string){
		"confirmed":   checkToConfirmed,
		"unconfirmed": checkToUnconfirmed,
	}
	for verdict, reach := range verdicts {
		t.Run(verdict+", issue again", func(t *testing.T) {
			_, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
			reach(t, operatorSessionID, invoiceID)
			wantFollowUps(t, verdict, followUpsOf(t, operatorSessionID, invoiceID), "issue_again", "reissue", "reverse_sale")
			if replacement := issueAgainOK(t, operatorSessionID, invoiceID, nil); replacement.Status != "owed" {
				t.Fatalf("replacement = %s; want owed", replacement.Status)
			}
			wantFollowUps(t, "once replaced", followUpsOf(t, operatorSessionID, invoiceID))
		})
		t.Run(verdict+", reissue", func(t *testing.T) {
			_, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
			reach(t, operatorSessionID, invoiceID)
			if corrected := reissueOK(t, operatorSessionID, invoiceID, companyRecipient()); corrected.Status != "owed" {
				t.Fatalf("corrected = %s; want owed", corrected.Status)
			}
		})
	}
}

// TestReissueOfAnAnnulledFacturaOwesTheCorrectedFacturaAlone: no Credit Note
// is owed against a factura the annulment already discharged, and the
// corrected factura is signed and authorized on the very next tick, with
// nothing to wait behind. The annulled factura stays annulled and on file.
func TestReissueOfAnAnnulledFacturaOwesTheCorrectedFacturaAlone(t *testing.T) {
	_, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
	checkToConfirmed(t, operatorSessionID, invoiceID)

	corrected := reissueOK(t, operatorSessionID, invoiceID, companyRecipient())
	if corrected.SupersedesInvoiceID == nil || *corrected.SupersedesInvoiceID != invoiceID ||
		corrected.Recipient.TaxIDType != "ruc" {
		t.Fatalf("corrected = %+v; want an owed factura to the company, naming the annulled one", corrected)
	}
	if notes := creditNotesOfSale(t, operatorSessionID, ref); len(notes) != 0 {
		t.Fatalf("the Sale's Credit Notes = %+v; want none, the annulment discharged the factura", notes)
	}
	if annulled := getReissuedInvoice(t, operatorSessionID, invoiceID); annulled.Status != "annulled" || annulled.CreditedByInvoiceID != nil {
		t.Fatalf("the annulled factura = %s credited by %v; want annulled, credited by nothing", annulled.Status, annulled.CreditedByInvoiceID)
	}

	sriStub.answerAsUsual()
	if result := drainSaleInvoices(t); result.Authorized != 1 || result.Claimed != 1 {
		t.Fatalf("next tick = %+v; want the corrected factura claimed and authorized at once", result)
	}
	if signed := getReissuedInvoice(t, operatorSessionID, corrected.ID); signed.Status != "authorized" {
		t.Fatalf("the corrected factura is %s; want authorized", signed.Status)
	}
	if notes := creditNotesOfSale(t, operatorSessionID, ref); len(notes) != 0 {
		t.Fatalf("after the tick the Sale's Credit Notes = %+v; want none", notes)
	}
	docs := documentsOfSale(t, operatorSessionID, ref)
	if len(docs) != 2 || statusOf(docs, invoiceID) != "annulled" || statusOf(docs, corrected.ID) != "authorized" {
		t.Fatalf("the Sale's documents = %+v; want the annulled factura and the authorized corrected one", docs)
	}
}

// TestAnOperatorReversalAfterAnAnnulmentOwesNoCreditNote: an annulled
// factura is owed no Credit Note by the Sale's reversal, at mismatch or
// not, and the Sale leaves the queue's unreplaced door; the chooser then
// says nothing follows. A Sale whose factura stands still credits it
// automatically.
func TestAnOperatorReversalAfterAnAnnulmentOwesNoCreditNote(t *testing.T) {
	operatorRoute := reversalRoutes[1]
	if operatorRoute.name != "operator" {
		t.Fatalf("reversalRoutes[1] = %s; want the operator's", operatorRoute.name)
	}
	for _, mismatch := range []bool{false, true} {
		name := "checking"
		if mismatch {
			name = "at mismatch"
		}
		t.Run(name, func(t *testing.T) {
			env, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
			if mismatch {
				checkToMismatch(t, operatorSessionID, invoiceID)
			}
			operatorRoute.reverse(t, env, operatorSessionID, ref)

			if notes := creditNotesOfSale(t, operatorSessionID, ref); len(notes) != 0 {
				t.Fatalf("the reversed Sale's Credit Notes = %+v; want none against an annulled factura", notes)
			}
			if result := drainSaleInvoices(t); result.Claimed != 0 {
				t.Fatalf("drain after the reversal = %+v; want nothing owed", result)
			}
			wantFollowUps(t, "a reversed Sale", followUpsOf(t, operatorSessionID, invoiceID), "nothing_follows")
			item := needsAttentionItem(t, operatorSessionID, invoiceID)
			switch {
			case mismatch && (item == nil || item.AttentionReason != "annulment_mismatch"):
				t.Fatalf("queue item = %+v; want the mismatch warning still standing", item)
			case !mismatch && item != nil:
				t.Fatalf("queue lists %+v; want a reversed Sale owed nothing", item)
			}
		})
	}

	t.Run("a standing factura", func(t *testing.T) {
		env := setupTest(t)
		sriStub.reset()
		operatorSessionID, invoiceID, _ := houseSaleAuthorized(t, env)
		ref := lastConfirmation(t, env).Reference
		operatorRoute.reverse(t, env, operatorSessionID, ref)
		notes := creditNotesOfSale(t, operatorSessionID, ref)
		if len(notes) != 1 || notes[0].Status != "owed" {
			t.Fatalf("the reversed Sale's Credit Notes = %+v; want one owed against %s", notes, invoiceID)
		}
	})
}

// TestTheFollowUpChooserByFactura: the detail's chooser for a standing
// Sale's annulled factura (Issue again, Reissue, Reverse the Sale), a
// reversed Sale's (nothing follows) and a manual Tax Invoice's (type it
// again by hand). A factura that is not annulled has none.
func TestTheFollowUpChooserByFactura(t *testing.T) {
	t.Run("a standing sale", func(t *testing.T) {
		_, operatorSessionID, invoiceID, _ := annulledAuthorizedSaleInvoice(t)
		wantFollowUps(t, "a standing Sale", followUpsOf(t, operatorSessionID, invoiceID), "issue_again", "reissue", "reverse_sale")
	})
	t.Run("a reversed sale", func(t *testing.T) {
		env, operatorSessionID, invoiceID, ref := annulledAuthorizedSaleInvoice(t)
		reversalRoutes[1].reverse(t, env, operatorSessionID, ref)
		wantFollowUps(t, "a reversed Sale", followUpsOf(t, operatorSessionID, invoiceID), "nothing_follows")
	})
	t.Run("a manual tax invoice", func(t *testing.T) {
		operatorSessionID, invoiceID := annulledManualInvoiceAt(t, fixedClock)
		wantFollowUps(t, "a manual Tax Invoice", followUpsOf(t, operatorSessionID, invoiceID), "type_again")
		if item := needsAttentionItem(t, operatorSessionID, invoiceID); item != nil {
			t.Fatalf("queue lists the annulled manual Tax Invoice %+v; want it typed again by hand, not queued", item)
		}
	})
	t.Run("not annulled", func(t *testing.T) {
		env := setupTest(t)
		sriStub.reset()
		operatorSessionID, invoiceID, _ := houseSaleAuthorized(t, env)
		resp, body := sriEnv.get(t, invoicesPath+"/"+invoiceID, authHeader(operatorSessionID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get invoice status=%d", resp.StatusCode)
		}
		var view struct {
			Annulment struct {
				FollowUps []followUpView `json:"follow_ups"`
			} `json:"annulment"`
		}
		if err := json.Unmarshal(body.Data, &view); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if view.Annulment.FollowUps != nil {
			t.Fatalf("follow-ups on an authorized factura = %+v; want null", view.Annulment.FollowUps)
		}
	})
}
