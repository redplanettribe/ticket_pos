package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// THE SWEPT ASSIGNMENT MAIL (#671, parent #665, ADR 0076), beside the
// Assignment Reminder's sweep tests and on their terms: the internal endpoint
// as Cloud Scheduler calls it, the captured inbox, the accept page and the
// buyer's own Sale page. SQL is read only for the owed marks and the ledger,
// which no surface exposes.
//
// FIXTURE SHAPE. An Event requiring Named Tickets with no Ticket Questions - so
// nothing here depends on what a reassignment must carry - and one paid Sale
// by Ana of four: her Self-held seat, Ben, her own address typed as an address
// book gives it, and Carla. Two mails are owed, to Ben and to Carla; the commit
// sent nothing.

// owedMailSweepResult is the sweep's response envelope, counts only.
type owedMailSweepResult struct {
	Sent       int `json:"sent"`
	Dropped    int `json:"dropped"`
	Failed     int `json:"failed"`
	Unrecorded int `json:"unrecorded"`
	Backlog    int `json:"backlog"`
}

// sweepOwedAssignmentMails hits the sweep as Cloud Scheduler would: no body,
// no parameters, no credential of the application's.
func sweepOwedAssignmentMails(t *testing.T, env *testEnv) owedMailSweepResult {
	t.Helper()
	resp, body := env.post(t, "/api/v1/internal/owed-assignment-mails/sweep", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owed mail sweep status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out owedMailSweepResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode owed mail sweep result: %v", err)
	}
	return out
}

// sweepOwedAssignmentMailsConcurrently is the same call for a goroutine other
// than the test's own, which may not fail the test itself: it reports instead.
func sweepOwedAssignmentMailsConcurrently(env *testEnv) (owedMailSweepResult, error) {
	var out owedMailSweepResult
	resp, err := http.Post(env.server.URL+"/api/v1/internal/owed-assignment-mails/sweep", "application/json", nil)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	var body envelope
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return out, err
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return out, json.Unmarshal(body.Data, &out)
}

type owedMailFixture struct {
	staffSession, eventID, gaID string
	saleID                      string
	ana                         string
	// seatID, benID, ownID and carlaID are the Sale's four Tickets by ordinal.
	seatID, benID, ownID, carlaID string
	startsAt                      time.Time
}

// newOwedMailFixture sells the fixture's Sale through a Named Tickets checkout
// and leaves the inbox empty.
func newOwedMailFixture(t *testing.T, env *testEnv) owedMailFixture {
	t.Helper()
	enableTicketAssignment(t)
	// Open for the buyer's Sale page, which lives behind it; the Event asks
	// nothing.
	enableTicketQuestions(t)
	f := owedMailFixture{staffSession: orgAdminSession(t, env)}
	f.eventID, f.gaID = publishNamedEvent(t, env, f.staffSession, "Named Fest", "named-fest", 2000, 20)
	f.startsAt = env.fixedClock.Add(72 * time.Hour)

	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(f.gaID, 4))
	body["holders"] = []map[string]any{
		holder(f.gaID, 2, "ben@example.com"),
		holder(f.gaID, 3, " Ana@Example.com"),
		holder(f.gaID, 4, "carla@example.com"),
	}
	begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", body)
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	f.saleID = saleIDOfPayment(t, env, begun.ClientTransactionID)

	f.ana = customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, f.ana, f.saleID)
	f.seatID = buyerTicketAt(t, tickets, 1).TicketID
	f.benID = buyerTicketAt(t, tickets, 2).TicketID
	f.ownID = buyerTicketAt(t, tickets, 3).TicketID
	f.carlaID = buyerTicketAt(t, tickets, 4).TicketID

	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Fatalf("the commit sent %d Assignment mails, want 0 - the sweep sends them", got)
	}
	if owed := owedMails(t, env, f.saleID); len(owed) != 2 {
		t.Fatalf("%d Assignment mails owed after the commit, want 2", len(owed))
	}
	sharedEmail.Reset()
	return f
}

// ledgerRowsFor reads one Ticket's Assignment mail ledger: how many rows, and
// how many of them the swept sender wrote.
func ledgerRowsFor(t *testing.T, env *testEnv, ticketID string) (total, checkoutNamed int) {
	t.Helper()
	if err := env.db.QueryRow(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE checkout_named)
		FROM ticket_assignment_mails WHERE ticket_id = $1
	`, ticketID).Scan(&total, &checkoutNamed); err != nil {
		t.Fatalf("read the Assignment mail ledger: %v", err)
	}
	return total, checkoutNamed
}

// withRecordedPacing replaces the pacer's sleep with a recorder for one test
// and restores a real sleep afterwards, as the Assignment Reminder's pacing
// test does: the service is shared across the package.
func withRecordedPacing(t *testing.T, gap time.Duration) *[]time.Duration {
	t.Helper()
	var mu sync.Mutex
	pauses := []time.Duration{}
	sharedApp.SalesService.WithReminderPacing(gap, func(_ context.Context, d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		pauses = append(pauses, d)
	})
	t.Cleanup(func() {
		sharedApp.SalesService.WithReminderPacing(gap, func(ctx context.Context, d time.Duration) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		})
	})
	return &pauses
}

// ONE SWEEP RUN SENDS EXACTLY ONE ASSIGNMENT MAIL PER NON-OWN ADDRESS, records
// each in the ledger, and owes nothing afterwards; a second run sends nothing.
// The buyer's own address was accepted at the commit and is mailed nothing.
func TestTheOwedMailSweepSendsOneAssignmentMailPerNamedAddress(t *testing.T) {
	env := setupTest(t)
	f := newOwedMailFixture(t, env)

	result := sweepOwedAssignmentMails(t, env)
	if result.Sent != 2 || result.Dropped != 0 || result.Failed != 0 || result.Unrecorded != 0 || result.Backlog != 0 {
		t.Fatalf("sweep = %+v, want 2 sent and nothing else", result)
	}
	for _, address := range []string{"ben@example.com", "carla@example.com"} {
		if got := len(assignmentMailsTo(env, address)); got != 1 {
			t.Errorf("%d Assignment mails to %s, want 1", got, address)
		}
	}
	if got := len(assignmentMailsTo(env, "ana@example.com")); got != 0 {
		t.Errorf("%d Assignment mails to the buyer's own address, want 0", got)
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 2 {
		t.Errorf("%d Assignment mails in all, want 2", got)
	}

	for name, ticketID := range map[string]string{"Ben's": f.benID, "Carla's": f.carlaID} {
		if total, named := ledgerRowsFor(t, env, ticketID); total != 1 || named != 1 {
			t.Errorf("%s Ticket has %d ledger rows (%d checkout-named), want 1 written by the sweep", name, total, named)
		}
	}
	for name, ticketID := range map[string]string{"the seat": f.seatID, "Ana's own": f.ownID} {
		if total, _ := ledgerRowsFor(t, env, ticketID); total != 0 {
			t.Errorf("%s Ticket has %d ledger rows, want 0", name, total)
		}
	}
	if owed := owedMails(t, env, f.saleID); len(owed) != 0 {
		t.Errorf("%d Assignment mails still owed after they were sent, want 0", len(owed))
	}

	again := sweepOwedAssignmentMails(t, env)
	if again != (owedMailSweepResult{}) {
		t.Errorf("a second sweep = %+v, want nothing", again)
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 2 {
		t.Errorf("%d Assignment mails after a second sweep, want still 2", got)
	}
}

// THE MAIL AND THE ACCEPT PAGE ARE AN AFTER-SALE ASSIGNMENT'S. Ben's swept mail
// is compared with the one Dora gets when Ana assigns her own Ticket to Dora
// after the sale: everything but the address and the token is the same, and
// Ben's link accepts exactly as Dora's does.
func TestASweptAssignmentMailIsTheOrdinaryOne(t *testing.T) {
	env := setupTest(t)
	f := newOwedMailFixture(t, env)

	sweepOwedAssignmentMails(t, env)
	assignTicketOK(t, env, f.ana, f.saleID, f.ownID, "dora@example.com")

	swept := assignmentMailFor(t, env, "ben@example.com")
	inline := assignmentMailFor(t, env, "dora@example.com")
	sweptToken, inlineToken := assignmentTokenFrom(t, swept), assignmentTokenFrom(t, inline)

	comparable := func(mail platform.TicketAssignment) platform.TicketAssignment {
		mail.To, mail.AcceptURL = "", ""
		return mail
	}
	if comparable(swept) != comparable(inline) {
		t.Errorf("the swept mail %+v differs from the after-sale one %+v beyond its address and link", swept, inline)
	}
	sweptView := acceptAssignmentOK(t, env, sweptToken)
	inlineView := acceptAssignmentOK(t, env, inlineToken)
	if sweptView.EventName != "Named Fest" || sweptView.TicketTypeName != "GA" {
		t.Errorf("Ben's accept page shows %q / %q, want Named Fest / GA", sweptView.EventName, sweptView.TicketTypeName)
	}
	if sweptView.EventName != inlineView.EventName || sweptView.TicketTypeName != inlineView.TicketTypeName ||
		len(sweptView.Questions) != len(inlineView.Questions) {
		t.Errorf("Ben's accept page %+v differs from Dora's %+v", sweptView, inlineView)
	}

	ben := buyerTicketAt(t, listBuyerTickets(t, env, f.ana, f.saleID), 2)
	if ben.AssignmentState != "accepted" || ben.AcceptedAt == nil {
		t.Errorf("after Ben's click the buyer reads %q, want accepted", ben.AssignmentState)
	}
}

// THE FIRST MAIL SPENDS THE TICKET'S LIFETIME ALLOWANCE AND NOT THE BUYER'S
// WINDOW. With an allowance of two per Ticket, Ben's Ticket - one mail spent by
// the sweep - takes one correction and refuses the next as at its cap, just as
// a Ticket assigned after the sale would. With a window of two per buyer, the
// sweep's two mails leave all of it: a correction on Carla's Ticket still goes.
func TestASweptMailSpendsTheTicketsAllowanceAndNotTheBuyersWindow(t *testing.T) {
	env := setupTest(t)
	f := newOwedMailFixture(t, env)
	withAssignmentMailLimits(t, catalog.AssignmentMailLimits{PerTicket: 2, PerBuyer: 2})

	if result := sweepOwedAssignmentMails(t, env); result.Sent != 2 {
		t.Fatalf("sweep = %+v, want 2 sent", result)
	}

	assignTicketOK(t, env, f.ana, f.saleID, f.benID, "dan@example.com")
	resp, body := assignTicket(t, env, f.ana, f.saleID, f.benID, "eve@example.com")
	assertAPIError(t, resp, body, http.StatusConflict, "ASSIGNMENT_MAIL_CAP_REACHED")

	// Ana's window holds Dan's one after-sale mail and nothing of the sweep's,
	// so a correction on another Ticket is not rate limited.
	assignTicketOK(t, env, f.ana, f.saleID, f.carlaID, "frank@example.com")
	for _, address := range []string{"dan@example.com", "frank@example.com"} {
		if got := len(assignmentMailsTo(env, address)); got != 1 {
			t.Errorf("%d Assignment mails to %s, want 1", got, address)
		}
	}
	if got := len(assignmentMailsTo(env, "eve@example.com")); got != 0 {
		t.Errorf("%d Assignment mails to eve@example.com past the Ticket's cap, want 0", got)
	}
}

// A REFUSED SEND STAYS OWED AND A LATER RUN SENDS IT. The provider refuses
// every send: nothing is recorded and both mails stay owed, backed off so the
// very next tick does not hammer a provider that just refused. A minute later
// the provider is well again and the next run sends both, once each.
func TestARefusedOwedMailIsSentByALaterRun(t *testing.T) {
	env := setupTest(t)
	f := newOwedMailFixture(t, env)

	sharedEmail.FailWith(errors.New("429 rate_limit_exceeded"))
	refused := sweepOwedAssignmentMails(t, env)
	sharedEmail.FailWith(nil)
	if refused.Sent != 0 || refused.Failed != 2 || refused.Backlog != 0 {
		t.Fatalf("sweep against a refusing provider = %+v, want 2 failed and none due again yet", refused)
	}
	if owed := owedMails(t, env, f.saleID); len(owed) != 2 {
		t.Fatalf("%d Assignment mails owed after the refusal, want both still owed", len(owed))
	}
	for _, ticketID := range []string{f.benID, f.carlaID} {
		if total, _ := ledgerRowsFor(t, env, ticketID); total != 0 {
			t.Errorf("a refused send left %d ledger rows, want 0 - nothing reached anybody", total)
		}
	}

	if early := sweepOwedAssignmentMails(t, env); early.Sent != 0 || early.Failed != 0 {
		t.Fatalf("a sweep in the same instant = %+v, want nothing: the refused mails are backed off", early)
	}

	moveClockTo(t, env.fixedClock.Add(time.Minute))
	later := sweepOwedAssignmentMails(t, env)
	if later.Sent != 2 || later.Failed != 0 {
		t.Fatalf("the later sweep = %+v, want both sent", later)
	}
	for _, address := range []string{"ben@example.com", "carla@example.com"} {
		if got := len(assignmentMailsTo(env, address)); got != 1 {
			t.Errorf("%d Assignment mails to %s, want 1", got, address)
		}
	}
	if owed := owedMails(t, env, f.saleID); len(owed) != 0 {
		t.Errorf("%d Assignment mails still owed, want 0", len(owed))
	}
}

// AN OWED MAIL WHOSE TICKET WAS REASSIGNED IS DROPPED AND NEVER SENT. Ana
// corrects Ben's address to Dan's before the sweep runs: Dan is mailed by the
// correction itself, as any after-sale assignment is, and Ben, whose link
// would open nowhere, is never mailed.
func TestAnOwedMailForAReassignedTicketIsDropped(t *testing.T) {
	env := setupTest(t)
	f := newOwedMailFixture(t, env)

	// A minute on, as a correction is: on the harness's fixed clock the
	// correction would otherwise carry the commit's own assigned_at.
	moveClockTo(t, env.fixedClock.Add(time.Minute))
	assignTicketOK(t, env, f.ana, f.saleID, f.benID, "dan@example.com")
	result := sweepOwedAssignmentMails(t, env)
	if result.Sent != 1 || result.Dropped != 1 {
		t.Fatalf("sweep = %+v, want Carla's sent and Ben's dropped", result)
	}
	if got := len(assignmentMailsTo(env, "ben@example.com")); got != 0 {
		t.Errorf("%d Assignment mails to Ben after his Ticket was reassigned, want 0", got)
	}
	if got := len(assignmentMailsTo(env, "dan@example.com")); got != 1 {
		t.Errorf("%d Assignment mails to Dan, want the correction's 1", got)
	}
	if owed := owedMails(t, env, f.saleID); len(owed) != 0 {
		t.Errorf("%d Assignment mails still owed, want 0 - a dropped one is gone", len(owed))
	}
	if again := sweepOwedAssignmentMails(t, env); again != (owedMailSweepResult{}) {
		t.Errorf("a second sweep = %+v, want nothing", again)
	}
}

// THE SEAM WITH #673: on an Event whose Tickets ask a required question, the
// correction must carry the new Holder's Answers. Refused without them, it
// writes nothing and Ben's mail stays owed for the assignment he holds. Given
// with them, it moves assigned_at, so the sweep drops Ben's mail as stale, and
// mails Dan inline - a mail that is rationed as every after-sale assignment is,
// in the buyer's window that the sweep's mails never enter.
func TestAReassignmentWithTheAnswersDropsTheOwedMailAndIsRationed(t *testing.T) {
	env := setupTest(t)
	f := newNamedCommitFixture(t, env, 2000)
	withAssignmentMailLimits(t, catalog.AssignmentMailLimits{PerTicket: 2, PerBuyer: 1})

	begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", f.familyBasket())
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	saleID := saleIDOfPayment(t, env, begun.ClientTransactionID)
	ana := customerSignIn(t, env, "ana@example.com")
	tickets := listBuyerTickets(t, env, ana, saleID)
	benID, benAgainID := buyerTicketAt(t, tickets, 2).TicketID, buyerTicketAt(t, tickets, 4).TicketID
	if owed := owedMails(t, env, saleID); len(owed) != 2 {
		t.Fatalf("%d Assignment mails owed after the commit, want Ben's two", len(owed))
	}
	sharedEmail.Reset()

	// A minute on, as a correction is: on the harness's fixed clock the
	// correction would otherwise carry the commit's own assigned_at.
	moveClockTo(t, env.fixedClock.Add(time.Minute))
	resp, body := assignTicket(t, env, ana, saleID, benID, "dan@example.com")
	refusedAsUnanswered(t, resp, body)
	if owed := owedMails(t, env, saleID)[benID]; !owed.current {
		t.Fatalf("after a refused correction Ben's owed mail is %+v, want still current", owed)
	}

	assignWithAnswersOK(t, env, ana, saleID, benID, "dan@example.com",
		givenAnswer(f.size.ID, map[string]any{"text": "XXL"}))
	if owed := owedMails(t, env, saleID)[benID]; owed.current {
		t.Fatalf("after the correction Ben's owed mail is %+v, want stale", owed)
	}
	if got := staffAnswerText(t, env, f.sessionID, f.eventID, benID, f.size.ID); got == nil || *got != "XXL" {
		t.Errorf("Event Staff read %v for the size, want Dan's XXL", got)
	}
	if got := len(assignmentMailsTo(env, "dan@example.com")); got != 1 {
		t.Fatalf("%d Assignment mails to Dan, want the correction's 1", got)
	}

	result := sweepOwedAssignmentMails(t, env)
	if result.Sent != 1 || result.Dropped != 1 {
		t.Fatalf("sweep = %+v, want Ben's other Ticket sent and the corrected one dropped", result)
	}
	if got := len(assignmentMailsTo(env, "ben@example.com")); got != 1 {
		t.Errorf("%d Assignment mails to Ben, want the one for the Ticket he still holds", got)
	}

	// Ana's window of one holds Dan's inline mail and nothing of the sweep's, so
	// the next correction is rate limited before it is written.
	resp, body = assignWithAnswers(t, env, ana, saleID, benAgainID, "eve@example.com",
		givenAnswer(f.size.ID, map[string]any{"text": "S"}))
	assertAPIError(t, resp, body, http.StatusTooManyRequests, "ASSIGNMENT_RATE_LIMITED")
	if got := len(assignmentMailsTo(env, "eve@example.com")); got != 0 {
		t.Errorf("%d Assignment mails to Eve past the buyer's window, want 0", got)
	}
}

// AN OWED MAIL ON A REVERSED SALE IS DROPPED AND NEVER SENT: Ana undoes her
// purchase before the sweep runs.
func TestAnOwedMailOnAReversedSaleIsDropped(t *testing.T) {
	env := setupTest(t)
	f := newOwedMailFixture(t, env)

	reverseSaleOK(t, env, f.ana, f.saleID)
	result := sweepOwedAssignmentMails(t, env)
	if result.Sent != 0 || result.Dropped != 2 {
		t.Fatalf("sweep = %+v, want both dropped", result)
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Errorf("%d Assignment mails about a reversed Sale, want 0", got)
	}
	if owed := owedMails(t, env, f.saleID); len(owed) != 0 {
		t.Errorf("%d Assignment mails still owed, want 0", len(owed))
	}
}

// AN OWED MAIL WHOSE EVENT HAS STARTED IS DROPPED AND NEVER SENT, whether or
// not the Holder Address Purge has taken the unaccepted addresses yet - the
// purge clears the address and leaves the owed mark behind.
func TestAnOwedMailWhoseEventHasStartedIsDropped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		purge bool
	}{
		{"before the purge", false},
		{"after the purge", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			f := newOwedMailFixture(t, env)

			moveClockTo(t, f.startsAt)
			if tc.purge {
				resp, body := env.post(t, "/api/v1/internal/holder-addresses/purge", nil, nil)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("purge status=%d error=%+v", resp.StatusCode, body.Error)
				}
			}
			result := sweepOwedAssignmentMails(t, env)
			if result.Sent != 0 || result.Dropped != 2 {
				t.Fatalf("sweep at the doors = %+v, want both dropped", result)
			}
			if got := len(env.email.TicketAssignmentsSent()); got != 0 {
				t.Errorf("%d Assignment mails after the doors opened, want 0", got)
			}
			if owed := owedMails(t, env, f.saleID); len(owed) != 0 {
				t.Errorf("%d Assignment mails still owed, want 0", len(owed))
			}
		})
	}
}

// HELD, NOT DROPPED, WHILE TICKET ASSIGNMENT IS CLOSED. The accept page refuses
// every link while the flag is off, so nothing is sent; but the buyer paid for
// these assignments and they stand, so the mails wait - reported as backlog -
// and go out once the flag reopens.
func TestOwedMailsWaitWhileTicketAssignmentIsClosed(t *testing.T) {
	env := setupTest(t)
	f := newOwedMailFixture(t, env)

	closeTicketAssignment(t)
	held := sweepOwedAssignmentMails(t, env)
	if held.Sent != 0 || held.Dropped != 0 || held.Failed != 0 || held.Backlog != 2 {
		t.Fatalf("sweep with Ticket Assignment closed = %+v, want nothing touched and a backlog of 2", held)
	}
	if got := len(env.email.TicketAssignmentsSent()); got != 0 {
		t.Fatalf("%d Assignment mails sent while Ticket Assignment was closed, want 0", got)
	}
	if owed := owedMails(t, env, f.saleID); len(owed) != 2 {
		t.Fatalf("%d Assignment mails owed while held, want 2", len(owed))
	}

	enableTicketAssignment(t)
	if result := sweepOwedAssignmentMails(t, env); result.Sent != 2 {
		t.Fatalf("sweep after reopening = %+v, want both sent", result)
	}
}

// A NINE-TICKET CHECKOUT IS SENT PACED, BY THE REMINDERS' PACER, AND NO MAIL
// GOES TWICE WHEN RUNS OVERLAP. Ana names eight friends; four sweeps start at
// once - a scheduled tick and three hands on the curl - and between them every
// friend gets exactly one mail, each one gap apart from the request before it.
func TestOverlappingSweepsSendANineTicketCheckoutOncePaced(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	staff := orgAdminSession(t, env)
	_, gaID := publishNamedEvent(t, env, staff, "Named Fest", "named-fest", 2000, 20)
	body := checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(gaID, 9))
	var holders []map[string]any
	for index := 2; index <= 9; index++ {
		holders = append(holders, holder(gaID, index, fmt.Sprintf("friend%d@example.com", index)))
	}
	body["holders"] = holders
	begun := beginCheckoutOK(t, env, testOrgSlug, "named-fest", body)
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	sharedEmail.Reset()

	const gap = 120 * time.Millisecond
	pauses := withRecordedPacing(t, gap)

	const runs = 4
	results := make([]owedMailSweepResult, runs)
	failures := make([]error, runs)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], failures[i] = sweepOwedAssignmentMailsConcurrently(env)
		}()
	}
	close(start)
	wg.Wait()

	sent := 0
	for i, result := range results {
		if failures[i] != nil {
			t.Fatalf("overlapping sweep %d: %v", i, failures[i])
		}
		sent += result.Sent
		if result.Failed != 0 || result.Dropped != 0 {
			t.Errorf("an overlapping sweep = %+v, want sends only", result)
		}
	}
	if sent != 8 {
		t.Fatalf("the overlapping sweeps sent %d in all, want 8", sent)
	}
	for index := 2; index <= 9; index++ {
		address := fmt.Sprintf("friend%d@example.com", index)
		if got := len(assignmentMailsTo(env, address)); got != 1 {
			t.Errorf("%d Assignment mails to %s, want exactly 1", got, address)
		}
	}
	// Every request is followed by one gap before its run's next claim, and
	// only a request is: eight sends, eight gaps, each the shared one.
	if len(*pauses) != sent {
		t.Errorf("the sweeps paused %d times for %d sends, want one gap after each", len(*pauses), sent)
	}
	for _, d := range *pauses {
		if d != gap {
			t.Errorf("pause = %v, want the shared %v", d, gap)
		}
	}
}
