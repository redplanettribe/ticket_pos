package integration

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
)

// The Closing Sweep and the Unrecorded Annulment (#750, spec #742, ADR
// 0078), driven at the HTTP seam: the invoicing clock moved by
// atInvoicingClock, the drain endpoint as the Drainer's tick, the fake SRI's
// ConsultaComprobante scripted per test, and everything read back from the
// drain response's `closing_sweep`, the fake's record of which claves were
// asked about, the factura's detail and list row, the needs-attention list
// and count, and the dashboard's `closing-sweep` line.
//
// The facturas are emitted at fixedClock (2026-07-07), so July 2026 is the
// month swept, from 00:00 Guayaquil on 2026-08-08 through the end of
// 2026-08-12. Only production facturas are swept, so the Issuer is moved to
// production before they are issued; the fake SRI answers both
// environments.

const closingSweepPath = "/api/v1/operator/invoicing/closing-sweep"

// gye is an instant in Guayaquil in 2026.
func gye(m time.Month, d, h, min int) time.Time {
	return time.Date(2026, m, d, h, min, 0, 0, sri.Guayaquil)
}

// sweepDrain is the drain response's Closing Sweep and Annulment Check
// counts.
type sweepDrain struct {
	ClosingSweep struct {
		Asked      int `json:"asked"`
		Authorized int `json:"authorized"`
		Annulled   int `json:"annulled"`
		NoReading  int `json:"no_reading"`
		Unfinished int `json:"unfinished"`
	} `json:"closing_sweep"`
	AnnulmentCheck struct {
		Asked int `json:"asked"`
	} `json:"annulment_check"`
}

func sweepTickAt(t *testing.T, at time.Time) sweepDrain {
	t.Helper()
	atInvoicingClock(t, at)
	resp, body := sriEnv.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var result sweepDrain
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode drain result: %v", err)
	}
	return result
}

// wantSwept asserts how many asks one tick's sweep made and how many
// ConsultaComprobante calls there have been in all.
func wantSwept(t *testing.T, what string, result sweepDrain, asked, consultas int) {
	t.Helper()
	if result.ClosingSweep.Asked != asked {
		t.Fatalf("%s: closing_sweep = %+v; want %d asked", what, result.ClosingSweep, asked)
	}
	if n := len(sriStub.consultaCalls()); n != consultas {
		t.Fatalf("%s: %d ConsultaComprobante calls; want %d", what, n, consultas)
	}
}

// productionIssuer moves the Issuer to production, keeping its certificate.
func productionIssuer(t *testing.T, sessionID string) {
	t.Helper()
	body := validEcuadorIssuerBody()
	body["environment"] = "production"
	putEcuadorIssuer(t, sriEnv, sessionID, body)
}

// productionManualInvoice issues an authorized production manual Tax
// Invoice with the invoicing clock where it stands, returning its id and
// clave.
func productionManualInvoice(t *testing.T, sessionID string) (id, clave string) {
	t.Helper()
	issued := issueOK(t, sessionID, validInvoiceBody())
	if issued.Status != "authorized" || issued.Environment != "production" {
		t.Fatalf("setup: manual invoice %s in %s; want authorized in production", issued.Status, issued.Environment)
	}
	return issued.ID, issued.Ecuador.AccessKey
}

// productionSaleInvoice is houseSaleAuthorized in production: the House
// Sale's Sale Invoice drained to authorized and delivered under a
// production Issuer.
func productionSaleInvoice(t *testing.T, env *testEnv) (operatorSessionID, invoiceID, ref string) {
	t.Helper()
	operatorSessionID, invoiceID = houseSaleOwed(t, env, 1000, 1)
	issuerReady(t, operatorSessionID)
	productionIssuer(t, operatorSessionID)
	if result := drainSaleInvoices(t); result.Authorized != 1 || result.Delivered != 1 {
		t.Fatalf("setup drain = %+v; want the Sale Invoice authorized and delivered", result)
	}
	return operatorSessionID, invoiceID, lastConfirmation(t, env).Reference
}

// sweepDetail is the detail as the sweep's tests read it.
type sweepDetail struct {
	ID                  string                   `json:"id"`
	Status              string                   `json:"status"`
	UnrecordedAnnulment *unrecordedAnnulmentView `json:"unrecorded_annulment"`
	Ecuador             struct {
		AccessKey string `json:"access_key"`
	} `json:"ecuador"`
	Annulment *struct {
		Current *struct {
			FromUnrecorded bool `json:"from_unrecorded"`
		} `json:"current"`
		Check       *annulmentCheckView `json:"check"`
		Acknowledge *struct {
			Admitted bool `json:"admitted"`
			Refusal  *struct {
				Code string `json:"code"`
			} `json:"refusal"`
		} `json:"acknowledge"`
	} `json:"annulment"`
}

type unrecordedAnnulmentView struct {
	FoundAt        string  `json:"found_at"`
	AttemptID      *int64  `json:"attempt_id"`
	AcknowledgedBy *string `json:"acknowledged_by"`
	AcknowledgedAt *string `json:"acknowledged_at"`
}

func getSweepDetail(t *testing.T, sessionID, id string) sweepDetail {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view sweepDetail
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	return view
}

// sweepQueueEntry is a needs-attention entry as the sweep's tests read it.
type sweepQueueEntry struct {
	ID                  string                   `json:"id"`
	Status              string                   `json:"status"`
	AttentionReason     string                   `json:"attention_reason"`
	AcknowledgeAdmitted bool                     `json:"acknowledge_admitted"`
	UnrecordedAnnulment *unrecordedAnnulmentView `json:"unrecorded_annulment"`
}

func sweepQueue(t *testing.T, sessionID string) []sweepQueueEntry {
	t.Helper()
	resp, env := sriEnv.get(t, needsAttentionPath+"?page_size=100", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("queue status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var page struct {
		Data []sweepQueueEntry `json:"data"`
	}
	if err := json.Unmarshal(env.Data, &page); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	return page.Data
}

func queueEntry(entries []sweepQueueEntry, id string) *sweepQueueEntry {
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i]
		}
	}
	return nil
}

// listRow is the factura's row on the invoicing list.
func listRow(t *testing.T, sessionID, id string) sweepQueueEntry {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"?page_size=100", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var page struct {
		Data []sweepQueueEntry `json:"data"`
	}
	if err := json.Unmarshal(env.Data, &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if row := queueEntry(page.Data, id); row != nil {
		return *row
	}
	t.Fatalf("invoice %s is not on the list", id)
	return sweepQueueEntry{}
}

type closingSweepStatusView struct {
	Month         string `json:"month"`
	UnsweptCount  int    `json:"unswept_count"`
	DueAgainCount int    `json:"due_again_count"`
}

func getClosingSweep(t *testing.T, sessionID string, at time.Time) closingSweepStatusView {
	t.Helper()
	atInvoicingClock(t, at)
	resp, env := sriEnv.get(t, closingSweepPath, authHeader(sessionID))
	return decodeClosingSweep(t, "closing sweep", resp, env)
}

func sweepAgain(t *testing.T, sessionID string, at time.Time) closingSweepStatusView {
	t.Helper()
	atInvoicingClock(t, at)
	resp, env := sriEnv.post(t, closingSweepPath+"/sweep-again", nil, authHeader(sessionID))
	return decodeClosingSweep(t, "sweep again", resp, env)
}

func decodeClosingSweep(t *testing.T, what string, resp *http.Response, env envelope) closingSweepStatusView {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status=%d error=%+v", what, resp.StatusCode, env.Error)
	}
	var view closingSweepStatusView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	return view
}

func acknowledgePath(id string) string {
	return invoicesPath + "/" + id + "/acknowledge-unrecorded-annulment"
}

// consultaBy answers ConsultaComprobante per clave: the clave's own answer,
// or the fallback.
func consultaBy(answers map[string]func(string) (int, string), fallback func(string) (int, string)) func(string) (int, string) {
	return func(clave string) (int, string) {
		if answer, ok := answers[clave]; ok {
			return answer(clave)
		}
		return fallback(clave)
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// TestClosingSweepAsksExactlyTheClosedMonthsAuthorizedProductionFacturas:
// nothing is asked before 00:00 Guayaquil on day 8; from then one tick asks
// once about every production factura of July held as authorized - manual
// and Sale Invoices, a credited one and its corrected successor included -
// and about nothing else: not a test-environment factura, not an annulled
// one, not a Credit Note, not one of August. One reading is final.
func TestClosingSweepAsksExactlyTheClosedMonthsAuthorizedProductionFacturas(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, credited, _ := productionSaleInvoice(t, env)

	// A Reissue credits the Sale Invoice and supersedes it with a corrected
	// one; both are authorized production facturas of July.
	reissueOK(t, operatorSessionID, credited, companyRecipient())
	if result := drainSaleInvoices(t); result.Authorized != 2 {
		t.Fatalf("setup reissue drain = %+v; want the Credit Note and the corrected factura authorized", result)
	}
	corrected := getReissuedInvoice(t, operatorSessionID, credited)
	if corrected.Status != "authorized" || corrected.SupersededByInvoiceID == nil || corrected.CreditedByInvoiceID == nil {
		t.Fatalf("setup: the reissued factura = %+v; want authorized, credited and superseded", corrected)
	}
	successor := *corrected.SupersededByInvoiceID

	manual, _ := productionManualInvoice(t, operatorSessionID)
	annulled, _ := productionManualInvoice(t, operatorSessionID)
	if view := annulOK(t, operatorSessionID, annulled); view.Status != "annulled" {
		t.Fatalf("setup: %s; want annulled", view.Status)
	}
	atInvoicingClock(t, gye(time.August, 1, 10, 0))
	august, _ := productionManualInvoice(t, operatorSessionID)
	atInvoicingClock(t, fixedClock)
	putEcuadorIssuer(t, sriEnv, operatorSessionID, validEcuadorIssuerBody())
	testEnvironment := issueOK(t, operatorSessionID, validInvoiceBody())
	if testEnvironment.Environment != "test" || testEnvironment.Status != "authorized" {
		t.Fatalf("setup: %s in %s; want authorized in test", testEnvironment.Status, testEnvironment.Environment)
	}

	var want []string
	for _, id := range []string{credited, successor, manual} {
		want = append(want, getSweepDetail(t, operatorSessionID, id).Ecuador.AccessKey)
	}
	// The annulled factura's own Annulment Check asks too; let it end
	// confirmed so that every call after it is the sweep's.
	sriStub.setConsulta(consultaAnnulled)
	sweepTickAt(t, fixedClock.Add(time.Minute))
	sriStub.reset()

	wantSwept(t, "on day 7 at 23:59", sweepTickAt(t, gye(time.August, 7, 23, 59)), 0, 0)
	result := sweepTickAt(t, gye(time.August, 8, 0, 0))
	wantSwept(t, "at 00:00 on day 8", result, 3, 3)
	if result.ClosingSweep.Authorized != 3 {
		t.Fatalf("closing_sweep = %+v; want three AUTORIZADO", result.ClosingSweep)
	}
	if got, w := sortedStrings(sriStub.consultaCalls()), sortedStrings(want); !equalStrings(got, w) {
		t.Fatalf("claves asked = %v; want exactly the credited, its successor and the manual factura %v", got, w)
	}
	for _, id := range []string{august, annulled, testEnvironment.ID} {
		clave := getSweepDetail(t, operatorSessionID, id).Ecuador.AccessKey
		for _, asked := range sriStub.consultaCalls() {
			if asked == clave {
				t.Fatalf("factura %s was asked about; want it outside the population", id)
			}
		}
	}

	wantSwept(t, "a later tick", sweepTickAt(t, gye(time.August, 8, 0, 5)), 0, 3)
	wantSwept(t, "day 12", sweepTickAt(t, gye(time.August, 12, 12, 0)), 0, 3)
	if line := getClosingSweep(t, operatorSessionID, gye(time.August, 13, 0, 0)); line.Month != "2026-07" || line.UnsweptCount != 0 {
		t.Fatalf("dashboard = %+v; want July with nothing unswept", line)
	}
}

// TestClosingSweepIsPacedAndAsksANoReadingAgainOnALaterTick: one tick asks
// at most ten facturas, one ask of up to three calls each; a factura with
// no reading is not asked again on the same tick but on a later one,
// through day 12 and not after; two concurrent ticks never ask about one
// factura twice.
func TestClosingSweepIsPacedAndAsksANoReadingAgainOnALaterTick(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, operatorSessionID)
	productionIssuer(t, operatorSessionID)
	for range 11 {
		productionManualInvoice(t, operatorSessionID)
	}
	sriStub.setConsulta(consultaRefused)

	result := sweepTickAt(t, gye(time.August, 8, 0, 0))
	wantSwept(t, "the first tick", result, 10, 30)
	if result.ClosingSweep.NoReading != 10 {
		t.Fatalf("closing_sweep = %+v; want ten asks of no reading", result.ClosingSweep)
	}
	if asked := uniqueStrings(sriStub.consultaCalls()); len(asked) != 10 {
		t.Fatalf("%d facturas asked on the first tick; want ten, each once", len(asked))
	}

	wantSwept(t, "the second tick", sweepTickAt(t, gye(time.August, 8, 0, 5)), 10, 60)
	sriStub.setConsulta(consultaAuthorized)
	result = sweepTickAt(t, gye(time.August, 12, 23, 59))
	wantSwept(t, "on day 12", result, 10, 70)
	if result.ClosingSweep.Authorized != 10 {
		t.Fatalf("closing_sweep = %+v; want ten AUTORIZADO", result.ClosingSweep)
	}
	wantSwept(t, "at 00:00 on day 13", sweepTickAt(t, gye(time.August, 13, 0, 0)), 0, 70)
	if line := getClosingSweep(t, operatorSessionID, gye(time.August, 13, 0, 1)); line.Month != "2026-07" || line.UnsweptCount != 1 {
		t.Fatalf("dashboard = %+v; want one July factura unswept", line)
	}

	// Two concurrent ticks after a Sweep again: the one factura is asked
	// once between them.
	sweepAgain(t, operatorSessionID, gye(time.August, 20, 9, 0))
	var wg sync.WaitGroup
	results := make([]sweepDrain, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, body := sriEnv.post(t, saleInvoiceDrainPath, nil, nil)
			if resp.StatusCode == http.StatusOK {
				_ = json.Unmarshal(body.Data, &results[i])
			}
		}()
	}
	wg.Wait()
	if asked := results[0].ClosingSweep.Asked + results[1].ClosingSweep.Asked; asked != 1 {
		t.Fatalf("concurrent ticks = %+v; want one ask between them", results)
	}
	if n := len(sriStub.consultaCalls()); n != 71 {
		t.Fatalf("%d ConsultaComprobante calls; want 71", n)
	}
}

// TestClosingSweepFlagsAnUnrecordedAnnulmentAndMarkAnnulledRecordsIt: an
// ANULADO raises the flag on the list row and the detail and a
// needs-attention entry, the factura stays authorized and the buyer hears
// nothing; acknowledging is refused while Mark annulled is admitted. Mark
// annulled records it, its check starting confirmed with no asks; the
// flag and its entry clear (the Sale now waits for a follow-up, as any
// annulled Sale Invoice does), and the Annulment Notice is sent from then.
func TestClosingSweepFlagsAnUnrecordedAnnulmentAndMarkAnnulledRecordsIt(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID, _ := productionSaleInvoice(t, env)
	sriStub.setConsulta(consultaAnnulled)

	found := gye(time.August, 8, 0, 0)
	result := sweepTickAt(t, found)
	wantSwept(t, "the sweep", result, 1, 1)
	if result.ClosingSweep.Annulled != 1 {
		t.Fatalf("closing_sweep = %+v; want one ANULADO", result.ClosingSweep)
	}

	view := getSweepDetail(t, operatorSessionID, invoiceID)
	if view.Status != "authorized" || view.UnrecordedAnnulment == nil || view.UnrecordedAnnulment.AttemptID == nil ||
		view.UnrecordedAnnulment.AcknowledgedAt != nil {
		t.Fatalf("detail = %+v; want authorized, flagged by the call that read ANULADO, unacknowledged", view)
	}
	if at := timeOf(t, &view.UnrecordedAnnulment.FoundAt); !at.Equal(found) {
		t.Fatalf("found_at = %s; want %s", at, found)
	}
	if a := view.Annulment; a == nil || a.Acknowledge == nil || a.Acknowledge.Admitted || a.Acknowledge.Refusal == nil ||
		a.Acknowledge.Refusal.Code != "INVOICE_ANNUL_INSTEAD" {
		t.Fatalf("annulment.acknowledge = %+v; want refused, Mark annulled being admitted", view.Annulment)
	}
	if row := listRow(t, operatorSessionID, invoiceID); row.UnrecordedAnnulment == nil {
		t.Fatalf("list row = %+v; want the warning", row)
	}
	entry := queueEntry(sweepQueue(t, operatorSessionID), invoiceID)
	if entry == nil || entry.Status != "authorized" || entry.AttentionReason != "unrecorded_annulment" || entry.AcknowledgeAdmitted {
		t.Fatalf("queue entry = %+v; want the factura, reason unrecorded_annulment, no acknowledgement offered", entry)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("needs-attention count = %d; want 1", n)
	}
	resp, body := sriEnv.post(t, acknowledgePath(invoiceID), nil, authHeader(operatorSessionID))
	expectRefusal(t, "acknowledge where Mark annulled is admitted", resp, body, http.StatusConflict, "INVOICE_ANNUL_INSTEAD")

	wantSwept(t, "a later tick", sweepTickAt(t, gye(time.August, 9, 0, 0)), 0, 1)
	wantNotices(t, "before the annulment is recorded", env, 0)

	recorded := annulOK(t, operatorSessionID, invoiceID)
	if recorded.Status != "annulled" {
		t.Fatalf("Mark annulled: %s; want annulled", recorded.Status)
	}
	view = getSweepDetail(t, operatorSessionID, invoiceID)
	if view.UnrecordedAnnulment != nil {
		t.Fatalf("unrecorded_annulment = %+v; want it cleared by the record", view.UnrecordedAnnulment)
	}
	check := view.Annulment.Check
	if view.Annulment.Current == nil || !view.Annulment.Current.FromUnrecorded || check == nil ||
		check.Verdict != "confirmed" || len(check.Asks) != 0 || check.NextAskAt != nil {
		t.Fatalf("annulment = %+v, check %+v; want from the Unrecorded Annulment, confirmed with no asks and none due", view.Annulment.Current, check)
	}
	// The Unrecorded Annulment's entry is gone; the annulled Sale Invoice of
	// a standing Sale now waits for its follow-up by the queue's own door.
	if entry := queueEntry(sweepQueue(t, operatorSessionID), invoiceID); entry == nil || entry.AttentionReason != "annulled" || entry.UnrecordedAnnulment != nil {
		t.Fatalf("queue entry = %+v; want the follow-up's, reason annulled, no warning", entry)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 1 {
		t.Fatalf("needs-attention count = %d; want 1", n)
	}

	result = sweepTickAt(t, gye(time.August, 9, 0, 5))
	if result.AnnulmentCheck.Asked != 0 || len(sriStub.consultaCalls()) != 1 {
		t.Fatalf("tick = %+v with %d consultas; want the check asking nothing", result, len(sriStub.consultaCalls()))
	}
	wantNotices(t, "once recorded", env, 1)
}

// TestAcknowledgeClearsAnUnrecordedAnnulmentACreditNoteKeepsUnrecorded: on
// a reversed Sale's credited factura Mark annulled is refused, so the queue
// entry offers the acknowledgement; it records who and when, clears the
// entry and keeps the flag. It is recorded once, and refused on a factura
// carrying no flag.
func TestAcknowledgeClearsAnUnrecordedAnnulmentACreditNoteKeepsUnrecorded(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID, invoiceID, ref := productionSaleInvoice(t, env)
	reversalRoutes[1].reverse(t, env, operatorSessionID, ref)
	if result := drainSaleInvoices(t); result.Authorized != 1 {
		t.Fatalf("setup drain = %+v; want the reversal's Credit Note authorized", result)
	}
	other, _ := productionManualInvoice(t, operatorSessionID)
	sriStub.setConsulta(consultaBy(map[string]func(string) (int, string){
		getSweepDetail(t, operatorSessionID, invoiceID).Ecuador.AccessKey: consultaAnnulled,
	}, consultaAuthorized))

	if result := sweepTickAt(t, gye(time.August, 8, 0, 0)); result.ClosingSweep.Annulled != 1 || result.ClosingSweep.Authorized != 1 {
		t.Fatalf("closing_sweep = %+v; want the credited factura found annulled and the other authorized", result.ClosingSweep)
	}
	entry := queueEntry(sweepQueue(t, operatorSessionID), invoiceID)
	if entry == nil || entry.AttentionReason != "unrecorded_annulment" || !entry.AcknowledgeAdmitted {
		t.Fatalf("queue entry = %+v; want the acknowledgement offered", entry)
	}
	resp, body := annulInvoice(t, operatorSessionID, invoiceID)
	expectRefusal(t, "Mark annulled on a credited factura", resp, body, http.StatusConflict, "INVOICE_CREDIT_NOTE_STANDS")
	if a := getSweepDetail(t, operatorSessionID, invoiceID).Annulment; a == nil || a.Acknowledge == nil || !a.Acknowledge.Admitted {
		t.Fatalf("annulment = %+v; want the acknowledgement admitted", a)
	}

	resp, body = sriEnv.post(t, acknowledgePath(other), nil, authHeader(operatorSessionID))
	expectRefusal(t, "acknowledge with no flag", resp, body, http.StatusConflict, "INVOICE_NO_UNRECORDED_ANNULMENT")

	acknowledged := gye(time.August, 10, 9, 30)
	atInvoicingClock(t, acknowledged)
	resp, body = sriEnv.post(t, acknowledgePath(invoiceID), nil, authHeader(operatorSessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("acknowledge status=%d error=%+v", resp.StatusCode, body.Error)
	}
	view := getSweepDetail(t, operatorSessionID, invoiceID)
	u := view.UnrecordedAnnulment
	if view.Status != "authorized" || u == nil || deref(u.AcknowledgedBy) != "operator@example.com" || u.AcknowledgedAt == nil {
		t.Fatalf("detail = %+v; want authorized with the flag kept and acknowledged by the operator", view)
	}
	if at := timeOf(t, u.AcknowledgedAt); !at.Equal(acknowledged) {
		t.Fatalf("acknowledged_at = %s; want %s", at, acknowledged)
	}
	if entry := queueEntry(sweepQueue(t, operatorSessionID), invoiceID); entry != nil {
		t.Fatalf("queue entry = %+v; want none once acknowledged", entry)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != 0 {
		t.Fatalf("needs-attention count = %d; want 0", n)
	}
	if row := listRow(t, operatorSessionID, invoiceID); row.UnrecordedAnnulment == nil || row.UnrecordedAnnulment.AcknowledgedAt == nil {
		t.Fatalf("list row = %+v; want the acknowledged warning kept", row)
	}
	resp, body = sriEnv.post(t, acknowledgePath(invoiceID), nil, authHeader(operatorSessionID))
	expectRefusal(t, "a second acknowledgement", resp, body, http.StatusConflict, "INVOICE_UNRECORDED_ANNULMENT_ACKNOWLEDGED")
}

// TestUnsweptFacturasAreCountedAfterDayTwelveAndSweepAgainAsksJustThose:
// facturas with no reading through day 12 are the dashboard's count from
// day 13; Sweep again makes just those due, and the next tick asks them,
// a factura with a reading never again and one of another month not at
// all.
func TestUnsweptFacturasAreCountedAfterDayTwelveAndSweepAgainAsksJustThose(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	issuerReady(t, operatorSessionID)
	productionIssuer(t, operatorSessionID)
	_, sweptClave := productionManualInvoice(t, operatorSessionID)
	healed, healedClave := productionManualInvoice(t, operatorSessionID)
	_, stuckClave := productionManualInvoice(t, operatorSessionID)
	atInvoicingClock(t, gye(time.August, 3, 10, 0))
	productionManualInvoice(t, operatorSessionID)

	sriStub.setConsulta(consultaBy(map[string]func(string) (int, string){sweptClave: consultaAuthorized}, consultaRefused))
	wantSwept(t, "day 8", sweepTickAt(t, gye(time.August, 8, 0, 0)), 3, 7)
	wantSwept(t, "day 12", sweepTickAt(t, gye(time.August, 12, 23, 0)), 2, 13)

	if line := getClosingSweep(t, operatorSessionID, gye(time.August, 12, 23, 30)); line.Month != "2026-06" || line.UnsweptCount != 0 {
		t.Fatalf("dashboard on day 12 = %+v; want June, nothing unswept, while July's sweep still runs", line)
	}
	wantSwept(t, "day 13", sweepTickAt(t, gye(time.August, 13, 0, 0)), 0, 13)
	if line := getClosingSweep(t, operatorSessionID, gye(time.August, 13, 0, 0)); line.Month != "2026-07" || line.UnsweptCount != 2 || line.DueAgainCount != 0 {
		t.Fatalf("dashboard on day 13 = %+v; want two July facturas unswept", line)
	}
	wantSwept(t, "a week later, before Sweep again", sweepTickAt(t, gye(time.August, 20, 0, 0)), 0, 13)

	sriStub.setConsulta(consultaBy(map[string]func(string) (int, string){healedClave: consultaAuthorized}, consultaRefused))
	if line := sweepAgain(t, operatorSessionID, gye(time.August, 20, 9, 0)); line.Month != "2026-07" || line.UnsweptCount != 2 || line.DueAgainCount != 2 {
		t.Fatalf("Sweep again = %+v; want the two unswept due again", line)
	}
	before := len(sriStub.consultaCalls())
	result := sweepTickAt(t, gye(time.August, 20, 9, 5))
	wantSwept(t, "the tick after Sweep again", result, 2, before+4)
	if got, w := sortedStrings(uniqueStrings(sriStub.consultaCalls()[before:])), sortedStrings([]string{healedClave, stuckClave}); !equalStrings(got, w) {
		t.Fatalf("claves asked after Sweep again = %v; want just the two unswept %v", got, w)
	}
	if line := getClosingSweep(t, operatorSessionID, gye(time.August, 20, 9, 6)); line.UnsweptCount != 1 || line.DueAgainCount != 0 {
		t.Fatalf("dashboard after the tick = %+v; want the one still without a reading", line)
	}
	wantSwept(t, "the tick after that", sweepTickAt(t, gye(time.August, 20, 9, 10)), 0, before+4)
	if view := getSweepDetail(t, operatorSessionID, healed); view.Status != "authorized" || view.UnrecordedAnnulment != nil {
		t.Fatalf("the healed factura = %+v; want authorized and unflagged", view)
	}
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
