package integration

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"
)

// The Annulment Check (#746, spec #742, ADR 0078), driven at the HTTP seam:
// the invoicing clock moved by atInvoicingClock, the drain endpoint as the
// Drainer's tick, the fake SRI's ConsultaComprobante scripted per test, and
// everything read back from the drain response, the factura's detail
// (`annulment.check`) and the needs-attention list and count.
//
// After Mark annulled the check asks at +1, +5, +15, +30 and +60 min, then
// every 6 h until the later of recording + 7 days and the window's close,
// and reaches a verdict: confirmed on an ANULADO, mismatch on AUTORIZADO
// from two asks the later at or after +30 min, unconfirmed after the +60 min
// ask with neither. It asks ConsultaComprobante alone, never autorización.
//
// The factura is a manual Tax Invoice in the test environment (every
// integration issuer is in Pruebas, so this is the celcer path against the
// fake SRI), issued and annulled at fixedClock unless a test says otherwise.

// annulmentCheckView is `annulment.check` on the detail.
type annulmentCheckView struct {
	Verdict   string  `json:"verdict"`
	Scheduled bool    `json:"scheduled"`
	Since     *string `json:"since"`
	NextAskAt *string `json:"next_ask_at"`
	AsksUntil *string `json:"asks_until"`
	Asks      []struct {
		AskedAt string `json:"asked_at"`
		Reading string `json:"reading"`
		Manual  bool   `json:"manual"`
	} `json:"asks"`
	Answers []struct {
		AttemptID int64  `json:"attempt_id"`
		StartedAt string `json:"started_at"`
		Service   string `json:"service"`
		Reading   string `json:"reading"`
		Messages  []struct {
			Identifier string `json:"identifier"`
			Message    string `json:"message"`
		} `json:"messages"`
		Error *string `json:"error"`
	} `json:"answers"`
}

type annulmentCheckDetail struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	AnnulledBy string `json:"annulled_by"`
	Annulment  *struct {
		Check *annulmentCheckView `json:"check"`
	} `json:"annulment"`
}

// annulmentCheckDrain is the drain response's Annulment Check counts.
type annulmentCheckDrain struct {
	Claimed        int `json:"claimed"`
	AnnulmentCheck struct {
		Asked       int `json:"asked"`
		Checking    int `json:"checking"`
		Confirmed   int `json:"confirmed"`
		Mismatch    int `json:"mismatch"`
		Unconfirmed int `json:"unconfirmed"`
		Unfinished  int `json:"unfinished"`
	} `json:"annulment_check"`
}

func getAnnulmentCheck(t *testing.T, sessionID, id string) (string, annulmentCheckView) {
	t.Helper()
	resp, env := sriEnv.get(t, invoicesPath+"/"+id, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get invoice status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view annulmentCheckDetail
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode invoice: %v", err)
	}
	if view.Annulment == nil || view.Annulment.Check == nil {
		t.Fatalf("invoice %s (%s) has no annulment check", id, view.Status)
	}
	return view.Status, *view.Annulment.Check
}

// tickAt moves the invoicing clock to at and ticks the Drainer once.
func tickAt(t *testing.T, at time.Time) annulmentCheckDrain {
	t.Helper()
	atInvoicingClock(t, at)
	resp, body := sriEnv.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("drain status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var result annulmentCheckDrain
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("decode drain result: %v", err)
	}
	return result
}

// annulledManualInvoiceAt issues an authorized manual Tax Invoice and records
// its annulment with the invoicing clock at `at`.
func annulledManualInvoiceAt(t *testing.T, at time.Time) (operatorSessionID, invoiceID string) {
	t.Helper()
	env := setupTest(t)
	sriStub.reset()
	atInvoicingClock(t, at)
	operatorSessionID, invoiceID = authorizedManualInvoice(t, env)
	if annulled := annulOK(t, operatorSessionID, invoiceID); annulled.Status != "annulled" {
		t.Fatalf("setup: status = %s; want annulled", annulled.Status)
	}
	return operatorSessionID, invoiceID
}

func timeOf(t *testing.T, s *string) time.Time {
	t.Helper()
	if s == nil {
		t.Fatal("want a time, got null")
	}
	at, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		t.Fatalf("time %q: %v", *s, err)
	}
	return at
}

func wantNextAsk(t *testing.T, check annulmentCheckView, want time.Time) {
	t.Helper()
	if got := timeOf(t, check.NextAskAt); !got.Equal(want) {
		t.Fatalf("next_ask_at = %s; want %s", got, want)
	}
}

func wantNoNextAsk(t *testing.T, check annulmentCheckView) {
	t.Helper()
	if check.NextAskAt != nil {
		t.Fatalf("next_ask_at = %s; want none", *check.NextAskAt)
	}
}

// wantAsked asserts how many asks one tick made, and how many
// ConsultaComprobante calls there have been in all.
func wantAsked(t *testing.T, what string, result annulmentCheckDrain, asked, consultas int) {
	t.Helper()
	if result.AnnulmentCheck.Asked != asked {
		t.Fatalf("%s: drain = %+v; want %d asked", what, result.AnnulmentCheck, asked)
	}
	if n := len(sriStub.consultaCalls()); n != consultas {
		t.Fatalf("%s: %d ConsultaComprobante calls; want %d", what, n, consultas)
	}
}

// TestAnnulmentCheckAsksOnTheScheduleAndAnANULADOConfirms: nothing is due
// before +1 min; from +1 min the asks fall due rung by rung, each one ask of
// up to three calls; a scripted ANULADO confirms at once and the check asks
// nothing more. autorización is never asked.
func TestAnnulmentCheckAsksOnTheScheduleAndAnANULADOConfirms(t *testing.T) {
	t0 := fixedClock
	operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
	queries := sriStub.authorizationQueryCount()
	sriStub.setConsulta(consultaRefused)

	status, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if status != "annulled" || check.Verdict != "checking" || !check.Scheduled || check.Since != nil || len(check.Asks) != 0 {
		t.Fatalf("freshly recorded: %s %+v; want checking, scheduled, no asks", status, check)
	}
	wantNextAsk(t, check, t0.Add(time.Minute))
	if check.AsksUntil == nil {
		t.Fatal("asks_until = null; want the schedule's end")
	}

	wantAsked(t, "at recording", tickAt(t, t0), 0, 0)
	wantAsked(t, "at +59 s", tickAt(t, t0.Add(59*time.Second)), 0, 0)

	result := tickAt(t, t0.Add(time.Minute))
	wantAsked(t, "at +1 min", result, 1, 3)
	if result.AnnulmentCheck.Checking != 1 {
		t.Fatalf("drain = %+v; want the ask left checking", result.AnnulmentCheck)
	}
	_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if check.Verdict != "checking" || len(check.Asks) != 1 || check.Asks[0].Reading != "none" || check.Asks[0].Manual {
		t.Fatalf("after +1 min: %+v; want one scheduled no reading", check)
	}
	wantNextAsk(t, check, t0.Add(5*time.Minute))
	if len(check.Answers) != 3 || check.Answers[0].Service != "ConsultaComprobante" || check.Answers[0].Reading != "none" ||
		len(check.Answers[0].Messages) == 0 {
		t.Fatalf("answers = %+v; want the three calls with the SRI's raw refusal", check.Answers)
	}

	wantAsked(t, "at +3 min", tickAt(t, t0.Add(3*time.Minute)), 0, 3)
	wantAsked(t, "at +5 min", tickAt(t, t0.Add(5*time.Minute)), 1, 6)
	_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
	wantNextAsk(t, check, t0.Add(15*time.Minute))

	sriStub.setConsulta(consultaAnnulled)
	result = tickAt(t, t0.Add(15*time.Minute))
	wantAsked(t, "at +15 min", result, 1, 7)
	if result.AnnulmentCheck.Confirmed != 1 {
		t.Fatalf("drain = %+v; want one confirmed", result.AnnulmentCheck)
	}
	status, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if status != "annulled" || check.Verdict != "confirmed" || !timeOf(t, check.Since).Equal(t0.Add(15*time.Minute)) {
		t.Fatalf("after ANULADO: %s %+v; want confirmed since +15 min", status, check)
	}
	wantNoNextAsk(t, check)

	wantAsked(t, "at +30 min", tickAt(t, t0.Add(30*time.Minute)), 0, 7)
	wantAsked(t, "at +7 h", tickAt(t, t0.Add(7*time.Hour)), 0, 7)
	if sriStub.authorizationQueryCount() != queries {
		t.Fatal("the Annulment Check asked autorizacionComprobante")
	}
}

// TestAnnulmentCheckReachesMismatchOnTwoAUTORIZADOAcrossThirtyMinutes: the
// SRI keeps answering AUTORIZADO. Three asks before +30 min are not a
// mismatch; the +30 min ask is. The factura stays annulled, enters the
// needs-attention list and count with the mismatch's reason, and leaves it
// when a later six-hourly ask reads ANULADO.
func TestAnnulmentCheckReachesMismatchOnTwoAUTORIZADOAcrossThirtyMinutes(t *testing.T) {
	t0 := fixedClock
	operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
	sriStub.setConsulta(consultaAuthorized)
	countBefore := getNeedsAttentionCount(t, operatorSessionID)

	for i, after := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute} {
		result := tickAt(t, t0.Add(after))
		wantAsked(t, "before +30 min", result, 1, i+1)
		if result.AnnulmentCheck.Checking != 1 {
			t.Fatalf("at +%s: drain = %+v; want still checking", after, result.AnnulmentCheck)
		}
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != countBefore {
		t.Fatalf("count while checking = %d; want %d", n, countBefore)
	}

	result := tickAt(t, t0.Add(30*time.Minute))
	if result.AnnulmentCheck.Mismatch != 1 {
		t.Fatalf("at +30 min: drain = %+v; want a mismatch", result.AnnulmentCheck)
	}
	status, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if status != "annulled" || check.Verdict != "mismatch" || !timeOf(t, check.Since).Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("after +30 min: %s %+v; want annulled at mismatch since +30 min", status, check)
	}
	wantNextAsk(t, check, t0.Add(60*time.Minute))

	if n := getNeedsAttentionCount(t, operatorSessionID); n != countBefore+1 {
		t.Fatalf("count at mismatch = %d; want %d", n, countBefore+1)
	}
	item := needsAttentionItem(t, operatorSessionID, invoiceID)
	if item == nil || item.Status != "annulled" || item.AttentionReason != "annulment_mismatch" ||
		!timeOf(t, item.MismatchSince).Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("queue item = %+v; want the annulled factura with the mismatch's reason since +30 min", item)
	}

	// Mismatch at +60 min stays mismatch, and the six-hourly asks go on.
	if result := tickAt(t, t0.Add(60*time.Minute)); result.AnnulmentCheck.Mismatch != 1 {
		t.Fatalf("at +60 min: drain = %+v; want still mismatch", result.AnnulmentCheck)
	}
	_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
	wantNextAsk(t, check, t0.Add(7*time.Hour))

	sriStub.setConsulta(consultaAnnulled)
	if result := tickAt(t, t0.Add(7*time.Hour)); result.AnnulmentCheck.Confirmed != 1 {
		t.Fatalf("at +7 h: drain = %+v; want confirmed", result.AnnulmentCheck)
	}
	if item := needsAttentionItem(t, operatorSessionID, invoiceID); item != nil {
		t.Fatalf("queue after ANULADO lists %+v; want it gone", item)
	}
	if n := getNeedsAttentionCount(t, operatorSessionID); n != countBefore {
		t.Fatalf("count after ANULADO = %d; want %d", n, countBefore)
	}
}

type needsAttentionCheckItem struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	AttentionReason string  `json:"attention_reason"`
	MismatchSince   *string `json:"mismatch_since"`
	AnnulledAt      *string `json:"annulled_at"`
}

func needsAttentionItem(t *testing.T, sessionID, id string) *needsAttentionCheckItem {
	t.Helper()
	resp, env := sriEnv.get(t, needsAttentionPath+"?page_size=100", authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("queue status=%d error=%+v", resp.StatusCode, env.Error)
	}
	var view struct {
		Data []needsAttentionCheckItem `json:"data"`
	}
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode queue: %v", err)
	}
	for i := range view.Data {
		if view.Data[i].ID == id {
			return &view.Data[i]
		}
	}
	return nil
}

// TestAnnulmentCheckReadsUnconfirmedAfterAnHourOfNoReadings: every call
// refused, through the +60 min ask, reads unconfirmed, still asked every
// 6 h; one later AUTORIZADO is not enough, the second turns it into a
// mismatch, and a mismatch never returns to unconfirmed.
func TestAnnulmentCheckReadsUnconfirmedAfterAnHourOfNoReadings(t *testing.T) {
	t0 := fixedClock
	operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
	sriStub.setConsulta(consultaSequence(consultaRefused, consultaRedirected, consultaDropped))

	for _, after := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute} {
		if result := tickAt(t, t0.Add(after)); result.AnnulmentCheck.Checking != 1 {
			t.Fatalf("at +%s: drain = %+v; want checking", after, result.AnnulmentCheck)
		}
	}
	result := tickAt(t, t0.Add(60*time.Minute))
	if result.AnnulmentCheck.Unconfirmed != 1 {
		t.Fatalf("at +60 min: drain = %+v; want unconfirmed", result.AnnulmentCheck)
	}
	_, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if check.Verdict != "unconfirmed" || !timeOf(t, check.Since).Equal(t0.Add(60*time.Minute)) || len(check.Asks) != 5 {
		t.Fatalf("after +60 min: %+v; want unconfirmed since +60 min after five asks", check)
	}
	wantNextAsk(t, check, t0.Add(7*time.Hour))
	if item := needsAttentionItem(t, operatorSessionID, invoiceID); item != nil {
		t.Fatalf("an unconfirmed check is queued: %+v", item)
	}

	sriStub.setConsulta(consultaAuthorized)
	if result := tickAt(t, t0.Add(7*time.Hour)); result.AnnulmentCheck.Unconfirmed != 1 {
		t.Fatalf("at +7 h: drain = %+v; want one AUTORIZADO to leave it unconfirmed", result.AnnulmentCheck)
	}
	if result := tickAt(t, t0.Add(13*time.Hour)); result.AnnulmentCheck.Mismatch != 1 {
		t.Fatalf("at +13 h: drain = %+v; want the second AUTORIZADO to turn it into a mismatch", result.AnnulmentCheck)
	}
	sriStub.setConsulta(consultaRefused)
	if result := tickAt(t, t0.Add(19*time.Hour)); result.AnnulmentCheck.Mismatch != 1 {
		t.Fatalf("at +19 h: drain = %+v; want mismatch never to return to unconfirmed", result.AnnulmentCheck)
	}
	status, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if status != "annulled" || check.Verdict != "mismatch" || !timeOf(t, check.Since).Equal(t0.Add(13*time.Hour)) {
		t.Fatalf("after +19 h: %s %+v; want annulled at mismatch since +13 h", status, check)
	}
}

// TestAnnulmentCheckSixHourlyAsksStopAtTheLaterOfSevenDaysAndTheWindowsClose:
// recorded early in the window the asks go on past +7 days to the window's
// close; recorded near its end they go on to +7 days. Then none.
func TestAnnulmentCheckSixHourlyAsksStopAtTheLaterOfSevenDaysAndTheWindowsClose(t *testing.T) {
	// fixedClock is 2026-07-07 07:00 in Guayaquil: the window closes at
	// 2026-08-08 00:00 there, 05:00 UTC.
	closes := time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC)

	t.Run("the window's close is the later", func(t *testing.T) {
		t0 := fixedClock
		operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
		sriStub.setConsulta(consultaRefused)
		tickAt(t, t0.Add(60*time.Minute))
		_, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
		if until := timeOf(t, check.AsksUntil); !until.Equal(closes) {
			t.Fatalf("asks_until = %s; want the window's close %s", until, closes)
		}
		// Past +7 days, still asked.
		tickAt(t, t0.Add(8*24*time.Hour))
		_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
		if check.NextAskAt == nil {
			t.Fatal("no next ask past +7 days while the window is open")
		}
		// The rungs fall at t0 + 1 h + 6 h k: the last one is 4 h before the
		// close.
		tickAt(t, closes.Add(-5*time.Hour))
		_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
		wantNextAsk(t, check, closes.Add(-4*time.Hour))
		consultas := len(sriStub.consultaCalls())
		wantAsked(t, "the last rung", tickAt(t, closes.Add(-4*time.Hour)), 1, consultas+3)
		_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
		wantNoNextAsk(t, check)
		wantAsked(t, "after the close", tickAt(t, closes.Add(24*time.Hour)), 0, consultas+3)
	})

	t.Run("recording + 7 days is the later", func(t *testing.T) {
		t0 := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
		env := setupTest(t)
		sriStub.reset()
		atInvoicingClock(t, fixedClock)
		operatorSessionID, invoiceID := authorizedManualInvoice(t, env)
		atInvoicingClock(t, t0)
		annulOK(t, operatorSessionID, invoiceID)
		sriStub.setConsulta(consultaRefused)
		tickAt(t, t0.Add(60*time.Minute))
		_, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
		sevenDays := t0.Add(7 * 24 * time.Hour)
		if until := timeOf(t, check.AsksUntil); !until.Equal(sevenDays) {
			t.Fatalf("asks_until = %s; want recording + 7 days %s", until, sevenDays)
		}
		// Past the window's close, still asked.
		tickAt(t, closes.Add(time.Hour))
		_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
		if check.NextAskAt == nil {
			t.Fatal("no next ask past the window's close before +7 days")
		}
		// The last rung is t0 + 1 h + 6 h * 27 = +163 h.
		last := t0.Add(163 * time.Hour)
		tickAt(t, last.Add(-time.Hour))
		_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
		wantNextAsk(t, check, last)
		consultas := len(sriStub.consultaCalls())
		wantAsked(t, "the last rung", tickAt(t, last), 1, consultas+3)
		_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
		wantNoNextAsk(t, check)
		wantAsked(t, "after +7 days", tickAt(t, sevenDays.Add(24*time.Hour)), 0, consultas+3)
	})
}

// TestAnnulmentCheckRunsWithTheSaleInvoicingFlagClosed: the pass is above
// the flag, so a manual Tax Invoice's annulment is checked on a tick of an
// app with SALE_INVOICING_ENABLED closed, which still answers 404 and signs
// nothing.
func TestAnnulmentCheckRunsWithTheSaleInvoicingFlagClosed(t *testing.T) {
	// Recorded ten minutes before the closed app's own clock, fixedClock.
	t0 := fixedClock.Add(-10 * time.Minute)
	operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
	closed := startAppWithSaleInvoicingClosed(t)
	sriStub.setConsulta(consultaAnnulled)
	received := sriStub.receptionCount()

	resp, body := closed.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("drain while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	if n := len(sriStub.consultaCalls()); n != 1 {
		t.Fatalf("%d ConsultaComprobante calls on the closed tick; want the one ask", n)
	}
	status, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if status != "annulled" || check.Verdict != "confirmed" || len(check.Asks) != 1 {
		t.Fatalf("after the closed tick: %s %+v; want confirmed on its one ask", status, check)
	}
	if sriStub.receptionCount() != received {
		t.Fatal("the closed tick sent something to the SRI's reception")
	}
}

// TestTwoConcurrentTicksNeverAskForTheSameFacturaTwice: two drains at once
// over one due check make one ask between them.
func TestTwoConcurrentTicksNeverAskForTheSameFacturaTwice(t *testing.T) {
	t0 := fixedClock
	annulledManualInvoiceAt(t, t0)
	sriStub.setConsulta(consultaAuthorized)
	sriStub.setSleep(300 * time.Millisecond)
	atInvoicingClock(t, t0.Add(time.Minute))

	var wg sync.WaitGroup
	results := make([]annulmentCheckDrain, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, body := sriEnv.post(t, saleInvoiceDrainPath, nil, nil)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("drain %d status=%d error=%+v", i, resp.StatusCode, body.Error)
				return
			}
			if err := json.Unmarshal(body.Data, &results[i]); err != nil {
				t.Errorf("decode drain %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	sriStub.setSleep(0)
	if asked := results[0].AnnulmentCheck.Asked + results[1].AnnulmentCheck.Asked; asked != 1 {
		t.Fatalf("the two ticks asked %d times (%+v, %+v); want once", asked, results[0].AnnulmentCheck, results[1].AnnulmentCheck)
	}
	if n := len(sriStub.consultaCalls()); n != 1 {
		t.Fatalf("%d ConsultaComprobante calls; want one", n)
	}
}

// TestCheckStatusPressedTwiceQuicklyNeitherSettlesAMismatchNorMovesTheSchedule:
// two AUTORIZADO presses at +2 min are two manual asks, still checking, and
// the next scheduled ask stays at +1 min; the schedule's own +30 min ask is
// the one that may settle the mismatch.
func TestCheckStatusPressedTwiceQuicklyNeitherSettlesAMismatchNorMovesTheSchedule(t *testing.T) {
	t0 := fixedClock
	operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
	sriStub.setConsulta(consultaAuthorized)
	queries := sriStub.authorizationQueryCount()

	atInvoicingClock(t, t0.Add(2*time.Minute))
	checkAnnulledOK(t, operatorSessionID, invoiceID)
	atInvoicingClock(t, t0.Add(2*time.Minute+5*time.Second))
	checkAnnulledOK(t, operatorSessionID, invoiceID)

	status, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if status != "annulled" || check.Verdict != "checking" || len(check.Asks) != 2 || !check.Asks[0].Manual || !check.Asks[1].Manual {
		t.Fatalf("after two presses: %s %+v; want checking with two manual asks", status, check)
	}
	wantNextAsk(t, check, t0.Add(time.Minute))
	if sriStub.authorizationQueryCount() != queries {
		t.Fatal("Check status on an annulled factura asked autorizacionComprobante")
	}

	// The scheduled asks then run as if nothing was pressed.
	tickAt(t, t0.Add(3*time.Minute))
	_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
	wantNextAsk(t, check, t0.Add(5*time.Minute))
	if check.Verdict != "checking" {
		t.Fatalf("verdict = %s before +30 min; want checking", check.Verdict)
	}
	if result := tickAt(t, t0.Add(30*time.Minute)); result.AnnulmentCheck.Mismatch != 1 {
		t.Fatalf("at +30 min: drain = %+v; want the mismatch", result.AnnulmentCheck)
	}
}

// TestCheckStatusThatReadsANULADOEndsTheCheck: a manual ask confirms as a
// scheduled one does, and nothing more is asked.
func TestCheckStatusThatReadsANULADOEndsTheCheck(t *testing.T) {
	t0 := fixedClock
	operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
	sriStub.setConsulta(consultaAnnulled)
	atInvoicingClock(t, t0.Add(30*time.Second))
	checkAnnulledOK(t, operatorSessionID, invoiceID)
	_, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
	if check.Verdict != "confirmed" {
		t.Fatalf("verdict = %s; want confirmed", check.Verdict)
	}
	wantNoNextAsk(t, check)
	wantAsked(t, "at +1 min", tickAt(t, t0.Add(time.Minute)), 0, 1)
}

// TestAFacturaAnnulledBeforeTheCheckReadsUnconfirmedAndIsNeverAsked: an
// annulment recorded before the check existed reads unconfirmed,
// unscheduled, and no tick asks about it. Two shapes are in production's
// past: the #477 one (the row's own pair, no ledger row) and a #745 ledger
// row migration 132 left with no check. Its Check status still asks, onto
// the attempts ledger alone.
func TestAFacturaAnnulledBeforeTheCheckReadsUnconfirmedAndIsNeverAsked(t *testing.T) {
	shapes := map[string]string{
		"no ledger row":              `DELETE FROM invoicing_annulments WHERE invoice_id = $1`,
		"a ledger row with no check": `UPDATE invoicing_annulments SET check_verdict = NULL, next_ask_at = NULL WHERE invoice_id = $1`,
	}
	for name, setup := range shapes {
		t.Run(name, func(t *testing.T) {
			t0 := fixedClock
			operatorSessionID, invoiceID := annulledManualInvoiceAt(t, t0)
			if _, err := sriEnv.db.Exec(setup, invoiceID); err != nil {
				t.Fatalf("setup: %v", err)
			}
			status, check := getAnnulmentCheck(t, operatorSessionID, invoiceID)
			if status != "annulled" || check.Verdict != "unconfirmed" || check.Scheduled || check.NextAskAt != nil || check.AsksUntil != nil {
				t.Fatalf("a pre-check annulment: %s %+v; want unconfirmed, never scheduled", status, check)
			}
			for _, after := range []time.Duration{time.Minute, time.Hour, 7 * time.Hour, 8 * 24 * time.Hour} {
				wantAsked(t, "a pre-check annulment", tickAt(t, t0.Add(after)), 0, 0)
			}
			if item := needsAttentionItem(t, operatorSessionID, invoiceID); item != nil {
				t.Fatalf("a pre-check annulment is queued: %+v", item)
			}

			sriStub.setConsulta(consultaAuthorized)
			checkAnnulledOK(t, operatorSessionID, invoiceID)
			_, check = getAnnulmentCheck(t, operatorSessionID, invoiceID)
			if check.Verdict != "unconfirmed" || len(check.Asks) != 0 || len(check.Answers) != 1 {
				t.Fatalf("after a Check status: %+v; want still unconfirmed, the answer on the ledger only", check)
			}
		})
	}
}
